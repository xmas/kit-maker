package ai

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed fixtures/*.json
var builtinFixtures embed.FS

// Fixture is one recorded answer. A fixture file is either a single recorded
// request (named by Request.Key, written by Recorder) or a route file
// (<route>.json) holding answers picked by the first Match found in the prompt;
// an answer with no Match is the fallback.
type Fixture struct {
	Route   Route           `json:"route"`
	Match   string          `json:"match,omitempty"`
	Prompt  string          `json:"prompt,omitempty"`
	Text    string          `json:"text,omitempty"`
	JSON    json.RawMessage `json:"json,omitempty"`
	Answers []Fixture       `json:"answers,omitempty"`
}

// Fake replays fixtures. It never calls a network.
type Fake struct {
	FS fs.FS // defaults to the fixtures built into the binary
}

// NewFake returns a fake that reads dir first (when set), then the built-in
// fixtures.
func NewFake(dir string) *Fake {
	if dir == "" {
		return &Fake{}
	}
	return &Fake{FS: layered{os.DirFS(dir), mustSub(builtinFixtures, "fixtures")}}
}

func (*Fake) Provider() string { return "fake" }

func (f *Fake) fsys() fs.FS {
	if f.FS != nil {
		return f.FS
	}
	return mustSub(builtinFixtures, "fixtures")
}

func (f *Fake) Generate(_ context.Context, req Request) (*Response, error) {
	fx, err := f.find(req)
	if err != nil {
		return nil, err
	}
	text := fx.Text
	if len(fx.JSON) > 0 {
		text = string(fx.JSON)
	}
	r := &Response{
		Text:       text,
		Model:      "fake",
		StopReason: "end_turn",
		Usage:      Usage{InputTokens: estimate(req), OutputTokens: int64(len(text)/4 + 1)},
	}
	if req.Schema != nil {
		if !json.Valid([]byte(text)) {
			return nil, fmt.Errorf("ai: fixture for %s is not JSON but the request has a schema", req.Route)
		}
		r.JSON = json.RawMessage(text)
	}
	return r, nil
}

// Stream replays the answer word by word.
func (f *Fake) Stream(ctx context.Context, req Request, onDelta func(string) error) (*Response, error) {
	r, err := f.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	for _, w := range strings.SplitAfter(r.Text, " ") {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := onDelta(w); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (f *Fake) find(req Request) (*Fixture, error) {
	fsys := f.fsys()
	if fx, err := readFixture(fsys, req.Key()+".json"); err == nil {
		return fx, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	fx, err := readFixture(fsys, string(req.Route)+".json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s (route %s)", ErrNoFixture, req.Key(), req.Route)
	}
	if err != nil {
		return nil, err
	}
	var fallback *Fixture
	for i := range fx.Answers {
		a := &fx.Answers[i]
		if a.Match == "" {
			if fallback == nil {
				fallback = a
			}
			continue
		}
		if strings.Contains(req.Prompt, a.Match) {
			return a, nil
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("%w: %s (route %s)", ErrNoFixture, req.Key(), req.Route)
}

func readFixture(fsys fs.FS, name string) (*Fixture, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	var fx Fixture
	if err := json.Unmarshal(b, &fx); err != nil {
		return nil, fmt.Errorf("fixture %s: %w", name, err)
	}
	return &fx, nil
}

func estimate(req Request) int64 {
	n := len(req.Prompt)
	for _, s := range append(req.System, req.Context...) {
		n += len(s)
	}
	return int64(n/4 + 1)
}

// Recorder passes calls to a real client and writes each answer as a fixture
// named by Request.Key, so a later Fake replays it exactly.
type Recorder struct {
	Client
	Dir string
}

func (r *Recorder) Generate(ctx context.Context, req Request) (*Response, error) {
	res, err := r.Client.Generate(ctx, req)
	if err == nil {
		err = r.save(req, res)
	}
	return res, err
}

func (r *Recorder) Stream(ctx context.Context, req Request, onDelta func(string) error) (*Response, error) {
	res, err := r.Client.Stream(ctx, req, onDelta)
	if err == nil {
		err = r.save(req, res)
	}
	return res, err
}

func (r *Recorder) save(req Request, res *Response) error {
	fx := Fixture{Route: req.Route, Prompt: req.Prompt, Text: res.Text}
	if len(res.JSON) > 0 {
		fx = Fixture{Route: req.Route, Prompt: req.Prompt, JSON: res.JSON}
	}
	b, err := json.MarshalIndent(fx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.Dir, req.Key()+".json"), append(b, '\n'), 0o644)
}

// layered reads from the first file system that has the file.
type layered []fs.FS

func (l layered) Open(name string) (fs.File, error) {
	var last error = fs.ErrNotExist
	for _, f := range l {
		file, err := f.Open(name)
		if err == nil {
			return file, nil
		}
		last = err
	}
	return nil, last
}

func mustSub(f fs.FS, dir string) fs.FS {
	s, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return s
}

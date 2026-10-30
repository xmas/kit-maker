//go:build js && wasm

// Command kitwasm is the Kit Maker engine (internal/lite) for the browser:
//
//	GOOS=js GOARCH=wasm go build -o cloudflare/public/kitmaker.wasm ./cmd/kitwasm
//
// It sets globalThis.kitmakerEngine, which web/lite/ calls in place of the
// server: handle(request) answers any app route the way internal/http does,
// and returns the documents the request changed so the page can store them.
// The page provides globalThis.kitmakerHost for what needs a backend
// (sign-in, history, AI).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall/js"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/lite"
)

var (
	mu     sync.Mutex // one request at a time
	engine *lite.Engine
)

func main() {
	api := js.Global().Get("Object").New()
	api.Set("init", js.FuncOf(initEngine))
	api.Set("load", js.FuncOf(load))
	api.Set("saved", js.FuncOf(saved))
	api.Set("setUser", js.FuncOf(setUser))
	api.Set("handle", js.FuncOf(handle))
	js.Global().Set("kitmakerEngine", api)
	if cb := js.Global().Get("kitmakerReady"); cb.Type() == js.TypeFunction {
		cb.Invoke()
	}
	select {}
}

// initEngine(starterJSON, mode, userJSON, version) sets up the store with
// the starter content.
func initEngine(_ js.Value, args []js.Value) any {
	var st struct {
		Team lite.Team  `json:"team"`
		Docs []lite.Doc `json:"docs"`
	}
	if err := json.Unmarshal([]byte(args[0].String()), &st); err != nil {
		return "starter: " + err.Error()
	}
	store := lite.NewStore(st.Team)
	if err := store.LoadDocs(st.Docs); err != nil {
		return "starter: " + err.Error()
	}
	engine = lite.NewEngine(store, host{}, args[1].String())
	engine.Version = args[3].String()
	setUser(js.Undefined(), args[2:3])
	return nil
}

// load(docsJSON) → Promise: adds stored documents over the starter content.
// It waits its turn behind a running request, so it runs in a goroutine
// (blocking the JS thread on the lock would deadlock).
func load(_ js.Value, args []js.Value) any {
	raw := args[0].String()
	return promise(func() (any, error) {
		mu.Lock()
		defer mu.Unlock()
		var docs []lite.Doc
		if err := json.Unmarshal([]byte(raw), &docs); err != nil {
			return nil, err
		}
		return nil, engine.Store.LoadDocs(docs)
	})
}

// saved(revsJSON) → Promise: records the store revs of written documents.
func saved(_ js.Value, args []js.Value) any {
	raw := args[0].String()
	return promise(func() (any, error) {
		mu.Lock()
		defer mu.Unlock()
		var revs map[string]int
		if err := json.Unmarshal([]byte(raw), &revs); err != nil {
			return nil, err
		}
		engine.Store.Saved(revs)
		return nil, nil
	})
}

// setUser(userJSON or null) sets who is signed in.
func setUser(_ js.Value, args []js.Value) any {
	engine.User = nil
	if len(args) > 0 && args[0].Type() == js.TypeString && args[0].String() != "" {
		var u lite.User
		if json.Unmarshal([]byte(args[0].String()), &u) == nil && u.Email != "" {
			engine.User = &u
		}
	}
	return nil
}

// handle({method, url, headers, body}) → Promise<{status, headers, body,
// writes, user}>.
func handle(_ js.Value, args []js.Value) any {
	req := args[0]
	method, url, body := req.Get("method").String(), req.Get("url").String(), ""
	if b := req.Get("body"); b.Type() == js.TypeString {
		body = b.String()
	}
	headers := map[string]string{}
	if h := req.Get("headers"); h.Type() == js.TypeObject {
		keys := js.Global().Get("Object").Call("keys", h)
		for i := 0; i < keys.Length(); i++ {
			k := keys.Index(i).String()
			headers[k] = h.Get(k).String()
		}
	}
	return promise(func() (any, error) {
		mu.Lock()
		defer mu.Unlock()
		r := httptest.NewRequest(strings.ToUpper(method), url, strings.NewReader(body))
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		res := w.Result()
		out := js.Global().Get("Object").New()
		out.Set("status", res.StatusCode)
		hs := js.Global().Get("Object").New()
		for k := range res.Header {
			hs.Set(strings.ToLower(k), res.Header.Get(k))
		}
		out.Set("headers", hs)
		data, _ := io.ReadAll(res.Body)
		arr := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(arr, data)
		out.Set("body", arr)
		writes, _ := json.Marshal(engine.Store.TakeWrites())
		out.Set("writes", string(writes))
		if engine.User != nil {
			u, _ := json.Marshal(engine.User)
			out.Set("user", string(u))
		} else {
			out.Set("user", js.Null())
		}
		return out, nil
	})
}

// promise runs fn in a goroutine (so it may await JS promises) and returns
// a JS promise of its result.
func promise(fn func() (any, error)) js.Value {
	var executor js.Func
	executor = js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolve, reject := args[0], args[1]
		go func() {
			defer executor.Release()
			v, err := fn()
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
				return
			}
			resolve.Invoke(v)
		}()
		return nil
	})
	return js.Global().Get("Promise").New(executor)
}

// await blocks the calling goroutine until p settles.
func await(p js.Value) (js.Value, error) {
	done := make(chan struct{})
	var val js.Value
	var err error
	then := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			val = args[0]
		}
		close(done)
		return nil
	})
	catch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "request failed"
		if len(args) > 0 {
			if m := args[0].Get("message"); m.Type() == js.TypeString {
				msg = m.String()
			} else {
				msg = args[0].String()
			}
		}
		err = errors.New(msg)
		close(done)
		return nil
	})
	defer then.Release()
	defer catch.Release()
	p.Call("then", then, catch)
	<-done
	return val, err
}

// host calls globalThis.kitmakerHost.
type host struct{}

func call(name string, args ...any) (js.Value, error) {
	h := js.Global().Get("kitmakerHost")
	if h.Type() != js.TypeObject || h.Get(name).Type() != js.TypeFunction {
		return js.Undefined(), errors.New("this deployment has no backend for " + name)
	}
	return await(h.Call(name, args...))
}

func (host) Login(email, password string) (*lite.User, error) {
	v, err := call("login", email, password)
	if err != nil {
		return nil, err
	}
	var u lite.User
	if err := json.Unmarshal([]byte(v.String()), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (host) Logout() error {
	_, err := call("logout")
	return err
}

func (host) Revisions(blueprintID string) ([]lite.Doc, error) {
	v, err := call("revisions", blueprintID)
	if err != nil {
		return nil, err
	}
	var docs []lite.Doc
	return docs, json.Unmarshal([]byte(v.String()), &docs)
}

func (host) Versions(blueprintID string) (*lite.PublishState, error) {
	v, err := call("versions", blueprintID)
	if err != nil {
		return nil, err
	}
	var st lite.PublishState
	return &st, json.Unmarshal([]byte(v.String()), &st)
}

func (host) Publish(req lite.PublishRequest) (*lite.PublishState, error) {
	b, _ := json.Marshal(req)
	v, err := call("publish", string(b))
	if err != nil {
		return nil, err
	}
	var st lite.PublishState
	return &st, json.Unmarshal([]byte(v.String()), &st)
}

func (host) AI() lite.AI {
	h := js.Global().Get("kitmakerHost")
	if h.Type() != js.TypeObject || !h.Get("aiEnabled").Truthy() {
		return nil
	}
	return model{}
}

// model forwards AI requests to the host (a Pages Function with the key).
type model struct{}

func (model) Generate(req ai.Request) (*ai.Response, error) { return aiCall(req, nil) }

func (model) Stream(req ai.Request, onDelta func(string) error) (*ai.Response, error) {
	return aiCall(req, onDelta)
}

func aiCall(req ai.Request, onDelta func(string) error) (*ai.Response, error) {
	body, _ := json.Marshal(map[string]any{
		"route": req.Route, "effort": ai.EffortFor(req.Route), "model": ai.Model,
		"system": req.System, "context": req.Context, "prompt": req.Prompt, "schema": req.Schema,
		"maxTokens": req.MaxTokens, "stream": onDelta != nil,
	})
	var cb js.Value = js.Null()
	if onDelta != nil {
		f := js.FuncOf(func(_ js.Value, args []js.Value) any {
			_ = onDelta(args[0].String())
			return nil
		})
		defer f.Release()
		cb = f.Value
	}
	v, err := call("ai", string(body), cb)
	if err != nil {
		if strings.Contains(err.Error(), "refus") || strings.Contains(err.Error(), "declin") {
			return nil, ai.ErrRefused
		}
		return nil, err
	}
	var res ai.Response
	dec := json.NewDecoder(bytes.NewReader([]byte(v.String())))
	if err := dec.Decode(&res); err != nil {
		return nil, err
	}
	return &res, nil
}

var _ http.Handler = (*lite.Engine)(nil)

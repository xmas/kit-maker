package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/docker/sandbox-kit-spec/v3/spec"
	"gopkg.in/yaml.v3"

	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/patch"
)

// Blueprint is the blueprint-level part of a compile.
type Blueprint struct {
	Slug        string
	DisplayName string
	Description string
	Kind        string // workload | mixin | set
	// Version, when set, compiles a published version: the descriptor
	// carries version: "<n>", which published provides entries need (spec
	// §9.2), and the header says vN instead of draft.
	Version int
}

// Block is a filled slot's effective block: base ⊕ overrides.
type Block struct {
	Name    string
	Slug    string
	Type    string
	Version int
	Edits   int
	Payload map[string]any // effective payload, item keys included
	// Base is the library version's payload. When set, entries the blueprint
	// added or changed are annotated in the YAML, and removed ones noted.
	Base map[string]any
}

// Slot is one place in the outline. Block is nil while the slot is blank.
type Slot struct {
	Key      string
	Label    string
	Section  string
	Required bool
	Block    *Block
}

// Input is everything a compile needs.
type Input struct {
	Blueprint Blueprint
	Slots     []Slot
}

// Rule is one network entry in the merged policy, with where it came from.
type Rule struct {
	Hosts   []string `json:"hosts"`
	Methods []string `json:"methods,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Source  string   `json:"source"` // slot key
	Label   string   `json:"label,omitempty"`
	Key     string   `json:"key,omitempty"` // the entry's _k in its block
	Phase   string   `json:"phase,omitempty"`
	List    string   `json:"list,omitempty"` // allow | deny
}

// Phase is a phase's merged allow and deny lists.
type Phase struct {
	Allow []Rule `json:"allow,omitempty"`
	Deny  []Rule `json:"deny,omitempty"`
}

// Policy is the merged network policy, for check evaluation.
type Policy struct {
	Install Phase `json:"install"`
	Runtime Phase `json:"runtime"`
}

// Output is a compile result. A blueprint with blanks still compiles; Valid says
// whether the spec package accepts what was produced.
type Output struct {
	YAML     string        `json:"yaml"`
	Valid    bool          `json:"valid"`
	Error    string        `json:"error,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
	Problems []Problem     `json:"problems,omitempty"`
	Blanks   []string      `json:"blanks,omitempty"`
	Digest   string        `json:"digest,omitempty"`
	Surface  *spec.Surface `json:"surface,omitempty"`
	// Spans are the YAML lines each slot produced (1-based, inclusive,
	// starting at its "# slot" comment), so the UI can highlight a block.
	Spans map[string][][2]int `json:"spans,omitempty"`
	// Marks are lines annotated as edits made in the blueprint (added, changed,
	// removed), keyed by 1-based line number.
	Marks map[int]string `json:"marks,omitempty"`
	// Conflicts are blocks working against each other (see conflicts.go).
	Conflicts []Conflict `json:"conflicts,omitempty"`
	Policy    Policy     `json:"-"`
}

// Problem is a block-level validation failure, shown on its slot.
type Problem struct {
	Slot    string `json:"slot"`
	Message string `json:"message"`
}

// Compile merges the filled slots into one kit v3 descriptor (singleton
// capabilities merged, DESIGN.md §2.4), emits commented YAML, and validates
// it with the spec package.
func Compile(in Input) Output {
	var out Output
	caps := &yaml.Node{Kind: yaml.SequenceNode}
	network := map[string]map[string]*yaml.Node{} // phase → allow|deny → seq
	life := newLifecycle()
	tops := map[string][]contrib{} // top-level field → contributions, in slot order
	singletons := map[string]string{}
	var others []*yaml.Node
	var claims []claim

	for _, s := range in.Slots {
		if s.Block == nil {
			if s.Required {
				out.Blanks = append(out.Blanks, s.Key)
			}
			continue
		}
		t, ok := blocktypes.Get(s.Block.Type)
		if !ok {
			out.Problems = append(out.Problems, Problem{s.Key, "unknown block type " + s.Block.Type})
			continue
		}
		if err := t.Validate(s.Block.Payload); err != nil {
			out.Problems = append(out.Problems, Problem{s.Key, err.Error()})
			continue
		}
		source := sourceComment(s.Key, s.Block)
		p, _ := patch.Strip(s.Block.Payload).(map[string]any)
		if t.Singleton {
			if prev, dup := singletons[t.Key]; dup {
				out.Problems = append(out.Problems, Problem{s.Key, fmt.Sprintf("a blueprint has one %s; slot %s already sets it", t.Label, prev)})
				continue
			}
			singletons[t.Key] = s.Key
		}
		switch t.Emit {
		case "network":
			addNetwork(network, &out.Policy, s, source)
		case "credential":
			others = append(others, credentialNode(p, s.Block.Name, source))
			claims = append(claims, instanceClaims(t, p, s)...)
		case "raw":
			others = append(others, rawNode(p, s.Block.Name, source))
		case "capability":
			others = append(others, capabilityNode(t, p, s.Block.Name, source))
			claims = append(claims, instanceClaims(t, p, s)...)
		case "lifecycle":
			if err := life.add(t.Lifecycle, p[t.Lifecycle], s.Key, source); err != nil {
				out.Problems = append(out.Problems, Problem{s.Key, err.Error()})
			}
		case "top":
			for _, f := range t.Fields {
				if v, ok := p[f]; ok && !empty(v) {
					tops[f] = append(tops[f], contrib{slot: s.Key, source: source, v: v})
				}
			}
		}
	}

	out.Conflicts = findConflicts(in.Slots, out.Policy, claims)

	kind := in.Blueprint.Kind
	switch {
	case kind == "set" && len(tops["kits"]) > 0:
	case kind != "workload":
		kind = "mixin"
	}
	doc := mapping()
	addPair(doc, "schemaVersion", quoted("3"))
	addPair(doc, "kind", scalar(kind))
	if in.Blueprint.DisplayName != "" {
		addPair(doc, "displayName", scalar(in.Blueprint.DisplayName))
	}
	if in.Blueprint.Description != "" {
		addPair(doc, "description", scalar(in.Blueprint.Description))
	}
	if in.Blueprint.Version > 0 {
		addPair(doc, "version", quoted(fmt.Sprint(in.Blueprint.Version)))
	}
	out.Problems = append(out.Problems, addTops(doc, tops)...)

	if len(network) > 0 {
		caps.Content = append(caps.Content, networkNode(network))
	}
	if n := life.node(); n != nil {
		caps.Content = append(caps.Content, n)
	}
	caps.Content = append(caps.Content, others...)
	if len(caps.Content) > 0 {
		addPair(doc, "capabilities", caps)
	}

	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{doc},
		HeadComment: "syntax=docker/sandbox-kit:3\nKit Maker · " + in.Blueprint.Slug + " · " + stage(in.Blueprint.Version)}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		out.Error = "emit yaml: " + err.Error()
		return out
	}
	out.YAML = buf.String()
	out.Spans = Spans(out.YAML)
	out.Marks = Marks(out.YAML)

	if len(out.Problems) > 0 {
		out.Error = "fix the blocks marked with problems"
		return out
	}
	d, warnings, err := Validate(buf.Bytes())
	out.Warnings = warnings
	if err != nil {
		out.Error = err.Error()
		return out
	}
	// The spec only enforces this when a kit is built (§3.5): a workload's
	// layers are its root filesystem, so it needs content. Catch it here so
	// the blueprint doesn't look runnable until publishing fails.
	if kind == "workload" && len(tops["build"]) == 0 && len(tops["kits"]) == 0 {
		out.Error = "A workload needs content, its root filesystem: add a Dockerfile or Included kits block under Agent & base."
		return out
	}
	out.Valid = true
	surface := spec.SurfaceOf(d)
	out.Surface = &surface
	canon, _ := json.Marshal(d)
	sum := sha256.Sum256(canon)
	out.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return out
}

func stage(version int) string {
	if version > 0 {
		return fmt.Sprintf("v%d", version)
	}
	return "draft"
}

// sourceComment marks where a slot's content starts: the slot key (unique in
// the blueprint, so the same block in two slots stays distinguishable), then the
// block and version it comes from.
func sourceComment(slotKey string, b *Block) string {
	c := fmt.Sprintf("slot %s · %s v%d", slotKey, b.Slug, b.Version)
	switch {
	case b.Edits == 1:
		c += " · 1 edit here"
	case b.Edits > 1:
		c += fmt.Sprintf(" · %d edits here", b.Edits)
	}
	return c
}

// Edit annotations, as YAML comments above the entries they describe.
const (
	markAdded   = "+ added in this blueprint"
	markChanged = "~ changed in this blueprint"
	markRemoved = "- removed in this blueprint: "
)

// withNotes puts removal notes right after the slot line of a head comment,
// before any mark that belongs to the entry itself.
func withNotes(head string, notes []string) string {
	if len(notes) == 0 {
		return head
	}
	lines := strings.Split(head, "\n")
	at := 0
	if head != "" && strings.HasPrefix(lines[0], "slot ") {
		at = 1
	}
	if head == "" {
		lines = nil
	}
	out := append(append(append([]string{}, lines[:at]...), notes...), lines[at:]...)
	return strings.Join(out, "\n")
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func sortedItems(m map[string]map[string]any) []map[string]any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

// describe names a removed entry in one line.
func describe(e map[string]any) string {
	var b strings.Builder
	if l, _ := e["label"].(string); l != "" {
		b.WriteString(l + " (")
	}
	b.WriteString(strings.Join(strs(e["hosts"]), ", "))
	if m := strs(e["methods"]); len(m) > 0 {
		b.WriteString(" " + strings.Join(m, ","))
	}
	if p := strs(e["paths"]); len(p) > 0 {
		b.WriteString(" " + strings.Join(p, ","))
	}
	if l, _ := e["label"].(string); l != "" {
		b.WriteString(")")
	}
	return b.String()
}

var markRE = regexp.MustCompile(`^(\s*)# ([+~-]) (added|changed|removed) in this blueprint`)

// Marks finds the lines annotated as blueprint edits: an added or changed mark
// covers its comment and the entry under it; a removed mark is its comment.
func Marks(yamlText string) map[int]string {
	lines := strings.Split(strings.TrimRight(yamlText, "\n"), "\n")
	marks := map[int]string{}
	for i := 0; i < len(lines); i++ {
		m := markRE.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		kind, indent := m[3], len(m[1])
		marks[i+1] = kind
		if kind == "removed" {
			continue
		}
		// Skip any further comments, then the "- " entry and its children.
		j := i + 1
		for j < len(lines) && strings.HasPrefix(strings.TrimLeft(lines[j], " "), "#") {
			j++
		}
		if j < len(lines) && strings.HasPrefix(strings.TrimLeft(lines[j], " "), "- ") {
			marks[j+1] = kind
			for k := j + 1; k < len(lines); k++ {
				t := strings.TrimLeft(lines[k], " ")
				if t == "" || len(lines[k])-len(t) <= indent {
					break
				}
				marks[k+1] = kind
			}
		}
	}
	return marks
}

var slotCommentRE = regexp.MustCompile(`^(\s*)# slot ([a-z0-9][a-z0-9-]*) · `)

// Spans finds each slot's lines in the emitted YAML. A slot's content starts
// at its "# slot" comment and runs over the list items at that indent (and
// everything nested under them) until another slot comment, a shallower
// line, or a non-item line at the same indent.
func Spans(yamlText string) map[string][][2]int {
	lines := strings.Split(strings.TrimRight(yamlText, "\n"), "\n")
	spans := map[string][][2]int{}
	keyed := map[string]bool{}
	key, indent, start := "", 0, 0
	closeSpan := func(end int) {
		if key != "" {
			spans[key] = append(spans[key], [2]int{start, end})
			key = ""
		}
	}
	for i, line := range lines {
		n := i + 1
		if m := slotCommentRE.FindStringSubmatch(line); m != nil {
			closeSpan(n - 1)
			key, indent, start = m[2], len(m[1]), n
			continue
		}
		if key == "" {
			continue
		}
		trimmed := strings.TrimLeft(line, " ")
		ind := len(line) - len(trimmed)
		if trimmed == "" || ind > indent {
			continue
		}
		if ind == indent && strings.HasPrefix(trimmed, "#") {
			continue
		}
		// The slot's first line is a mapping key (a top-level field or
		// lifecycle's interactive): it owns that key and what's nested
		// under it, nothing after.
		if ind == indent && !strings.HasPrefix(trimmed, "- ") && !keyed[key+fmt.Sprint(start)] && n == firstContent(lines, start) {
			keyed[key+fmt.Sprint(start)] = true
			continue
		}
		if ind == indent && strings.HasPrefix(trimmed, "- ") && !keyed[key+fmt.Sprint(start)] {
			continue
		}
		closeSpan(n - 1)
	}
	closeSpan(len(lines))
	return spans
}

// firstContent is the first line after start (1-based) that isn't a comment.
func firstContent(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		if t := strings.TrimLeft(lines[i], " "); t != "" && !strings.HasPrefix(t, "#") {
			return i + 1
		}
	}
	return 0
}

// --- network: merged into one network-policy@2 ---------------------------------------

func addNetwork(network map[string]map[string]*yaml.Node, pol *Policy, s Slot, source string) {
	var firstEntry *yaml.Node
	var removed []map[string]any // removed from a list where the block has nothing left
	defer func() {
		if firstEntry == nil {
			return
		}
		var notes []string
		for _, b := range removed {
			notes = append(notes, markRemoved+describe(b))
		}
		firstEntry.HeadComment = withNotes(firstEntry.HeadComment, notes)
	}()
	for _, phase := range []string{"install", "runtime"} {
		rules, _ := s.Block.Payload[phase].(map[string]any)
		for _, list := range []string{"allow", "deny"} {
			items, _ := rules[list].([]any)
			if len(items) == 0 {
				// The blueprint may have removed every library entry in this list.
				if s.Block.Base != nil {
					bph, _ := s.Block.Base[phase].(map[string]any)
					bl, _ := bph[list].([]any)
					for _, it := range bl {
						if e, ok := it.(map[string]any); ok {
							removed = append(removed, e)
						}
					}
				}
				continue
			}
			if network[phase] == nil {
				network[phase] = map[string]*yaml.Node{}
			}
			seq := network[phase][list]
			if seq == nil {
				seq = &yaml.Node{Kind: yaml.SequenceNode}
				network[phase][list] = seq
			}
			baseItems := map[string]map[string]any{}
			if s.Block.Base != nil {
				bph, _ := s.Block.Base[phase].(map[string]any)
				bl, _ := bph[list].([]any)
				for _, it := range bl {
					if e, ok := it.(map[string]any); ok {
						if k, _ := e["_k"].(string); k != "" {
							baseItems[k] = e
						}
					}
				}
			}
			for i, it := range items {
				e, _ := it.(map[string]any)
				r := Rule{Hosts: strs(e["hosts"]), Methods: strs(e["methods"]), Paths: strs(e["paths"]), Source: s.Key, Phase: phase, List: list}
				r.Label, _ = e["label"].(string)
				r.Key, _ = e["_k"].(string)
				n := entryNode(r)
				var head []string
				if i == 0 {
					head = append(head, source)
				}
				if s.Block.Base != nil {
					k, _ := e["_k"].(string)
					switch b, inBase := baseItems[k]; {
					case !inBase:
						head = append(head, markAdded)
					case !sameJSON(b, e):
						head = append(head, markChanged)
					}
					delete(baseItems, k)
				}
				n.HeadComment = strings.Join(head, "\n")
				seq.Content = append(seq.Content, n)
				if firstEntry == nil {
					firstEntry = n
				}
				ph := &pol.Runtime
				if phase == "install" {
					ph = &pol.Install
				}
				if list == "allow" {
					ph.Allow = append(ph.Allow, r)
				} else {
					ph.Deny = append(ph.Deny, r)
				}
			}
			// Library entries the blueprint removed: noted above this block's
			// first entry in the list, so the YAML says what is gone.
			if len(baseItems) > 0 && len(items) > 0 {
				first := seq.Content[len(seq.Content)-len(items)]
				var notes []string
				for _, b := range sortedItems(baseItems) {
					notes = append(notes, markRemoved+describe(b))
				}
				first.HeadComment = withNotes(first.HeadComment, notes)
				baseItems = nil
			}
			removed = append(removed, sortedItems(baseItems)...)
		}
	}
}

func entryNode(r Rule) *yaml.Node {
	if len(r.Methods) == 0 && len(r.Paths) == 0 && len(r.Hosts) == 1 {
		return hostScalar(r.Hosts[0])
	}
	m := mapping()
	addPair(m, "hosts", flowSeq(r.Hosts))
	if len(r.Methods) > 0 {
		addPair(m, "methods", flowSeq(r.Methods))
	}
	if len(r.Paths) > 0 {
		addPair(m, "paths", flowSeq(r.Paths))
	}
	return m
}

func networkNode(network map[string]map[string]*yaml.Node) *yaml.Node {
	cfg := mapping()
	for _, phase := range []string{"install", "runtime"} {
		lists, ok := network[phase]
		if !ok {
			continue
		}
		ph := mapping()
		for _, list := range []string{"allow", "deny"} {
			if seq, ok := lists[list]; ok {
				addPair(ph, list, seq)
			}
		}
		addPair(cfg, phase, ph)
	}
	c := mapping()
	addPair(c, "type", scalar("com.docker.sandbox/network-policy@2"))
	addPair(c, "config", cfg)
	return c
}

// --- credentials and raw capabilities ------------------------------------------------

func credentialNode(p map[string]any, name, source string) *yaml.Node {
	c := mapping()
	c.HeadComment = source
	addPair(c, "type", scalar("com.docker.sandbox/credential@1"))
	if name != "" {
		addPair(c, "name", value(name, nil))
	}
	if opt, _ := p["optional"].(bool); opt {
		addPair(c, "optional", scalar("true"))
	}
	if d, _ := p["description"].(string); d != "" {
		addPair(c, "description", scalar(d))
	}
	cfg := mapping()
	addPair(cfg, "service", scalar(p["service"].(string)))
	addPair(cfg, "phase", flowSeq(strs(p["phase"])))
	if api, ok := p["apiKey"].(map[string]any); ok {
		addPair(cfg, "apiKey", value(api, []string{"name", "proxyManaged", "inject"}))
	}
	addPair(c, "config", cfg)
	return c
}

func rawNode(p map[string]any, name, source string) *yaml.Node {
	c := mapping()
	c.HeadComment = source
	addPair(c, "type", scalar(p["type"].(string)))
	if name != "" {
		addPair(c, "name", value(name, nil))
	}
	if opt, _ := p["optional"].(bool); opt {
		addPair(c, "optional", scalar("true"))
	}
	if d, _ := p["description"].(string); d != "" {
		addPair(c, "description", scalar(d))
	}
	if cfg, ok := p["config"].(map[string]any); ok && len(cfg) > 0 {
		addPair(c, "config", value(cfg, nil))
	}
	return c
}

// capabilityNode is a capability block's own entry: the block's name as the
// entry's display label, optional and description lifted out of the
// payload, and the rest as config.
func capabilityNode(t blocktypes.Type, p map[string]any, name, source string) *yaml.Node {
	c := mapping()
	c.HeadComment = source
	addPair(c, "type", scalar(t.Capability))
	if name != "" {
		addPair(c, "name", value(name, nil))
	}
	if opt, _ := p["optional"].(bool); opt {
		addPair(c, "optional", scalar("true"))
	}
	if d, _ := p["description"].(string); d != "" {
		addPair(c, "description", value(d, nil))
	}
	if t.ConfigLess {
		return c
	}
	cfg := map[string]any{}
	for k, v := range p {
		if k != "optional" && k != "description" {
			cfg[k] = v
		}
	}
	if len(cfg) > 0 {
		addPair(c, "config", value(cfg, configOrder))
	}
	return c
}

// configOrder puts the identifying keys of common configs first.
var configOrder = []string{"service", "phase", "agent", "name", "path", "directory", "filename", "command", "user",
	"container", "transport", "prompt", "resume", "continue", "newSession", "sessionPicker", "list"}

// --- lifecycle: setup blocks merged into one lifecycle@1 ---------------------------

type lifecycle struct {
	lists       map[string]*yaml.Node // install | startup | files → items, slot comment on each block's first
	interactive *yaml.Node
	owner       string // slot that set interactive
}

func newLifecycle() *lifecycle { return &lifecycle{lists: map[string]*yaml.Node{}} }

func (l *lifecycle) add(list string, v any, slot, source string) error {
	items, _ := v.([]any)
	if list == "interactive" {
		if l.interactive != nil {
			return fmt.Errorf("a blueprint has one interactive launch; slot %s already sets it", l.owner)
		}
		l.interactive, l.owner = value(items, nil), slot
		l.interactive.HeadComment = source
		return nil
	}
	if len(items) == 0 {
		return fmt.Errorf("add at least one entry to %s", list)
	}
	seq := l.lists[list]
	if seq == nil {
		seq = &yaml.Node{Kind: yaml.SequenceNode}
		l.lists[list] = seq
	}
	for i, it := range items {
		n := value(it, []string{"command", "path", "user", "content", "mode", "env", "background", "overwrite", "description"})
		if i == 0 {
			n.HeadComment = source
		}
		seq.Content = append(seq.Content, n)
	}
	return nil
}

func (l *lifecycle) node() *yaml.Node {
	if len(l.lists) == 0 && l.interactive == nil {
		return nil
	}
	cfg := mapping()
	for _, k := range []string{"install", "startup", "files"} {
		if seq := l.lists[k]; seq != nil {
			addPair(cfg, k, seq)
		}
	}
	if l.interactive != nil {
		k := scalar("interactive")
		k.HeadComment, l.interactive.HeadComment = l.interactive.HeadComment, ""
		cfg.Content = append(cfg.Content, k, l.interactive)
	}
	c := mapping()
	addPair(c, "type", scalar("com.docker.sandbox/lifecycle@1"))
	addPair(c, "config", cfg)
	return c
}

// --- top-level fields from About, Dockerfile, Included kits, Dependencies and Arguments ---

type contrib struct {
	slot, source string
	v            any
}

var topOrder = []string{"author", "sourceUrl", "iconUrl", "licenses", "provides", "requires", "integrates", "conflicts", "args", "build", "kits"}

// addTops writes the top-level fields in descriptor order, each preceded by
// the slot comment of the block that set it. Scalars and the content recipe
// have one owner; lists union; args merge by name.
func addTops(doc *yaml.Node, tops map[string][]contrib) []Problem {
	var problems []Problem
	if len(tops["build"]) > 0 && len(tops["kits"]) > 0 {
		problems = append(problems, Problem{tops["kits"][0].slot, fmt.Sprintf("a blueprint's content is a Dockerfile or included kits, not both (slot %s has a Dockerfile)", tops["build"][0].slot)})
		delete(tops, "kits")
	}
	for _, f := range topOrder {
		cs := tops[f]
		if len(cs) == 0 {
			continue
		}
		switch f {
		case "licenses", "provides", "requires", "integrates", "conflicts":
			if len(cs) == 1 {
				addKeyed(doc, f, value(cs[0].v, nil), cs[0].source)
				continue
			}
			seq := &yaml.Node{Kind: yaml.SequenceNode}
			for _, c := range cs {
				for i, it := range c.v.([]any) {
					n := value(it, nil)
					if i == 0 {
						n.HeadComment = c.source
					}
					seq.Content = append(seq.Content, n)
				}
			}
			addPair(doc, f, seq)
		case "args":
			m, seen := mapping(), map[string]string{}
			for _, c := range cs {
				args, _ := c.v.(map[string]any)
				first := true
				for _, name := range orderedKeys(args, nil) {
					if prev, dup := seen[name]; dup {
						problems = append(problems, Problem{c.slot, fmt.Sprintf("argument %q is already declared in slot %s", name, prev)})
						continue
					}
					seen[name] = c.slot
					k := scalar(name)
					if first {
						k.HeadComment, first = c.source, false
					}
					m.Content = append(m.Content, k, value(args[name], []string{"default", "required", "description", "enum", "pattern", "env", "buildArg"}))
				}
			}
			if len(m.Content) > 0 {
				addPair(doc, f, m)
			}
		default: // author, sourceUrl, iconUrl, build, kits: one owner
			for _, c := range cs[1:] {
				if !sameJSON(c.v, cs[0].v) {
					problems = append(problems, Problem{c.slot, fmt.Sprintf("%s is already set in slot %s", f, cs[0].slot)})
				}
			}
			addKeyed(doc, f, value(cs[0].v, []string{"ref", "digest", "args"}), cs[0].source)
		}
	}
	return problems
}

func addKeyed(m *yaml.Node, k string, v *yaml.Node, comment string) {
	key := scalar(k)
	key.HeadComment = comment
	m.Content = append(m.Content, key, v)
}

func empty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

// --- yaml node helpers -------------------------------------------------------------

func mapping() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode} }

func scalar(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: s} }

func quoted(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s, Style: yaml.DoubleQuotedStyle}
}

// hostScalar quotes hosts that YAML would misread ("*.example.com" is an alias).
func hostScalar(h string) *yaml.Node {
	if strings.ContainsAny(h, "*:") || strings.Trim(h, "0123456789.") == "" {
		return quoted(h)
	}
	return scalar(h)
}

func addPair(m *yaml.Node, k string, v *yaml.Node) {
	m.Content = append(m.Content, scalar(k), v)
}

func flowSeq(items []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, it := range items {
		n.Content = append(n.Content, hostScalar(it))
	}
	return n
}

// value converts decoded JSON into a node, keys in `order` first, then
// alphabetical. Lists of scalars and maps of scalars render in flow style.
func value(v any, order []string) *yaml.Node {
	switch t := v.(type) {
	case map[string]any:
		m := mapping()
		allScalar := true
		for _, k := range orderedKeys(t, order) {
			child := value(t[k], order)
			if child.Kind != yaml.ScalarNode || child.Style == yaml.LiteralStyle {
				allScalar = false
			}
			addPair(m, k, child)
		}
		if allScalar && len(t) <= 5 {
			m.Style = yaml.FlowStyle
		}
		return m
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode}
		allScalar := true
		for _, it := range t {
			child := value(it, order)
			if child.Kind != yaml.ScalarNode || child.Style == yaml.LiteralStyle {
				allScalar = false
			}
			n.Content = append(n.Content, child)
		}
		if allScalar {
			n.Style = yaml.FlowStyle
		}
		return n
	case string:
		if strings.Contains(t, "\n") {
			return &yaml.Node{Kind: yaml.ScalarNode, Value: t, Style: yaml.LiteralStyle}
		}
		if !plainString(t) || strings.ContainsAny(t, "{}[],#%") || strings.Contains(t, ": ") || strings.ContainsAny(t[:1], "*&!|>'\"@`:-?") {
			return quoted(t)
		}
		return scalar(t)
	case bool:
		return scalar(fmt.Sprint(t))
	case float64:
		return scalar(strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%f", t), "0"), "."))
	case nil:
		return scalar("null")
	default:
		return scalar(fmt.Sprint(t))
	}
}

// plainString reports whether s reads back as the same string unquoted
// ("0644", "true", "8080" and "" don't).
func plainString(s string) bool {
	if s == "" || strings.TrimSpace(s) != s {
		return false
	}
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err != nil {
		return false
	}
	str, ok := v.(string)
	return ok && str == s
}

func orderedKeys(m map[string]any, order []string) []string {
	rank := map[string]int{}
	for i, k := range order {
		rank[k] = i + 1
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, rj := rank[keys[i]], rank[keys[j]]
		switch {
		case ri > 0 && rj > 0:
			return ri < rj
		case ri > 0:
			return true
		case rj > 0:
			return false
		}
		return keys[i] < keys[j]
	})
	return keys
}

func strs(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

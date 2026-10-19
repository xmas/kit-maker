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

	"github.com/docker/kit-maker/internal/kits/blocktypes"
	"github.com/docker/kit-maker/internal/kits/patch"
)

// Kit is the kit-level part of a compile.
type Kit struct {
	Slug        string
	DisplayName string
	Description string
	Kind        string // workload | mixin (sets and environments come later)
}

// Block is a filled slot's effective block: base ⊕ overrides.
type Block struct {
	Name    string
	Slug    string
	Type    string
	Version int
	Edits   int
	Payload map[string]any // effective payload, item keys included
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
	Kit   Kit
	Slots []Slot
}

// Rule is one network entry in the merged policy, with where it came from.
type Rule struct {
	Hosts   []string `json:"hosts"`
	Methods []string `json:"methods,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Source  string   `json:"source"` // slot key
	Label   string   `json:"label,omitempty"`
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

// Output is a compile result. A kit with blanks still compiles; Valid says
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
	Spans  map[string][][2]int `json:"spans,omitempty"`
	Policy Policy              `json:"-"`
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
	kind := in.Kit.Kind
	if kind != "workload" {
		kind = "mixin"
	}

	doc := mapping()
	addPair(doc, "schemaVersion", quoted("3"))
	addPair(doc, "kind", scalar(kind))
	if in.Kit.DisplayName != "" {
		addPair(doc, "displayName", scalar(in.Kit.DisplayName))
	}
	if in.Kit.Description != "" {
		addPair(doc, "description", scalar(in.Kit.Description))
	}

	caps := &yaml.Node{Kind: yaml.SequenceNode}
	network := map[string]map[string]*yaml.Node{} // phase → allow|deny → seq
	var others []*yaml.Node

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
		switch t.Key {
		case blocktypes.NetworkRules:
			addNetwork(network, &out.Policy, s, source)
		case blocktypes.Credential:
			others = append(others, credentialNode(s.Block.Payload, source))
		case blocktypes.RawCapability:
			others = append(others, rawNode(s.Block.Payload, source))
		}
	}
	if len(network) > 0 {
		caps.Content = append(caps.Content, networkNode(network))
	}
	caps.Content = append(caps.Content, others...)
	if len(caps.Content) > 0 {
		addPair(doc, "capabilities", caps)
	}

	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{doc},
		HeadComment: "syntax=docker/sandbox-kit:3\nKit Maker · " + in.Kit.Slug + " · draft"}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		out.Error = "emit yaml: " + err.Error()
		return out
	}
	out.YAML = buf.String()
	out.Spans = Spans(out.YAML)

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
	out.Valid = true
	surface := spec.SurfaceOf(d)
	out.Surface = &surface
	canon, _ := json.Marshal(d)
	sum := sha256.Sum256(canon)
	out.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return out
}

// sourceComment marks where a slot's content starts: the slot key (unique in
// the kit, so the same block in two slots stays distinguishable), then the
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

var slotCommentRE = regexp.MustCompile(`^(\s*)# slot ([a-z0-9][a-z0-9-]*) · `)

// Spans finds each slot's lines in the emitted YAML. A slot's content starts
// at its "# slot" comment and runs over the list items at that indent (and
// everything nested under them) until another slot comment, a shallower
// line, or a non-item line at the same indent.
func Spans(yamlText string) map[string][][2]int {
	lines := strings.Split(strings.TrimRight(yamlText, "\n"), "\n")
	spans := map[string][][2]int{}
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
		if trimmed == "" || ind > indent || (ind == indent && strings.HasPrefix(trimmed, "- ")) {
			continue
		}
		closeSpan(n - 1)
	}
	closeSpan(len(lines))
	return spans
}

// --- network: merged into one network-policy@2 ---------------------------------------

func addNetwork(network map[string]map[string]*yaml.Node, pol *Policy, s Slot, source string) {
	for _, phase := range []string{"install", "runtime"} {
		rules, _ := s.Block.Payload[phase].(map[string]any)
		for _, list := range []string{"allow", "deny"} {
			items, _ := rules[list].([]any)
			if len(items) == 0 {
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
			for i, it := range items {
				e, _ := it.(map[string]any)
				r := Rule{Hosts: strs(e["hosts"]), Methods: strs(e["methods"]), Paths: strs(e["paths"]), Source: s.Key}
				r.Label, _ = e["label"].(string)
				n := entryNode(r)
				if i == 0 {
					n.HeadComment = source
				}
				seq.Content = append(seq.Content, n)
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

func credentialNode(p map[string]any, source string) *yaml.Node {
	p = patch.Strip(p).(map[string]any)
	c := mapping()
	c.HeadComment = source
	addPair(c, "type", scalar("com.docker.sandbox/credential@1"))
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

func rawNode(p map[string]any, source string) *yaml.Node {
	p = patch.Strip(p).(map[string]any)
	c := mapping()
	c.HeadComment = source
	addPair(c, "type", scalar(p["type"].(string)))
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
			if child.Kind != yaml.ScalarNode {
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
			if child.Kind != yaml.ScalarNode {
				allScalar = false
			}
			n.Content = append(n.Content, child)
		}
		if allScalar {
			n.Style = yaml.FlowStyle
		}
		return n
	case string:
		if strings.ContainsAny(t, "%*:{}[]#,") || t == "" {
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

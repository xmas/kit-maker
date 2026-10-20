// Package patch keeps a blueprint's edits to a linked block as override ops on top
// of the library version (DESIGN.md §2.2), so library updates can keep flowing
// in underneath.
//
// Payloads are JSON objects. Lists of objects carry stable item keys in "_k",
// so ops address items by key, not position, and survive reordering:
//
//	set     {path, value}          replace a field (scalars, scalar lists, objects)
//	unset   {path}                 remove a field
//	add     {path, value, after}   add an item to a keyed list
//	remove  {path, key}            remove an item from a keyed list
//	replace {path, key, value}     replace an item in a keyed list
package patch

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// KeyField holds an item's stable key in keyed lists.
const KeyField = "_k"

// Op is one override operation.
type Op struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Key   string `json:"key,omitempty"`
	After string `json:"after,omitempty"`
	Value any    `json:"value,omitempty"`
}

// Origin says where a field or list item in the effective payload comes from.
type Origin string

const (
	Inherited Origin = "inherited"
	Changed   Origin = "changed"
	Added     Origin = "added"
	Removed   Origin = "removed"
)

// Origins maps a path ("/runtime/allow" + "#" + key for items, or a field
// path) to its origin. Only non-inherited entries are listed.
type Origins map[string]Origin

// ItemPath names a keyed list item in Origins.
func ItemPath(listPath, key string) string { return listPath + "#" + key }

// Apply returns base with ops applied, and the origin of everything ops
// touched. Ops whose target no longer exists (the library removed it) are
// skipped and returned as conflicts for the rebase UI.
func Apply(base map[string]any, ops []Op) (map[string]any, Origins, []Op) {
	out := deepCopy(base).(map[string]any)
	origins := Origins{}
	var conflicts []Op
	for _, op := range ops {
		if err := applyOne(out, op, origins); err != nil {
			conflicts = append(conflicts, op)
		}
	}
	return out, origins, conflicts
}

func applyOne(root map[string]any, op Op, origins Origins) error {
	parent, field, err := locate(root, op.Path, op.Op == "set" || op.Op == "add")
	if err != nil {
		return err
	}
	switch op.Op {
	case "set":
		parent[field] = deepCopy(op.Value)
		origins[op.Path] = Changed
	case "unset":
		if _, ok := parent[field]; !ok {
			return fmt.Errorf("unset %s: missing", op.Path)
		}
		delete(parent, field)
		origins[op.Path] = Removed
	case "add":
		list, _ := parent[field].([]any)
		item, ok := deepCopy(op.Value).(map[string]any)
		if !ok {
			return fmt.Errorf("add %s: value must be an object", op.Path)
		}
		key, _ := item[KeyField].(string)
		if key == "" || indexOf(list, key) >= 0 {
			return fmt.Errorf("add %s: missing or duplicate key %q", op.Path, key)
		}
		at := len(list)
		if op.After != "" {
			if i := indexOf(list, op.After); i >= 0 {
				at = i + 1
			}
		} else if diffedFromStart(op) {
			at = 0
		}
		list = append(list[:at], append([]any{item}, list[at:]...)...)
		parent[field] = list
		origins[ItemPath(op.Path, key)] = Added
	case "remove":
		list, _ := parent[field].([]any)
		i := indexOf(list, op.Key)
		if i < 0 {
			return fmt.Errorf("remove %s#%s: missing", op.Path, op.Key)
		}
		parent[field] = append(list[:i], list[i+1:]...)
		origins[ItemPath(op.Path, op.Key)] = Removed
	case "replace":
		list, _ := parent[field].([]any)
		i := indexOf(list, op.Key)
		if i < 0 {
			return fmt.Errorf("replace %s#%s: missing", op.Path, op.Key)
		}
		item, ok := deepCopy(op.Value).(map[string]any)
		if !ok {
			return fmt.Errorf("replace %s: value must be an object", op.Path)
		}
		item[KeyField] = op.Key
		list[i] = item
		origins[ItemPath(op.Path, op.Key)] = Changed
	default:
		return fmt.Errorf("unknown op %q", op.Op)
	}
	return nil
}

// Diff returns the ops that turn base into edited. Keyed lists diff by item
// key; other values are replaced whole.
func Diff(base, edited map[string]any) []Op {
	var ops []Op
	diffObject("", base, edited, &ops)
	return ops
}

func diffObject(path string, base, edited map[string]any, ops *[]Op) {
	for _, k := range sortedKeys(base, edited) {
		p := path + "/" + escape(k)
		b, inBase := base[k]
		e, inEdited := edited[k]
		switch {
		case inBase && !inEdited:
			*ops = append(*ops, Op{Op: "unset", Path: p})
		case !inBase && inEdited:
			if el, ok := e.([]any); ok && keyed(el) {
				diffList(p, nil, el, ops)
				continue
			}
			*ops = append(*ops, Op{Op: "set", Path: p, Value: e})
		case reflect.DeepEqual(normalize(b), normalize(e)):
		default:
			bm, bok := b.(map[string]any)
			em, eok := e.(map[string]any)
			if bok && eok {
				diffObject(p, bm, em, ops)
				continue
			}
			bl, blok := b.([]any)
			el, elok := e.([]any)
			if blok && elok && keyed(bl) && keyed(el) {
				diffList(p, bl, el, ops)
				continue
			}
			*ops = append(*ops, Op{Op: "set", Path: p, Value: e})
		}
	}
}

func diffList(path string, base, edited []any, ops *[]Op) {
	baseByKey := map[string]any{}
	for _, it := range base {
		baseByKey[itemKey(it)] = it
	}
	editedKeys := map[string]bool{}
	prev := ""
	for _, it := range edited {
		k := itemKey(it)
		editedKeys[k] = true
		b, ok := baseByKey[k]
		switch {
		case !ok:
			op := Op{Op: "add", Path: path, After: prev, Value: it}
			if prev == "" && len(base) > 0 {
				op.Key = "^"
			}
			*ops = append(*ops, op)
		case !reflect.DeepEqual(normalize(b), normalize(it)):
			*ops = append(*ops, Op{Op: "replace", Path: path, Key: k, Value: it})
		}
		prev = k
	}
	for _, it := range base {
		if k := itemKey(it); !editedKeys[k] {
			*ops = append(*ops, Op{Op: "remove", Path: path, Key: k})
		}
	}
}

// AssignKeys gives every item of every list of objects a stable key when it
// has none, so new items from the UI or an import can be diffed by key.
func AssignKeys(v any) {
	switch t := v.(type) {
	case map[string]any:
		for _, x := range t {
			AssignKeys(x)
		}
	case []any:
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				if k, _ := m[KeyField].(string); k == "" {
					m[KeyField] = NewKey()
				}
				AssignKeys(m)
			}
		}
	}
}

// NewKey returns a short random item key.
func NewKey() string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Hash returns a stable hash of ops (empty for none).
func Hash(ops []Op) string {
	if len(ops) == 0 {
		return ""
	}
	b, _ := json.Marshal(ops)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Strip returns v without item keys, for compiling into a descriptor.
func Strip(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			if k == KeyField {
				continue
			}
			out[k] = Strip(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = Strip(x)
		}
		return out
	default:
		return v
	}
}

// --- helpers ---------------------------------------------------------------------

func locate(root map[string]any, path string, create bool) (map[string]any, string, error) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if path == "" || len(parts) == 0 || parts[0] == "" {
		return nil, "", fmt.Errorf("empty path")
	}
	cur := root
	for _, p := range parts[:len(parts)-1] {
		p = unescape(p)
		next, ok := cur[p].(map[string]any)
		if !ok {
			if !create {
				return nil, "", fmt.Errorf("path %s: %s missing", path, p)
			}
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	return cur, unescape(parts[len(parts)-1]), nil
}

// diffedFromStart reports an add that Diff produced for the first item of a
// list (no predecessor): it belongs at the start, not the end.
func diffedFromStart(op Op) bool { return op.Key == "^" }

func indexOf(list []any, key string) int {
	for i, it := range list {
		if itemKey(it) == key {
			return i
		}
	}
	return -1
}

func itemKey(v any) string {
	m, _ := v.(map[string]any)
	k, _ := m[KeyField].(string)
	return k
}

func keyed(list []any) bool {
	if len(list) == 0 {
		return false
	}
	for _, it := range list {
		if itemKey(it) == "" {
			return false
		}
	}
	return true
}

func sortedKeys(maps ...map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range maps {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// normalize round-trips through JSON so numbers and nested types compare
// equal regardless of how they were built.
func normalize(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func deepCopy(v any) any { return normalize(v) }

func escape(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1") }
func unescape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
}

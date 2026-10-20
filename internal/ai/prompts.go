package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SystemPrefix is the frozen product prefix every route starts with. It is
// the first cache breakpoint, so keep it byte-stable: no timestamps, IDs or
// per-team content here (that goes in Request.Context or the prompt).
const SystemPrefix = `You are the assistant inside Kit Maker, a tool for building blueprints: designs for Docker Sandboxes kits (Kit v3). Users say "blueprint"; a kit is the image a published blueprint version becomes, and the word appears only in YAML and sbx commands.

A kit describes what an AI agent's sandbox may do: which hosts it can reach (network-policy@2 capability, with install and runtime phases, allow and deny rules; deny wins), which credentials the sbx proxy injects into requests (credential@1: the sandbox sees a placeholder env var, the proxy adds the real secret only for listed inject domains), setup scripts, ports, volumes and resources. A workload kit runs an agent; a mixin adds capabilities to one.

In Kit Maker every part of a blueprint is a reusable block from a shared library. A blueprint links blocks into slots. Edits made in a blueprint are stored as overrides on the linked block version, so the blueprint keeps receiving new versions of the block. Origins: inherited (unchanged from the library), modified (edited in this blueprint), custom (private to this blueprint), blank (not filled yet).

Each blueprint has a goal in plain language and checks derived from it (for example "GET https://api.github.com/repos/acme/shop → expect reach"). Checks are evaluated against the merged network policy.

Be concrete and brief. Use the user's own block and rule names. Never invent hosts, secrets or capabilities that are not in the material you are given. Never widen access on your own: if something would grant more access, say so plainly.`

// ExplainInput is what the explain route knows about a slot.
type ExplainInput struct {
	Blueprint        string
	Goal             string
	Slot             string
	BlockName        string
	BlockType        string
	BlockDescription string
	Payload          map[string]any
	// Checks are the goal checks this slot decides, already formatted.
	Checks []string
	// Question is optional; empty asks for a general explanation.
	Question string
}

// ExplainRequest builds the explain route's request.
func ExplainRequest(in ExplainInput) Request {
	payload, _ := json.MarshalIndent(in.Payload, "", "  ")
	var b strings.Builder
	fmt.Fprintf(&b, "Blueprint: %s\nGoal: %s\n\nSlot: %s\nBlock: %s (block type: %s)\n", in.Blueprint, in.Goal, in.Slot, in.BlockName, in.BlockType)
	if in.BlockDescription != "" {
		fmt.Fprintf(&b, "Block description: %s\n", in.BlockDescription)
	}
	fmt.Fprintf(&b, "\nEffective payload in this blueprint:\n%s\n", payload)
	if len(in.Checks) > 0 {
		b.WriteString("\nGoal checks this block decides:\n")
		for _, c := range in.Checks {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	q := strings.TrimSpace(in.Question)
	if q == "" {
		q = "Explain what this block does in this blueprint, in one short paragraph of plain prose (no headings or lists). Mention anything that looks broader than the goal needs."
	}
	fmt.Fprintf(&b, "\nQuestion: %s\n", q)
	return Request{Route: RouteExplain, System: []string{SystemPrefix}, Prompt: b.String(), MaxTokens: 1024}
}

// SearchItem is one block offered to the search route.
type SearchItem struct {
	ID, Name, Type, Description string
	Abilities                   []string
	Content                     string // the block's values, trimmed
}

// SearchMatch is one block the model picked, with why.
type SearchMatch struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// SearchRequest asks which library blocks fit a plain-language need.
func SearchRequest(query string, items []SearchItem) Request {
	var b strings.Builder
	fmt.Fprintf(&b, "Find the library blocks that fit this need: %q\n\nBlocks:\n", query)
	for _, it := range items {
		content := it.Content
		if len(content) > 300 {
			content = content[:300] + "…"
		}
		fmt.Fprintf(&b, "- id=%s · %s (%s) · %s · can: %s · contains: %s\n", it.ID, it.Name, it.Type, it.Description, strings.Join(it.Abilities, ", "), content)
	}
	b.WriteString("\nReturn the blocks that fit, best first, at most 10, each with a short reason (one sentence, plain words). Only use ids from the list. If none fit, return none.")
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"matches": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"properties":           map[string]any{"id": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}},
					"required":             []any{"id", "reason"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []any{"matches"},
		"additionalProperties": false,
	}
	return Request{Route: RouteSearch, System: []string{SystemPrefix}, Prompt: b.String(), Schema: schema, MaxTokens: 1024}
}

// DraftInput is what the draft route knows about the block to write.
type DraftInput struct {
	Blueprint, Goal      string
	BlockType, TypeLabel string
	TypeDescription      string
	Capability           string
	Current              map[string]any // the block's payload now (a starter for a new block)
	Request              string         // what the person asked for
}

// DraftAnswer is the model's block: the whole new payload as JSON text
// (block payloads have no fixed schema), and a one-paragraph note.
type DraftAnswer struct {
	PayloadJSON string `json:"payload_json"`
	Note        string `json:"note"`
}

// DraftRequest asks for a block's content from a plain-language request.
func DraftRequest(in DraftInput) Request {
	cur, _ := json.MarshalIndent(in.Current, "", "  ")
	var b strings.Builder
	fmt.Fprintf(&b, "Blueprint: %s\nGoal: %s\n\nBlock type: %s (%s)", in.Blueprint, in.Goal, in.TypeLabel, in.BlockType)
	if in.Capability != "" {
		fmt.Fprintf(&b, ", compiles into %s", in.Capability)
	}
	fmt.Fprintf(&b, "\n%s\n\nCurrent payload (keep this shape; list items carry stable \"_k\" keys: keep the keys of items you keep, leave them off new items):\n%s\n", in.TypeDescription, cur)
	fmt.Fprintf(&b, "\nRequest: %s\n\n", in.Request)
	b.WriteString("Write the block's complete new payload as JSON in payload_json, in exactly the shape above. Grant only what the request needs: prefer bounded rules (methods, paths) over whole hosts, and never add hosts, secrets or capabilities the request doesn't call for. In note, say in one or two plain sentences what the block now does and anything the person should check.")
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"payload_json": map[string]any{"type": "string"},
			"note":         map[string]any{"type": "string"},
		},
		"required":             []any{"payload_json", "note"},
		"additionalProperties": false,
	}
	return Request{Route: RouteEdit, System: []string{SystemPrefix}, Prompt: b.String(), Schema: schema, MaxTokens: 4096}
}

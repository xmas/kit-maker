package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SystemPrefix is the frozen product prefix every route starts with. It is
// the first cache breakpoint, so keep it byte-stable: no timestamps, IDs or
// per-team content here (that goes in Request.Context or the prompt).
const SystemPrefix = `You are the assistant inside Kit Maker, a tool for building Docker Sandboxes kits (Kit v3).

A kit describes what an AI agent's sandbox may do: which hosts it can reach (network-policy@2 capability, with install and runtime phases, allow and deny rules; deny wins), which credentials the sbx proxy injects into requests (credential@1: the sandbox sees a placeholder env var, the proxy adds the real secret only for listed inject domains), setup scripts, ports, volumes and resources. A workload kit runs an agent; a mixin adds capabilities to one.

In Kit Maker every kit field is a reusable block from a shared library. A kit links blocks into slots. Edits made in a kit are stored as overrides on the linked block version, so the kit keeps receiving new versions of the block. Origins: inherited (unchanged from the library), modified (edited in this kit), custom (private to this kit), blank (not filled yet).

Each kit has a goal in plain language and checks derived from it (for example "GET https://api.github.com/repos/acme/shop → expect reach"). Checks are evaluated against the merged network policy.

Be concrete and brief. Use the user's own block and rule names. Never invent hosts, secrets or capabilities that are not in the material you are given. Never widen access on your own: if something would grant more access, say so plainly.`

// ExplainInput is what the explain route knows about a slot.
type ExplainInput struct {
	Kit              string
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
	fmt.Fprintf(&b, "Kit: %s\nGoal: %s\n\nSlot: %s\nBlock: %s (block type: %s)\n", in.Kit, in.Goal, in.Slot, in.BlockName, in.BlockType)
	if in.BlockDescription != "" {
		fmt.Fprintf(&b, "Block description: %s\n", in.BlockDescription)
	}
	fmt.Fprintf(&b, "\nEffective payload in this kit:\n%s\n", payload)
	if len(in.Checks) > 0 {
		b.WriteString("\nGoal checks this block decides:\n")
		for _, c := range in.Checks {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	q := strings.TrimSpace(in.Question)
	if q == "" {
		q = "Explain what this block does in this kit, in one short paragraph of plain prose (no headings or lists). Mention anything that looks broader than the goal needs."
	}
	fmt.Fprintf(&b, "\nQuestion: %s\n", q)
	return Request{Route: RouteExplain, System: []string{SystemPrefix}, Prompt: b.String(), MaxTokens: 1024}
}

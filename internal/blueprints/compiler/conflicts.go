package compiler

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
)

// Conflict is two (or more) blocks in a blueprint working against each
// other, or a block that needs something no other block provides. The spec
// would accept most of these, which is the point: the merged kit quietly
// does something neither block's author meant.
//
//	blocked    a deny in one block blocks what another block allows (deny wins)
//	widened    one block allows a host outright, so another's limits on it are moot
//	duplicate  two blocks declare the same thing (same credential, path, port…)
//	gap        a credential injects into a host no network block allows
//	missing    the Dockerfile uses a build arg nothing gives a value
type Conflict struct {
	Kind    string   `json:"kind"`
	Message string   `json:"message"`
	Slots   []string `json:"slots"`
	// Fixes hide one entry of one block in this blueprint only (an
	// override; the block stays linked).
	Fixes []Fix `json:"fixes,omitempty"`
}

// Fix removes the entry Key from the list at Path in Slot's block.
type Fix struct {
	Slot  string `json:"slot"`
	Path  string `json:"path"`
	Key   string `json:"key"`
	Label string `json:"label"`
}

// claim is one instance key a capability block takes (DESIGN.md §2.4; the
// spec's instance-shaped types allow one owner per key).
type claim struct {
	key, what, slot string
}

// instanceClaims lists the keys a block's entry occupies.
func instanceClaims(t blocktypes.Type, p map[string]any, s Slot) []claim {
	var out []claim
	add := func(key, what string) { out = append(out, claim{t.Capability + " " + key, what, s.Key}) }
	cfg := func(k string) string { v, _ := p[k].(string); return v }
	switch t.Capability {
	case "com.docker.sandbox/credential@1":
		phases := strs(p["phase"])
		if ph := cfg("phase"); ph != "" {
			phases = []string{ph}
		}
		for _, ph := range phases {
			add(cfg("service")+" "+ph, fmt.Sprintf("the %s credential at %s", cfg("service"), ph))
		}
	case "com.docker.sandbox/volume@1", "com.docker.sandbox/host-mount@1":
		// Volumes and host mounts share one storage key per path.
		out = append(out, claim{"storage " + path.Clean(cfg("path")), "storage at " + path.Clean(cfg("path")), s.Key})
	case "com.docker.sandbox/port@1":
		tr := cfg("transport")
		if tr == "" {
			tr = "tcp"
		}
		add(fmt.Sprint(p["container"])+"/"+tr, fmt.Sprintf("port %v/%s", p["container"], tr))
	case "com.docker.sandbox/agent-context@2":
		add(cfg("directory")+"/"+cfg("filename"), "agent context in "+path.Join(cfg("directory"), cfg("filename")))
	case "com.docker.sandbox/acp@1":
		add(strings.ToLower(cfg("agent")), "the ACP agent "+cfg("agent"))
	case "com.docker.sandbox/agent-skills@1":
		add(path.Clean(cfg("path")), "the skills folder "+cfg("path"))
	case "com.docker.sandbox/agent-skill@1":
		name := cfg("name")
		if name == "" {
			name = path.Base(cfg("path"))
		}
		add(name, "the skill "+name)
	}
	return out
}

func findConflicts(slots []Slot, pol Policy, claims []claim) []Conflict {
	label := map[string]string{}
	for _, s := range slots {
		label[s.Key] = s.Label
		if s.Block != nil && s.Block.Name != "" {
			label[s.Key] = s.Block.Name
		}
	}
	var out []Conflict
	for _, ph := range []struct {
		name string
		p    Phase
	}{{"install", pol.Install}, {"runtime", pol.Runtime}} {
		out = append(out, networkConflicts(ph.name, ph.p, label)...)
	}

	// Duplicates: one owner per instance key.
	owners := map[string]claim{}
	for _, c := range claims {
		if first, taken := owners[c.key]; taken && first.slot != c.slot {
			out = append(out, Conflict{Kind: "duplicate", Slots: []string{first.slot, c.slot},
				Message: fmt.Sprintf("%s and %s both declare %s; a blueprint can only have one.", label[first.slot], label[c.slot], c.what)})
			continue
		}
		owners[c.key] = c
	}

	// Gaps: credential inject domains need an allow in the same phase.
	for _, s := range slots {
		if s.Block == nil || (s.Block.Type != blocktypes.Credential && s.Block.Type != "oauth-credential") {
			continue
		}
		p := s.Block.Payload
		phases := strs(p["phase"])
		if ph, _ := p["phase"].(string); ph != "" {
			phases = []string{ph}
		}
		api, _ := p["apiKey"].(map[string]any)
		for _, it := range asItems(api["inject"]) {
			d, _ := it["domain"].(string)
			for _, ph := range phases {
				rules := pol.Runtime
				if ph == "install" {
					rules = pol.Install
				}
				if d != "" && !allowed(rules, d) {
					out = append(out, Conflict{Kind: "gap", Slots: []string{s.Key},
						Message: fmt.Sprintf("%s sends its secret to %s, but no network block allows %s at %s.", label[s.Key], d, d, ph)})
				}
			}
		}
	}
	return append(out, missingBuildArgs(slots, label)...)
}

// BuildKit fills these itself.
var automaticArgs = map[string]bool{
	"TARGETPLATFORM": true, "TARGETOS": true, "TARGETARCH": true, "TARGETVARIANT": true,
	"BUILDPLATFORM": true, "BUILDOS": true, "BUILDARCH": true, "BUILDVARIANT": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "FTP_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
	"http_proxy": true, "https_proxy": true, "ftp_proxy": true, "no_proxy": true, "all_proxy": true,
}

// missingBuildArgs finds ARGs in a Dockerfile block with no default that no
// argument (an Arguments block's buildArg) supplies: the build runs with
// them empty, which usually breaks it (a download URL without its version).
func missingBuildArgs(slots []Slot, label map[string]string) []Conflict {
	supplied := map[string]bool{}
	for _, s := range slots {
		if s.Block == nil || s.Block.Type != "args" {
			continue
		}
		args, _ := s.Block.Payload["args"].(map[string]any)
		for _, a := range args {
			if m, ok := a.(map[string]any); ok {
				if b, _ := m["buildArg"].(string); b != "" {
					supplied[b] = true
				}
			}
		}
	}
	var out []Conflict
	for _, s := range slots {
		if s.Block == nil || s.Block.Type != "dockerfile" {
			continue
		}
		build, _ := s.Block.Payload["build"].(string)
		defaulted, declared := map[string]bool{}, []string{}
		for _, line := range strings.Split(build, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || !strings.EqualFold(fields[0], "ARG") {
				continue
			}
			for _, f := range fields[1:] {
				name, _, hasDefault := strings.Cut(f, "=")
				if hasDefault {
					defaulted[name] = true
				}
				if !slices.Contains(declared, name) {
					declared = append(declared, name)
				}
			}
		}
		var missing []string
		for _, name := range declared {
			if !defaulted[name] && !supplied[name] && !automaticArgs[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) == 0 {
			continue
		}
		what := "build argument " + missing[0]
		if len(missing) > 1 {
			what = "build arguments " + strings.Join(missing, ", ")
		}
		out = append(out, Conflict{Kind: "missing", Slots: []string{s.Key},
			Message: fmt.Sprintf("%s uses %s, but nothing gives %s a value, so the build gets it empty. Add an Arguments block whose argument has buildArg: %s and a default, or give the ARG a default.",
				label[s.Key], what, map[bool]string{true: "them", false: "it"}[len(missing) > 1], missing[0])})
	}
	return out
}

// networkConflicts finds, within one phase, denies that fully block another
// block's allow, and outright allows that cancel another block's limits.
func networkConflicts(phase string, p Phase, label map[string]string) []Conflict {
	var out []Conflict
	seen := map[string]bool{}
	for _, a := range p.Allow {
		for _, d := range p.Deny {
			if a.Source == d.Source {
				continue // one block refining itself is its own business
			}
			host, ok := overlap(a.Hosts, d.Hosts)
			if !ok || !covers(d, a) {
				continue
			}
			id := "blocked " + a.Source + a.Key + d.Source + d.Key
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, Conflict{Kind: "blocked", Slots: []string{a.Source, d.Source},
				Message: fmt.Sprintf("%s blocks %s%s at %s, which %s allows. Deny wins, so the allow does nothing.",
					label[d.Source], host, bounds(d), phase, label[a.Source]),
				Fixes: []Fix{
					{Slot: d.Source, Path: "/" + phase + "/deny", Key: d.Key, Label: fmt.Sprintf("Stop blocking it (hide the deny in %s)", label[d.Source])},
					{Slot: a.Source, Path: "/" + phase + "/allow", Key: a.Key, Label: fmt.Sprintf("Keep it blocked (hide the allow in %s)", label[a.Source])},
				}})
		}
		if len(a.Methods) == 0 {
			continue
		}
		for _, b := range p.Allow {
			if b.Source == a.Source || len(b.Methods) > 0 {
				continue
			}
			host, ok := overlap(a.Hosts, b.Hosts)
			if !ok {
				continue
			}
			id := "widened " + a.Source + a.Key + b.Source + b.Key
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, Conflict{Kind: "widened", Slots: []string{a.Source, b.Source},
				Message: fmt.Sprintf("%s allows %s outright at %s, so %s's limit (%s only) doesn't apply.",
					label[b.Source], host, phase, label[a.Source], strings.Join(a.Methods, ", ")),
				Fixes: []Fix{
					{Slot: b.Source, Path: "/" + phase + "/allow", Key: b.Key, Label: fmt.Sprintf("Keep the limit (hide the outright allow in %s)", label[b.Source])},
					{Slot: a.Source, Path: "/" + phase + "/allow", Key: a.Key, Label: fmt.Sprintf("Allow it fully (hide the limited rule in %s)", label[a.Source])},
				}})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// covers reports whether deny d blocks every request allow a lets through.
func covers(d, a Rule) bool {
	if len(d.Methods) == 0 || slices.Contains(d.Methods, "ANY") {
		return len(d.Paths) == 0 || slices.Contains(d.Paths, "/**")
	}
	if len(a.Methods) == 0 {
		return false // a allows every method; d blocks only some
	}
	for _, m := range a.Methods {
		if !slices.Contains(d.Methods, m) {
			return false
		}
	}
	return len(d.Paths) == 0 || slices.Contains(d.Paths, "/**")
}

func bounds(r Rule) string {
	if len(r.Methods) == 0 {
		return ""
	}
	return " (" + strings.Join(r.Methods, ", ") + ")"
}

// overlap returns a host of allow matched by one of patterns.
func overlap(hosts, patterns []string) (string, bool) {
	for _, h := range hosts {
		for _, pat := range patterns {
			if hostMatch(pat, h) || hostMatch(h, pat) {
				return h, true
			}
		}
	}
	return "", false
}

func allowed(p Phase, host string) bool {
	for _, a := range p.Allow {
		for _, pat := range a.Hosts {
			if hostMatch(pat, host) {
				return true
			}
		}
	}
	return false
}

// hostMatch matches a host against a pattern: exact, "*.example.com" for
// any subdomain, "*" / "**" for everything. Ports are ignored when only one
// side names one.
func hostMatch(pattern, host string) bool {
	pattern, host = strings.ToLower(pattern), strings.ToLower(host)
	if pattern == "*" || pattern == "**" {
		return true
	}
	ph, pp := splitPort(pattern)
	hh, hp := splitPort(host)
	if pp != "" && hp != "" && pp != hp {
		return false
	}
	if strings.HasPrefix(ph, "*.") || strings.HasPrefix(ph, "**.") {
		suffix := ph[strings.Index(ph, "."):]
		return strings.HasSuffix(hh, suffix)
	}
	return ph == hh
}

func splitPort(h string) (string, string) {
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i+1:], ".") {
		return h[:i], h[i+1:]
	}
	return h, ""
}

func asItems(v any) []map[string]any {
	var out []map[string]any
	for _, it := range asList(v) {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

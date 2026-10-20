// Package checks evaluates a blueprint's goal checks against its compiled form
// (DESIGN.md §2). Network checks ask "would this request be allowed?" of the
// merged policy: deny wins, then allow, otherwise blocked.
package checks

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/docker/kit-maker/internal/blueprints/compiler"
)

// Check is one expectation derived from the goal.
type Check struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Type   string `json:"type"`   // network (more types come later)
	URL    string `json:"url"`    // network: target URL
	Method string `json:"method"` // network: HTTP method, default GET
	Expect string `json:"expect"` // reach | blocked
}

// Result is a check's outcome.
type Result struct {
	Check
	Status string `json:"status"` // pass | fail | pending
	Actual string `json:"actual,omitempty"`
	Why    string `json:"why"`
	// Source is the slot whose rule decided the outcome, if any.
	Source string `json:"source,omitempty"`
}

// Evaluate runs every check against the merged policy. slotLabels names
// slots in explanations.
func Evaluate(pol compiler.Policy, list []Check, slotLabels map[string]string) []Result {
	out := make([]Result, 0, len(list))
	for _, c := range list {
		out = append(out, evaluate(pol, c, slotLabels))
	}
	return out
}

func evaluate(pol compiler.Policy, c Check, labels map[string]string) Result {
	r := Result{Check: c}
	if c.Type != "network" {
		r.Status, r.Why = "pending", "this check type isn't evaluated yet"
		return r
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Hostname() == "" {
		r.Status, r.Why = "fail", fmt.Sprintf("%q is not a URL", c.URL)
		return r
	}
	method := strings.ToUpper(c.Method)
	if method == "" {
		method = "GET"
	}
	reqPath := u.EscapedPath()
	if reqPath == "" {
		reqPath = "/"
	}
	host := u.Hostname()
	name := func(slot string) string {
		if l := labels[slot]; l != "" {
			return l
		}
		return slot
	}

	if rule, ok := match(pol.Runtime.Deny, host, method, reqPath); ok {
		r.Actual, r.Source = "blocked", rule.Source
		r.Why = fmt.Sprintf("denied by %s (%s)", name(rule.Source), describe(rule))
	} else if rule, ok := match(pol.Runtime.Allow, host, method, reqPath); ok {
		r.Actual, r.Source = "reach", rule.Source
		r.Why = fmt.Sprintf("allowed by %s (%s)", name(rule.Source), describe(rule))
	} else {
		r.Actual = "blocked"
		r.Why = fmt.Sprintf("no rule allows %s %s", method, host)
		if hostAllowedOtherwise(pol.Runtime.Allow, host) {
			r.Why = fmt.Sprintf("%s is allowed, but not %s %s", host, method, reqPath)
		}
	}
	if r.Actual == c.Expect {
		r.Status = "pass"
	} else {
		r.Status = "fail"
	}
	return r
}

func match(rules []compiler.Rule, host, method, reqPath string) (compiler.Rule, bool) {
	for _, rule := range rules {
		if !slices.ContainsFunc(rule.Hosts, func(p string) bool { return hostMatches(p, host) }) {
			continue
		}
		if len(rule.Methods) > 0 && !slices.Contains(rule.Methods, method) && !slices.Contains(rule.Methods, "ANY") {
			continue
		}
		if len(rule.Paths) > 0 && !slices.ContainsFunc(rule.Paths, func(p string) bool { return pathMatches(p, reqPath) }) {
			continue
		}
		return rule, true
	}
	return compiler.Rule{}, false
}

func hostAllowedOtherwise(rules []compiler.Rule, host string) bool {
	for _, rule := range rules {
		if slices.ContainsFunc(rule.Hosts, func(p string) bool { return hostMatches(p, host) }) {
			return true
		}
	}
	return false
}

// hostMatches implements the spec's host patterns: exact host, host:port
// (port ignored), *.example.com / **.example.com (any subdomain), * or **.
func hostMatches(pattern, host string) bool {
	pattern = strings.ToLower(pattern)
	host = strings.ToLower(host)
	if h, _, ok := strings.Cut(pattern, ":"); ok && !strings.Contains(h, "]") {
		pattern = h
	}
	switch {
	case pattern == "*" || pattern == "**":
		return true
	case strings.HasPrefix(pattern, "**."):
		return strings.HasSuffix(host, pattern[2:])
	case strings.HasPrefix(pattern, "*."):
		return strings.HasSuffix(host, pattern[1:])
	default:
		return pattern == host
	}
}

// pathMatches supports * (one segment) and ** (any depth).
func pathMatches(pattern, p string) bool {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return p == prefix || strings.HasPrefix(p, prefix+"/")
	}
	if strings.Contains(pattern, "**") {
		prefix, _, _ := strings.Cut(pattern, "**")
		return strings.HasPrefix(p, prefix)
	}
	ok, err := path.Match(pattern, p)
	return err == nil && ok
}

func describe(r compiler.Rule) string {
	s := strings.Join(r.Hosts, ", ")
	if len(r.Methods) > 0 {
		s += " " + strings.Join(r.Methods, ",")
	}
	if len(r.Paths) > 0 {
		s += " " + strings.Join(r.Paths, ",")
	}
	return s
}

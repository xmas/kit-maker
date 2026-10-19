package checks

import (
	"strings"
	"testing"

	"github.com/docker/kit-maker/internal/kits/compiler"
)

func TestEvaluate(t *testing.T) {
	pol := compiler.Policy{Runtime: compiler.Phase{
		Allow: []compiler.Rule{
			{Hosts: []string{"github.com"}, Source: "github-egress"},
			{Hosts: []string{"api.github.com"}, Methods: []string{"GET", "HEAD"}, Source: "github-egress"},
			{Hosts: []string{"api.github.com"}, Methods: []string{"POST", "PATCH", "PUT"}, Paths: []string{"/repos/acme/**"}, Source: "github-egress"},
			{Hosts: []string{"*.corp.internal"}, Source: "internal"},
		},
		Deny: []compiler.Rule{
			{Hosts: []string{"169.254.169.254"}, Source: "deny-metadata"},
			{Hosts: []string{"api.github.com"}, Methods: []string{"DELETE"}, Paths: []string{"/repos/**"}, Source: "github-egress"},
		},
	}}
	labels := map[string]string{"github-egress": "GitHub egress", "deny-metadata": "Deny cloud metadata"}
	cases := []struct {
		url, method, expect, status, why string
	}{
		{"https://api.github.com/repos/acme/shop", "GET", "reach", "pass", "allowed by GitHub egress"},
		{"https://api.github.com/repos/acme/shop/pulls/42", "PATCH", "reach", "pass", "/repos/acme/**"},
		{"https://api.github.com/repos/torvalds/linux/issues", "POST", "blocked", "pass", "is allowed, but not POST"},
		{"https://api.github.com/repos/acme/shop", "DELETE", "blocked", "pass", "denied by GitHub egress"},
		{"http://169.254.169.254/latest/meta-data/", "GET", "blocked", "pass", "denied by Deny cloud metadata"},
		{"https://git.corp.internal/x", "GET", "reach", "pass", "internal"},
		{"https://corp.internal/", "GET", "blocked", "pass", "no rule allows"},
		{"https://repo.packagist.com/acme/", "GET", "reach", "fail", "no rule allows GET repo.packagist.com"},
	}
	for _, c := range cases {
		r := Evaluate(pol, []Check{{Type: "network", URL: c.url, Method: c.method, Expect: c.expect}}, labels)[0]
		if r.Status != c.status || !strings.Contains(r.Why, c.why) {
			t.Errorf("%s %s: got %s (%s), want %s containing %q", c.method, c.url, r.Status, r.Why, c.status, c.why)
		}
	}
}

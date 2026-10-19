package blocks

import "testing"

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Acme Shop":        "acme-shop",
		"  npm  mirror!! ": "npm-mirror",
		"GitHub (egress)":  "github-egress",
		"---":              "",
		"Ünïcode 2":        "n-code-2",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

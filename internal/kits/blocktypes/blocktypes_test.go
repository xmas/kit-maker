package blocktypes

import "testing"

// A new block starts from its type's starter, so every starter must validate.
func TestStartersValidate(t *testing.T) {
	for _, ty := range All() {
		for _, slug := range []string{"npm-mirror", "stripe-key", "x"} {
			if err := ty.Validate(Starter(ty.Key, slug)); err != nil {
				t.Errorf("%s starter for %q: %v", ty.Key, slug, err)
			}
		}
	}
}

// Package compiler turns a kit (sub-kits, block links, overrides) into a
// spec kit descriptor, YAML, permission surface and digest (DESIGN.md §4).
//
// It builds on github.com/docker/sandbox-kit-spec/v3/spec, the same package
// the sbx runtime imports, so Kit Maker and sbx agree on what a kit means.
// Phase 0 only wires in decoding and validation.
package compiler

import (
	"fmt"

	"github.com/docker/sandbox-kit-spec/v3/spec"
)

// Validate decodes descriptor YAML strictly and checks every grammar rule,
// the same check `sbx kit validate` runs. Warnings don't make it invalid.
func Validate(raw []byte) (*spec.Descriptor, []string, error) {
	d, err := spec.Decode(raw)
	if err != nil {
		return nil, nil, err
	}
	warnings, err := spec.ValidateRaw(raw, d)
	if err != nil {
		return d, warnings, fmt.Errorf("validate kit: %w", err)
	}
	return d, warnings, nil
}

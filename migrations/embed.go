// Package migrations holds the Atlas versioned migrations generated from the
// Ent schema. They are embedded in the binary and applied by `kitmaker migrate`.
package migrations

import "embed"

// FS contains the migration files and atlas.sum.
//
//go:embed *.sql atlas.sum
var FS embed.FS

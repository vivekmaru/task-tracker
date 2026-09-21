// Package migrations provides the SQL migrations bundled with Forge.
package migrations

import "embed"

// Files contains the original migration files, including Goose markers.
//
//go:embed *.sql
var Files embed.FS

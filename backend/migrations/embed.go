// Package migrations embeds the versioned SQL migrations into the binary.
package migrations

import "embed"

// FS contains every migration file.
//
//go:embed *.sql
var FS embed.FS

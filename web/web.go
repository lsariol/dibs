// Package web holds Dibs's HTML templates and static files, built into the binary.
package web

import "embed"

// FS has templates/ and static/.
//
//go:embed templates static
var FS embed.FS

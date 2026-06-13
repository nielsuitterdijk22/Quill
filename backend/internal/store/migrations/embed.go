// Package migrations embeds the SQL migration files so they ship inside the
// binary and run automatically on startup.
package migrations

import "embed"

// FS holds the golang-migrate up/down files (000001_init.up.sql, ...).
//
//go:embed *.sql
var FS embed.FS

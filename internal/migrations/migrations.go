// Package migrations embeds the SQL schema migrations for the control DB.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

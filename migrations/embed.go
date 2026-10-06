// Package migrations embeds the SQL files so the binaries can apply them
// at deploy time (see internal/store/migrate.go).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

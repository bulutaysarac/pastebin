// Package migrations is the database schema: numbered goose SQL files,
// embedded in the binary and applied in order when the app starts.
//
// Migrations only go forward. To undo one, add a new one. A file that has been
// applied anywhere is never edited again.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

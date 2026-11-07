package migrations

import "embed"

// FS содержит SQL-миграции goose, встроенные в бинарник.
//
//go:embed *.sql
var FS embed.FS

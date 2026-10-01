// Package migrations хранит миграции сайта (goose) и встраивает их в бинарник.
// Таблица версий — goose_site_version; down-секции не пишем.
package migrations

import "embed"

// FS — SQL-миграции сайта.
//
//go:embed *.sql
var FS embed.FS

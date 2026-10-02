// Package migrations хранит миграции модуля images (goose) и встраивает их в бинарник сайта.
//
// Сайт применяет их своей командой migrate до миграций сайта, с отдельной таблицей версий Table.
// Только прямые миграции: down-секций нет.
package migrations

import "embed"

// Table — таблица версий goose для миграций модуля.
const Table = "goose_images_version"

// FS — SQL-миграции модуля.
//
//go:embed *.sql
var FS embed.FS

package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

// schemaFile — итоговая схема БД после всех миграций (модули и сайт): её читают люди и агенты, чтобы не собирать
// таблицу по цепочке миграций. Обновляется командой make schema; TestSchemaSnapshot следит, что она не отстала.
const schemaFile = "../../db/schema.sql"

const schemaHeader = `-- Итоговая схема БД после всех миграций (модули и сайт). Не редактировать: make schema.
-- Источник правды — миграции в db/migrations/ и у модулей; таблицы версий goose не показаны.
`

// TestSchemaSnapshot сверяет db/schema.sql со схемой БД после migrate; UPDATE_SCHEMA=1 перезаписывает файл.
func TestSchemaSnapshot(t *testing.T) {
	s := migratedSite(t, false)
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT sql FROM sqlite_master
		WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' AND tbl_name NOT LIKE 'goose_%'
		ORDER BY tbl_name, type = 'table' DESC, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	b.WriteString(schemaHeader)
	for rows.Next() {
		var stmt string
		if err := rows.Scan(&stmt); err != nil {
			t.Fatal(err)
		}
		b.WriteString("\n" + strings.TrimSpace(stmt) + ";\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if os.Getenv("UPDATE_SCHEMA") == "1" {
		if err := os.WriteFile(schemaFile, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Fatalf("%v (создайте: make schema)", err)
	}
	if string(want) != got {
		t.Errorf("db/schema.sql отстал от миграций — обновите: make schema")
	}
}

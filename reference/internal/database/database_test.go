package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

var testSources = []Source{
	{Name: "mod", Table: "goose_mod_version", FS: fstest.MapFS{
		"00001_a.sql": {Data: []byte("-- +goose Up\nCREATE TABLE mod_a (id INTEGER PRIMARY KEY);\n")},
	}},
	{Name: "site", Table: "goose_site_version", FS: fstest.MapFS{
		"00001_b.sql": {Data: []byte("-- +goose Up\nCREATE TABLE site_b (id INTEGER PRIMARY KEY);\n")},
		"00002_c.sql": {Data: []byte("-- +goose Up\nALTER TABLE site_b ADD COLUMN name TEXT;\n")},
	}},
}

func TestMigrateEmptyDB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, filepath.Join(dir, "site.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	before, err := Status(ctx, db, testSources)
	if err != nil {
		t.Fatal(err)
	}
	if got := Pending(before); got != 3 {
		t.Fatalf("pending before = %d, want 3", got)
	}
	// Status не пишет в БД: таблицы версий не созданы.
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'goose_%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("Status created %d version tables, want 0", n)
	}

	backups := filepath.Join(dir, "backups")
	snap, err := Migrate(ctx, db, testSources, backups)
	if err != nil {
		t.Fatal(err)
	}
	if snap == "" {
		t.Fatal("no snapshot path")
	}

	after, err := Status(ctx, db, testSources)
	if err != nil {
		t.Fatal(err)
	}
	want := []Schema{{Name: "mod", Version: 1}, {Name: "site", Version: 2}}
	for i, s := range after {
		if s != want[i] {
			t.Errorf("schema[%d] = %+v, want %+v", i, s, want[i])
		}
	}
	if _, err := db.Exec(`INSERT INTO site_b (id, name) VALUES (1, 'x')`); err != nil {
		t.Errorf("migrated table: %v", err)
	}

	// Повторный запуск: применять нечего, нового снимка нет.
	snap2, err := Migrate(ctx, db, testSources, backups)
	if err != nil {
		t.Fatal(err)
	}
	if snap2 != "" {
		t.Errorf("second migrate made snapshot %s", snap2)
	}
	entries, _ := os.ReadDir(backups)
	if len(entries) != 1 {
		t.Errorf("backups: %d files, want 1", len(entries))
	}
}

func TestSnapshotIsValidDB(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, filepath.Join(dir, "site.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}

	path, err := Snapshot(ctx, db, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snap.Close() }()
	var v string
	if err := snap.QueryRow(`SELECT v FROM t`).Scan(&v); err != nil || v != "kept" {
		t.Errorf("snapshot data: %q, %v", v, err)
	}
}

func TestMigrateRefusesWithoutSnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(ctx, filepath.Join(dir, "site.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Каталог снимков нельзя создать: на его месте файл.
	blocker := filepath.Join(dir, "backups")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, db, testSources, blocker); err == nil {
		t.Fatal("Migrate succeeded without snapshot")
	}

	st, err := Status(ctx, db, testSources)
	if err != nil {
		t.Fatal(err)
	}
	if got := Pending(st); got != 3 {
		t.Errorf("pending after refused migrate = %d, want 3", got)
	}
}

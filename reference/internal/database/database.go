// Package database открывает SQLite и применяет миграции: модулей и сайта, каждую со своей таблицей версий.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // драйвер "sqlite" без CGO
)

// Open открывает (или создаёт) файл БД с прагмами сайта: WAL, внешние ключи, ожидание блокировки.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

// Source — набор миграций одного владельца: модуля или сайта.
type Source struct {
	Name  string // имя в /healthz и логах: "images", "site"
	FS    fs.FS  // *.sql в корне
	Table string // таблица версий goose: goose_<name>_version
}

// Schema — состояние схемы одного источника.
type Schema struct {
	Name    string
	Version int64 // последняя применённая миграция; 0 — ни одной
	Pending int   // число непримененных миграций
}

// Status возвращает состояние схем, ничего не записывая в БД.
func Status(ctx context.Context, db *sql.DB, sources []Source) ([]Schema, error) {
	out := make([]Schema, 0, len(sources))
	for _, src := range sources {
		s, err := status(ctx, db, src)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Pending — суммарное число непримененных миграций.
func Pending(schemas []Schema) int {
	n := 0
	for _, s := range schemas {
		n += s.Pending
	}
	return n
}

func status(ctx context.Context, db *sql.DB, src Source) (Schema, error) {
	p, err := provider(db, src)
	if err != nil {
		return Schema{}, err
	}
	// goose создаёт таблицу версий при любом обращении; без неё все миграции — непримененные.
	var n int
	err = db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, src.Table).Scan(&n)
	if err != nil {
		return Schema{}, fmt.Errorf("%s: check version table: %w", src.Name, err)
	}
	if n == 0 {
		return Schema{Name: src.Name, Pending: len(p.ListSources())}, nil
	}

	st, err := p.Status(ctx)
	if err != nil {
		return Schema{}, fmt.Errorf("%s: migration status: %w", src.Name, err)
	}
	s := Schema{Name: src.Name}
	for _, m := range st {
		if m.State == goose.StateApplied {
			s.Version = max(s.Version, m.Source.Version)
		} else {
			s.Pending++
		}
	}
	return s, nil
}

// Migrate делает снимок БД в backupDir и применяет миграции источников по порядку.
// Без снимка миграции не применяются. Если применять нечего, снимок не делается и path пуст.
func Migrate(ctx context.Context, db *sql.DB, sources []Source, backupDir string) (path string, err error) {
	schemas, err := Status(ctx, db, sources)
	if err != nil {
		return "", err
	}
	if Pending(schemas) == 0 {
		return "", nil
	}

	path, err = Snapshot(ctx, db, backupDir)
	if err != nil {
		return "", fmt.Errorf("snapshot before migrate (migrations not applied): %w", err)
	}

	for _, src := range sources {
		p, err := provider(db, src)
		if err != nil {
			return path, err
		}
		if _, err := p.Up(ctx); err != nil {
			return path, fmt.Errorf("%s: migrate: %w", src.Name, err)
		}
	}
	return path, nil
}

// Snapshot сохраняет согласованную копию БД через VACUUM INTO в dir/<UTC-время>.db.
func Snapshot(ctx context.Context, db *sql.DB, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	path := filepath.Join(dir, time.Now().UTC().Format("20060102T150405.000000Z")+".db")
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return "", fmt.Errorf("vacuum into %s: %w", path, err)
	}
	if fi, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("check snapshot: %w", err)
	} else if fi.Size() == 0 {
		return "", errors.New("check snapshot: empty file " + path)
	}
	return path, nil
}

func provider(db *sql.DB, src Source) (*goose.Provider, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, db, src.FS,
		goose.WithTableName(src.Table),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return nil, fmt.Errorf("%s: load migrations: %w", src.Name, err)
	}
	return p, nil
}

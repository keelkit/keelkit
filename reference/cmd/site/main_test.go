package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/voffkin/keelkit/reference/internal/admin"
)

// testConfig — каталоги данных и снимков во временной папке теста.
func testConfig(t *testing.T) config {
	t.Helper()
	dir := t.TempDir()
	return config{
		Addr:      "127.0.0.1:0",
		DataDir:   filepath.Join(dir, "data"),
		BackupDir: filepath.Join(dir, "backups"),
	}
}

// migratedDB — БД после site migrate (и site seed, если withSeed).
func migratedDB(t *testing.T, withSeed bool) *sql.DB {
	t.Helper()
	ctx := context.Background()
	cfg := testConfig(t)
	if err := migrate(ctx, cfg); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if withSeed {
		if err := seed(ctx, cfg); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	db, err := openDB(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func get(t *testing.T, db *sql.DB, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	newMux(db, config{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServeRefusesWithPendingMigrations(t *testing.T) {
	err := serve(context.Background(), testConfig(t))
	if !errors.Is(err, errPendingMigrations) {
		t.Fatalf("serve error = %v, want errPendingMigrations", err)
	}
}

func TestMigrateCreatesSnapshot(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	if err := migrate(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(cfg.BackupDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("backups: %v, %v; want 1 snapshot", entries, err)
	}
	// Повторный migrate: применять нечего, нового снимка нет.
	if err := migrate(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(cfg.BackupDir); len(entries) != 1 {
		t.Errorf("backups after second migrate: %d, want 1", len(entries))
	}
}

func TestSeedIdempotent(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	if err := migrate(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := seed(ctx, cfg); err != nil {
			t.Fatal(err)
		}
	}
	db, err := openDB(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM site_settings`).Scan(&n); err != nil || n != 1 {
		t.Errorf("site_settings rows = %d, %v; want 1", n, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM pages`).Scan(&n); err != nil || n != len(seedPages) {
		t.Errorf("pages rows = %d, %v; want %d", n, err, len(seedPages))
	}
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil || n != 1 {
		t.Errorf("users rows = %d, %v; want 1", n, err)
	}
}

func TestHealthz(t *testing.T) {
	rec := get(t, migratedDB(t, false), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "ok" || got.Version != version {
		t.Errorf("got %+v, want status ok and version %q", got, version)
	}
	if s := got.Schema["site"]; s.Version != 4 || s.Pending != 0 {
		t.Errorf("schema site = %+v, want version 4, pending 0", s)
	}
}

// passwords — readPassword для userCreate: отдаёт ответы по очереди.
func passwords(answers ...string) func(string) (string, error) {
	return func(string) (string, error) {
		if len(answers) == 0 {
			return "", errors.New("no more answers")
		}
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}
}

func TestUserCreate(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	if err := migrate(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	if err := userCreate(ctx, cfg, "Editor@Example.com", passwords("long enough", "long enough")); err != nil {
		t.Fatalf("userCreate: %v", err)
	}
	db, err := openDB(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var hash, role string
	err = db.QueryRow(`SELECT password_hash, role FROM users WHERE email = 'editor@example.com'`).Scan(&hash, &role)
	if err != nil {
		t.Fatalf("user not stored: %v", err)
	}
	if ok, _, err := admin.VerifyPassword(ctx, hash, "long enough"); !ok || err != nil || role != "admin" {
		t.Errorf("stored user: password ok %v (err %v), role %q", ok, err, role)
	}

	tests := []struct {
		name, email string
		answers     []string
		wantErr     error
	}{
		{"duplicate", "editor@example.com", []string{"long enough", "long enough"}, errUserExists},
		{"bad email", "not-an-email", nil, nil},
		{"named email", "Bob <bob@example.com>", nil, nil},
		{"short password", "a@example.com", []string{"short"}, admin.ErrPasswordLength},
		{"mismatch", "a@example.com", []string{"long enough", "long enougH"}, nil},
	}
	for _, tt := range tests {
		err := userCreate(ctx, cfg, tt.email, passwords(tt.answers...))
		if err == nil || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.wantErr)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil || n != 1 {
		t.Errorf("users = %d, %v; want 1", n, err)
	}
}

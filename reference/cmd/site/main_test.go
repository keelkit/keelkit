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
	"strings"
	"testing"

	"github.com/voffkin/keelkit/modules/auth"
	"github.com/voffkin/keelkit/modules/images"
)

// testConfig — каталоги данных и снимков во временной папке теста.
func testConfig(t *testing.T) config {
	t.Helper()
	dir := t.TempDir()
	return config{
		Addr:      "127.0.0.1:0",
		DataDir:   filepath.Join(dir, "data"),
		BackupDir: filepath.Join(dir, "backups"),
		BaseURL:   testBaseURL,
	}
}

// testBaseURL — адрес сайта в тестах (BASE_URL): абсолютные ссылки canonical, sitemap и RSS начинаются с него.
const testBaseURL = "https://example.test"

// testSite — сайт после site migrate: БД и сервис изображений над каталогом загрузок теста.
type testSite struct {
	cfg   config
	db    *sql.DB
	media *images.Service
}

// migratedSite — сайт после site migrate; withSeed — ещё и dev-данные без изображений (seedContent):
// обработка изображений заметно дольше остального. Изображения из seed — seedImages(ctx, s.media).
func migratedSite(t *testing.T, withSeed bool) testSite {
	t.Helper()
	ctx := context.Background()
	cfg := testConfig(t)
	if err := migrate(ctx, cfg); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := openDB(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if withSeed {
		if err := seedContent(ctx, db, nil); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	media, closeMedia, err := openMedia(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeMedia)
	return testSite{cfg: cfg, db: db, media: media}
}

// migratedSiteWithImages — сайт после migrate и полного seed: изображения из seed и страницы, которые на них
// ссылаются. Возвращает и id изображений по имени файла.
func migratedSiteWithImages(t *testing.T) (testSite, map[string]string) {
	t.Helper()
	s := migratedSite(t, false)
	ctx := context.Background()
	imgs, err := seedImages(ctx, s.media)
	if err != nil {
		t.Fatalf("seed images: %v", err)
	}
	if err := seedContent(ctx, s.db, imgs); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return s, imgs
}

// handler — все маршруты сайта (newMux) с настройками cfg; без BaseURL — testBaseURL.
func (s testSite) handler(cfg config) http.Handler {
	if cfg.BaseURL == "" {
		cfg.BaseURL = testBaseURL
	}
	return newMux(s.db, s.media, cfg)
}

func get(t *testing.T, s testSite, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handler(config{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestServeRefusesWithPendingMigrations(t *testing.T) {
	err := serve(context.Background(), testConfig(t))
	if !errors.Is(err, errPendingMigrations) {
		t.Fatalf("serve error = %v, want errPendingMigrations", err)
	}
}

func TestServeRefusesWithoutBaseURL(t *testing.T) {
	cfg := testConfig(t)
	cfg.BaseURL = ""
	if err := serve(context.Background(), cfg); !errors.Is(err, errNoBaseURL) {
		t.Fatalf("serve error = %v, want errNoBaseURL", err)
	}
}

func TestParseBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com":       "https://example.com",
		"https://example.com/":      "https://example.com",
		"http://localhost:9090":     "http://localhost:9090",
		"https://Example.com:8443/": "https://Example.com:8443",
	} {
		if got, err := parseBaseURL(in); err != nil || got != want {
			t.Errorf("parseBaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"example.com", "//example.com", "ftp://example.com", "https://", "https://example.com/blog",
		"https://example.com/?a=1", "https://example.com/#x", "https://user:pass@example.com", "https://exa mple.com",
	} {
		if got, err := parseBaseURL(in); err == nil {
			t.Errorf("parseBaseURL(%q) = %q, want an error", in, got)
		}
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
	if err := db.QueryRow(`SELECT count(*) FROM pages`).Scan(&n); err != nil || n != len(seedPages(nil)) {
		t.Errorf("pages rows = %d, %v; want %d", n, err, len(seedPages(nil)))
	}
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil || n != 1 {
		t.Errorf("users rows = %d, %v; want 1", n, err)
	}
	media, closeMedia, err := openMedia(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeMedia()
	imgs, err := media.List(ctx, 100, 0)
	if want := countSeedImages(t); err != nil || len(imgs) != want {
		t.Errorf("images = %d, %v; want %d", len(imgs), err, want)
	}
}

func countSeedImages(t *testing.T) int {
	t.Helper()
	entries, err := seedImageFiles.ReadDir("seeddata")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestHealthz(t *testing.T) {
	rec := get(t, migratedSite(t, false), "/healthz")
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
	if s := got.Schema["site"]; s.Version != 12 || s.Pending != 0 {
		t.Errorf("schema site = %+v, want version 12, pending 0", s)
	}
	if s := got.Schema["images"]; s.Version != 1 || s.Pending != 0 {
		t.Errorf("schema images = %+v, want version 1, pending 0", s)
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
	if !strings.HasPrefix(hash, "$argon2id$") || role != "admin" { // пароль сверяют тесты модуля auth
		t.Errorf("stored user: hash %q, role %q; want argon2id, admin", hash, role)
	}

	tests := []struct {
		name, email string
		answers     []string
		wantErr     error
	}{
		{"duplicate", "editor@example.com", []string{"long enough", "long enough"}, auth.ErrUserExists},
		{"bad email", "not-an-email", nil, nil},
		{"named email", "Bob <bob@example.com>", nil, nil},
		{"short password", "a@example.com", []string{"short"}, auth.ErrPasswordLength},
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

package main

// Миграции, меняющие живые данные, — тест на копии реалистичных данных (AGENTS.md, «Миграции»):
// БД доводится до версии перед миграцией, заполняется данными в прежней схеме, затем миграция применяется.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/database"
)

// migrateTo применяет миграции модулей полностью, а сайта — до версии site.
func migrateTo(t *testing.T, cfg config, site int64) {
	t.Helper()
	ctx := context.Background()
	db, err := openDB(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, src := range migrationSources {
		p, err := goose.NewProvider(goose.DialectSQLite3, db, src.FS,
			goose.WithTableName(src.Table), goose.WithDisableGlobalRegistry(true))
		if err != nil {
			t.Fatal(err)
		}
		if src.Name == "site" {
			_, err = p.UpTo(ctx, site)
		} else {
			_, err = p.Up(ctx)
		}
		if err != nil {
			t.Fatalf("%s: %v", src.Name, err)
		}
	}
}

// legacyPage — страница в схеме до 00005 (pages.title/slug/body/draft).
type legacyPage struct {
	title, slug, body string
	draft             bool
}

// legacyPages — то, что лежит в БД живого сайта до этапа 8.5: seed этапов 3–5 (Markdown с таблицей и кодом,
// HTML для санитайзера), черновик, пустой текст, символы, которые надо экранировать в JSON, CRLF (если его
// записали в обход админки), длинный текст.
var legacyPages = []legacyPage{
	{"Добро пожаловать", "home", "Это эталонный сайт **Keelkit**.\n\n- [О проекте](/about)\n- [Контакты](/contacts)\n", false},
	{"О проекте", "about", "## Из чего состоит\n\n| Часть | Где |\n|---|---|\n| Модули | `modules/` |\n\n```go\nfunc main() {\n\tfmt.Println(\"привет\")\n}\n```\n\n> Писать код дёшево.\n", false},
	{"Контакты", "contacts", "Почта: hello@example.com.\n\n<script>alert(1)</script>\n\n[Опасная ссылка](javascript:alert(3))\n", false},
	{"Черновик", "chernovik", "Ещё **не опубликовано**.", true},
	{"Пустая", "pustaya", "", false},
	{"Кавычки \"и\" слеши", "escapes", "Кавычки \"двойные\", обратный слеш \\, юникод   и эмодзи 🚢, табуляция\tи {\"json\": [1]}", false},
	{"CRLF", "crlf", "строка 1\r\nстрока 2", true},
	{"Длинная", "long", strings.Repeat("Абзац длинного текста. ", 20000), false},
}

func TestMigrationPageContents(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	migrateTo(t, cfg, 4)

	db, err := openDB(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	type stamps struct{ created, updated string }
	before := map[string]stamps{}
	for i, p := range legacyPages {
		// Разное время создания и обновления — должно сохраниться как есть.
		created := "2025-01-0" + string(rune('1'+i)) + "T10:00:00Z"
		updated := "2026-02-0" + string(rune('1'+i)) + "T12:30:00Z"
		draft := 0
		if p.draft {
			draft = 1
		}
		_, err := db.ExecContext(ctx, `INSERT INTO pages (title, slug, body, draft, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			p.title, p.slug, p.body, draft, created, updated)
		if err != nil {
			t.Fatal(err)
		}
		before[p.slug] = stamps{created, updated}
	}

	// Миграция — штатной командой: снимок БД, затем всё непримененное.
	if err := migrate(ctx, cfg); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if snaps, _ := filepath.Glob(filepath.Join(cfg.BackupDir, "*.db")); len(snaps) != 1 {
		t.Errorf("snapshots = %d, want 1: migrate snapshots the data before 00005", len(snaps))
	}

	for _, p := range legacyPages {
		var id int64
		var s stamps
		err := db.QueryRowContext(ctx, `SELECT p.id, p.created_at, p.updated_at FROM pages p
			JOIN page_contents c ON c.page_id = p.id AND c.kind = 'draft' WHERE c.slug = ?`, p.slug).Scan(&id, &s.created, &s.updated)
		if err != nil {
			t.Fatalf("%s: no draft: %v", p.slug, err)
		}
		if s != before[p.slug] {
			t.Errorf("%s: timestamps %+v, want %+v", p.slug, s, before[p.slug])
		}
		var legacyID int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM pages_legacy WHERE slug = ?`, p.slug).Scan(&legacyID); err != nil || legacyID != id {
			t.Errorf("%s: id %d, legacy id %d (%v): ids must be kept", p.slug, id, legacyID, err)
		}

		var title, data string
		if err := db.QueryRowContext(ctx, `SELECT title, blocks FROM page_contents WHERE page_id = ? AND kind = 'draft'`, id).Scan(&title, &data); err != nil {
			t.Fatal(err)
		}
		list, err := blocks.Unmarshal(data)
		if err != nil {
			t.Fatalf("%s: %v", p.slug, err)
		}
		if title != p.title {
			t.Errorf("%s: title %q", p.slug, title)
		}
		switch {
		case p.body == "":
			if len(list) != 0 {
				t.Errorf("%s: empty body must give no blocks, got %d", p.slug, len(list))
			}
		case len(list) != 1 || list[0].Type != "text":
			t.Errorf("%s: want one text block, got %+v", p.slug, list)
		default:
			if got := list[0].Content.(*blocks.Text).Body; got != p.body {
				t.Errorf("%s: body changed:\n got %.200q\nwant %.200q", p.slug, got, p.body)
			}
			// id блока из миграции читается как есть (иначе Unmarshal выдал бы новый при каждом чтении).
			again, _ := blocks.Unmarshal(data)
			if again[0].ID != list[0].ID {
				t.Errorf("%s: migrated block id %q is not stable", p.slug, list[0].ID)
			}
		}

		var pubData string
		err = db.QueryRowContext(ctx, `SELECT blocks FROM page_contents WHERE page_id = ? AND kind = 'published'`, id).Scan(&pubData)
		switch {
		case p.draft && err == nil:
			t.Errorf("%s: draft got a published copy", p.slug)
		case !p.draft && err != nil:
			t.Errorf("%s: published page has no published copy: %v", p.slug, err)
		case !p.draft && pubData != data:
			t.Errorf("%s: published copy differs from the draft", p.slug)
		}
	}

	// Старые данные целы до следующей версии (двухэтапное удаление).
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pages_legacy`).Scan(&n); err != nil || n != len(legacyPages) {
		t.Errorf("pages_legacy rows = %d, %v; want %d", n, err, len(legacyPages))
	}
	schemas, err := database.Status(ctx, db, migrationSources)
	if err != nil || database.Pending(schemas) != 0 {
		t.Fatalf("pending after migrate: %+v, %v", schemas, err)
	}

	// Сайт после миграции: опубликованные страницы видны как раньше, черновики — нет; админка их открывает.
	media, closeMedia, err := openMedia(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeMedia()
	srv := httptest.NewServer(newMux(db, media, config{}))
	defer srv.Close()
	if got := text(page(t, srv, "/about", http.StatusOK).Find("main article pre code.language-go")); !strings.Contains(got, "привет") {
		t.Errorf("about: code block = %q", got)
	}
	if body := text(page(t, srv, "/contacts", http.StatusOK).Find("main article")); strings.Contains(body, "alert(1)") {
		t.Error("contacts: sanitizer bypassed after migration")
	}
	if got := text(page(t, srv, "/escapes", http.StatusOK).Find("main article")); !strings.Contains(got, `{"json": [1]}`) || !strings.Contains(got, "🚢") {
		t.Errorf("escapes: %q", got)
	}
	page(t, srv, "/pustaya", http.StatusOK)
	page(t, srv, "/chernovik", http.StatusNotFound)
	page(t, srv, "/crlf", http.StatusNotFound)

	client := adminClient(t)
	if err := seedContent(ctx, db, nil); err != nil { // dev-пользователь; страницы с теми же slug уже есть
		t.Fatal(err)
	}
	loginOK(t, client, srv)
	_, html := fetch(t, client, srv.URL+"/admin/pages")
	doc := parse(t, html)
	if rows := doc.Find(".admin-table tbody tr").Length(); rows != len(legacyPages) {
		t.Errorf("admin list rows = %d, want %d", rows, len(legacyPages))
	}
	if got := text(doc.Find(`.admin-table tr:has(a:contains("Черновик")) .status`)); got != "Черновик" {
		t.Errorf("draft status = %q", got)
	}
	if got := text(doc.Find(`.admin-table tr:has(a:contains("О проекте")) .status`)); got != "Опубликована" {
		t.Errorf("published status = %q", got)
	}
}

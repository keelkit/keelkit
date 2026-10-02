package main

// e2e раздела «Страницы» в админке: создание, редактирование, удаление, ошибки валидации, превью Markdown.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// htmx — заголовки запроса, который отправляет htmx.
var htmx = http.Header{"Hx-Request": {"true"}}

// pageAdmin — сайт с seed и вошедший «браузер» с CSRF-токеном сессии.
type pageAdmin struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
	token  string
}

func newPageAdmin(t *testing.T) *pageAdmin {
	t.Helper()
	srv := e2eServer(t, true)
	client := adminClient(t)
	return &pageAdmin{t: t, srv: srv, client: client, token: loginOK(t, client, srv)}
}

// save отправляет форму страницы (path — /admin/pages или /admin/pages/{id}).
func (a *pageAdmin) save(path, title, slug, body string, header http.Header) (response, string) {
	a.t.Helper()
	return post(a.t, a.client, a.srv.URL+path, url.Values{
		"csrf_token": {a.token}, "title": {title}, "slug": {slug}, "body": {body},
	}, header)
}

func (a *pageAdmin) get(path string) (response, string) {
	a.t.Helper()
	return fetch(a.t, a.client, a.srv.URL+path)
}

var editURL = regexp.MustCompile(`^/admin/pages/(\d+)\?saved=1$`)

func TestE2EAdminPageCreate(t *testing.T) {
	a := newPageAdmin(t)

	// Раздел в меню и ссылка со счётчика дашборда.
	_, body := a.get("/admin")
	doc := parse(t, body)
	if href, _ := doc.Find(".counter a").Attr("href"); href != "/admin/pages" {
		t.Errorf("counter link = %q", href)
	}
	resp, body := a.get("/admin/pages")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/pages: status %d", resp.StatusCode)
	}
	doc = parse(t, body)
	if cur := text(doc.Find(`.site-nav a[aria-current="page"]`)); cur != "Страницы" {
		t.Errorf("current menu item = %q", cur)
	}
	if n := doc.Find(".admin-table tbody tr").Length(); n != 3 {
		t.Errorf("list rows = %d, want 3", n)
	}

	resp, body = a.get("/admin/pages/new")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/pages/new: status %d", resp.StatusCode)
	}
	doc = parse(t, body)
	if action, _ := doc.Find("#page-form").Attr("hx-post"); action != "/admin/pages" {
		t.Errorf("form hx-post = %q", action)
	}
	if got := text(doc.Find("#page-preview")); got != "Здесь появится превью текста." {
		t.Errorf("empty preview = %q", got)
	}

	// Пустой slug составляется из заголовка; CRLF из textarea хранится как LF.
	resp, _ = a.save("/admin/pages", "Тестовая", "", "Текст: **жирный**\r\n\r\nВторой абзац.", nil)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !editURL.MatchString(loc) {
		t.Fatalf("create: status %d, Location %q; want 303 to /admin/pages/{id}?saved=1", resp.StatusCode, loc)
	}

	resp, body = a.get(loc)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", loc, resp.StatusCode)
	}
	doc = parse(t, body)
	if got := text(doc.Find(".flash")); got != "Сохранено." {
		t.Errorf("flash = %q", got)
	}
	if v, _ := doc.Find(`input[name="slug"]`).Attr("value"); v != "testovaya" {
		t.Errorf("slug = %q, want testovaya", v)
	}
	if got := doc.Find(`textarea[name="body"]`).Text(); got != "Текст: **жирный**\n\nВторой абзац." {
		t.Errorf("body = %q", got)
	}
	if got := text(doc.Find("#page-preview strong")); got != "жирный" {
		t.Errorf("preview strong = %q", got)
	}
	if href, _ := doc.Find(`a:contains("Открыть на сайте")`).Attr("href"); href != "/testovaya" {
		t.Errorf("public link = %q", href)
	}

	// Страница сразу видна на сайте и в меню.
	pub := page(t, a.srv, "/testovaya", http.StatusOK)
	if got := text(pub.Find("main h1")); got != "Тестовая" {
		t.Errorf("public h1 = %q", got)
	}
	if got := text(pub.Find("main article strong")); got != "жирный" {
		t.Errorf("public strong = %q", got)
	}
	if got := text(pub.Find(`.site-nav a[href="/testovaya"]`)); got != "Тестовая" {
		t.Errorf("nav item = %q", got)
	}

	_, body = a.get("/admin")
	if got := text(parse(t, body).Find(".counter")); got != "Страниц: 4" {
		t.Errorf("counter = %q, want «Страниц: 4»", got)
	}
}

func TestE2EAdminPageValidation(t *testing.T) {
	a := newPageAdmin(t)

	for _, tc := range []struct {
		name, title, slug string
		field, err        string
	}{
		{"duplicate slug", "Ещё о проекте", "about", "slug", "Страница с адресом «about» уже есть: выберите другой slug."},
		{"duplicate generated slug", "О проекте", "", "slug", "Страница с адресом «o-proekte» уже есть: выберите другой slug."},
		{"reserved slug", "Админ", "admin", "slug", "Адрес «/admin» занят разделом сайта: выберите другой slug."},
		{"reserved after normalize", "Статика", " Static ", "slug", "Адрес «/static» занят разделом сайта: выберите другой slug."},
		{"slug without letters", "Знаки", "!!!", "slug", "В slug нужны буквы или цифры."},
		{"title without letters", "???", "", "slug", "Из заголовка не получился адрес: укажите slug латиницей."},
		{"empty title", "  ", "x", "title", "Укажите заголовок."},
		{"long title", strings.Repeat("я", 201), "x", "title", "Заголовок длиннее 200 символов."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "duplicate generated slug" {
				// Сначала создаём страницу со slug o-proekte, затем ещё одну с тем же заголовком.
				if resp, _ := a.save("/admin/pages", tc.title, "", "", nil); resp.StatusCode != http.StatusSeeOther {
					t.Fatalf("first create: status %d", resp.StatusCode)
				}
			}
			body := "Текст **сохраняется**"

			// Обычная форма: 422 и страница целиком, введённые данные на месте.
			resp, html := a.save("/admin/pages", tc.title, tc.slug, body, nil)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422", resp.StatusCode)
			}
			doc := parse(t, html)
			if doc.Find("html head title").Length() != 1 || doc.Find(".site-header").Length() != 1 {
				t.Error("full page expected without HX-Request")
			}
			if got := text(doc.Find("#" + tc.field + "-error")); got != tc.err {
				t.Errorf("%s error = %q, want %q", tc.field, got, tc.err)
			}
			if v, _ := doc.Find(`input[name="` + tc.field + `"]`).Attr("aria-invalid"); v != "true" {
				t.Errorf("%s: aria-invalid = %q", tc.field, v)
			}
			if got := doc.Find(`textarea[name="body"]`).Text(); got != body {
				t.Errorf("body not kept: %q", got)
			}
			if v, _ := doc.Find(`input[name="title"]`).Attr("value"); v != strings.TrimSpace(tc.title) {
				t.Errorf("title not kept: %q", v)
			}
			if got := text(doc.Find("#page-preview strong")); got != "сохраняется" {
				t.Errorf("preview strong = %q", got)
			}

			// htmx: 422 и только фрагмент формы — его подставит hx-swap.
			resp, html = a.save("/admin/pages", tc.title, tc.slug, body, htmx)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("htmx: status %d, want 422", resp.StatusCode)
			}
			if !strings.HasPrefix(strings.TrimSpace(html), `<form id="page-form"`) || strings.Contains(html, "<html") {
				t.Errorf("htmx: want only the form fragment, got %.120q", html)
			}
			if got := text(parse(t, html).Find("#" + tc.field + "-error")); got != tc.err {
				t.Errorf("htmx: %s error = %q", tc.field, got)
			}
		})
	}

	// Ни одна ошибочная страница не сохранилась: 3 из seed и одна «О проекте» (o-proekte).
	_, html := a.get("/admin/pages")
	if n := parse(t, html).Find(".admin-table tbody tr").Length(); n != 4 {
		t.Errorf("list rows = %d, want 4", n)
	}
}

func TestE2EAdminPagePreview(t *testing.T) {
	a := newPageAdmin(t)
	src := "# Заголовок\n\n**жирный** и [ссылка](javascript:alert(1))\n\n<script>alert(2)</script>"

	resp, html := post(t, a.client, a.srv.URL+"/admin/pages/preview", url.Values{"body": {src}},
		http.Header{"Hx-Request": {"true"}, "X-Csrf-Token": {a.token}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview: status %d", resp.StatusCode)
	}
	if strings.Contains(html, "<html") || strings.Contains(html, "<script") || strings.Contains(html, "javascript:") {
		t.Errorf("preview must be a sanitized fragment: %q", html)
	}
	doc := parse(t, html)
	if got := text(doc.Find("h1")); got != "Заголовок" {
		t.Errorf("h1 = %q", got)
	}
	if got := text(doc.Find("strong")); got != "жирный" {
		t.Errorf("strong = %q", got)
	}

	// Пустой текст — подсказка.
	_, html = post(t, a.client, a.srv.URL+"/admin/pages/preview", url.Values{"body": {""}},
		http.Header{"Hx-Request": {"true"}, "X-Csrf-Token": {a.token}})
	if got := text(parse(t, html).Find(".preview-empty")); got == "" {
		t.Errorf("empty preview: %q", html)
	}

	// Без CSRF-токена — 403.
	resp, _ = post(t, a.client, a.srv.URL+"/admin/pages/preview", url.Values{"body": {src}}, htmx)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("preview without token: status %d, want 403", resp.StatusCode)
	}
}

func TestE2EAdminPageEditDelete(t *testing.T) {
	a := newPageAdmin(t)

	// Найти «О проекте» в списке.
	_, html := a.get("/admin/pages")
	edit, ok := parse(t, html).Find(`.admin-table a:contains("О проекте")`).Attr("href")
	if !ok {
		t.Fatal("no edit link for «О проекте»")
	}

	// Сохранение через htmx: 204 и HX-Redirect на форму.
	resp, _ := a.save(edit, "О нас", "about-us", "Новый текст.", htmx)
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Hx-Redirect") != edit+"?saved=1" {
		t.Fatalf("htmx update: status %d, HX-Redirect %q", resp.StatusCode, resp.Header.Get("Hx-Redirect"))
	}
	page(t, a.srv, "/about", http.StatusNotFound)
	doc := page(t, a.srv, "/about-us", http.StatusOK)
	if got := text(doc.Find("main h1")); got != "О нас" {
		t.Errorf("h1 = %q", got)
	}

	// Свой slug при сохранении не считается занятым.
	if resp, _ := a.save(edit, "О нас", "about-us", "Ещё раз.", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("update with same slug: status %d, want 303", resp.StatusCode)
	}
	// Чужой — занят.
	if resp, _ := a.save(edit, "О нас", "contacts", "", nil); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("update with taken slug: status %d, want 422", resp.StatusCode)
	}

	// Удаление: подтверждение, затем POST.
	resp, html = a.get(edit + "/delete")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET delete: status %d", resp.StatusCode)
	}
	if got := text(parse(t, html).Find("main h1")); got != "Удалить страницу «О нас»?" {
		t.Errorf("confirm h1 = %q", got)
	}
	// Без CSRF-токена не удаляется.
	if resp, _ := post(t, a.client, a.srv.URL+edit+"/delete", nil, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("delete without token: status %d, want 403", resp.StatusCode)
	}
	resp, _ = post(t, a.client, a.srv.URL+edit+"/delete", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/pages?deleted=1" {
		t.Fatalf("delete: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	page(t, a.srv, "/about-us", http.StatusNotFound)
	_, html = a.get("/admin/pages?deleted=1")
	if got := text(parse(t, html).Find(".flash")); got != "Страница удалена." {
		t.Errorf("flash = %q", got)
	}

	// Удалённой страницы больше нет ни для формы, ни для повторного удаления.
	for _, path := range []string{edit, edit + "/delete", "/admin/pages/999999", "/admin/pages/abc", "/admin/pages/0"} {
		if resp, _ := a.get(path); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, resp.StatusCode)
		}
	}
	if resp, _ := post(t, a.client, a.srv.URL+edit+"/delete", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("second delete: status %d, want 404", resp.StatusCode)
	}
	if resp, _ := a.save(edit, "X", "x", "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("update deleted: status %d, want 404", resp.StatusCode)
	}
}

// Маршруты из reservedSlugs перекрывают страницу с тем же slug: даже если она попала в БД в обход админки,
// по её адресу открывается раздел сайта, а не страница.
func TestE2EReservedSlugsShadowPages(t *testing.T) {
	db := migratedDB(t, false)
	srv := httptest.NewServer(newMux(db, config{}))
	t.Cleanup(srv.Close)

	for _, s := range reservedSlugs {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO pages (title, slug, body) VALUES (?, ?, '')`, "Страница-"+s, s); err != nil {
			t.Fatal(err)
		}
		resp, body := fetch(t, noRedirect, srv.URL+"/"+s)
		// Название страницы есть в меню любой HTML-страницы, поэтому смотрим на заголовок содержимого.
		if strings.Contains(body, "<html") && text(parse(t, body).Find("main h1")) == "Страница-"+s {
			t.Errorf("GET /%s: served the page (status %d), want the site route", s, resp.StatusCode)
		}
	}
}

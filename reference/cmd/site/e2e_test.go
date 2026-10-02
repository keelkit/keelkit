package main

// e2e-тесты ключевых сценариев: настоящий HTTP-сервер (httptest) над БД после migrate и seed,
// разбор HTML через goquery. Имена — TestE2E*: их запускает make e2e (и make test).

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// e2eServer — сайт на случайном порту; withSeed — с dev-данными.
func e2eServer(t *testing.T, withSeed bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newMux(migratedDB(t, withSeed), config{}))
	t.Cleanup(srv.Close)
	return srv
}

// noRedirect — клиент, который возвращает редирект как есть.
var noRedirect = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// response — ответ с уже прочитанным и закрытым телом.
type response struct {
	StatusCode int
	Header     http.Header
}

// Cookies — cookie из заголовков Set-Cookie ответа.
func (r response) Cookies() []*http.Cookie {
	return (&http.Response{Header: r.Header}).Cookies()
}

// fetch выполняет GET и возвращает статус, заголовки и тело.
func fetch(t *testing.T, client *http.Client, url string) (response, string) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return response{StatusCode: resp.StatusCode, Header: resp.Header}, string(body)
}

// page выполняет GET, проверяет статус и HTML и возвращает документ.
func page(t *testing.T, srv *httptest.Server, path string, wantStatus int) *goquery.Document {
	t.Helper()
	resp, body := fetch(t, srv.Client(), srv.URL+path)
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s: status %d, want %d", path, resp.StatusCode, wantStatus)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("GET %s: Content-Type %q", path, ct)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

func text(s *goquery.Selection) string { return strings.TrimSpace(s.Text()) }

func TestE2EHome(t *testing.T) {
	srv := e2eServer(t, true)
	doc := page(t, srv, "/", http.StatusOK)

	if got, want := text(doc.Find("title")), "Keelkit — эталонный сайт"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got := text(doc.Find(".site-name")); got != "Keelkit — эталонный сайт" {
		t.Errorf("site name = %q", got)
	}
	if got := text(doc.Find("main h1")); got != "Добро пожаловать" {
		t.Errorf("h1 = %q", got)
	}
	// Меню — все страницы, кроме главной, в порядке создания.
	var nav []string
	doc.Find(".site-nav a").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		nav = append(nav, text(a)+" "+href)
	})
	if got, want := strings.Join(nav, ", "), "О проекте /about, Контакты /contacts"; got != want {
		t.Errorf("nav = %q, want %q", got, want)
	}

	// Стили, скрипт и иконка из layout отдаются из embed.
	for _, sel := range []string{`link[rel="stylesheet"]`, `link[rel="icon"]`, `script[src]`} {
		attr := "href"
		if sel == `script[src]` {
			attr = "src"
		}
		src, ok := doc.Find(sel).Attr(attr)
		if !ok {
			t.Errorf("%s: no %s", sel, attr)
			continue
		}
		if resp, _ := fetch(t, srv.Client(), srv.URL+src); resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status %d", src, resp.StatusCode)
		}
	}
}

func TestE2EHomeWithoutContent(t *testing.T) {
	doc := page(t, e2eServer(t, false), "/", http.StatusOK)
	if got := text(doc.Find("main h1")); got != defaultSiteName {
		t.Errorf("h1 = %q, want %q", got, defaultSiteName)
	}
	if doc.Find(".site-nav").Length() != 0 {
		t.Error("nav is shown without pages")
	}
}

func TestE2EPageBySlug(t *testing.T) {
	doc := page(t, e2eServer(t, true), "/about", http.StatusOK)

	if got, want := text(doc.Find("title")), "О проекте — Keelkit — эталонный сайт"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got := text(doc.Find("main h1")); got != "О проекте" {
		t.Errorf("h1 = %q", got)
	}
	if cur, _ := doc.Find(`.site-nav a[aria-current="page"]`).Attr("href"); cur != "/about" {
		t.Errorf("current nav item = %q, want /about", cur)
	}

	// Markdown: таблица, блок кода с языком, ссылки.
	article := doc.Find("main article")
	if n := article.Find("table tbody tr").Length(); n != 3 {
		t.Errorf("table rows = %d, want 3", n)
	}
	if article.Find("pre code.language-go").Length() != 1 {
		t.Error("no Go code block")
	}
	if href, _ := article.Find(`a[href="/contacts"]`).Attr("href"); href == "" {
		t.Error("no internal link to /contacts")
	}
	if rel, _ := article.Find(`a[href="https://go.dev/doc/"]`).Attr("rel"); rel != "nofollow" {
		t.Errorf("external link rel = %q, want nofollow", rel)
	}
}

func TestE2ESanitizer(t *testing.T) {
	srv := e2eServer(t, true)
	resp, body := fetch(t, srv.Client(), srv.URL+"/contacts")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for _, bad := range []string{"<script>alert", "onerror", "javascript:alert"} {
		if strings.Contains(body, bad) {
			t.Errorf("body contains %q", bad)
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	// Единственный скрипт на странице — htmx из layout.
	if n := doc.Find("script").Length(); n != 1 {
		t.Errorf("script elements = %d, want 1", n)
	}
	if n := doc.Find("main script").Length(); n != 0 {
		t.Errorf("script elements in main = %d, want 0", n)
	}
}

func TestE2ENotFound(t *testing.T) {
	srv := e2eServer(t, true)
	for _, path := range []string{"/nope", "/about/extra", "/nope/"} {
		doc := page(t, srv, path, http.StatusNotFound)
		if got := text(doc.Find("main h1")); got != "Страница не найдена" {
			t.Errorf("GET %s: h1 = %q", path, got)
		}
		if got := text(doc.Find(".site-name")); got != "Keelkit — эталонный сайт" {
			t.Errorf("GET %s: 404 page without layout, site name = %q", path, got)
		}
	}
}

// Статика: простой 404 без layout, список файлов каталога не отдаётся.
func TestE2EStaticNotFound(t *testing.T) {
	srv := e2eServer(t, false)
	for _, path := range []string{"/static/", "/static/nope.css"} {
		if resp, _ := fetch(t, srv.Client(), srv.URL+path); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestE2EHomeSlugRedirects(t *testing.T) {
	srv := e2eServer(t, true)
	resp, _ := fetch(t, noRedirect, srv.URL+"/home")
	if resp.StatusCode != http.StatusMovedPermanently || resp.Header.Get("Location") != "/" {
		t.Errorf("GET /home: status %d, Location %q; want 301 to /", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestE2EStatic(t *testing.T) {
	srv := e2eServer(t, false)
	tests := []struct {
		path, contentType, cacheControl string
	}{
		{"/static/htmx-2.0.11.min.js", "text/javascript; charset=utf-8", "no-cache"},
		{"/static/style.css?v=1", "text/css; charset=utf-8", "public, max-age=31536000, immutable"},
	}
	for _, tt := range tests {
		resp, body := fetch(t, srv.Client(), srv.URL+tt.path)
		if resp.StatusCode != http.StatusOK || body == "" {
			t.Errorf("GET %s: status %d, %d bytes", tt.path, resp.StatusCode, len(body))
			continue
		}
		if ct := resp.Header.Get("Content-Type"); ct != tt.contentType {
			t.Errorf("GET %s: Content-Type %q, want %q", tt.path, ct, tt.contentType)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != tt.cacheControl {
			t.Errorf("GET %s: Cache-Control %q, want %q", tt.path, cc, tt.cacheControl)
		}
	}
}

package main

// e2e живого предпросмотра в редакторе (admin.Live, POST /admin/<type>/live): разметка панели в форме,
// страница сайта из несохранённой формы, ничего не пишется в БД, CSRF.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// live отправляет форму на адрес живого предпросмотра так, как это делает static/live-preview.js:
// обычный POST без HX-Request, CSRF-токен — полем формы.
func (a *pageAdmin) live(path string, form url.Values) (response, string) {
	a.t.Helper()
	return a.submit(path, form, "", nil)
}

// assertLiveShell — у формы редактора есть панель предпросмотра: адрес, id формы внутри, скрытая до скрипта панель.
func assertLiveShell(t *testing.T, html, liveURL, formID string) {
	t.Helper()
	doc := parse(t, html)
	live := doc.Find(".live")
	if got, _ := live.Attr("data-live-url"); got != liveURL {
		t.Errorf("data-live-url = %q, want %q", got, liveURL)
	}
	if got, _ := live.Attr("data-live-form"); got != formID {
		t.Errorf("data-live-form = %q, want %q", got, formID)
	}
	if live.Find(".live-editor #"+formID).Length() != 1 {
		t.Errorf("form #%s is not inside .live-editor", formID)
	}
	if _, hidden := live.Find(".live-pane").Attr("hidden"); !hidden {
		t.Error(".live-pane must be hidden until live-preview.js enables it")
	}
	if live.Find(".live-pane iframe.live-frame").Length() != 1 {
		t.Error("no iframe in .live-pane")
	}
	// Кнопки ширины — на панели и над формой (вкладки на узком экране); у каждой — режим полей картинок.
	for _, group := range []string{".live-pane", ".live-tabs"} {
		if got := live.Find(group + " [data-live-width][data-live-mode]").Length(); got != 2 {
			t.Errorf("%s: %d device width buttons, want 2", group, got)
		}
	}
}

// assertLivePage — ответ предпросмотра: страница сайта целиком, не для поисковиков, без плашки черновика.
func assertLivePage(t *testing.T, resp response, html string) {
	t.Helper()
	if resp.StatusCode != http.StatusOK || !strings.Contains(html, "<html") {
		t.Fatalf("live: status %d, want a full page\n%.300s", resp.StatusCode, html)
	}
	doc := parse(t, html)
	if robots, _ := doc.Find(`meta[name="robots"]`).Attr("content"); robots != "noindex" {
		t.Errorf("robots = %q, want noindex", robots)
	}
	if doc.Find(`link[rel="canonical"]`).Length() != 0 {
		t.Error("live preview must have no canonical")
	}
	if doc.Find(".draft-banner").Length() != 0 {
		t.Error("live preview must have no draft banner: the editor is next to it")
	}
}

func TestE2EAdminPageLive(t *testing.T) {
	a := newPageAdmin(t)
	edit := a.editLink("О проекте")
	_, html := a.get(edit)
	assertLiveShell(t, html, "/admin/pages/live", "page-form")

	// Несохранённые правки: новый заголовок и цитата в конце — видны в предпросмотре с якорем блока.
	form := a.form(edit)
	form.Set("title", "О проекте — правка")
	addBlock(form, "livequote", "quote", map[string]string{"text": "Цитата из предпросмотра", "author": "Автор"})
	resp, html := a.live("/admin/pages/live", form)
	assertLivePage(t, resp, html)
	doc := parse(t, html)
	if got := text(doc.Find("article h1")); got != "О проекте — правка" {
		t.Errorf("h1 = %q", got)
	}
	if got := text(doc.Find("#block-livequote blockquote")); got != "Цитата из предпросмотра" {
		t.Errorf("quote = %q", got)
	}
	if n := doc.Find(".block[id^='block-']").Length(); n < 2 {
		t.Errorf("blocks with anchors = %d", n)
	}

	// Ничего не сохранено: черновик и сайт прежние, статус не изменился.
	if got := a.form(edit).Get("title"); got != "О проекте" {
		t.Errorf("draft title after live = %q", got)
	}
	if got := text(page(t, a.srv, "/about", http.StatusOK).Find("article h1")); got != "О проекте" {
		t.Errorf("public h1 after live = %q", got)
	}
	if got := a.listStatus("О проекте"); got != "Опубликована" {
		t.Errorf("status after live = %q", got)
	}

	// Новая страница: предпросмотр без id, страница не создаётся.
	_, html = a.get("/admin/pages/new")
	assertLiveShell(t, html, "/admin/pages/live", "page-form")
	var before int
	if err := a.site.db.QueryRow(`SELECT count(*) FROM pages`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	form = a.form("/admin/pages/new")
	form.Set("title", "Ещё не создана")
	resp, html = a.live("/admin/pages/live", form)
	assertLivePage(t, resp, html)
	if got := text(parse(t, html).Find("article h1")); got != "Ещё не создана" {
		t.Errorf("new page h1 = %q", got)
	}
	var after int
	if err := a.site.db.QueryRow(`SELECT count(*) FROM pages`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("pages: %d → %d after live", before, after)
	}

	// Без CSRF-токена — 403.
	resp, _ = post(t, a.client, a.srv.URL+"/admin/pages/live", url.Values{"title": {"x"}}, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("live without token: status %d, want 403", resp.StatusCode)
	}
}

func TestE2EAdminPostLive(t *testing.T) {
	a := newPageAdmin(t)
	_, html := a.get("/admin/posts/new")
	assertLiveShell(t, html, "/admin/posts/live", "post-form")

	// Лид, категория и дата из формы — как на сайте; пост не создаётся.
	form := newPostForm("Пост в предпросмотре", "", a.categoryID("web"), "2026-03-09T10:00")
	form.Set("lead", "Лид поста.")
	resp, html := a.live("/admin/posts/live", form)
	assertLivePage(t, resp, html)
	doc := parse(t, html)
	if got := text(doc.Find("article h1")); got != "Пост в предпросмотре" {
		t.Errorf("h1 = %q", got)
	}
	if got := text(doc.Find(".post-lead")); got != "Лид поста." {
		t.Errorf("lead = %q", got)
	}
	if got := text(doc.Find(".post-meta a")); got != "Веб" {
		t.Errorf("category = %q", got)
	}
	if got := text(doc.Find(".post-meta time")); got != "9 марта 2026" {
		t.Errorf("date = %q", got)
	}
	if got := text(doc.Find("#block-intro strong")); got != "Пост в\u00a0предпросмотре" { // типографика: неразрывный пробел после «в»
		t.Errorf("text block = %q", got)
	}
	var n int
	if err := a.site.db.QueryRow(`SELECT count(*) FROM post_contents WHERE title = ?`, "Пост в предпросмотре").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("live created %d post rows", n)
	}

	// Дата ещё не дописана — пост без даты, не ошибка.
	form.Set("publish_date", "2026-03")
	resp, html = a.live("/admin/posts/live", form)
	assertLivePage(t, resp, html)
	if parse(t, html).Find(".post-meta time").Length() != 0 {
		t.Error("unfinished date is shown")
	}
}

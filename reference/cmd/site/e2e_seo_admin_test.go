package main

// e2e SEO записи (internal/seofields) у страниц и постов: поля в форме, превью сниппета (при открытии формы
// и через htmx), <title> и Open Graph на сайте, подстановка в пустые поля, проверка полей, ссылка на картинку.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// snippet — превью сниппета из ответа: заголовок и описание в поиске, есть ли картинка в карточке соцсетей.
func snippet(t *testing.T, html string) (title, desc, url string, image bool) {
	t.Helper()
	s := parse(t, html).Find("#seo-snippet")
	if s.Length() != 1 {
		t.Fatalf("no #seo-snippet in\n%.300s", html)
	}
	return text(s.Find(".seo-snippet-search .seo-snippet-title")), text(s.Find(".seo-snippet-search .seo-snippet-desc")),
		text(s.Find(".seo-snippet-url")), s.Find(".seo-snippet-card picture").Length() == 1
}

func TestE2EAdminPostSEO(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)
	img := imgs["small.jpg"]

	form := newPostForm("Пост для поиска", "post-dlya-poiska", "", "")
	form.Set("excerpt", "Ручной анонс поста.")
	edit := a.createPost(form, "publish")

	// Ничего не задано: раздел свёрнут, превью — подстановка (заголовок с названием сайта, анонс, без картинки).
	_, html := a.get(edit)
	doc := parse(t, html)
	if _, open := doc.Find("details.seo-fields").Attr("open"); open {
		t.Error("empty SEO section is open")
	}
	title, desc, u, image := snippet(t, html)
	if title != "Пост для поиска — Keelkit — эталонный сайт" || desc != "Ручной анонс поста." || image {
		t.Errorf("auto snippet: title %q, desc %q, image %v", title, desc, image)
	}
	if u != "example.test/blog/post-dlya-poiska" {
		t.Errorf("snippet url = %q", u)
	}
	h := head{page(t, a.srv, "/blog/post-dlya-poiska", http.StatusOK)}
	if got := text(h.doc.Find("title")); got != "Пост для поиска — Keelkit — эталонный сайт" {
		t.Errorf("auto <title> = %q", got)
	}

	// Превью через htmx — из присланной формы, без сохранения; пробелы и переводы строк — один пробел.
	f := a.postForm(edit)
	f.Set("seo_title", "  Заголовок\r\nдля поиска ")
	f.Set("seo_image", img)
	resp, html := a.submit("/admin/posts/seo", f, "", http.Header{"Hx-Request": {"true"}, "X-Csrf-Token": {a.token}})
	if resp.StatusCode != http.StatusOK || strings.Contains(html, "<html") {
		t.Fatalf("snippet: status %d\n%.200s", resp.StatusCode, html)
	}
	if title, desc, _, image = snippet(t, html); title != "Заголовок для поиска" || desc != "Ручной анонс поста." || !image {
		t.Errorf("htmx snippet: title %q, desc %q, image %v", title, desc, image)
	}

	// Сохранить и опубликовать: <title> и og:title — SEO-заголовок как есть, описание и картинка — свои.
	f.Set("seo_description", "Описание для поисковиков.")
	if resp, _ := a.submit(edit, f, "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d", resp.StatusCode)
	}
	if got := a.postStatus("Пост для поиска"); got != "Есть неопубликованные изменения" {
		t.Errorf("status after SEO edit = %q", got)
	}
	_, html = a.get(edit)
	if _, open := parse(t, html).Find("details.seo-fields").Attr("open"); !open {
		t.Error("filled SEO section is closed")
	}
	if resp, _ := a.submit(edit, a.postForm(edit), "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	h = head{page(t, a.srv, "/blog/post-dlya-poiska", http.StatusOK)}
	if got := text(h.doc.Find("title")); got != "Заголовок для поиска" || h.og("og:title") != "Заголовок для поиска" {
		t.Errorf("<title> %q, og:title %q", got, h.og("og:title"))
	}
	if d := h.attr(`meta[name="description"]`, "content"); d != "Описание для поисковиков." || h.og("og:description") != d {
		t.Errorf("description %q, og:description %q", d, h.og("og:description"))
	}
	if og := h.og("og:image"); !strings.HasPrefix(og, testBaseURL+"/media/"+img+"/") || h.og("og:image:alt") != "" {
		t.Errorf("og:image %q, og:image:alt %q", og, h.og("og:image:alt"))
	}
	if got := text(h.doc.Find("main h1")); got != "Пост для поиска" {
		t.Errorf("h1 = %q: SEO title must not change the page", got)
	}
	// Анонс в ленте и RSS остаётся своим: SEO-описание — только для <head>.
	if got := text(page(t, a.srv, "/blog", http.StatusOK).Find(`.post-card:contains("Пост для поиска") .post-card-body > p:last-child`)); got != "Ручной анонс поста." {
		t.Errorf("card summary = %q", got)
	}

	// Картинка SEO держит изображение: удалить нельзя.
	id := strings.TrimPrefix(edit, "/admin/posts/")
	var n int
	if err := site.db.QueryRow(`SELECT count(*) FROM post_images WHERE post_id = ? AND image_id = ?`, id, img).Scan(&n); err != nil || n != 2 {
		t.Errorf("post_images for seo_image = %d, %v; want 2", n, err)
	}
	if resp, _ := post(t, a.client, a.srv.URL+"/admin/media/"+img+"/delete", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("delete SEO image: status %d, want 409", resp.StatusCode)
	}

	// Очистить — снова подстановка.
	f = a.postForm(edit)
	f.Set("seo_title", "")
	f.Set("seo_description", "")
	f.Set("seo_image", "")
	if resp, _ := a.submit(edit, f, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish cleared: status %d", resp.StatusCode)
	}
	h = head{page(t, a.srv, "/blog/post-dlya-poiska", http.StatusOK)}
	if got := text(h.doc.Find("title")); got != "Пост для поиска — Keelkit — эталонный сайт" || h.og("og:title") != "Пост для поиска" {
		t.Errorf("cleared: <title> %q, og:title %q", got, h.og("og:title"))
	}
	if d := h.attr(`meta[name="description"]`, "content"); d != "Ручной анонс поста." || h.og("og:image") != "" {
		t.Errorf("cleared: description %q, og:image %q", d, h.og("og:image"))
	}
}

func TestE2EAdminPageSEO(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)

	// Главная: без SEO-заголовка в <title> — название сайта; с ним — он.
	edit := a.editLink("Добро пожаловать")
	_, html := a.get(edit)
	if title, _, u, _ := snippet(t, html); title != "Keelkit — эталонный сайт" || u != "example.test" {
		t.Errorf("home snippet: title %q, url %q", title, u)
	}
	f := a.form(edit)
	f.Set("seo_title", "Keelkit — сайты на Go")
	f.Set("seo_description", "Описание главной для поисковиков.")
	if resp, _ := a.submit(edit, f, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish home: status %d", resp.StatusCode)
	}
	h := head{page(t, a.srv, "/", http.StatusOK)}
	if got := text(h.doc.Find("title")); got != "Keelkit — сайты на Go" || h.og("og:title") != "Keelkit — сайты на Go" {
		t.Errorf("home: <title> %q, og:title %q", got, h.og("og:title"))
	}
	if d := h.attr(`meta[name="description"]`, "content"); d != "Описание главной для поисковиков." {
		t.Errorf("home description = %q", d)
	}

	// Страница с картинкой в блоках: своя картинка для соцсетей заменяет первое изображение блоков.
	edit = a.editLink("О проекте")
	h = head{page(t, a.srv, "/about", http.StatusOK)}
	auto := h.og("og:image")
	f = a.form(edit)
	f.Set("seo_image", imgs["small.jpg"])
	if resp, _ := a.submit(edit, f, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish about: status %d", resp.StatusCode)
	}
	h = head{page(t, a.srv, "/about", http.StatusOK)}
	if og := h.og("og:image"); og == auto || !strings.HasPrefix(og, testBaseURL+"/media/"+imgs["small.jpg"]+"/") {
		t.Errorf("about og:image %q (auto was %q)", og, auto)
	}
	if got := a.listStatus("О проекте"); got != "Опубликована" {
		t.Errorf("status after publish = %q", got)
	}

	// Превью страницы через htmx.
	f.Set("seo_title", "")
	resp, html := a.submit("/admin/pages/seo", f, "", http.Header{"Hx-Request": {"true"}, "X-Csrf-Token": {a.token}})
	if title, _, u, image := snippet(t, html); resp.StatusCode != http.StatusOK || title != "О проекте — Keelkit — эталонный сайт" || u != "example.test/about" || !image {
		t.Errorf("page snippet: status %d, title %q, url %q, image %v", resp.StatusCode, title, u, image)
	}
}

func TestE2EAdminSEOValidation(t *testing.T) {
	a := newPageAdmin(t)
	for _, tc := range []struct{ field, value, err string }{
		{"seo_title", strings.Repeat("з", 201), "SEO-заголовок длиннее 200 символов."},
		{"seo_description", strings.Repeat("о", 501), "SEO-описание длиннее 500 символов."},
		{"seo_image", "aaaaaaaaaaaaaaaaaaaaaaaaaa", "Изображения нет в медиатеке: выберите другое."},
	} {
		for _, typ := range []struct {
			path string
			form url.Values
		}{
			{"/admin/pages", newPageForm("SEO с ошибкой", "")},
			{"/admin/posts", newPostForm("SEO с ошибкой", "", "", "")},
		} {
			form := typ.form
			form.Set(tc.field, tc.value)
			for _, h := range []http.Header{nil, htmx} {
				resp, html := a.submit(typ.path, form, "save", h)
				doc := parse(t, html)
				if resp.StatusCode != http.StatusUnprocessableEntity || text(doc.Find("#"+tc.field+"-error")) != tc.err {
					t.Errorf("%s %s (htmx %v): status %d, error %q", typ.path, tc.field, h != nil, resp.StatusCode, text(doc.Find("#"+tc.field+"-error")))
				}
				if v, _ := doc.Find(`[name="` + tc.field + `"]`).Attr("aria-invalid"); v != "true" {
					t.Errorf("%s %s: aria-invalid = %q", typ.path, tc.field, v)
				}
				if _, open := doc.Find("details.seo-fields").Attr("open"); !open {
					t.Errorf("%s %s: SEO section with an error is closed", typ.path, tc.field)
				}
			}
		}
	}
}

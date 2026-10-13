package main

// e2e разделов «Посты» и «Категории» в админке: черновик-копия и публикация, дата публикации (пустая и в будущем),
// ошибки валидации, снятие с публикации и удаление, обложка из медиатеки, категории и запрет удалить используемую.

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/voffkin/keelkit/reference/internal/web"
)

var postEditURL = regexp.MustCompile(`^/admin/posts/(\d+)\?(saved|published)=1$`)

// createPost создаёт пост (action — save или publish) и возвращает адрес его формы.
func (a *pageAdmin) createPost(form url.Values, action string) string {
	a.t.Helper()
	resp, body := a.submit("/admin/posts", form, action, nil)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !postEditURL.MatchString(loc) {
		a.t.Fatalf("create post: status %d, Location %q; want 303 to the form\n%.500s", resp.StatusCode, loc, body)
	}
	return "/admin/posts/" + postEditURL.FindStringSubmatch(loc)[1]
}

// postForm — поля формы поста (path — /admin/posts/new или /admin/posts/{id}).
func (a *pageAdmin) postForm(path string) url.Values {
	a.t.Helper()
	return a.formByID(path, "post-form")
}

// postStatus — статус поста в списке по заголовку.
func (a *pageAdmin) postStatus(title string) string {
	a.t.Helper()
	_, html := a.get("/admin/posts")
	return text(parse(a.t, html).Find(`.admin-table tr:contains("` + title + `") .status`))
}

// categoryID — id категории seed по slug.
func (a *pageAdmin) categoryID(slug string) string {
	a.t.Helper()
	var id string
	if err := a.site.db.QueryRow(`SELECT id FROM categories WHERE slug = ?`, slug).Scan(&id); err != nil {
		a.t.Fatalf("category %s: %v", slug, err)
	}
	return id
}

// newPostForm — поля формы нового поста с одним текстовым блоком.
func newPostForm(title, slug, category, date string) url.Values {
	f := url.Values{"title": {title}, "slug": {slug}, "category": {category}, "publish_date": {date}, "cover": {""}}
	addBlock(f, "intro", "text", map[string]string{"body": "Текст поста **" + title + "**."})
	return f
}

// blogTitles — заголовки первой страницы ленты.
func (a *pageAdmin) blogTitles() []string {
	a.t.Helper()
	_, html := a.get(blogPath)
	return cardTitles(parse(a.t, html))
}

// localInput — время в формате поля datetime-local (часовой пояс сервера).
func localInput(t time.Time) string { return t.In(time.Local).Format("2006-01-02T15:04") }

func TestE2EAdminPostPublish(t *testing.T) {
	a := newPageAdmin(t)

	// Раздел в меню и счётчик на дашборде; статусы seed в списке.
	_, body := a.get("/admin")
	doc := parse(t, body)
	if href, _ := doc.Find(`.site-nav a:contains("Посты")`).Attr("href"); href != "/admin/posts" {
		t.Errorf("menu link = %q", href)
	}
	if got := text(doc.Find(`.counter:contains("Постов")`)); got != "Постов: 15" {
		t.Errorf("counter = %q", got)
	}
	if got := a.postStatus("Черновик поста"); got != "Черновик" {
		t.Errorf("draft status = %q", got)
	}
	if got := a.postStatus("Пост из будущего"); !strings.HasPrefix(got, "Запланирован на ") {
		t.Errorf("future status = %q", got)
	}
	if got := a.postStatus(seedPostTitles[0]); got != "Опубликован" {
		t.Errorf("published status = %q", got)
	}

	// «Сохранить» — только черновик: на сайте поста нет, дата публикации пуста.
	edit := a.createPost(newPostForm("Свежий пост", "", a.categoryID("web"), ""), "save")
	if got := a.postStatus("Свежий пост"); got != "Черновик" {
		t.Errorf("status after save = %q", got)
	}
	if resp, _ := a.get(blogPath + "/svezhiy-post"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("draft on site: status %d", resp.StatusCode)
	}
	if f := a.postForm(edit); f.Get("publish_date") != "" || f.Get("slug") != "svezhiy-post" {
		t.Errorf("draft form: publish_date %q, slug %q", f.Get("publish_date"), f.Get("slug"))
	}

	// «Опубликовать» с пустой датой: дата — момент публикации (и в черновике), пост — первый в ленте.
	before := time.Now().Truncate(time.Minute)
	form := a.postForm(edit)
	resp, _ := a.submit(edit, form, "publish", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != edit+"?published=1" {
		t.Fatalf("publish: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	date := a.postForm(edit).Get("publish_date")
	if date < localInput(before) || date > localInput(time.Now()) {
		t.Errorf("publish_date after publish = %q, want now (%s)", date, localInput(before))
	}
	if got := a.blogTitles(); len(got) == 0 || got[0] != "Свежий пост" {
		t.Errorf("blog after publish = %q", got)
	}
	if got := a.postStatus("Свежий пост"); got != "Опубликован" {
		t.Errorf("status after publish = %q", got)
	}
	doc = page(t, a.srv, blogPath+"/svezhiy-post", http.StatusOK)
	if got := text(doc.Find("article .post-meta a")); got != "Веб" {
		t.Errorf("category on site = %q", got)
	}

	// Правка опубликованного: на сайте старая версия, статус — неопубликованные изменения, предпросмотр — новая.
	form = a.postForm(edit)
	form.Set("title", "Свежий пост, исправленный")
	if resp, _ := a.submit(edit, form, "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d", resp.StatusCode)
	}
	if got := a.postStatus("Свежий пост, исправленный"); got != "Есть неопубликованные изменения" {
		t.Errorf("status after edit = %q", got)
	}
	doc = page(t, a.srv, blogPath+"/svezhiy-post", http.StatusOK)
	if got := text(doc.Find("main h1")); got != "Свежий пост" {
		t.Errorf("site h1 after draft edit = %q", got)
	}
	_, body = a.get(edit + "/view")
	doc = parse(t, body)
	if text(doc.Find("main h1")) != "Свежий пост, исправленный" || doc.Find(".draft-banner").Length() != 1 {
		t.Errorf("preview: h1 %q, banner %d", text(doc.Find("main h1")), doc.Find(".draft-banner").Length())
	}
	if got := text(doc.Find("article .post-meta a")); got != "Веб" {
		t.Errorf("preview category = %q", got)
	}
}

// Дата в будущем: опубликованный пост появляется на сайте только когда она наступит.
func TestE2EAdminPostScheduled(t *testing.T) {
	a := newPageAdmin(t)

	future := localInput(time.Now().Add(48 * time.Hour))
	edit := a.createPost(newPostForm("Анонс", "anons", "", future), "publish")
	if got := a.postStatus("Анонс"); !strings.HasPrefix(got, "Запланирован на ") {
		t.Errorf("status = %q", got)
	}
	if resp, _ := a.get(blogPath + "/anons"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("scheduled post on site: status %d", resp.StatusCode)
	}
	_, body := a.get(edit)
	if parse(t, body).Find(`a:contains("Открыть на сайте")`).Length() != 0 {
		t.Error("scheduled post: «Открыть на сайте» shown")
	}

	// Дата в прошлом и «Опубликовать» — пост на сайте, по дате — после самого нового поста seed.
	form := a.postForm(edit)
	form.Set("publish_date", localInput(time.Now().Add(-36*time.Hour)))
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	page(t, a.srv, blogPath+"/anons", http.StatusOK)
	if got := a.blogTitles(); len(got) < 2 || got[0] != seedPostTitles[0] || got[1] != "Анонс" {
		t.Errorf("blog order = %q", got)
	}
}

func TestE2EAdminPostValidation(t *testing.T) {
	a := newPageAdmin(t)
	go_ := a.categoryID("go")

	for _, tc := range []struct {
		name                        string
		title, slug, category, date string
		cover                       string
		field, err                  string
	}{
		{"empty title", " ", "x", go_, "", "", "title", "Укажите заголовок."},
		{"duplicate slug", "Ещё раз", "post-13", go_, "", "", "slug", "Пост с адресом «post-13» уже есть (опубликованный или черновик): выберите другой slug."},
		{"duplicate draft slug", "Ещё черновик", seedDraftPost, "", "", "", "slug", "Пост с адресом «draft-post» уже есть (опубликованный или черновик): выберите другой slug."},
		{"bad date", "Дата", "", go_, "вчера", "", "publish_date", "Укажите дату и время публикации или оставьте поле пустым."},
		{"unknown category", "Категория", "", "999", "", "", "category", "Такой категории нет: выберите другую."},
		{"unknown cover", "Обложка", "", "", "", "aaaaaaaaaaaaaaaaaaaaaaaaaa", "cover", "Изображения нет в медиатеке: выберите другое."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := newPostForm(tc.title, tc.slug, tc.category, tc.date)
			form.Set("cover", tc.cover)

			resp, html := a.submit("/admin/posts", form, "publish", nil)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422", resp.StatusCode)
			}
			doc := parse(t, html)
			if doc.Find(".site-header").Length() != 1 {
				t.Error("full page expected without HX-Request")
			}
			if got := text(doc.Find("#" + tc.field + "-error")); got != tc.err {
				t.Errorf("%s error = %q, want %q", tc.field, got, tc.err)
			}
			if v, _ := doc.Find(`[name="` + tc.field + `"]`).Attr("aria-invalid"); v != "true" {
				t.Errorf("%s: aria-invalid = %q", tc.field, v)
			}
			if got := doc.Find(`textarea[name="block-intro-body"]`).Text(); got != "Текст поста **"+tc.title+"**." {
				t.Errorf("block body not kept: %q", got)
			}
			if tc.date != "" {
				if v, _ := doc.Find(`input[name="publish_date"]`).Attr("value"); v != tc.date {
					t.Errorf("publish_date not kept: %q", v)
				}
			}
			if tc.category == go_ {
				if v, _ := doc.Find(`select[name="category"] option[selected]`).Attr("value"); v != go_ {
					t.Errorf("category not kept: %q", v)
				}
			}

			resp, html = a.submit("/admin/posts", form, "publish", htmx)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("htmx: status %d, want 422", resp.StatusCode)
			}
			if !strings.HasPrefix(strings.TrimSpace(html), `<form id="post-form"`) || strings.Contains(html, "<html") {
				t.Errorf("htmx: want only the form fragment, got %.120q", html)
			}
			if got := text(parse(t, html).Find("#" + tc.field + "-error")); got != tc.err {
				t.Errorf("htmx: %s error = %q", tc.field, got)
			}
		})
	}

	// Испорченная форма (категория не числом) — 400, а не ошибка у поля.
	form := newPostForm("Сломано", "", "go", "")
	if resp, _ := a.submit("/admin/posts", form, "save", nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("category not a number: status %d, want 400", resp.StatusCode)
	}

	var n int
	if err := a.site.db.QueryRow(`SELECT count(*) FROM posts`).Scan(&n); err != nil || n != 15 {
		t.Errorf("posts = %d, %v; want 15 (nothing saved)", n, err)
	}
}

func TestE2EAdminPostUnpublishDelete(t *testing.T) {
	a := newPageAdmin(t)
	edit := a.createPost(newPostForm("Временный", "vremennyy", "", ""), "publish")
	page(t, a.srv, blogPath+"/vremennyy", http.StatusOK)

	// Опубликованный пост не удаляется: подтверждение и POST — 409.
	if resp, _ := a.get(edit + "/delete"); resp.StatusCode != http.StatusConflict {
		t.Errorf("GET delete published: status %d", resp.StatusCode)
	}
	if resp, _ := post(t, a.client, a.srv.URL+edit+"/delete", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("POST delete published: status %d", resp.StatusCode)
	}

	// Снять с публикации: с сайта пропал, черновик остался.
	resp, _ := post(t, a.client, a.srv.URL+edit+"/unpublish", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != edit+"?unpublished=1" {
		t.Fatalf("unpublish: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	page(t, a.srv, blogPath+"/vremennyy", http.StatusNotFound)
	if got := a.postStatus("Временный"); got != "Черновик" {
		t.Errorf("status after unpublish = %q", got)
	}

	// Теперь удаляется.
	if resp, _ := a.get(edit + "/delete"); resp.StatusCode != http.StatusOK {
		t.Errorf("GET delete draft: status %d", resp.StatusCode)
	}
	resp, _ = post(t, a.client, a.srv.URL+edit+"/delete", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/posts?deleted=1" {
		t.Fatalf("delete: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := a.get(edit); resp.StatusCode != http.StatusNotFound {
		t.Errorf("deleted post form: status %d", resp.StatusCode)
	}
}

// Обложка: выбор из медиатеки (htmx-фрагмент с превью), вывод в ленте и в посте, ссылки в post_images
// и запрет удалить изображение, пока оно обложка поста.
func TestE2EAdminPostCover(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)
	img := imgs["small.jpg"]

	// Фрагмент поля обложки: превью выбранного изображения.
	resp, html := post(t, a.client, a.srv.URL+"/admin/posts/cover", url.Values{"cover": {img}}, http.Header{
		"Hx-Request": {"true"}, "X-Csrf-Token": {a.token},
	})
	doc := parse(t, html)
	if resp.StatusCode != http.StatusOK || doc.Find(`#cover-field [data-image-for="desktop"] picture`).Length() != 1 {
		t.Fatalf("cover fragment: status %d\n%.300s", resp.StatusCode, html)
	}
	if v, _ := doc.Find(`select[name="cover"] option[selected]`).Attr("value"); v != img {
		t.Errorf("selected cover = %q", v)
	}

	form := newPostForm("С обложкой", "s-oblozhkoy", "", "")
	form.Set("cover", img)
	edit := a.createPost(form, "publish")
	id := strings.TrimPrefix(edit, "/admin/posts/")

	var n int
	if err := site.db.QueryRow(`SELECT count(*) FROM post_images WHERE post_id = ? AND image_id = ?`, id, img).Scan(&n); err != nil || n != 2 {
		t.Errorf("post_images for cover = %d, %v; want 2 (draft and published)", n, err)
	}
	doc = page(t, a.srv, blogPath, http.StatusOK)
	if doc.Find(`.post-card:contains("С обложкой") .post-card-cover picture`).Length() != 1 {
		t.Error("blog: no cover in the card")
	}
	doc = page(t, a.srv, blogPath+"/s-oblozhkoy", http.StatusOK)
	if src, _ := doc.Find(".post-cover img").Attr("src"); !strings.HasPrefix(src, "/media/"+img+"/") {
		t.Errorf("post cover src = %q", src)
	}

	// Изображение — обложка поста: медиатека показывает пост и не даёт удалить.
	show := "/admin/media/" + img
	_, html = a.get(show)
	if href, _ := parse(t, html).Find(`.usage-list a:contains("С обложкой")`).Attr("href"); href != edit {
		t.Errorf("usage link = %q, want %s", href, edit)
	}
	if resp, _ := post(t, a.client, a.srv.URL+show+"/delete", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("delete cover image: status %d, want 409", resp.StatusCode)
	}

	// Обложку убрали из черновика — опубликованная копия всё ещё держит изображение.
	form = a.postForm(edit)
	form.Set("cover", "")
	if resp, _ := a.submit(edit, form, "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d", resp.StatusCode)
	}
	if resp, _ := post(t, a.client, a.srv.URL+show+"/delete", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("delete image of published cover: status %d, want 409", resp.StatusCode)
	}
}

// Своя обложка для телефона: htmx-фрагмент поля, ошибки, сохранение и публикация (ссылки в post_images), вывод
// в посте и в ленте (<source media>), og:image — основная, неопубликованное изменение, 409 медиатеки.
func TestE2EAdminPostCoverMobile(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)
	main, phone := imgs["photo.jpg"], imgs["small.jpg"]
	hx := http.Header{"Hx-Request": {"true"}, "X-Csrf-Token": {a.token}}

	// Фрагмент поля: без своей — приглушённое превью основной, со своей — её превью и метка.
	_, html := post(t, a.client, a.srv.URL+"/admin/posts/cover", url.Values{"cover": {main}}, hx)
	doc := parse(t, html)
	if doc.Find(`#cover-field [data-image-for="mobile"] .image-inherited picture`).Length() != 1 || doc.Find(".status-mobile").Length() != 0 {
		t.Errorf("cover fragment without mobile:\n%.500s", html)
	}
	_, html = post(t, a.client, a.srv.URL+"/admin/posts/cover", url.Values{"cover": {main}, "cover_mobile": {phone}, "cover_alt": {"Кот"}}, hx)
	doc = parse(t, html)
	if v, _ := doc.Find(`select[name="cover_mobile"] option[selected]`).Attr("value"); v != phone {
		t.Errorf("cover fragment: cover_mobile = %q", v)
	}
	if doc.Find(".status-mobile").Length() != 1 || doc.Find(`#cover-field [data-image-for="mobile"] .image-inherited`).Length() != 0 {
		t.Errorf("cover fragment with mobile:\n%.500s", html)
	}

	// Своя без основной — ошибка у поля (форма и htmx).
	form := newPostForm("Кадр", "kadr", "", "")
	form.Set("cover_mobile", phone)
	for _, hdr := range []http.Header{nil, htmx} {
		resp, body := a.submit("/admin/posts", form, "save", hdr)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("mobile without cover: status %d, want 422", resp.StatusCode)
		}
		if got := text(parse(t, body).Find("#cover_mobile-error")); got != "Сначала выберите основную картинку." {
			t.Errorf("mobile without cover: error %q (htmx=%v)", got, hdr != nil)
		}
	}

	form.Set("cover", main)
	form.Set("cover_alt", "Кот")
	edit := a.createPost(form, "publish")
	id := strings.TrimPrefix(edit, "/admin/posts/")
	var n int
	if err := site.db.QueryRow(`SELECT count(*) FROM post_images WHERE post_id = ? AND image_id = ?`, id, phone).Scan(&n); err != nil || n != 2 {
		t.Errorf("post_images for cover_mobile = %d, %v; want 2 (draft and published)", n, err)
	}
	if got := a.postForm(edit).Get("cover_mobile"); got != phone {
		t.Errorf("saved cover_mobile = %q", got)
	}

	// На сайте: пост и карточка ленты — <source media> своей; img и og:image — основная.
	doc = page(t, a.srv, blogPath+"/kadr", http.StatusOK)
	if media, _ := doc.Find(".post-cover source").First().Attr("media"); media != web.MobileMedia {
		t.Errorf("post cover: first <source> media = %q", media)
	}
	if srcset, _ := doc.Find(".post-cover source[media][type='image/jpeg']").Attr("srcset"); !strings.Contains(srcset, phone) {
		t.Errorf("post cover: mobile srcset = %q", srcset)
	}
	if src, _ := doc.Find(".post-cover img").Attr("src"); !strings.Contains(src, main) {
		t.Errorf("post cover img = %q, want the main cover", src)
	}
	if og, _ := doc.Find(`meta[property="og:image"]`).Attr("content"); !strings.Contains(og, main) {
		t.Errorf("og:image = %q, want the main cover", og)
	}
	doc = page(t, a.srv, blogPath, http.StatusOK)
	if doc.Find(`.post-card:contains("Кадр") .post-card-cover source[media]`).Length() != 2 {
		t.Error("blog card: no <source media> of the mobile cover")
	}

	// Используемая своя обложка не удаляется из медиатеки.
	if resp, _ := post(t, a.client, a.srv.URL+"/admin/media/"+phone+"/delete", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("delete mobile cover: status %d, want 409", resp.StatusCode)
	}

	// Убрать свою в черновике — неопубликованное изменение; после публикации на сайте только основная.
	form = a.postForm(edit)
	form.Set("cover_mobile", "")
	if resp, _ := a.submit(edit, form, "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d", resp.StatusCode)
	}
	if got := a.postStatus("Кадр"); got != "Есть неопубликованные изменения" {
		t.Errorf("status after removing mobile cover = %q", got)
	}
	if resp, _ := a.submit(edit, a.postForm(edit), "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	if n := page(t, a.srv, blogPath+"/kadr", http.StatusOK).Find(".post-cover source[media]").Length(); n != 0 {
		t.Errorf("mobile cover removed: <source media> = %d", n)
	}
}

// Лид, ручной анонс и alt обложки: сохраняются в черновике, их правка — неопубликованное изменение, на сайте —
// лид под заголовком, анонс в ленте и описании, alt у обложки; alt без обложки не сохраняется; длина проверяется.
func TestE2EAdminPostFields(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)
	img := imgs["small.jpg"]

	// Поле обложки через htmx сохраняет введённый alt.
	_, html := post(t, a.client, a.srv.URL+"/admin/posts/cover", url.Values{"cover": {img}, "cover_alt": {"Кот"}}, http.Header{
		"Hx-Request": {"true"}, "X-Csrf-Token": {a.token},
	})
	if v, _ := parse(t, html).Find(`input[name="cover_alt"]`).Attr("value"); v != "Кот" {
		t.Errorf("cover fragment: cover_alt = %q", v)
	}

	form := newPostForm("С лидом", "s-lidom", "", "")
	form.Set("lead", "  Подзаголовок\r\nв две строки  ")
	form.Set("excerpt", "Анонс\r\nвручную")
	form.Set("cover", img)
	form.Set("cover_alt", " Кот на подоконнике ")
	edit := a.createPost(form, "publish")
	f := a.postForm(edit)
	if f.Get("lead") != "Подзаголовок\nв две строки" || f.Get("excerpt") != "Анонс\nвручную" || f.Get("cover_alt") != "Кот на подоконнике" {
		t.Errorf("saved: lead %q, excerpt %q, cover_alt %q", f.Get("lead"), f.Get("excerpt"), f.Get("cover_alt"))
	}

	doc := page(t, a.srv, blogPath+"/s-lidom", http.StatusOK)
	if got := text(doc.Find("article .post-lead")); got != "Подзаголовок\nв две строки" {
		t.Errorf("lead on site = %q", got)
	}
	if alt, _ := doc.Find(".post-cover img").Attr("alt"); alt != "Кот на подоконнике" {
		t.Errorf("cover alt = %q", alt)
	}
	if d, _ := doc.Find(`meta[name="description"]`).Attr("content"); d != "Анонс вручную" {
		t.Errorf("description = %q", d)
	}
	doc = page(t, a.srv, blogPath, http.StatusOK)
	if got := text(doc.Find(`.post-card:contains("С лидом") .post-card-body > p:last-child`)); got != "Анонс вручную" {
		t.Errorf("card summary = %q", got)
	}

	// Правка анонса — неопубликованное изменение; пустой анонс — снова начало текста.
	f.Set("excerpt", "")
	if resp, _ := a.submit(edit, f, "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d", resp.StatusCode)
	}
	if got := a.postStatus("С лидом"); got != "Есть неопубликованные изменения" {
		t.Errorf("status after excerpt edit = %q", got)
	}
	_, html = a.get(edit + "/view")
	if d, _ := parse(t, html).Find(`meta[name="description"]`).Attr("content"); d != "Текст поста С лидом." {
		t.Errorf("preview description = %q, want the start of the text", d)
	}

	// Без обложки alt не сохраняется.
	f = a.postForm(edit)
	f.Set("cover", "")
	if resp, _ := a.submit(edit, f, "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save without cover: status %d", resp.StatusCode)
	}
	if got := a.postForm(edit).Get("cover_alt"); got != "" {
		t.Errorf("cover_alt without cover = %q", got)
	}

	// Длина: ошибка у поля, со страницей и фрагментом htmx.
	for _, tc := range []struct{ field, value, err string }{
		{"lead", strings.Repeat("л", 501), "Лид длиннее 500 символов."},
		{"excerpt", strings.Repeat("а", 501), "Анонс длиннее 500 символов."},
		{"cover_alt", strings.Repeat("к", 301), "Описание обложки длиннее 300 символов."},
	} {
		form := newPostForm("Длинное поле", "", "", "")
		form.Set("cover", img)
		form.Set(tc.field, tc.value)
		for _, h := range []http.Header{nil, htmx} {
			resp, html := a.submit("/admin/posts", form, "save", h)
			doc := parse(t, html)
			if resp.StatusCode != http.StatusUnprocessableEntity || text(doc.Find("#"+tc.field+"-error")) != tc.err {
				t.Errorf("%s (htmx %v): status %d, error %q", tc.field, h != nil, resp.StatusCode, text(doc.Find("#"+tc.field+"-error")))
			}
			if v, _ := doc.Find(`[name="` + tc.field + `"]`).Attr("aria-invalid"); v != "true" {
				t.Errorf("%s: aria-invalid = %q", tc.field, v)
			}
		}
	}
}

func TestE2EAdminCategories(t *testing.T) {
	a := newPageAdmin(t)

	_, body := a.get("/admin/categories")
	doc := parse(t, body)
	if n := doc.Find(".admin-table tbody tr").Length(); n != 3 {
		t.Errorf("categories = %d, want 3", n)
	}
	if got := text(doc.Find(`.admin-table tr:contains("Go") td`).Eq(2)); got != "6" {
		t.Errorf("posts in Go = %q, want 6 (5 published and the draft)", got)
	}

	// Создание: slug из названия; дубль slug — ошибка у поля (и во фрагменте htmx).
	resp, _ := a.submit("/admin/categories", url.Values{"name": {"Новости"}, "slug": {""}}, "", nil)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasSuffix(loc, "?saved=1") {
		t.Fatalf("create: status %d, Location %q", resp.StatusCode, loc)
	}
	edit := strings.TrimSuffix(loc, "?saved=1")
	if f := a.formByID(edit, "category-form"); f.Get("slug") != "novosti" {
		t.Errorf("generated slug = %q", f.Get("slug"))
	}
	page(t, a.srv, blogPath+"/category/novosti", http.StatusOK)

	for _, header := range []http.Header{nil, htmx} {
		resp, html := a.submit("/admin/categories", url.Values{"name": {"Ещё Go"}, "slug": {"go"}}, "", header)
		doc := parse(t, html)
		if resp.StatusCode != http.StatusUnprocessableEntity || text(doc.Find("#slug-error")) != "Категория с адресом «go» уже есть: выберите другой slug." {
			t.Errorf("duplicate slug (htmx %v): status %d, error %q", header != nil, resp.StatusCode, text(doc.Find("#slug-error")))
		}
		if header != nil && !strings.HasPrefix(strings.TrimSpace(html), `<form id="category-form"`) {
			t.Errorf("htmx: want only the form fragment, got %.120q", html)
		}
	}
	resp, html := a.submit("/admin/categories", url.Values{"name": {" "}}, "", nil)
	if resp.StatusCode != http.StatusUnprocessableEntity || text(parse(t, html).Find("#name-error")) != "Укажите название." {
		t.Errorf("empty name: status %d", resp.StatusCode)
	}

	// Переименование видно на сайте сразу: в ленте категории и у поста.
	goEdit := "/admin/categories/" + a.categoryID("go")
	form := a.formByID(goEdit, "category-form")
	form.Set("name", "Язык Go")
	if resp, _ := a.submit(goEdit, form, "", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("rename: status %d", resp.StatusCode)
	}
	doc = page(t, a.srv, blogPath+"/post-13", http.StatusOK)
	if got := text(doc.Find("article .post-meta a")); got != "Язык Go" {
		t.Errorf("post category after rename = %q", got)
	}

	// Категорию с постами (и с черновиком) не удалить: 409 со списком постов.
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		var resp response
		var html string
		if method == http.MethodGet {
			resp, html = a.get(goEdit + "/delete")
		} else {
			resp, html = post(t, a.client, a.srv.URL+goEdit+"/delete", url.Values{"csrf_token": {a.token}}, nil)
		}
		doc := parse(t, html)
		if resp.StatusCode != http.StatusConflict || doc.Find(".usage-list li").Length() != 6 ||
			doc.Find(`.usage-list a:contains("Черновик поста")`).Length() != 1 {
			t.Errorf("%s delete used category: status %d, usage %q", method, resp.StatusCode, text(doc.Find(".usage-list")))
		}
	}

	// Пустую — можно.
	if resp, _ := a.get(edit + "/delete"); resp.StatusCode != http.StatusOK {
		t.Errorf("GET delete empty category: status %d", resp.StatusCode)
	}
	resp, _ = post(t, a.client, a.srv.URL+edit+"/delete", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/categories?deleted=1" {
		t.Errorf("delete: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	page(t, a.srv, blogPath+"/category/novosti", http.StatusNotFound)
}

// Внешний ключ защищает категорию и в обход админки: удалить используемую напрямую нельзя.
func TestPostCategoryForeignKey(t *testing.T) {
	site := migratedSite(t, true)
	_, err := site.db.ExecContext(context.Background(), `DELETE FROM categories WHERE slug = 'go'`)
	if err == nil {
		t.Fatal("deleted a category used by posts")
	}
}

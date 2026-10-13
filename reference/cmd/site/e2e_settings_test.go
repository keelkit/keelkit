package main

// e2e настроек сайта (internal/settings): раздел админки (сохранение, ошибки у полей — формой и через htmx),
// ссылки на соцсети (поле-список), вывод на сайте — контакты и соцсети в подвале, коды подтверждения, картинка для соцсетей по умолчанию, баннер cookies
// и согласие с ним (форма без JavaScript и htmx).

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/voffkin/keelkit/reference/internal/listfield"
	"github.com/voffkin/keelkit/reference/internal/settings"
	"github.com/voffkin/keelkit/reference/internal/web"
)

// settingsForm — поля формы настроек так, как их отправит браузер.
func (a *pageAdmin) settingsForm() url.Values {
	a.t.Helper()
	return a.formByID(settings.AdminPath, "settings-form")
}

func TestE2EAdminSettings(t *testing.T) {
	a := newPageAdmin(t)

	// Раздел — в группе «Сайт» бокового меню; форма заполнена данными из seed.
	_, html := a.get(settings.AdminPath)
	doc := parse(t, html)
	if cur := text(doc.Find(`.admin-sidebar a[aria-current="page"]`)); cur != "Настройки" {
		t.Errorf("current menu item = %q", cur)
	}
	form := a.settingsForm()
	if form.Get("name") != "Keelkit — эталонный сайт" || form.Get("phone") != "+7 (495) 000-00-00" {
		t.Errorf("form from seed: name %q, phone %q", form.Get("name"), form.Get("phone"))
	}

	// Сайт с seed: контакты в подвале, баннер cookies со ссылкой, кодов подтверждения нет.
	home := page(t, a.srv, "/", http.StatusOK)
	if href, _ := home.Find(`.site-footer a[href^="tel:"]`).Attr("href"); href != "tel:+74950000000" {
		t.Errorf("tel link = %q", href)
	}
	if href, _ := home.Find(`.site-footer a[href^="mailto:"]`).Attr("href"); href != "mailto:hello@example.com" {
		t.Errorf("mailto link = %q", href)
	}
	if got := home.Find(".contacts-address").Text(); got != "Москва, ул. Примерная, д. 1\nофис 101" {
		t.Errorf("address = %q", got)
	}
	if href, _ := home.Find(".cookie-banner a").Attr("href"); href != "/about" {
		t.Errorf("cookie banner link = %q", href)
	}
	h := head{home}
	if h.attr(`meta[name="yandex-verification"]`, "content") != "" || h.attr(`meta[name="google-site-verification"]`, "content") != "" {
		t.Error("verification meta without codes in settings")
	}

	// Сохранение: мета-тег вставлен целиком — остаётся код; пустой текст cookies — баннера нет.
	form.Set("name", "  Новое   название ")
	form.Set("phone", "8 812 111-22-33")
	form.Set("email", "")
	form.Set("address", " Санкт-Петербург \r\n\r\n Невский, 1 ")
	form.Set("cookie_text", "")
	form.Set("yandex_verification", `<meta name="yandex-verification" content="39d68ec668b4661d" />`)
	form.Set("google_verification", "  2a49dc546a_123-524 ")
	resp, body := a.submit(settings.AdminPath, form, "", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != settings.AdminPath+"?saved=1" {
		t.Fatalf("save: status %d, Location %q\n%.500s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	_, html = a.get(settings.AdminPath + "?saved=1")
	if got := text(parse(t, html).Find(".flash")); !strings.HasPrefix(got, "Сохранено.") {
		t.Errorf("flash = %q", got)
	}
	form = a.settingsForm()
	if form.Get("name") != "Новое название" || form.Get("address") != "Санкт-Петербург\nНевский, 1" ||
		form.Get("yandex_verification") != "39d68ec668b4661d" || form.Get("google_verification") != "2a49dc546a_123-524" {
		t.Errorf("saved form: %v", form)
	}

	// На сайте — сразу, без публикации.
	home = page(t, a.srv, "/about", http.StatusOK)
	h = head{home}
	if got := text(home.Find("title")); !strings.HasSuffix(got, "— Новое название") {
		t.Errorf("<title> = %q", got)
	}
	if got := h.attr(`meta[name="yandex-verification"]`, "content"); got != "39d68ec668b4661d" {
		t.Errorf("yandex-verification = %q", got)
	}
	if got := h.attr(`meta[name="google-site-verification"]`, "content"); got != "2a49dc546a_123-524" {
		t.Errorf("google-site-verification = %q", got)
	}
	if href, _ := home.Find(`.site-footer a[href^="tel:"]`).Attr("href"); href != "tel:88121112233" {
		t.Errorf("tel link = %q", href)
	}
	if home.Find(`.site-footer a[href^="mailto:"]`).Length() != 0 {
		t.Error("mailto link with empty email")
	}
	if home.Find(".cookie-banner").Length() != 0 {
		t.Error("cookie banner with empty text")
	}

	// Через htmx — 204 и HX-Redirect.
	resp, _ = a.submit(settings.AdminPath, form, "", htmx)
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Hx-Redirect") != settings.AdminPath+"?saved=1" {
		t.Errorf("htmx save: status %d, HX-Redirect %q", resp.StatusCode, resp.Header.Get("Hx-Redirect"))
	}

	// Без CSRF-токена не сохраняется.
	form.Del("csrf_token")
	if resp, _ := post(t, a.client, a.srv.URL+settings.AdminPath, form, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("save without CSRF: status %d, want 403", resp.StatusCode)
	}
}

// Каждая ошибка — 422 с сообщением у поля и введёнными данными; без htmx — страница, с htmx — только форма.
func TestE2EAdminSettingsValidation(t *testing.T) {
	a := newPageAdmin(t)
	cases := []struct{ field, value, want string }{
		{"name", "   ", "Укажите название"},
		{"name", strings.Repeat("я", 101), "длиннее 100"},
		{"description", strings.Repeat("я", 301), "длиннее 300"},
		{"share_image", "nosuchimage", "нет в медиатеке"},
		{"phone", "звоните", "нет цифр"},
		{"email", "info@", "Неверный email"},
		{"email", "Отдел <info@example.com>", "Неверный email"},
		{"address", strings.Repeat("я", 301), "длиннее 300"},
		{"cookie_text", strings.Repeat("я", 1001), "длиннее 1000"},
		{"cookie_link", "//evil.example", "путь на сайте"},
		{"cookie_link", "javascript:alert(1)", "путь на сайте"},
		{"cookie_link", "privacy", "путь на сайте"},
		{"yandex_verification", "код с пробелами", "латиница, цифры"},
		{"google_verification", `<meta name="google-site-verification" content="a b">`, "латиница, цифры"},
	}
	for _, c := range cases {
		for _, header := range []http.Header{nil, htmx} {
			form := a.settingsForm()
			form.Set(c.field, c.value)
			resp, html := a.submit(settings.AdminPath, form, "", header)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("%s=%q (htmx %v): status %d, want 422", c.field, c.value, header != nil, resp.StatusCode)
				continue
			}
			if isPage := strings.Contains(html, "<html"); isPage == (header != nil) {
				t.Errorf("%s=%q (htmx %v): full page = %v", c.field, c.value, header != nil, isPage)
			}
			doc := parse(t, html)
			if got := text(doc.Find("#" + c.field + "-error")); !strings.Contains(got, c.want) {
				t.Errorf("%s=%q: error %q, want %q", c.field, c.value, got, c.want)
			}
			if inv, _ := doc.Find(`[name="` + c.field + `"]`).Attr("aria-invalid"); inv != "true" {
				t.Errorf("%s: no aria-invalid", c.field)
			}
			if got := formValues(doc.Find("#settings-form")).Get("name"); c.field != "name" && got != "Keelkit — эталонный сайт" {
				t.Errorf("%s: entered name lost: %q", c.field, got)
			}
		}
	}
	// Ничего не сохранилось.
	if got := a.settingsForm().Get("phone"); got != "+7 (495) 000-00-00" {
		t.Errorf("phone after errors = %q", got)
	}
}

// Картинка для соцсетей по умолчанию: og:image страниц без своей картинки и превью сниппета в админке;
// своя картинка записи важнее; используемое изображение медиатека не удаляет.
func TestE2EAdminSettingsShareImage(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)
	img := imgs["small.jpg"]

	// Лента блога своей картинки не имеет.
	if og := (head{page(t, a.srv, blogPath, http.StatusOK)}).og("og:image"); og != "" {
		t.Fatalf("blog og:image before settings = %q", og)
	}
	edit := a.createPost(newPostForm("Без обложки", "bez-oblozhki", "", ""), "publish")

	// Фрагмент поля через htmx: превью выбранной картинки.
	resp, html := post(t, a.client, a.srv.URL+settings.AdminPath+"/image", url.Values{"share_image": {img}}, http.Header{
		"Hx-Request": {"true"}, "X-Csrf-Token": {a.token},
	})
	if doc := parse(t, html); resp.StatusCode != http.StatusOK || doc.Find("#share_image-field picture").Length() != 1 {
		t.Fatalf("image fragment: status %d\n%.300s", resp.StatusCode, html)
	}

	form := a.settingsForm()
	form.Set("share_image", img)
	if resp, body := a.submit(settings.AdminPath, form, "", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d\n%.500s", resp.StatusCode, body)
	}

	h := head{page(t, a.srv, blogPath, http.StatusOK)}
	if og := h.og("og:image"); !strings.HasPrefix(og, testBaseURL+"/media/"+img+"/") || !strings.HasSuffix(og, ".jpg") {
		t.Errorf("blog og:image = %q, want the share image", og)
	}
	if alt := h.og("og:image:alt"); alt != "" {
		t.Errorf("og:image:alt of the default image = %q", alt)
	}
	h = head{page(t, a.srv, blogPath+"/bez-oblozhki", http.StatusOK)}
	if og := h.og("og:image"); !strings.Contains(og, "/media/"+img+"/") {
		t.Errorf("post without cover: og:image = %q", og)
	}
	// Своя картинка (обложка) важнее.
	cover := imgs["photo.jpg"]
	f := a.postForm(edit)
	f.Set("cover", cover)
	if resp, _ := a.submit(edit, f, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish cover: status %d", resp.StatusCode)
	}
	if og := (head{page(t, a.srv, blogPath+"/bez-oblozhki", http.StatusOK)}).og("og:image"); !strings.Contains(og, "/media/"+cover+"/") {
		t.Errorf("post with cover: og:image = %q", og)
	}

	// Превью сниппета записи без своей картинки показывает картинку из настроек.
	other := a.createPost(newPostForm("Ещё без обложки", "eshchyo-bez-oblozhki", "", ""), "save")
	_, html = a.get(other)
	if _, _, _, image := snippet(t, html); !image {
		t.Error("snippet card without the default image")
	}

	// Используемое изображение: медиатека показывает настройки и не даёт удалить.
	show := "/admin/media/" + img
	_, html = a.get(show)
	if href, _ := parse(t, html).Find(`.usage-list a:contains("Настройки")`).Attr("href"); href != settings.AdminPath {
		t.Errorf("usage link = %q", href)
	}
	if resp, _ := post(t, a.client, a.srv.URL+show+"/delete", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("delete share image: status %d, want 409", resp.StatusCode)
	}
}

// Баннер cookies: «Понятно» без JavaScript — 303 обратно на страницу (только путь на этом сайте), с htmx —
// пустой ответ; после согласия баннера нет. Запрос с чужого сайта отклоняется.
func TestE2ECookieBanner(t *testing.T) {
	srv := e2eServer(t, true)

	doc := page(t, srv, blogPath+"?page=2", http.StatusOK)
	banner := doc.Find(".cookie-banner")
	if banner.Length() != 1 {
		t.Fatal("no cookie banner")
	}
	if action, _ := banner.Find("form").Attr("action"); action != web.CookieNoticePath {
		t.Errorf("form action = %q", action)
	}
	if back, _ := banner.Find(`input[name="back"]`).Attr("value"); back != blogPath+"?page=2" {
		t.Errorf("back = %q", back)
	}

	for back, want := range map[string]string{
		blogPath + "?page=2":    blogPath + "?page=2",
		"//evil.example/":       "/",
		"/\\evil.example/":      "/",
		"https://evil.example/": "/",
		"":                      "/",
	} {
		resp, _ := post(t, noRedirect, srv.URL+web.CookieNoticePath, url.Values{"back": {back}}, nil)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Errorf("back %q: status %d, Location %q, want 303 to %q", back, resp.StatusCode, resp.Header.Get("Location"), want)
		}
		c := resp.Cookies()
		if len(c) != 1 || c[0].Name != web.CookieNoticeName || c[0].Value != "1" || !c[0].HttpOnly || c[0].MaxAge < 300*24*3600 {
			t.Errorf("back %q: cookies %v", back, c)
		}
	}

	resp, body := post(t, noRedirect, srv.URL+web.CookieNoticePath, url.Values{"back": {"/"}}, htmx)
	if resp.StatusCode != http.StatusOK || body != "" || len(resp.Cookies()) != 1 {
		t.Errorf("htmx: status %d, body %q, cookies %v", resp.StatusCode, body, resp.Cookies())
	}

	// С cookie согласия баннера нет.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: web.CookieNoticeName, Value: "1"})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if parse(t, string(b)).Find(".cookie-banner").Length() != 0 {
		t.Error("cookie banner after consent")
	}

	// С чужого сайта — 403 (http.CrossOriginProtection).
	resp, _ = post(t, noRedirect, srv.URL+web.CookieNoticePath, url.Values{"back": {"/"}}, http.Header{"Sec-Fetch-Site": {"cross-site"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site: status %d, want 403", resp.StatusCode)
	}
	// GET /cookie-notice — обычный адрес страницы: такой страницы нет.
	page(t, srv, web.CookieNoticePath, http.StatusNotFound)
}

// Ссылки на соцсети — поле-список (internal/listfield): кнопки ↑ ↓ «Убрать» «Добавить» возвращают форму
// изменённой без сохранения (формой и через htmx), «Сохранить» пишет порядок, пустой элемент отбрасывается,
// ошибки — у поля элемента; на сайте — ссылки в подвале.
func TestE2EAdminSettingsSocial(t *testing.T) {
	a := newPageAdmin(t)
	social := settings.SocialField
	order := listfield.Order(social)
	name := func(id, field string) string { return listfield.Name(social, id, field) }
	footer := func() []string {
		t.Helper()
		var got []string
		page(t, a.srv, "/", http.StatusOK).Find(".site-footer .social-links a").Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			got = append(got, text(s)+" "+href)
		})
		return got
	}
	// apply — кнопка списка: форма возвращается изменённой (200, плашка «не сохранены»); с htmx — только форма.
	apply := func(form url.Values, action string, header http.Header) url.Values {
		t.Helper()
		resp, html := a.submit(settings.AdminPath, form, action, header)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d\n%.500s", action, resp.StatusCode, html)
		}
		if isPage := strings.Contains(html, "<html"); isPage == (header != nil) {
			t.Errorf("%s (htmx %v): full page = %v", action, header != nil, isPage)
		}
		doc := parse(t, html)
		if doc.Find(".flash-warning").Length() != 1 {
			t.Errorf("%s: no unsaved warning", action)
		}
		return formValues(doc.Find("#settings-form"))
	}

	// Seed: две ссылки в подвале.
	want := []string{"Telegram https://t.me/example", "ВКонтакте https://vk.com/example"}
	if got := footer(); !slices.Equal(got, want) {
		t.Fatalf("footer social = %q, want %q", got, want)
	}
	if rel, _ := page(t, a.srv, "/", http.StatusOK).Find(".social-links a").Attr("rel"); rel != "me noopener" {
		t.Errorf("rel = %q", rel)
	}
	form := a.settingsForm()
	ids := form[order]
	if len(ids) != 2 || form.Get(name(ids[0], "name")) != "Telegram" || form.Get(name(ids[1], "url")) != "https://vk.com/example" {
		t.Fatalf("form from seed: %v", form)
	}
	tg, vk := ids[0], ids[1]

	// ↓ без JavaScript: порядок поменялся в форме, на сайте — нет.
	form = apply(form, listfield.ActionValue("down", social, tg), nil)
	if got := form[order]; !slices.Equal(got, []string{vk, tg}) {
		t.Errorf("after down = %v", got)
	}
	if got := footer(); !slices.Equal(got, want) {
		t.Errorf("footer changed before save: %q", got)
	}
	// «Добавить» через htmx — два пустых элемента в конец; первый заполняем, второй остаётся пустым.
	form = apply(form, listfield.ActionValue("add", social, ""), htmx)
	form = apply(form, listfield.ActionValue("add", social, ""), htmx)
	ids = form[order]
	if len(ids) != 4 || ids[0] != vk || ids[1] != tg || form.Get(name(ids[2], "name")) != "" {
		t.Fatalf("after add = %v", form)
	}
	yt, empty := ids[2], ids[3]
	form.Set(name(yt, "name"), "  YouTube ")
	form.Set(name(yt, "url"), " https://youtube.com/@example ")
	// «Убрать» сохраняет введённое в остальных элементах.
	form = apply(form, listfield.ActionValue("remove", social, tg), htmx)
	if got := form[order]; !slices.Equal(got, []string{vk, yt, empty}) || form.Get(name(yt, "name")) != "  YouTube " {
		t.Fatalf("after remove = %v", form)
	}

	if resp, body := a.submit(settings.AdminPath, form, "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d\n%.500s", resp.StatusCode, body)
	}
	want = []string{"ВКонтакте https://vk.com/example", "YouTube https://youtube.com/@example"}
	if got := footer(); !slices.Equal(got, want) {
		t.Errorf("footer after save = %q, want %q", got, want)
	}
	// Пустой элемент отброшен, id сохранённых не меняются.
	if got := a.settingsForm()[order]; !slices.Equal(got, []string{vk, yt}) {
		t.Errorf("saved ids = %v", got)
	}

	// Все ссылки убраны — в подвале списка нет.
	form = a.settingsForm()
	form.Del(order)
	if resp, _ := a.submit(settings.AdminPath, form, "", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save empty: status %d", resp.StatusCode)
	}
	if page(t, a.srv, "/", http.StatusOK).Find(".social-links").Length() != 0 {
		t.Error("social list in footer without links")
	}
}

// Ошибки ссылок на соцсети — у поля элемента, 422 (формой и через htmx); испорченная форма или кнопка — 400.
func TestE2EAdminSettingsSocialValidation(t *testing.T) {
	a := newPageAdmin(t)
	social := settings.SocialField
	order := listfield.Order(social)
	base := a.settingsForm()
	id := base[order][0]
	name := func(field string) string { return listfield.Name(social, id, field) }

	cases := []struct{ field, value, want string }{
		{"name", "  ", "Укажите название"},
		{"name", strings.Repeat("я", 51), "длиннее 50"},
		{"url", "", "Укажите адрес"},
		{"url", "t.me/example", "https://"},
		{"url", "javascript:alert(1)", "https://"},
		{"url", "/about", "https://"},
	}
	for _, c := range cases {
		for _, header := range []http.Header{nil, htmx} {
			form := a.settingsForm()
			form.Set(name(c.field), c.value)
			resp, html := a.submit(settings.AdminPath, form, "", header)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("%s=%q (htmx %v): status %d, want 422", c.field, c.value, header != nil, resp.StatusCode)
				continue
			}
			doc := parse(t, html)
			if got := text(doc.Find("#" + name(c.field) + "-error")); !strings.Contains(got, c.want) {
				t.Errorf("%s=%q: error %q, want %q", c.field, c.value, got, c.want)
			}
			if inv, _ := doc.Find(`[name="` + name(c.field) + `"]`).Attr("aria-invalid"); inv != "true" {
				t.Errorf("%s: no aria-invalid", c.field)
			}
		}
	}

	bad := []struct {
		what   string
		edit   func(url.Values)
		action string
	}{
		{"unknown item", nil, listfield.ActionValue("up", social, "zzz")},
		{"unknown list field", nil, listfield.ActionValue("up", "menu", id)},
		{"unknown op", nil, listfield.ActionValue("hide", social, id)},
		{"add with item", nil, listfield.ActionValue("add", social, id)},
		{"up without item", nil, "list-up:" + social},
		{"unknown action", nil, "publish"},
		{"bad item id", func(f url.Values) { f.Add(order, "Bad id!") }, ""},
		{"duplicate item id", func(f url.Values) { f.Add(order, id) }, ""},
	}
	for _, b := range bad {
		form := a.settingsForm()
		if b.edit != nil {
			b.edit(form)
		}
		if resp, _ := a.submit(settings.AdminPath, form, b.action, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", b.what, resp.StatusCode)
		}
	}
	// Ничего не сохранилось.
	if got := a.settingsForm(); !slices.Equal(got[order], base[order]) || got.Get(name("url")) != base.Get(name("url")) {
		t.Errorf("social after errors = %v", got)
	}
}

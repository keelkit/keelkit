package main

// e2e раздела «Страницы» в админке: черновик-копия и публикация, блоки (добавление, порядок, скрытие, удаление),
// ошибки валидации у полей и у полей блоков, снятие с публикации, удаление неопубликованной, превью Markdown.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/voffkin/keelkit/reference/internal/web"
)

// htmx — заголовки запроса, который отправляет htmx.
var htmx = http.Header{"Hx-Request": {"true"}}

// pageAdmin — сайт с seed и вошедший «браузер» с CSRF-токеном сессии.
type pageAdmin struct {
	t      *testing.T
	site   testSite
	srv    *httptest.Server
	client *http.Client
	token  string
}

func newPageAdmin(t *testing.T) *pageAdmin {
	t.Helper()
	return newPageAdminOn(t, migratedSite(t, true))
}

func newPageAdminOn(t *testing.T, site testSite) *pageAdmin {
	t.Helper()
	srv := httptest.NewServer(site.handler(config{}))
	t.Cleanup(srv.Close)
	client := adminClient(t)
	return &pageAdmin{t: t, site: site, srv: srv, client: client, token: loginOK(t, client, srv)}
}

func (a *pageAdmin) get(path string) (response, string) {
	a.t.Helper()
	return fetch(a.t, a.client, a.srv.URL+path)
}

// form открывает форму страницы (path — /admin/pages/new или /admin/pages/{id}) и возвращает её поля так,
// как их отправит браузер.
func (a *pageAdmin) form(path string) url.Values {
	a.t.Helper()
	return a.formByID(path, "page-form")
}

// formByID — поля формы с id из страницы path (форма поста, категории…).
func (a *pageAdmin) formByID(path, id string) url.Values {
	a.t.Helper()
	resp, body := a.get(path)
	if resp.StatusCode != http.StatusOK {
		a.t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	return formValues(parse(a.t, body).Find("#" + id))
}

// submit отправляет форму страницы с кнопкой action ("save", "publish", "up:<id>"…).
// path — /admin/pages (создать) или /admin/pages/{id}.
func (a *pageAdmin) submit(path string, form url.Values, action string, header http.Header) (response, string) {
	a.t.Helper()
	f := url.Values{}
	for k, v := range form {
		f[k] = v
	}
	f.Set("csrf_token", a.token)
	if action != "" {
		f.Set("action", action)
	}
	return post(a.t, a.client, a.srv.URL+path, f, header)
}

// formValues — поля формы так, как их отправит браузер: input (кроме кнопок и неотмеченных флажков),
// выбранные option у select, textarea. Кнопки не входят: их значение добавляет submit.
func formValues(form *goquery.Selection) url.Values {
	v := url.Values{}
	form.Find("input, select, textarea").Each(func(_ int, s *goquery.Selection) {
		name, ok := s.Attr("name")
		if !ok {
			return
		}
		switch goquery.NodeName(s) {
		case "textarea":
			v.Add(name, s.Text())
		case "select":
			val, _ := s.Find("option[selected]").Attr("value")
			v.Add(name, val)
		default:
			typ, _ := s.Attr("type")
			if typ == "submit" || typ == "button" {
				return
			}
			if _, checked := s.Attr("checked"); (typ == "checkbox" || typ == "radio") && !checked {
				return
			}
			val, _ := s.Attr("value")
			v.Add(name, val)
		}
	})
	return v
}

// blockIDs — id блоков формы по порядку.
func blockIDs(form url.Values) []string { return form["block"] }

// addBlock добавляет в поля формы блок typ с id и полями fields (без префикса block-<id>-).
func addBlock(form url.Values, id, typ string, fields map[string]string) {
	form.Add("block", id)
	form.Set("block-"+id+"-type", typ)
	for k, val := range fields {
		form.Set("block-"+id+"-"+k, val)
	}
}

// newPageForm — поля формы новой страницы без блоков.
func newPageForm(title, slug string) url.Values {
	return url.Values{"title": {title}, "slug": {slug}}
}

var editURL = regexp.MustCompile(`^/admin/pages/(\d+)\?(saved|published)=1$`)

// create создаёт страницу (action — save или publish) и возвращает адрес её формы.
func (a *pageAdmin) create(form url.Values, action string) string {
	a.t.Helper()
	resp, body := a.submit("/admin/pages", form, action, nil)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !editURL.MatchString(loc) {
		a.t.Fatalf("create: status %d, Location %q; want 303 to the form\n%.500s", resp.StatusCode, loc, body)
	}
	return "/admin/pages/" + editURL.FindStringSubmatch(loc)[1]
}

// editLink — адрес формы страницы из списка по заголовку.
func (a *pageAdmin) editLink(title string) string {
	a.t.Helper()
	_, html := a.get("/admin/pages")
	href, ok := parse(a.t, html).Find(`.admin-table a:contains("` + title + `")`).Attr("href")
	if !ok {
		a.t.Fatalf("no edit link for «%s»", title)
	}
	return href
}

// listStatus — статус страницы в списке по заголовку.
func (a *pageAdmin) listStatus(title string) string {
	a.t.Helper()
	_, html := a.get("/admin/pages")
	return text(parse(a.t, html).Find(`.admin-table tr:contains("` + title + `") .status`))
}

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
	if n, want := doc.Find(".admin-table tbody tr").Length(), len(seedPages(nil)); n != want {
		t.Errorf("list rows = %d, want %d", n, want)
	}
	if got := a.listStatus("О проекте"); got != "Опубликована" {
		t.Errorf("seed page status = %q", got)
	}

	// Новая страница: один пустой текстовый блок и кнопки «Добавить блок» для каждого типа из site.yaml.
	resp, body = a.get("/admin/pages/new")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/pages/new: status %d", resp.StatusCode)
	}
	doc = parse(t, body)
	if action, _ := doc.Find("#page-form").Attr("hx-post"); action != "/admin/pages" {
		t.Errorf("form hx-post = %q", action)
	}
	if n := doc.Find(".block-form").Length(); n != 1 || text(doc.Find(".block-form h3")) != "Текст" {
		t.Errorf("new page blocks = %d (%q), want one text block", n, text(doc.Find(".block-form h3")))
	}
	if got := text(doc.Find(".preview-empty")); got != "Здесь появится превью текста." {
		t.Errorf("empty preview = %q", got)
	}
	// Панель оформления Markdown: управляет своим textarea, метка связана через for (кнопки не внутри label),
	// кнопки не отправляют форму; скрипт панели подключён.
	ta := doc.Find(".block-form .md-field textarea")
	taID, _ := ta.Attr("id")
	taName, _ := ta.Attr("name")
	if taID == "" || taID != taName {
		t.Errorf("markdown textarea id = %q, name = %q", taID, taName)
	}
	if v, _ := doc.Find(".block-form .md-toolbar").Attr("aria-controls"); v != taID {
		t.Errorf("toolbar aria-controls = %q, want %q", v, taID)
	}
	if v, _ := doc.Find(".block-form .md-field > label").Attr("for"); v != taID {
		t.Errorf("markdown label for = %q, want %q", v, taID)
	}
	var tools []string
	doc.Find(".block-form .md-toolbar button").Each(func(_ int, b *goquery.Selection) {
		if typ, _ := b.Attr("type"); typ != "button" {
			t.Errorf("toolbar button type = %q, want button", typ)
		}
		v, _ := b.Attr("data-md")
		tools = append(tools, v)
	})
	if got := strings.Join(tools, ","); got != "bold,italic,strike,link,heading,ul,ol,quote,code" {
		t.Errorf("toolbar buttons = %s", got)
	}
	if doc.Find(`script[src^="/static/editor.js?v="]`).Length() != 1 {
		t.Error("admin layout: no editor.js")
	}
	var add []string
	doc.Find(`.block-add button`).Each(func(_ int, b *goquery.Selection) {
		v, _ := b.Attr("value")
		add = append(add, v)
	})
	if got := strings.Join(add, ","); got != "add:text,add:image,add:image_text,add:quote,add:gallery,add:file,add:people" {
		t.Errorf("add buttons = %s", got)
	}
	if doc.Find(".danger-zone").Length() != 0 {
		t.Error("new page: no danger zone expected")
	}
	if href, _ := doc.Find(`.form-actions a:contains("Отмена")`).Attr("href"); href != "/admin/pages" {
		t.Errorf("cancel link = %q", href)
	}

	// «Сохранить»: пустой slug составляется из заголовка; CRLF из textarea хранится как LF.
	form := formValues(doc.Find("#page-form"))
	id := blockIDs(form)[0]
	form.Set("title", "Тестовая")
	form.Set("block-"+id+"-body", "Текст: **жирный**\r\n\r\nВторой абзац.")
	edit := a.create(form, "save")

	resp, body = a.get(edit + "?saved=1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", edit, resp.StatusCode)
	}
	doc = parse(t, body)
	if got := text(doc.Find(".flash")); got != "Черновик сохранён." {
		t.Errorf("flash = %q", got)
	}
	if v, _ := doc.Find(`input[name="slug"]`).Attr("value"); v != "testovaya" {
		t.Errorf("slug = %q, want testovaya", v)
	}
	if got := doc.Find(`textarea[name="block-` + id + `-body"]`).Text(); got != "Текст: **жирный**\n\nВторой абзац." {
		t.Errorf("body = %q", got)
	}
	if got := text(doc.Find("#block-" + id + "-body-preview strong")); got != "жирный" {
		t.Errorf("preview strong = %q", got)
	}

	// Черновик: на сайте и в меню его нет, в админке — предпросмотр и удаление.
	if got := text(doc.Find(".admin-title .status")); got != "Черновик" {
		t.Errorf("status = %q", got)
	}
	if doc.Find(`a:contains("Открыть на сайте")`).Length() != 0 {
		t.Error("draft: no public link expected")
	}
	if href, _ := doc.Find(`a:contains("Предпросмотр")`).Attr("href"); href != edit+"/view" {
		t.Errorf("preview link = %q", href)
	}
	if href, _ := doc.Find(`.danger-zone a`).Attr("href"); href != edit+"/delete" {
		t.Errorf("delete link = %q", href)
	}
	if doc.Find(`.danger-zone form`).Length() != 0 {
		t.Error("draft: no unpublish form expected")
	}
	page(t, a.srv, "/testovaya", http.StatusNotFound)
	if n := page(t, a.srv, "/", http.StatusOK).Find(`.site-nav a[href="/testovaya"]`).Length(); n != 0 {
		t.Error("draft must not be in the menu")
	}
	if got := a.listStatus("Тестовая"); got != "Черновик" {
		t.Errorf("list status = %q", got)
	}

	// Предпросмотр — страница сайта с плашкой; только после входа.
	resp, body = a.get(edit + "/view")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET view: status %d", resp.StatusCode)
	}
	doc = parse(t, body)
	if got := text(doc.Find("main article #block-" + id + " strong")); got != "жирный" {
		t.Errorf("view strong = %q", got)
	}
	if href, _ := doc.Find(".draft-banner a").Attr("href"); href != edit {
		t.Errorf("banner link = %q", href)
	}
	if resp, _ := fetch(t, noRedirect, a.srv.URL+edit+"/view"); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("view without session: status %d, want 303", resp.StatusCode)
	}

	// «Опубликовать».
	resp, _ = a.submit(edit, a.form(edit), "publish", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != edit+"?published=1" {
		t.Fatalf("publish: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, body = a.get(edit + "?published=1")
	doc = parse(t, body)
	if got := text(doc.Find(".flash")); got != "Опубликовано." {
		t.Errorf("flash = %q", got)
	}
	if href, _ := doc.Find(`a:contains("Открыть на сайте")`).Attr("href"); href != "/testovaya" {
		t.Errorf("public link = %q", href)
	}
	if doc.Find(".danger-zone a").Length() != 0 || doc.Find(`.danger-zone form button:contains("Снять с публикации")`).Length() != 1 {
		t.Error("published: want the unpublish button and no delete link")
	}

	// Теперь страница видна на сайте и в меню, описание — из текста блоков.
	pub := page(t, a.srv, "/testovaya", http.StatusOK)
	if got := text(pub.Find("main h1")); got != "Тестовая" {
		t.Errorf("public h1 = %q", got)
	}
	if pub.Find(".draft-banner").Length() != 0 {
		t.Error("public page: no draft banner expected")
	}
	if got := text(pub.Find("main article strong")); got != "жирный" {
		t.Errorf("public strong = %q", got)
	}
	if got, _ := pub.Find(`meta[name="description"]`).Attr("content"); got != "Текст: жирный Второй абзац." {
		t.Errorf("description = %q", got)
	}
	// В меню новая страница сама не попадает: пункты задаёт раздел «Меню».
	if pub.Find(`.site-nav a[href="/testovaya"]`).Length() != 0 {
		t.Error("a new page must not be added to the menu by itself")
	}
	if got := a.listStatus("Тестовая"); got != "Опубликована" {
		t.Errorf("list status = %q", got)
	}

	_, body = a.get("/admin")
	if got, want := text(parse(t, body).Find(".counter").First()), "Страниц: "+strconv.Itoa(len(seedPages(nil))+1); got != want {
		t.Errorf("counter = %q, want %q", got, want)
	}

	// Новую страницу можно и сразу опубликовать.
	form = newPageForm("Сразу", "")
	addBlock(form, "b1", "quote", map[string]string{"text": "Цитата"})
	a.create(form, "publish")
	page(t, a.srv, "/srazu", http.StatusOK)
}

// Правки опубликованной страницы не видны на сайте до «Опубликовать»; slug — часть версии.
func TestE2EAdminPageDraftCopy(t *testing.T) {
	a := newPageAdmin(t)
	edit := a.editLink("О проекте")

	form := a.form(edit)
	form.Set("title", "О проекте (новое)")
	addBlock(form, "new-quote", "quote", map[string]string{"text": "Новая цитата", "author": "Редактор"})
	if resp, _ := a.submit(edit, form, "save", htmx); resp.StatusCode != http.StatusNoContent || resp.Header.Get("Hx-Redirect") != edit+"?saved=1" {
		t.Fatalf("htmx save: status %d, HX-Redirect %q", resp.StatusCode, resp.Header.Get("Hx-Redirect"))
	}

	pub := page(t, a.srv, "/about", http.StatusOK)
	if got := text(pub.Find("main h1")); got != "О проекте" {
		t.Errorf("public h1 after save = %q, want the published title", got)
	}
	if pub.Find("#block-new-quote").Length() != 0 {
		t.Error("unpublished block is on the site")
	}
	if got := text(page(t, a.srv, "/", http.StatusOK).Find(`.site-nav a[href="/about"]`)); got != "О проекте" {
		t.Errorf("menu shows the draft title: %q", got)
	}
	if got := a.listStatus("О проекте (новое)"); got != "Есть неопубликованные изменения" {
		t.Errorf("list status = %q", got)
	}
	_, body := a.get(edit)
	if got := text(parse(t, body).Find(".admin-title .status")); got != "Есть неопубликованные изменения" {
		t.Errorf("form status = %q", got)
	}
	_, body = a.get(edit + "/view")
	if got := text(parse(t, body).Find("#block-new-quote blockquote")); got != "Новая цитата" {
		t.Errorf("preview quote = %q", got)
	}

	// Сохранить без изменений — статус тот же; опубликовать — изменения на сайте.
	if resp, _ := a.submit(edit, a.form(edit), "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	pub = page(t, a.srv, "/about", http.StatusOK)
	if got := text(pub.Find("main h1")); got != "О проекте (новое)" {
		t.Errorf("public h1 after publish = %q", got)
	}
	if got := text(pub.Find("#block-new-quote figcaption")); got != "Редактор" {
		t.Errorf("published quote author = %q", got)
	}
	if got := a.listStatus("О проекте (новое)"); got != "Опубликована" {
		t.Errorf("list status after publish = %q", got)
	}

	// Новый slug: сохранён — сайт открывает старый адрес; опубликован — новый 200, старый — 301 на новый.
	form = a.form(edit)
	form.Set("slug", "about-us")
	if resp, _ := a.submit(edit, form, "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save slug: status %d", resp.StatusCode)
	}
	page(t, a.srv, "/about", http.StatusOK)
	page(t, a.srv, "/about-us", http.StatusNotFound)
	_, body = a.get(edit)
	if href, _ := parse(t, body).Find(`a:contains("Открыть на сайте")`).Attr("href"); href != "/about" {
		t.Errorf("public link = %q, want the published address", href)
	}
	if resp, _ := a.submit(edit, a.form(edit), "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish slug: status %d", resp.StatusCode)
	}
	page(t, a.srv, "/about-us", http.StatusOK)
	assertRedirect(t, a.srv, "/about", "/about-us")
}

// Кнопки блоков без JavaScript: форма возвращается изменённой, ничего не сохраняется; с htmx — фрагмент формы.
func TestE2EAdminPageBlocks(t *testing.T) {
	a := newPageAdmin(t)
	form := newPageForm("Блоки", "")
	addBlock(form, "one", "text", map[string]string{"body": "Первый"})
	addBlock(form, "two", "quote", map[string]string{"text": "Второй"})
	addBlock(form, "three", "text", map[string]string{"body": "Третий"})
	edit := a.create(form, "publish")

	// apply нажимает кнопку блока и возвращает новые поля формы.
	apply := func(form url.Values, action string, header http.Header) url.Values {
		t.Helper()
		resp, body := a.submit(edit, form, action, header)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", action, resp.StatusCode)
		}
		doc := parse(t, body)
		if got := text(doc.Find(".flash-warning")); !strings.Contains(got, "не сохранены") {
			t.Errorf("%s: no unsaved notice, got %q", action, got)
		}
		if header != nil && (strings.Contains(body, "<html") || !strings.HasPrefix(strings.TrimSpace(body), `<form id="page-form"`)) {
			t.Errorf("%s via htmx: want only the form fragment", action)
		}
		return formValues(doc.Find("#page-form"))
	}
	order := func(form url.Values) string { return strings.Join(blockIDs(form), ",") }

	form = a.form(edit)
	if got := order(form); got != "one,two,three" {
		t.Fatalf("order = %s", got)
	}
	form = apply(form, "up:three", nil)
	if got := order(form); got != "one,three,two" {
		t.Errorf("after up:three = %s", got)
	}
	form = apply(form, "down:one", htmx)
	if got := order(form); got != "three,one,two" {
		t.Errorf("after down:one = %s", got)
	}
	form = apply(form, "hide:two", nil)
	if form.Get("block-two-hidden") != "1" {
		t.Error("hide:two did not hide")
	}
	form = apply(form, "add:quote", nil)
	ids := blockIDs(form)
	if len(ids) != 4 || form.Get("block-"+ids[3]+"-type") != "quote" {
		t.Fatalf("after add:quote = %v", ids)
	}
	form = apply(form, "remove:"+ids[3], htmx)
	if got := order(form); got != "three,one,two" {
		t.Errorf("after remove = %s", got)
	}
	// Введённое в поля не теряется при действиях блоков.
	if form.Get("block-one-body") != "Первый" || form.Get("title") != "Блоки" {
		t.Errorf("fields lost: %v", form)
	}

	// Ничего не сохранено: на сайте прежний порядок и все блоки.
	pubOrder := func() string {
		var got []string
		page(t, a.srv, "/bloki", http.StatusOK).Find("main article .block").Each(func(_ int, s *goquery.Selection) {
			id, _ := s.Attr("id")
			got = append(got, strings.TrimPrefix(id, "block-"))
		})
		return strings.Join(got, ",")
	}
	if got := pubOrder(); got != "one,two,three" {
		t.Errorf("site changed before save: %s", got)
	}

	// Опубликовать: новый порядок, скрытого блока на сайте нет, в админке он есть с отметкой.
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	if got := pubOrder(); got != "three,one" {
		t.Errorf("site order = %s, want three,one (two is hidden)", got)
	}
	_, body := a.get(edit)
	doc := parse(t, body)
	if got := text(doc.Find("#block-two.block-hidden .status")); got != "Скрыт" {
		t.Errorf("hidden block in admin: %q", got)
	}
	if got := text(doc.Find(`#block-two button[value="hide:two"]`)); got != "Показать" {
		t.Errorf("hidden block button = %q", got)
	}

	// Удалённый блок пропадает после публикации.
	form = apply(a.form(edit), "remove:one", nil)
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish after remove: status %d", resp.StatusCode)
	}
	if got := pubOrder(); got != "three" {
		t.Errorf("site order after remove = %s", got)
	}

	// Неизвестное действие и испорченная форма — 400.
	for _, tc := range []struct {
		name   string
		form   url.Values
		action string
	}{
		{"unknown action", a.form(edit), "jump:three"},
		{"add not allowed", a.form(edit), "add:carousel"},
		{"bad block id", url.Values{"title": {"x"}, "block": {"<b>"}, "block-<b>-type": {"text"}}, "save"},
		{"unknown type", url.Values{"title": {"x"}, "block": {"z"}, "block-z-type": {"script"}}, "save"},
	} {
		if resp, _ := a.submit(edit, tc.form, tc.action, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", tc.name, resp.StatusCode)
		}
	}
}

// «Добавить блок» через htmx: фрагмент формы нового блока; блок из формы — перерисовка после выбора картинки.
func TestE2EAdminPageBlockFragment(t *testing.T) {
	a := newPageAdmin(t)
	hdr := http.Header{"Hx-Request": {"true"}, "X-Csrf-Token": {a.token}}

	resp, body := post(t, a.client, a.srv.URL+"/admin/pages/block?type=quote", nil, hdr)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(body), `<section class="block-form"`) {
		t.Fatalf("new block: status %d, body %.200q", resp.StatusCode, body)
	}
	v := formValues(parse(t, body).Selection)
	id := v.Get("block")
	if id == "" || v.Get("block-"+id+"-type") != "quote" {
		t.Errorf("new block fields: %v", v)
	}

	form := url.Values{}
	addBlock(form, "pic", "image", map[string]string{"alt": "Описание", "caption": "Подпись"})
	resp, body = post(t, a.client, a.srv.URL+"/admin/pages/block?id=pic", form, hdr)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("existing block: status %d", resp.StatusCode)
	}
	if v := formValues(parse(t, body).Selection); v.Get("block-pic-alt") != "Описание" || v.Get("block-pic-caption") != "Подпись" {
		t.Errorf("existing block lost fields: %v", v)
	}

	for _, path := range []string{"/admin/pages/block?type=carousel", "/admin/pages/block?id=nope", "/admin/pages/block"} {
		if resp, _ := post(t, a.client, a.srv.URL+path, form, hdr); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST %s: status %d, want 400", path, resp.StatusCode)
		}
	}
	if resp, _ := post(t, a.client, a.srv.URL+"/admin/pages/block?type=quote", nil, htmx); resp.StatusCode != http.StatusForbidden {
		t.Errorf("without CSRF token: status %d, want 403", resp.StatusCode)
	}
}

func TestE2EAdminPageValidation(t *testing.T) {
	a := newPageAdmin(t)
	const taken = "Страница с адресом «%s» уже есть (опубликованная или черновик): выберите другой slug."

	for _, tc := range []struct {
		name, title, slug string
		field, err        string
	}{
		{"duplicate slug", "Ещё о проекте", "about", "slug", strings.Replace(taken, "%s", "about", 1)},
		{"duplicate generated slug", "О проекте", "", "slug", strings.Replace(taken, "%s", "o-proekte", 1)},
		{"reserved slug", "Админ", "admin", "slug", "Адрес «/admin» занят разделом сайта: выберите другой slug."},
		{"reserved after normalize", "Статика", " Static ", "slug", "Адрес «/static» занят разделом сайта: выберите другой slug."},
		{"slug without letters", "Знаки", "!!!", "slug", "В slug нужны буквы или цифры."},
		{"title without letters", "???", "", "slug", "Из заголовка не получился адрес: укажите slug латиницей."},
		{"empty title", "  ", "x", "title", "Укажите заголовок."},
		{"long title", strings.Repeat("я", 201), "x", "title", "Заголовок длиннее 200 символов."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "duplicate generated slug" {
				// Сначала создаём черновик со slug o-proekte, затем ещё одну страницу с тем же заголовком.
				a.create(newPageForm(tc.title, ""), "save")
			}
			form := newPageForm(tc.title, tc.slug)
			addBlock(form, "keep", "text", map[string]string{"body": "Текст **сохраняется**"})

			// Обычная форма: 422 и страница целиком, введённые данные на месте.
			resp, html := a.submit("/admin/pages", form, "save", nil)
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
			if got := doc.Find(`textarea[name="block-keep-body"]`).Text(); got != "Текст **сохраняется**" {
				t.Errorf("block body not kept: %q", got)
			}
			if v, _ := doc.Find(`input[name="title"]`).Attr("value"); v != strings.TrimSpace(tc.title) {
				t.Errorf("title not kept: %q", v)
			}
			if got := text(doc.Find("#block-keep-body-preview strong")); got != "сохраняется" {
				t.Errorf("preview strong = %q", got)
			}

			// htmx: 422 и только фрагмент формы — его подставит hx-swap.
			resp, html = a.submit("/admin/pages", form, "save", htmx)
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

	// Ни одна ошибочная страница не сохранилась: страницы seed и черновик «О проекте» (o-proekte).
	_, html := a.get("/admin/pages")
	if n, want := parse(t, html).Find(".admin-table tbody tr").Length(), len(seedPages(nil))+1; n != want {
		t.Errorf("list rows = %d, want %d", n, want)
	}
}

// Ошибка в поле блока — у этого поля (#block-<id>-<поле>-error); остальные блоки и введённое на месте.
func TestE2EAdminPageBlockErrors(t *testing.T) {
	a := newPageAdmin(t)
	edit := a.editLink("Контакты")
	form := a.form(edit)
	before := blockIDs(form)
	addBlock(form, "q", "quote", map[string]string{"text": "  ", "author": "Автор"})
	addBlock(form, "img", "image", map[string]string{"image": "", "alt": "", "caption": "Подпись"})
	addBlock(form, "it", "image_text", map[string]string{"image": "nosuchimage", "alt": "x", "body": "Текст"})
	addBlock(form, "empty", "text", map[string]string{"body": "\r\n"})

	for _, header := range []http.Header{nil, htmx} {
		resp, html := a.submit(edit, form, "publish", header)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422", resp.StatusCode)
		}
		doc := parse(t, html)
		for id, msg := range map[string]string{
			"block-q-text":     "Введите цитату или удалите блок.",
			"block-img-image":  "Выберите изображение.",
			"block-img-alt":    "Опишите изображение для тех, кто его не видит (alt).",
			"block-it-image":   "Изображения нет в медиатеке: выберите другое.",
			"block-empty-body": "Введите текст или удалите блок.",
		} {
			if got := text(doc.Find("#" + id + "-error")); got != msg {
				t.Errorf("#%s-error = %q, want %q", id, got, msg)
			}
			if v, _ := doc.Find(`[name="` + id + `"]`).Attr("aria-invalid"); v != "true" {
				t.Errorf("%s: aria-invalid = %q", id, v)
			}
		}
		got := formValues(doc.Find("#page-form"))
		if n := len(blockIDs(got)); n != len(before)+4 {
			t.Errorf("blocks in form = %d, want %d", n, len(before)+4)
		}
		if got.Get("block-q-author") != "Автор" || got.Get("block-img-caption") != "Подпись" || got.Get("block-it-body") != "Текст" {
			t.Errorf("block fields not kept: %v", got)
		}
	}
	// Ничего не сохранилось.
	if got := blockIDs(a.form(edit)); len(got) != len(before) {
		t.Errorf("draft changed after errors: %v", got)
	}
}

func TestE2EAdminPagePreview(t *testing.T) {
	a := newPageAdmin(t)
	src := "# Заголовок\n\n**жирный** и [ссылка](javascript:alert(1))\n\n<script>alert(2)</script>"
	hdr := http.Header{"Hx-Request": {"true"}, "X-Csrf-Token": {a.token}}
	path := "/admin/pages/preview?field=block-x-body"

	resp, html := post(t, a.client, a.srv.URL+path, url.Values{"block-x-body": {src}}, hdr)
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
	_, html = post(t, a.client, a.srv.URL+path, url.Values{"block-x-body": {""}}, hdr)
	if got := text(parse(t, html).Find(".preview-empty")); got == "" {
		t.Errorf("empty preview: %q", html)
	}

	// Без CSRF-токена — 403.
	resp, _ = post(t, a.client, a.srv.URL+path, url.Values{"block-x-body": {src}}, htmx)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("preview without token: status %d, want 403", resp.StatusCode)
	}
}

func TestE2EAdminPageUnpublishDelete(t *testing.T) {
	a := newPageAdmin(t)
	edit := a.editLink("О проекте")

	// Опубликованную страницу удалить нельзя: ни через подтверждение, ни прямым POST.
	_, html := a.get(edit)
	if doc := parse(t, html); doc.Find(".danger-zone a").Length() != 0 || !strings.Contains(text(doc.Find(".danger-zone")), "сначала снимите её с публикации") {
		t.Errorf("published: want a hint instead of the delete link, got %q", text(doc.Find(".danger-zone")))
	}
	resp, html := a.get(edit + "/delete")
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("GET delete published: status %d, want 409", resp.StatusCode)
	}
	if got := text(parse(t, html).Find("main h1")); got != "Страница «О проекте» опубликована" {
		t.Errorf("blocked h1 = %q", got)
	}
	resp, _ = post(t, a.client, a.srv.URL+edit+"/delete", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("POST delete published: status %d, want 409", resp.StatusCode)
	}
	page(t, a.srv, "/about", http.StatusOK)

	// «Снять с публикации»: только с CSRF-токеном; страница пропадает с сайта и из меню, черновик остаётся.
	if resp, _ := post(t, a.client, a.srv.URL+edit+"/unpublish", nil, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("unpublish without token: status %d, want 403", resp.StatusCode)
	}
	resp, _ = post(t, a.client, a.srv.URL+edit+"/unpublish", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != edit+"?unpublished=1" {
		t.Fatalf("unpublish: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	page(t, a.srv, "/about", http.StatusNotFound)
	if page(t, a.srv, "/", http.StatusOK).Find(`.site-nav a[href="/about"]`).Length() != 0 {
		t.Error("unpublished page is in the menu")
	}
	_, html = a.get(edit + "?unpublished=1")
	doc := parse(t, html)
	if got := text(doc.Find(".admin-title .status")); got != "Черновик" {
		t.Errorf("status = %q", got)
	}
	if got := doc.Find(`textarea[name="block-about-intro-body"]`).Text(); !strings.Contains(got, "Keelkit") {
		t.Error("draft content lost after unpublish")
	}

	// Страница стоит в меню (seed): и подтверждение, и POST — 409 со ссылками на эти меню.
	pageID := strings.TrimPrefix(edit, "/admin/pages/")
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		var resp response
		if method == http.MethodGet {
			resp, html = a.get(edit + "/delete")
		} else {
			resp, html = post(t, a.client, a.srv.URL+edit+"/delete", url.Values{"csrf_token": {a.token}}, nil)
		}
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("%s delete of a page in a menu: status %d, want 409", method, resp.StatusCode)
		}
		var used []string
		parse(t, html).Find(".usage-list a").Each(func(_ int, l *goquery.Selection) {
			href, _ := l.Attr("href")
			used = append(used, text(l)+" "+href)
		})
		if got, want := strings.Join(used, ", "), "Меню в подвале /admin/menus/footer, Основное меню /admin/menus/main"; got != want {
			t.Errorf("%s delete: menus = %q, want %q", method, got, want)
		}
	}
	if _, err := a.site.db.Exec(`DELETE FROM menu_items WHERE page_id = ?`, pageID); err != nil {
		t.Fatal(err)
	}

	// Удаление неопубликованной: подтверждение, затем POST.
	resp, html = a.get(edit + "/delete")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET delete: status %d", resp.StatusCode)
	}
	doc = parse(t, html)
	if got := text(doc.Find("main h1")); got != "Удалить страницу «О проекте»?" {
		t.Errorf("confirm h1 = %q", got)
	}
	if href, _ := doc.Find(`main form a:contains("Отмена")`).Attr("href"); href != edit {
		t.Errorf("confirm cancel link = %q", href)
	}
	if resp, _ := post(t, a.client, a.srv.URL+edit+"/delete", nil, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("delete without token: status %d, want 403", resp.StatusCode)
	}
	resp, _ = post(t, a.client, a.srv.URL+edit+"/delete", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/pages?deleted=1" {
		t.Fatalf("delete: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, html = a.get("/admin/pages?deleted=1")
	if got := text(parse(t, html).Find(".flash")); got != "Страница удалена." {
		t.Errorf("flash = %q", got)
	}
	var n int
	if err := a.site.db.QueryRow(`SELECT count(*) FROM page_contents WHERE page_id = ?`, pageID).Scan(&n); err != nil || n != 0 {
		t.Errorf("page_contents rows after delete = %d, %v", n, err)
	}

	// Удалённой страницы больше нет ни для формы, ни для действий.
	for _, path := range []string{edit, edit + "/delete", edit + "/view", "/admin/pages/999999", "/admin/pages/abc", "/admin/pages/0"} {
		if resp, _ := a.get(path); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, resp.StatusCode)
		}
	}
	for _, path := range []string{edit + "/delete", edit + "/unpublish"} {
		if resp, _ := post(t, a.client, a.srv.URL+path, url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s: status %d, want 404", path, resp.StatusCode)
		}
	}
	if resp, _ := a.submit(edit, newPageForm("X", "x"), "save", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("update deleted: status %d, want 404", resp.StatusCode)
	}
}

// Главная всегда опубликована: не снимается с публикации, не удаляется, не меняет адрес.
func TestE2EAdminPageHome(t *testing.T) {
	a := newPageAdmin(t)
	edit := a.editLink("Добро пожаловать")

	_, html := a.get(edit)
	doc := parse(t, html)
	if doc.Find(`.danger-zone form`).Length() != 0 {
		t.Error("home: no unpublish button expected")
	}
	if got := text(doc.Find(".danger-zone")); !strings.Contains(got, "Главная страница всегда опубликована") {
		t.Errorf("home hint = %q", got)
	}
	resp, _ := post(t, a.client, a.srv.URL+edit+"/unpublish", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("unpublish home: status %d, want 409", resp.StatusCode)
	}
	if resp, _ := a.get(edit + "/delete"); resp.StatusCode != http.StatusConflict {
		t.Errorf("delete home: status %d, want 409", resp.StatusCode)
	}
	form := a.form(edit)
	form.Set("slug", "welcome")
	resp, html = a.submit(edit, form, "publish", nil)
	if got := text(parse(t, html).Find("#slug-error")); resp.StatusCode != http.StatusUnprocessableEntity || got != "Это главная страница: её адрес — «home»." {
		t.Errorf("home slug change: status %d, error %q", resp.StatusCode, got)
	}
	page(t, a.srv, "/", http.StatusOK)
}

// Маршруты из reservedSlugs перекрывают страницу с тем же slug: даже если она попала в БД в обход админки,
// по её адресу открывается раздел сайта, а не страница.
func TestE2EReservedSlugsShadowPages(t *testing.T) {
	site := migratedSite(t, false)
	db := site.db
	srv := httptest.NewServer(site.handler(config{}))
	t.Cleanup(srv.Close)

	for i, s := range reservedSlugs {
		ctx := context.Background()
		if _, err := db.ExecContext(ctx, `INSERT INTO pages (id) VALUES (?)`, i+1); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"draft", "published"} {
			if _, err := db.ExecContext(ctx, `INSERT INTO page_contents (page_id, kind, title, slug) VALUES (?, ?, ?, ?)`,
				i+1, kind, "Страница-"+s, s); err != nil {
				t.Fatal(err)
			}
		}
		resp, body := fetch(t, noRedirect, srv.URL+"/"+s)
		// Название страницы есть в меню любой HTML-страницы, поэтому смотрим на заголовок содержимого.
		if strings.Contains(body, "<html") && text(parse(t, body).Find("main h1")) == "Страница-"+s {
			t.Errorf("GET /%s: served the page (status %d), want the site route", s, resp.StatusCode)
		}
	}
}

// Изображения в блоках: адаптивная картинка на сайте, выбор из медиатеки в админке, ссылки в page_images
// и запрет удалить используемое изображение (со списком страниц).
func TestE2EPageImages(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)

	// /about: текст, картинка, картинка с текстом справа, текст, цитата, галерея — в порядке seed.
	doc := page(t, a.srv, "/about", http.StatusOK)
	var kinds []string
	doc.Find("main article .block").Each(func(_ int, s *goquery.Selection) {
		class, _ := s.Attr("class")
		kinds = append(kinds, strings.TrimPrefix(strings.Fields(class)[1], "block-"))
	})
	if got := strings.Join(kinds, ","); got != "text,image,image-text,text,quote,gallery" {
		t.Errorf("about blocks = %s", got)
	}
	if got := text(doc.Find("#block-about-side figcaption")); got != "PNG с прозрачностью" {
		t.Errorf("image_text caption = %q", got)
	}
	pic := doc.Find("#block-about-photo picture")
	srcset, _ := pic.Find(`source[type="image/webp"]`).Attr("srcset")
	if !strings.Contains(srcset, ".webp 480w") || !strings.Contains(srcset, ".webp 2400w") {
		t.Errorf("webp srcset = %q", srcset)
	}
	if alt, _ := pic.Find("img").Attr("alt"); alt != "Тестовая фотография из модуля images" {
		t.Errorf("alt = %q", alt)
	}
	if got := text(doc.Find("#block-about-photo figcaption")); got == "" {
		t.Error("no caption")
	}
	if doc.Find("#block-about-side.block-image-right picture").Length() != 1 {
		t.Error("image_text: no picture on the right")
	}
	// Адреса вариантов отдаёт модуль.
	src, _ := pic.Find("img").Attr("src")
	if resp, _ := fetch(t, a.client, a.srv.URL+src); resp.StatusCode != http.StatusOK {
		t.Errorf("GET %s: status %d", src, resp.StatusCode)
	}

	// В админке блок изображения — выбор из медиатеки с превью выбранного.
	edit := a.editLink("О проекте")
	_, html := a.get(edit)
	doc = parse(t, html)
	sel := doc.Find(`select[name="block-about-photo-image"]`)
	if v, _ := sel.Find("option[selected]").Attr("value"); v != imgs["photo.jpg"] {
		t.Errorf("selected image = %q, want photo.jpg (%s)", v, imgs["photo.jpg"])
	}
	if n := sel.Find("option").Length(); n != len(imgs)+1 {
		t.Errorf("image options = %d, want %d and «— выберите —»", n, len(imgs)+1)
	}
	if doc.Find(`#block-about-photo .block-thumb [data-image-for="desktop"] picture`).Length() != 1 {
		t.Error("no thumbnail of the selected image")
	}

	// Используемое изображение не удаляется: карточка показывает страницу (первой, затем посты seed с этой
	// обложкой), подтверждение и POST — 409.
	show := "/admin/media/" + imgs["photo.jpg"]
	_, html = a.get(show)
	if href, _ := parse(t, html).Find(".usage-list a").First().Attr("href"); href != edit {
		t.Errorf("usage link = %q, want %s", href, edit)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		var resp response
		if method == http.MethodGet {
			resp, html = a.get(show + "/delete")
		} else {
			resp, html = post(t, a.client, a.srv.URL+show+"/delete", url.Values{"csrf_token": {a.token}}, nil)
		}
		doc := parse(t, html)
		if resp.StatusCode != http.StatusConflict || text(doc.Find(".usage-list a").First()) != "Страница «О проекте»" {
			t.Errorf("%s delete used image: status %d, usage %q", method, resp.StatusCode, text(doc.Find(".usage-list")))
		}
	}

	// Новая страница с картинкой из формы: page_images у черновика, после публикации — и у опубликованной копии.
	form := newPageForm("Галерея", "")
	addBlock(form, "pic", "image", map[string]string{"image": imgs["small.jpg"], "alt": "Маленькая"})
	gallery := a.create(form, "save")
	id := strings.TrimPrefix(gallery, "/admin/pages/")
	refs := func() string {
		t.Helper()
		rows, err := site.db.Query(`SELECT kind FROM page_images WHERE page_id = ? AND image_id = ? ORDER BY kind`, id, imgs["small.jpg"])
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var kinds []string
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				t.Fatal(err)
			}
			kinds = append(kinds, k)
		}
		return strings.Join(kinds, ",")
	}
	if got := refs(); got != "draft" {
		t.Errorf("page_images after save = %q, want draft", got)
	}
	if resp, _ := a.submit(gallery, a.form(gallery), "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	if got := refs(); got != "draft,published" {
		t.Errorf("page_images after publish = %q", got)
	}
	if page(t, a.srv, "/galereya", http.StatusOK).Find("#block-pic picture img").Length() != 1 {
		t.Error("published image block has no picture")
	}

	// Скрытый блок тоже держит изображение. Блок убран из черновика — опубликованная копия всё ещё держит;
	// снята с публикации — изображение свободно.
	form = a.form(gallery)
	resp, body := a.submit(gallery, form, "hide:pic", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hide: status %d", resp.StatusCode)
	}
	if resp, _ := a.submit(gallery, formValues(parse(t, body).Find("#page-form")), "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save hidden: status %d", resp.StatusCode)
	}
	if got := refs(); got != "draft,published" {
		t.Errorf("hidden block: page_images = %q", got)
	}
	resp, body = a.submit(gallery, a.form(gallery), "remove:pic", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove: status %d", resp.StatusCode)
	}
	if resp, _ := a.submit(gallery, formValues(parse(t, body).Find("#page-form")), "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save removed: status %d", resp.StatusCode)
	}
	if got := refs(); got != "published" {
		t.Errorf("removed from draft: page_images = %q, want published", got)
	}
	if resp, _ := post(t, a.client, a.srv.URL+"/admin/media/"+imgs["small.jpg"]+"/delete", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("delete image of the published copy: status %d, want 409", resp.StatusCode)
	}
	if resp, _ := post(t, a.client, a.srv.URL+gallery+"/unpublish", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unpublish: status %d", resp.StatusCode)
	}
	if got := refs(); got != "" {
		t.Errorf("after unpublish: page_images = %q, want none", got)
	}
	resp, _ = post(t, a.client, a.srv.URL+"/admin/media/"+imgs["small.jpg"]+"/delete", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("delete unused image: status %d, want 303", resp.StatusCode)
	}
}

// Галерея — блок со списком элементов: добавление, порядок и удаление изображений кнопками, ошибка у поля
// элемента, публикация (слайды на сайте по порядку), используемое галереей изображение не удаляется из медиатеки.
func TestE2EAdminPageGallery(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)
	photo, small := imgs["photo.jpg"], imgs["small.jpg"]

	form := newPageForm("Галерея фото", "")
	addBlock(form, "g", "gallery", map[string]string{
		"i-a-image": photo, "i-a-alt": "Фото", "i-a-caption": "Первое",
	})
	form.Add("block-g-item", "a")
	edit := a.create(form, "save")

	apply := func(form url.Values, action string, header http.Header) url.Values {
		t.Helper()
		resp, body := a.submit(edit, form, action, header)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", action, resp.StatusCode)
		}
		return formValues(parse(t, body).Find("#page-form"))
	}
	items := func(form url.Values) []string { return form["block-g-item"] }

	// «Добавить изображение» — пустой элемент в конец; заполняем его и меняем порядок.
	form = apply(a.form(edit), "item-add:g", nil)
	ids := items(form)
	if len(ids) != 2 || ids[0] != "a" {
		t.Fatalf("after item-add = %v", ids)
	}
	b := ids[1]
	form.Set("block-g-i-"+b+"-image", small)
	form.Set("block-g-i-"+b+"-caption", "Второе")
	form = apply(form, "item-up:g:"+b, htmx)
	if got := strings.Join(items(form), ","); got != b+",a" {
		t.Errorf("after item-up = %s", got)
	}
	if form.Get("block-g-i-a-caption") != "Первое" || form.Get("block-g-i-"+b+"-caption") != "Второе" {
		t.Errorf("item fields lost: %v", form)
	}

	// Пустой alt у элемента — 422 и ошибка у поля этого элемента; введённое на месте.
	for _, hdr := range []http.Header{nil, htmx} {
		resp, body := a.submit(edit, form, "publish", hdr)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("empty alt: status %d, want 422", resp.StatusCode)
		}
		doc := parse(t, body)
		if got := text(doc.Find("#block-g-i-" + b + "-alt-error")); got == "" {
			t.Errorf("no error at item alt (htmx=%v)", hdr != nil)
		}
		if v, _ := doc.Find(`input[name="block-g-i-` + b + `-alt"]`).Attr("aria-invalid"); v != "true" {
			t.Errorf("item alt not marked invalid")
		}
		if got := formValues(doc.Find("#page-form")).Get("block-g-i-a-caption"); got != "Первое" {
			t.Errorf("caption lost on error: %q", got)
		}
	}

	form.Set("block-g-i-"+b+"-alt", "Маленькое")
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	captions := func() string {
		var got []string
		page(t, a.srv, "/galereya-foto", http.StatusOK).Find("#block-g .gallery-slide figcaption").Each(func(_ int, s *goquery.Selection) {
			got = append(got, text(s))
		})
		return strings.Join(got, ",")
	}
	if got := captions(); got != "Второе,Первое" {
		t.Errorf("slides = %s, want Второе,Первое", got)
	}

	// Изображение из галереи не удаляется из медиатеки: 409.
	resp, _ := post(t, a.client, a.srv.URL+"/admin/media/"+small+"/delete", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("delete image used by gallery: status %d, want 409", resp.StatusCode)
	}

	// Убрать элемент и опубликовать — слайда больше нет.
	form = apply(a.form(edit), "item-remove:g:"+b, nil)
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish after remove: status %d", resp.StatusCode)
	}
	if got := captions(); got != "Первое" {
		t.Errorf("slides after remove = %s", got)
	}

	// Подделанные id элементов и действие над чужим блоком — 400.
	for _, tc := range []struct {
		name   string
		form   url.Values
		action string
	}{
		{"bad item id", url.Values{"title": {"x"}, "block": {"g"}, "block-g-type": {"gallery"}, "block-g-item": {"<b>"}}, "save"},
		{"duplicate item", url.Values{"title": {"x"}, "block": {"g"}, "block-g-type": {"gallery"}, "block-g-item": {"a", "a"}}, "save"},
		{"unknown item", a.form(edit), "item-up:g:zzz"},
		{"unknown block", a.form(edit), "item-add:zzz"},
	} {
		if resp, _ := a.submit(edit, tc.form, tc.action, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", tc.name, resp.StatusCode)
		}
	}
}

// Своя картинка для телефона у блоков image, image_text и элемента галереи: сохранение, ошибки, публикация копирует
// её вместе со ссылками, на сайте — <source media>, используемую нельзя удалить из медиатеки.
func TestE2EPageMobileImages(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)
	photo, small, png, webp := imgs["photo.jpg"], imgs["small.jpg"], imgs["transparent.png"], imgs["orientation-6.webp"]

	form := newPageForm("Кадры", "")
	addBlock(form, "pic", "image", map[string]string{"image": photo, "image_mobile": small, "alt": "Фото"})
	addBlock(form, "side", "image_text", map[string]string{"image": photo, "image_mobile": png, "alt": "Фото", "body": "Текст"})
	addBlock(form, "g", "gallery", map[string]string{"i-a-image": photo, "i-a-image_mobile": webp, "i-a-alt": "Слайд"})
	form.Add("block-g-item", "a")
	edit := a.create(form, "save")
	id := strings.TrimPrefix(edit, "/admin/pages/")

	refs := func(img string) string {
		t.Helper()
		rows, err := site.db.Query(`SELECT kind FROM page_images WHERE page_id = ? AND image_id = ? ORDER BY kind`, id, img)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var kinds []string
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				t.Fatal(err)
			}
			kinds = append(kinds, k)
		}
		return strings.Join(kinds, ",")
	}
	for _, img := range []string{small, png, webp} {
		if got := refs(img); got != "draft" {
			t.Errorf("page_images of mobile %s after save = %q, want draft", img, got)
		}
	}

	// Форма: выбранная мобильная картинка и метка у каждого блока и элемента.
	_, html := a.get(edit)
	doc := parse(t, html)
	for name, want := range map[string]string{
		"block-pic-image_mobile":   small,
		"block-side-image_mobile":  png,
		"block-g-i-a-image_mobile": webp,
	} {
		if v, _ := doc.Find(`select[name="` + name + `"] option[selected]`).Attr("value"); v != want {
			t.Errorf("%s = %q, want %q", name, v, want)
		}
	}
	if n := doc.Find(".status-mobile").Length(); n != 3 {
		t.Errorf("«Своя для телефона» badges = %d, want 3", n)
	}

	// Публикация копирует мобильные картинки и их ссылки; на сайте — <source media> первыми.
	if resp, _ := a.submit(edit, a.form(edit), "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	if got := refs(small); got != "draft,published" {
		t.Errorf("page_images of mobile after publish = %q", got)
	}
	doc = page(t, a.srv, "/kadry", http.StatusOK)
	for _, sel := range []string{"#block-pic picture", "#block-side picture", "#block-g picture"} {
		media, _ := doc.Find(sel + " source").First().Attr("media")
		if media != web.MobileMedia {
			t.Errorf("%s: first <source> media = %q, want %q", sel, media, web.MobileMedia)
		}
		if n := doc.Find(sel + " source[media]").Length(); n != 2 {
			t.Errorf("%s: <source media> = %d, want 2 (WebP and JPEG)", sel, n)
		}
	}
	srcset, _ := doc.Find("#block-pic source[media][type='image/jpeg']").Attr("srcset")
	if !strings.Contains(srcset, small) {
		t.Errorf("mobile srcset = %q, want variants of small.jpg (%s)", srcset, small)
	}
	// og:image — основная, а не мобильная.
	if og, _ := doc.Find(`meta[property="og:image"]`).Attr("content"); !strings.Contains(og, photo) {
		t.Errorf("og:image = %q, want the main image %s", og, photo)
	}

	// Используемая мобильная картинка не удаляется из медиатеки: 409.
	for _, img := range []string{small, png, webp} {
		resp, _ := post(t, a.client, a.srv.URL+"/admin/media/"+img+"/delete", url.Values{"csrf_token": {a.token}}, nil)
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("delete mobile image %s in use: status %d, want 409", img, resp.StatusCode)
		}
	}

	// Мобильная без основной — ошибка у поля (форма и htmx); убрать мобильную — на сайте прежняя разметка.
	form = a.form(edit)
	form.Set("block-pic-image", "")
	for _, hdr := range []http.Header{nil, htmx} {
		resp, body := a.submit(edit, form, "save", hdr)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("mobile without main: status %d, want 422", resp.StatusCode)
		}
		if got := text(parse(t, body).Find("#block-pic-image_mobile-error")); got != "Сначала выберите основную картинку." {
			t.Errorf("mobile without main: error %q (htmx=%v)", got, hdr != nil)
		}
	}
	form = a.form(edit)
	form.Set("block-pic-image_mobile", "")
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish without mobile: status %d", resp.StatusCode)
	}
	if n := page(t, a.srv, "/kadry", http.StatusOK).Find("#block-pic source[media]").Length(); n != 0 {
		t.Errorf("mobile removed: <source media> = %d, want 0", n)
	}
	if got := refs(small); got != "" {
		t.Errorf("page_images of removed mobile = %q, want none", got)
	}
}

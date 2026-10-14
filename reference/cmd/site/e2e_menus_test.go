package main

// e2e меню (internal/menu): раздел админки (список меню, форма с кнопками пунктов — формой и через htmx, ошибки
// у полей пункта), вывод в шапке и подвале: адрес страницы по её текущему slug, подпункты вручную и из записей
// раздела, пункт на неопубликованную страницу не виден.

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/keelkit/keelkit/reference/internal/listfield"
	"github.com/keelkit/keelkit/reference/internal/menu"
)

// menuForm — поля формы меню name так, как их отправит браузер.
func (a *pageAdmin) menuForm(name string) url.Values {
	a.t.Helper()
	return a.formByID(menu.EditPath(name), "menu-form")
}

// menuApply — кнопка пункта: форма возвращается изменённой (200, плашка «не сохранены»); с htmx — только форма.
func (a *pageAdmin) menuApply(name string, form url.Values, op, item string, header http.Header) url.Values {
	a.t.Helper()
	action := listfield.ActionValue(op, menu.ItemsField, item)
	resp, html := a.submit(menu.EditPath(name), form, action, header)
	if resp.StatusCode != http.StatusOK {
		a.t.Fatalf("%s: status %d\n%.500s", action, resp.StatusCode, html)
	}
	if isPage := strings.Contains(html, "<html"); isPage == (header != nil) {
		a.t.Errorf("%s (htmx %v): full page = %v", action, header != nil, isPage)
	}
	doc := parse(a.t, html)
	if doc.Find(".flash-warning").Length() != 1 {
		a.t.Errorf("%s: no unsaved warning", action)
	}
	return formValues(doc.Find("#menu-form"))
}

// menuSave сохраняет форму меню name: 303 на форму с ?saved=1.
func (a *pageAdmin) menuSave(name string, form url.Values) {
	a.t.Helper()
	resp, html := a.submit(menu.EditPath(name), form, "save", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != menu.EditPath(name)+"?saved=1" {
		a.t.Fatalf("save menu %s: status %d, Location %q\n%.500s", name, resp.StatusCode, resp.Header.Get("Location"), html)
	}
}

// siteMenu — пункты меню на главной (menuLinks): nav — .site-nav (шапка) или .footer-nav (подвал).
func (a *pageAdmin) siteMenu(nav string) string {
	a.t.Helper()
	return menuLinks(page(a.t, a.srv, "/", http.StatusOK).Find(nav))
}

func menuField(id, field string) string { return listfield.Name(menu.ItemsField, id, field) }

func TestE2EAdminMenus(t *testing.T) {
	a := newPageAdmin(t)
	order := listfield.Order(menu.ItemsField)
	const cats = "Go " + blogPath + "/category/go, Веб " + blogPath + "/category/web, Заметки " + blogPath + "/category/notes"

	// Раздел — в группе «Сайт» бокового меню: меню из site.yaml и число пунктов.
	_, html := a.get(menu.AdminPath)
	doc := parse(t, html)
	if cur := text(doc.Find(`.admin-sidebar a[aria-current="page"]`)); cur != "Меню" {
		t.Errorf("current sidebar item = %q", cur)
	}
	var rows []string
	doc.Find(".admin-table tbody tr").Each(func(_ int, tr *goquery.Selection) {
		href, _ := tr.Find("a").Attr("href")
		rows = append(rows, text(tr.Find("td").First())+" "+href+" "+text(tr.Find("td").Last()))
	})
	if got, want := strings.Join(rows, ", "), "Основное меню /admin/menus/main 3, Меню в подвале /admin/menus/footer 3"; got != want {
		t.Errorf("menus = %q, want %q", got, want)
	}
	if resp, _ := a.get(menu.AdminPath + "/sidebar"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown menu: status %d, want 404", resp.StatusCode)
	}
	if resp, _ := a.submit(menu.AdminPath+"/sidebar", url.Values{}, "save", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST unknown menu: status %d, want 404", resp.StatusCode)
	}

	// Форма из seed: страницы по id, блог с подпунктами из записей.
	form := a.menuForm("main")
	ids := form[order]
	if len(ids) != 3 {
		t.Fatalf("main menu items = %v", form)
	}
	about, contacts, blog := ids[0], ids[1], ids[2]
	if !strings.HasPrefix(form.Get(menuField(about, "target")), "page:") ||
		form.Get(menuField(blog, "target")) != menu.SectionTarget("blog") || form.Get(menuField(blog, "children")) != "1" {
		t.Errorf("form from seed: %v", form)
	}

	// Смена slug страницы: пункт ведёт на новый адрес (страница — по id).
	edit := a.editLink("О проекте")
	pf := a.form(edit)
	pf.Set("slug", "o-proekte")
	if resp, _ := a.submit(edit, pf, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish new slug: status %d", resp.StatusCode)
	}
	if got, want := a.siteMenu(".site-nav"), "О проекте /o-proekte, Контакты /contacts, Блог "+blogPath+" ["+cats+"]"; got != want {
		t.Errorf("menu after slug change = %q, want %q", got, want)
	}

	// Кнопки без сохранения: «Добавить» (htmx), пункт со своим адресом «Вложить» под блог (форма),
	// блог с подпунктом «↑» (группой), своё название у страницы.
	form = a.menuApply("main", form, "add", "", htmx)
	price := form[order][3]
	form.Set(menuField(price, "target"), menu.URLTarget)
	form.Set(menuField(price, "title"), " Прайс ")
	form.Set(menuField(price, "url"), " /price ")
	form = a.menuApply("main", form, "indent", price, nil)
	if form.Get(menuField(price, "child")) != "1" {
		t.Fatalf("after indent: %v", form)
	}
	form = a.menuApply("main", form, "up", blog, htmx)
	if got := form[order]; !slices.Equal(got, []string{about, blog, price, contacts}) {
		t.Fatalf("after up = %v", got)
	}
	form.Set(menuField(about, "title"), "Компания")
	if got := a.siteMenu(".site-nav"); !strings.HasPrefix(got, "О проекте /o-proekte, Контакты") {
		t.Errorf("menu changed before save: %q", got)
	}
	a.menuSave("main", form)
	// Подпункты вручную идут раньше подпунктов из записей.
	if got, want := a.siteMenu(".site-nav"), "Компания /o-proekte, Блог "+blogPath+" [Прайс /price, "+cats+"], Контакты /contacts"; got != want {
		t.Errorf("menu after save = %q, want %q", got, want)
	}

	// Сохранённая форма — в том же порядке (id пунктов — новые: меню перезаписывается целиком); без подпунктов
	// из записей и со «Вынести» — категорий нет, «Прайс» снова пункт верхнего уровня.
	form = a.menuForm("main")
	ids = form[order]
	if len(ids) != 4 || form.Get(menuField(ids[2], "child")) != "1" || form.Get(menuField(ids[2], "url")) != "/price" ||
		form.Get(menuField(ids[0], "title")) != "Компания" {
		t.Fatalf("saved form: %v", form)
	}
	form.Del(menuField(ids[1], "children"))
	form = a.menuApply("main", form, "outdent", ids[2], nil)
	form.Set(menuField(ids[0], "title"), "")
	a.menuSave("main", form)
	if got, want := a.siteMenu(".site-nav"), "О проекте /o-proekte, Блог "+blogPath+", Прайс /price, Контакты /contacts"; got != want {
		t.Errorf("menu without auto children = %q, want %q", got, want)
	}

	// Неопубликованная страница: пункт не виден на сайте, в админке помечен.
	contactsEdit := a.editLink("Контакты")
	if resp, _ := post(t, a.client, a.srv.URL+contactsEdit+"/unpublish", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unpublish: status %d", resp.StatusCode)
	}
	if got, want := a.siteMenu(".site-nav"), "О проекте /o-proekte, Блог "+blogPath+", Прайс /price"; got != want {
		t.Errorf("menu with an unpublished page = %q, want %q", got, want)
	}
	// В подвале «Контакты» — подпункт «О проекте»: пропал только он.
	if got, want := a.siteMenu(".footer-nav"), "О проекте /o-proekte, RSS /rss.xml"; got != want {
		t.Errorf("footer with an unpublished page = %q, want %q", got, want)
	}
	_, html = a.get(menu.EditPath("main"))
	doc = parse(t, html)
	if got := text(doc.Find(".menu-item .status")); got != "Страница не опубликована" {
		t.Errorf("unpublished label = %q", got)
	}
	if got := text(doc.Find(`option[value="` + form.Get(menuField(ids[3], "target")) + `"]`).First()); got != "Контакты (не опубликована)" {
		t.Errorf("page option = %q", got)
	}

	// Пустое меню не выводится, в списке — 0 пунктов.
	form = a.menuForm("footer")
	form.Del(order)
	a.menuSave("footer", form)
	if n := page(t, a.srv, "/", http.StatusOK).Find(".footer-nav").Length(); n != 0 {
		t.Error("empty footer menu is on the site")
	}
	_, html = a.get(menu.AdminPath)
	if got := text(parse(t, html).Find(".admin-table tbody tr").Last().Find("td").Last()); got != "0" {
		t.Errorf("footer items = %q, want 0", got)
	}
}

// Ошибки пункта — у его поля, 422 (формой и через htmx); пустой пункт отбрасывается; испорченная форма или
// кнопка — 400.
func TestE2EAdminMenusValidation(t *testing.T) {
	a := newPageAdmin(t)
	order := listfield.Order(menu.ItemsField)
	base := a.menuForm("main")
	id := base[order][0]

	cases := []struct {
		what   string
		fields map[string]string
		field  string
		want   string
	}{
		{"no target", map[string]string{"target": "", "title": "Пункт"}, "target", "Выберите"},
		{"unknown target", map[string]string{"target": "post:1"}, "target", "Выберите"},
		{"deleted page", map[string]string{"target": menu.PageTarget(999999)}, "target", "страницы больше нет"},
		{"unknown section", map[string]string{"target": menu.SectionTarget("shop")}, "target", "раздела больше нет"},
		{"url without title", map[string]string{"target": menu.URLTarget, "title": " ", "url": "/price"}, "title", "Укажите название"},
		{"no url", map[string]string{"target": menu.URLTarget, "title": "Прайс", "url": ""}, "url", "Укажите адрес"},
		{"javascript url", map[string]string{"target": menu.URLTarget, "title": "Прайс", "url": "javascript:alert(1)"}, "url", "https://"},
		{"protocol-relative url", map[string]string{"target": menu.URLTarget, "title": "Прайс", "url": "//evil.example"}, "url", "https://"},
		{"long title", map[string]string{"title": strings.Repeat("я", 101)}, "title", "длиннее 100"},
	}
	for _, c := range cases {
		for _, header := range []http.Header{nil, htmx} {
			form := a.menuForm("main")
			for k, v := range c.fields {
				form.Set(menuField(id, k), v)
			}
			resp, html := a.submit(menu.EditPath("main"), form, "", header)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("%s (htmx %v): status %d, want 422", c.what, header != nil, resp.StatusCode)
				continue
			}
			if isPage := strings.Contains(html, "<html"); isPage == (header != nil) {
				t.Errorf("%s (htmx %v): full page = %v", c.what, header != nil, isPage)
			}
			doc := parse(t, html)
			name := menuField(id, c.field)
			if got := text(doc.Find("#" + name + "-error")); !strings.Contains(got, c.want) {
				t.Errorf("%s: error %q, want %q", c.what, got, c.want)
			}
			if inv, _ := doc.Find(`[name="` + name + `"]`).Attr("aria-invalid"); inv != "true" {
				t.Errorf("%s: no aria-invalid", c.what)
			}
		}
	}

	// Добавленный и нетронутый пункт при сохранении отбрасывается.
	form := a.menuApply("main", a.menuForm("main"), "add", "", nil)
	a.menuSave("main", form)
	if got := a.menuForm("main")[order]; len(got) != 3 {
		t.Errorf("items after saving an empty one = %v", got)
	}

	id = a.menuForm("main")[order][0]
	bad := []struct {
		what   string
		edit   func(url.Values)
		action string
	}{
		{"unknown item", nil, listfield.ActionValue("up", menu.ItemsField, "zzz")},
		{"unknown list field", nil, listfield.ActionValue("up", "social", id)},
		{"unknown op", nil, listfield.ActionValue("hide", menu.ItemsField, id)},
		{"add with item", nil, listfield.ActionValue("add", menu.ItemsField, id)},
		{"unknown action", nil, "publish"},
		{"bad item id", func(f url.Values) { f.Add(order, "Bad id!") }, ""},
		{"duplicate item id", func(f url.Values) { f.Add(order, id) }, ""},
	}
	for _, b := range bad {
		form := a.menuForm("main")
		if b.edit != nil {
			b.edit(form)
		}
		if resp, _ := a.submit(menu.EditPath("main"), form, b.action, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", b.what, resp.StatusCode)
		}
	}
	// Ничего не сохранилось: меню на сайте — из seed.
	if got := a.siteMenu(".site-nav"); !strings.HasPrefix(got, "О проекте /about, Контакты /contacts, Блог "+blogPath+" [") {
		t.Errorf("menu after errors = %q", got)
	}
}

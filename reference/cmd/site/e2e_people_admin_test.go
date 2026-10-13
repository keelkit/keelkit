package main

// e2e раздела «Люди» (тип без своего адреса, feature order) и блока-списка people: порядок кнопками ↑ ↓ (формой
// и htmx), создание и ошибки полей, фото из медиатеки, удаление; вывод на странице «Команда» в порядке из админки.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// teamNames — имена людей на странице «Команда» по порядку.
func teamNames(t *testing.T, a *pageAdmin) []string {
	t.Helper()
	var names []string
	page(t, a.srv, "/team", http.StatusOK).Find("#block-team-people .person-name").Each(func(_ int, s *goquery.Selection) {
		names = append(names, text(s))
	})
	return names
}

// personID — id человека из списка в админке по имени (строка — tr#person-<id>).
func personID(t *testing.T, a *pageAdmin, name string) string {
	t.Helper()
	_, html := a.get("/admin/people")
	id, ok := parse(t, html).Find(`.admin-table tr:has(a:contains("` + name + `"))`).Attr("id")
	if !ok {
		t.Fatalf("no row for %s", name)
	}
	return strings.TrimPrefix(id, "person-")
}

func TestE2EPeopleOrder(t *testing.T) {
	a := newPageAdmin(t)
	want := "Анна Петрова,Борис Иванов,Вера Смирнова"
	if got := strings.Join(teamNames(t, a), ","); got != want {
		t.Fatalf("team = %s, want %s", got, want)
	}
	doc := page(t, a.srv, "/team", http.StatusOK)
	if got := text(doc.Find("#block-team-people.block-list.block-people .person-role").First()); got != "Руководитель проекта" {
		t.Errorf("role = %q", got)
	}
	if doc.Find("#block-team-people .person-nophoto").Length() != 3 {
		t.Error("seed without images: every person needs a placeholder")
	}

	// Список в админке — в том же порядке; у первого «↑» и у последнего «↓» выключены.
	_, html := a.get("/admin/people")
	list := parse(t, html)
	if n := list.Find(".admin-table tbody tr").Length(); n != 3 {
		t.Fatalf("rows = %d", n)
	}
	if _, ok := list.Find(`tbody tr:first-child button[value^="up:"]`).Attr("disabled"); !ok {
		t.Error("first row: ↑ must be disabled")
	}
	if _, ok := list.Find(`tbody tr:last-child button[value^="down:"]`).Attr("disabled"); !ok {
		t.Error("last row: ↓ must be disabled")
	}

	// Без JavaScript: Вера ↑ — 303 на список, порядок на сайте сразу другой.
	vera := personID(t, a, "Вера Смирнова")
	resp, _ := a.submit("/admin/people/order", nil, "up:"+vera, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/people" {
		t.Fatalf("order: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if got := strings.Join(teamNames(t, a), ","); got != "Анна Петрова,Вера Смирнова,Борис Иванов" {
		t.Errorf("after up: %s", got)
	}

	// htmx: Анна ↓ — в ответе только таблица в новом порядке.
	resp, body := a.submit("/admin/people/order", nil, "down:"+personID(t, a, "Анна Петрова"), htmx)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("order htmx: status %d", resp.StatusCode)
	}
	frag := parse(t, body)
	if frag.Find("#people-order").Length() != 1 || frag.Find(".admin-title").Length() != 0 {
		t.Errorf("order htmx: want the table fragment only\n%.300s", body)
	}
	var rows []string
	frag.Find(".admin-table tbody td a").Each(func(_ int, s *goquery.Selection) { rows = append(rows, text(s)) })
	if got := strings.Join(rows, ","); got != "Вера Смирнова,Анна Петрова,Борис Иванов" {
		t.Errorf("fragment rows = %s", got)
	}
	if got := strings.Join(teamNames(t, a), ","); got != "Вера Смирнова,Анна Петрова,Борис Иванов" {
		t.Errorf("after down: %s", got)
	}

	// Крайний в нужную сторону остаётся на месте.
	a.submit("/admin/people/order", nil, "up:"+vera, nil)
	if got := teamNames(t, a)[0]; got != "Вера Смирнова" {
		t.Errorf("first after up of the first = %s", got)
	}

	// Испорченное действие — 400, чужой id — 404.
	for _, action := range []string{"", "left:" + vera, "up:abc", "up"} {
		if resp, _ := a.submit("/admin/people/order", nil, action, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("action %q: status %d, want 400", action, resp.StatusCode)
		}
	}
	if resp, _ := a.submit("/admin/people/order", nil, "up:99999", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: status %d, want 404", resp.StatusCode)
	}
}

var personURL = regexp.MustCompile(`^/admin/people/(\d+)\?saved=1$`)

func TestE2EAdminPeopleCRUD(t *testing.T) {
	a := newPageAdmin(t)

	_, body := a.get("/admin")
	if got := text(parse(t, body).Find(`.counter:contains("Людей")`)); got != "Людей: 3" {
		t.Errorf("counter = %q", got)
	}

	// Ошибки — 422 у поля: без htmx — страница, с htmx — только форма; введённое сохраняется.
	long := strings.Repeat("я", 201)
	for _, header := range []http.Header{nil, htmx} {
		resp, body := a.submit("/admin/people", url.Values{"name": {"  "}, "role": {long}, "photo": {"nope"}}, "", header)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("invalid: status %d", resp.StatusCode)
		}
		doc := parse(t, body)
		if (doc.Find(".admin-title").Length() == 0) != (header != nil) {
			t.Errorf("htmx=%v: page vs fragment mismatch", header != nil)
		}
		for field, msg := range map[string]string{"name": "Укажите имя.", "role": "Должность длиннее 200 символов.",
			"photo": "Изображения нет в медиатеке: выберите другое."} {
			if got := text(doc.Find("#" + field + "-error")); got != msg {
				t.Errorf("%s error = %q, want %q", field, got, msg)
			}
		}
		if v, _ := doc.Find(`input[name="role"]`).Attr("value"); v != long {
			t.Error("role value lost")
		}
	}

	// Создание: имя без лишних пробелов, человек — в конце списка и на сайте.
	resp, _ := a.submit("/admin/people", url.Values{"name": {"  Глеб   Орлов "}, "role": {"Тестировщик"}}, "", nil)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !personURL.MatchString(loc) {
		t.Fatalf("create: status %d, Location %q", resp.StatusCode, loc)
	}
	edit := strings.TrimSuffix(loc, "?saved=1")
	if names := teamNames(t, a); names[len(names)-1] != "Глеб Орлов" {
		t.Errorf("team = %v, want the new person last", names)
	}
	_, body = a.get(loc)
	if got := text(parse(t, body).Find(".flash")); !strings.Contains(got, "Сохранено") {
		t.Errorf("flash = %q", got)
	}

	// Правка через htmx — 204 и HX-Redirect.
	form := a.formByID(edit, "person-form")
	form.Set("role", "Ведущий тестировщик")
	resp, _ = a.submit(edit, form, "", htmx)
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("HX-Redirect") != edit+"?saved=1" {
		t.Errorf("update htmx: status %d, HX-Redirect %q", resp.StatusCode, resp.Header.Get("HX-Redirect"))
	}
	if got := text(page(t, a.srv, "/team", http.StatusOK).Find(`.person:contains("Глеб Орлов") .person-role`)); got != "Ведущий тестировщик" {
		t.Errorf("role on site = %q", got)
	}

	// Удаление — через подтверждение; со страницы человек пропадает.
	if resp, _ := a.get(edit + "/delete"); resp.StatusCode != http.StatusOK {
		t.Errorf("confirm delete: status %d", resp.StatusCode)
	}
	resp, _ = a.submit(edit+"/delete", nil, "", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/people?deleted=1" {
		t.Errorf("delete: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if strings.Contains(strings.Join(teamNames(t, a), ","), "Глеб") {
		t.Error("deleted person still on the site")
	}
	for _, p := range []string{edit, edit + "/delete", "/admin/people/abc"} {
		if resp, _ := a.get(p); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", p, resp.StatusCode)
		}
	}
}

// Блок people на странице: форма админки ссылается на раздел, скрытый блок не выводит людей, без людей — пусто.
func TestE2EPeopleBlock(t *testing.T) {
	a := newPageAdmin(t)
	edit := a.editLink("Команда")
	_, body := a.get(edit)
	doc := parse(t, body)
	if href, _ := doc.Find(`#block-team-people a[href="/admin/people"]`).Attr("href"); href == "" {
		t.Error("people block form: no link to the section")
	}

	// Предпросмотр черновика тоже выводит людей.
	if n := parse(t, func() string { _, b := a.get(edit + "/view"); return b }()).Find(".person").Length(); n != 3 {
		t.Errorf("preview: %d people", n)
	}

	form := a.form(edit)
	form.Set("block-team-people-hidden", "1")
	if resp, _ := a.submit(edit, form, "publish", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: status %d", resp.StatusCode)
	}
	if doc := page(t, a.srv, "/team", http.StatusOK); doc.Find(".people-list, #block-team-people").Length() != 0 {
		t.Error("hidden people block is on the site")
	}

	form.Del("block-team-people-hidden")
	a.submit(edit, form, "publish", nil)
	if _, err := a.site.db.Exec(`DELETE FROM people`); err != nil {
		t.Fatal(err)
	}
	doc = page(t, a.srv, "/team", http.StatusOK)
	if doc.Find("#block-team-people").Length() != 1 || doc.Find(".people-list").Length() != 0 {
		t.Error("no people: want an empty block without the list")
	}
}

// Фото человека: выбор через htmx перерисовывает поле, на сайте — картинка, медиатека не даёт удалить фото.
func TestE2EPeoplePhoto(t *testing.T) {
	site, imgs := migratedSiteWithImages(t)
	a := newPageAdminOn(t, site)
	photo := imgs["photo.jpg"]

	resp, body := a.submit("/admin/people/photo", url.Values{"photo": {imgs["transparent.png"]}}, "", htmx)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("photo field: status %d", resp.StatusCode)
	}
	frag := parse(t, body)
	if frag.Find("#photo-field .block-thumb picture").Length() != 1 {
		t.Errorf("photo field: no preview\n%.300s", body)
	}
	if v, _ := frag.Find(`option[selected]`).Attr("value"); v != imgs["transparent.png"] {
		t.Errorf("selected = %q", v)
	}

	doc := page(t, a.srv, "/team", http.StatusOK)
	if doc.Find(`.person:contains("Анна Петрова") picture img[alt=""]`).Length() != 1 {
		t.Error("Анна: no decorative photo on the site")
	}

	_, body = a.get("/admin/media/" + photo)
	if !strings.Contains(text(parse(t, body).Find(".usage-list")), "Люди: Анна Петрова") {
		t.Error("media card: person not in usage list")
	}
	resp, body = a.submit("/admin/media/"+photo+"/delete", nil, "", nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body, "Анна Петрова") {
		t.Errorf("delete used photo: status %d", resp.StatusCode)
	}
}

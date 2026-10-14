package menu

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/keelkit/keelkit/reference/internal/admin"
	"github.com/keelkit/keelkit/reference/internal/database"
	"github.com/keelkit/keelkit/reference/internal/listfield"
	"github.com/keelkit/keelkit/reference/internal/web"
)

// AdminPath — раздел меню в админке: список меню, форма меню — EditPath.
const AdminPath = "/admin/menus"

// EditPath — форма меню name в админке.
func EditPath(name string) string { return AdminPath + "/" + url.PathEscape(name) }

// ItemsField — поле формы с пунктами меню (listfield): имена полей пунктов и ключ общей ошибки.
const ItemsField = "items"

// Ограничения полей (символов) и формы.
const (
	maxTitleLen = 100
	maxURLLen   = 500
	maxFormSize = 64 << 10 // байт
)

// Admin — раздел «Меню»: список меню из site.yaml и форма каждого, правки сразу на сайте.
// Маршруты оборачиваются в admin.RequireUser: сессия и CSRF-токен уже проверены.
type Admin struct {
	db       *sql.DB
	q        *Queries
	menus    []Menu
	sections []Section
}

// NewAdmin создаёт раздел. menus — меню из site.yaml по порядку, sections — разделы, на которые может вести пункт.
func NewAdmin(db *sql.DB, menus []Menu, sections []Section) *Admin {
	return &Admin{db: db, q: New(db), menus: menus, sections: sections}
}

// menuSummary — строка списка меню.
type menuSummary struct {
	Menu
	Items int64
}

// List — GET /admin/menus: меню сайта и число пунктов в каждом.
func (a *Admin) List(w http.ResponseWriter, r *http.Request) {
	counts, err := a.q.CountMenuItems(r.Context())
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	list := make([]menuSummary, 0, len(a.menus))
	for _, m := range a.menus {
		s := menuSummary{Menu: m}
		for _, c := range counts {
			if c.Menu == m.Name {
				s.Items = c.Items
			}
		}
		list = append(list, s)
	}
	web.Render(w, r, http.StatusOK, ListPage(list))
}

// menuForm — форма меню.
type menuForm struct {
	Menu     Menu
	Items    []Item
	Pages    []ListMenuPagesRow // выбор страницы: все страницы по заголовку черновика
	Sections []Section
	Errors   map[string]string
	Flash    string
	Unsaved  bool // форму изменила кнопка пункта (↑, ↓, «Вложить», «Убрать»…), но не сохранила
}

// Edit — GET /admin/menus/{name}; неизвестное меню — 404 админки.
func (a *Admin) Edit(w http.ResponseWriter, r *http.Request) {
	m, ok := a.menu(w, r)
	if !ok {
		return
	}
	items, err := Load(r.Context(), a.q, m.Name)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f := menuForm{Menu: m, Items: items}
	if r.URL.Query().Has("saved") {
		f.Flash = "Сохранено. Меню уже на сайте."
	}
	if err := a.env(r.Context(), &f); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, FormPage(f))
}

// Update — POST /admin/menus/{name}. Кнопка — поле action: пусто или "save" сохраняет, действие над пунктом
// (listfield.ParseAction: up, down, indent, outdent, remove, add) возвращает форму изменённой без сохранения (200).
// Ошибки — 422 (с htmx — только форма), успех — 303 на форму с ?saved=1 (с htmx — HX-Redirect).
func (a *Admin) Update(w http.ResponseWriter, r *http.Request) {
	m, ok := a.menu(w, r)
	if !ok {
		return
	}
	f, ok := parse(w, r, m)
	if !ok {
		return
	}
	if err := a.env(r.Context(), &f); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if action := r.PostFormValue("action"); action != "" && action != "save" {
		act, ok := listfield.ParseAction(action)
		if ok && act.Field == ItemsField {
			f.Items, ok = apply(f.Items, act.Op, act.Item)
		} else {
			ok = false
		}
		if !ok {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		f.Unsaved = true
		render(w, r, http.StatusOK, f)
		return
	}
	if !validate(&f) {
		render(w, r, http.StatusUnprocessableEntity, f)
		return
	}
	err := Save(r.Context(), a.db, m.Name, f.Items)
	switch {
	case err == nil:
		admin.Redirect(w, r, EditPath(m.Name)+"?saved=1")
	case database.IsForeignKeyViolation(err):
		// Страницу удалили между проверкой и сохранением: проверка по свежему списку покажет, у какого пункта.
		if err := a.env(r.Context(), &f); err != nil {
			admin.ServerError(w, r, err)
			return
		}
		if validate(&f) {
			f.Errors = map[string]string{ItemsField: "Страницы в меню изменились: проверьте пункты и сохраните ещё раз."}
		}
		render(w, r, http.StatusUnprocessableEntity, f)
	default:
		admin.ServerError(w, r, err)
	}
}

// menu — меню по {name} из пути; неизвестное — 404 админки.
func (a *Admin) menu(w http.ResponseWriter, r *http.Request) (Menu, bool) {
	name := r.PathValue("name")
	for _, m := range a.menus {
		if m.Name == name {
			return m, true
		}
	}
	admin.NotFound(w, r)
	return Menu{}, false
}

// env загружает страницы и разделы для выбора, куда ведёт пункт.
func (a *Admin) env(ctx context.Context, f *menuForm) error {
	list, err := a.q.ListMenuPages(ctx)
	if err != nil {
		return fmt.Errorf("menu pages: %w", err)
	}
	f.Pages, f.Sections = list, a.sections
	return nil
}

// render — форма с ошибками или изменённая кнопкой пункта: с htmx — только форма, без него — страница.
func render(w http.ResponseWriter, r *http.Request, status int, f menuForm) {
	c := FormPage(f)
	if admin.IsHTMX(r) {
		c = Form(f)
	}
	web.Render(w, r, status, c)
}

// parse — пункты из присланной формы (без проверки); тело больше лимита, не форма или испорченные id
// пунктов — 400.
func parse(w http.ResponseWriter, r *http.Request, m Menu) (menuForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return menuForm{}, false
	}
	ids, err := listfield.IDs(r.PostForm, ItemsField)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return menuForm{}, false
	}
	v := func(id, field string) string { return r.PostFormValue(listfield.Name(ItemsField, id, field)) }
	items := make([]Item, 0, len(ids))
	for _, id := range ids {
		items = append(items, Item{
			ID: id, Child: v(id, "child") == "1", Target: v(id, "target"),
			Title: v(id, "title"), URL: v(id, "url"), Children: v(id, "children") == "1",
		})
	}
	return menuForm{Menu: m, Items: normalize(items)}, true
}

// normalize — первый пункт всегда верхнего уровня: подпункту не к чему относиться.
func normalize(items []Item) []Item {
	if len(items) > 0 {
		items[0].Child = false
	}
	return items
}

// group — конец группы пункта i: индекс после его последнего подпункта (у подпункта — i+1).
func group(items []Item, i int) int {
	j := i + 1
	if !items[i].Child {
		for j < len(items) && items[j].Child {
			j++
		}
	}
	return j
}

// hasChildren — у пункта верхнего уровня i есть подпункты.
func hasChildren(items []Item, i int) bool {
	return !items[i].Child && i+1 < len(items) && items[i+1].Child
}

// apply — копия списка после действия над пунктом id. Пункт верхнего уровня переносится и убирается вместе
// с подпунктами, подпункт переставляется только среди подпунктов своего пункта. indent делает пункт подпунктом
// пункта выше (только пункт без своих подпунктов: вложенность — один уровень), outdent — снова пунктом верхнего
// уровня (подпункты ниже него становятся его подпунктами). add — новый пункт в конец (не больше
// listfield.MaxItems). Неизвестный пункт или действие — ok == false. Входной список не меняется.
func apply(items []Item, op, id string) ([]Item, bool) {
	items = slices.Clone(items)
	if op == "add" {
		if len(items) < listfield.MaxItems {
			items = append(items, Item{ID: listfield.NewID()})
		}
		return items, true
	}
	i := slices.IndexFunc(items, func(it Item) bool { return it.ID == id })
	if i < 0 {
		return items, false
	}
	end := group(items, i)
	switch op {
	case "up":
		if items[i].Child {
			if i > 0 && items[i-1].Child {
				items[i-1], items[i] = items[i], items[i-1]
			}
			break
		}
		prev := i - 1 // начало группы выше
		for prev >= 0 && items[prev].Child {
			prev--
		}
		if prev >= 0 {
			items = slices.Concat(items[:prev], items[i:end], items[prev:i], items[end:])
		}
	case "down":
		if items[i].Child {
			if i+1 < len(items) && items[i+1].Child {
				items[i], items[i+1] = items[i+1], items[i]
			}
			break
		}
		if end < len(items) {
			next := group(items, end)
			items = slices.Concat(items[:i], items[end:next], items[i:end], items[next:])
		}
	case "indent":
		if !items[i].Child && i > 0 && !hasChildren(items, i) {
			items[i].Child, items[i].Children = true, false
		}
	case "outdent":
		items[i].Child = false
	case "remove":
		items = slices.Delete(items, i, end)
	default:
		return items, false
	}
	return normalize(items), true
}

// validate нормализует пункты и проверяет их; ошибки — в f.Errors. Пункт, где ничего не выбрано и не заполнено
// (добавили и не тронули), отбрасывается молча. Страницы и разделы для проверки должны быть уже загружены (env).
func validate(f *menuForm) bool {
	errs := map[string]string{}
	pageIDs := make(map[int64]bool, len(f.Pages))
	for _, p := range f.Pages {
		pageIDs[p.ID] = true
	}
	items := f.Items[:0]
	for _, it := range f.Items {
		it.Title, it.URL = strings.Join(strings.Fields(it.Title), " "), strings.TrimSpace(it.URL)
		if it.Target == "" && it.Title == "" && it.URL == "" {
			continue
		}
		name := func(field string) string { return listfield.Name(ItemsField, it.ID, field) }
		if utf8.RuneCountInString(it.Title) > maxTitleLen {
			errs[name("title")] = fmt.Sprintf("Название длиннее %d символов.", maxTitleLen)
		}
		sec, isSection := f.section(it)
		switch id, isPage := it.page(); {
		case it.Target == "":
			errs[name("target")] = "Выберите, куда ведёт пункт."
		case isPage:
			if !pageIDs[id] {
				errs[name("target")] = "Этой страницы больше нет: выберите другую."
			}
		case isSection:
			if sec.Key == "" {
				errs[name("target")] = "Этого раздела больше нет: выберите другой."
			}
		case it.Target == URLTarget:
			if it.Title == "" {
				errs[name("title")] = "Укажите название: у своего адреса его неоткуда взять."
			}
			switch {
			case it.URL == "":
				errs[name("url")] = "Укажите адрес."
			case !validLink(it.URL):
				errs[name("url")] = "Нужен путь на сайте (/privacy) или адрес, начинающийся с https://."
			}
		default:
			errs[name("target")] = "Выберите, куда ведёт пункт."
		}
		if it.Target != URLTarget {
			it.URL = ""
		}
		// Подпункты из записей — только у раздела, где они есть, и только у пункта верхнего уровня.
		it.Children = it.Children && !it.Child && sec.ChildrenTitle != ""
		items = append(items, it)
	}
	f.Items = normalize(items)
	if len(items) > listfield.MaxItems {
		errs[ItemsField] = fmt.Sprintf("Пунктов больше %d.", listfield.MaxItems)
	}
	if len(errs) > 0 {
		f.Errors = errs
		return false
	}
	return true
}

// section — раздел, на который ведёт пункт: ok — пункт ведёт на раздел; раздела нет в коде — пустой Section.
func (f menuForm) section(it Item) (Section, bool) {
	key, ok := it.section()
	if !ok {
		return Section{}, false
	}
	for _, s := range f.Sections {
		if s.Key == key {
			return s, true
		}
	}
	return Section{}, true
}

// pageRow — страница, на которую ведёт пункт; ok == false — пункт не на страницу или её нет.
func (f menuForm) pageRow(it Item) (ListMenuPagesRow, bool) {
	id, ok := it.page()
	if !ok {
		return ListMenuPagesRow{}, false
	}
	for _, p := range f.Pages {
		if p.ID == id {
			return p, true
		}
	}
	return ListMenuPagesRow{}, false
}

// defaultTitle — название пункта, если поле пустое: заголовок страницы или раздела (подсказка в поле).
func (f menuForm) defaultTitle(it Item) string {
	if p, ok := f.pageRow(it); ok {
		return p.Title
	}
	if s, ok := f.section(it); ok {
		return s.Title
	}
	return ""
}

// unpublished — пункт ведёт на страницу, которой нет на сайте: на сайте пункт не выводится.
func (f menuForm) unpublished(it Item) bool {
	p, ok := f.pageRow(it)
	return ok && p.Published == 0
}

// pageOption — страница в выборе «Куда ведёт»: заголовок черновика, у неопубликованной — пометка.
func pageOption(p ListMenuPagesRow) string {
	if p.Published == 0 {
		return p.Title + " (не опубликована)"
	}
	return p.Title
}

// autoSections — разделы с подпунктами из записей (флажок «Подпункты из записей»).
func (f menuForm) autoSections() []Section {
	var list []Section
	for _, s := range f.Sections {
		if s.ChildrenTitle != "" {
			list = append(list, s)
		}
	}
	return list
}

// validLink — путь на этом сайте («/privacy», не «//host») или абсолютный адрес http(s) с хостом
// (то же правило, что у ссылки баннера cookies в internal/settings).
func validLink(s string) bool {
	if utf8.RuneCountInString(s) > maxURLLen || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	if strings.HasPrefix(s, "/") {
		return !strings.HasPrefix(s, "//") && !strings.HasPrefix(s, "/\\")
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// describedBy — атрибуты доступности поля: подсказка и ошибка (aria-describedby), aria-invalid при ошибке.
func describedBy(f menuForm, name string, hint bool) templ.Attributes {
	var ids []string
	if hint {
		ids = append(ids, name+"-hint")
	}
	a := templ.Attributes{}
	if f.Errors[name] != "" {
		a["aria-invalid"] = "true"
		ids = append(ids, name+"-error")
	}
	if len(ids) > 0 {
		a["aria-describedby"] = strings.Join(ids, " ")
	}
	return a
}

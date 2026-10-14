// Package menu — меню сайта, которые редактируют в админке (site.yaml: menus): пункты в таблице menu_items,
// правки сразу на сайте, черновика нет. Пункт ведёт на страницу (по id — смена slug меню не ломает), на свой адрес
// или на раздел сайта; у раздела подпункты могут выводиться сами из его записей (у блога — категории).
// Вложенность — один уровень.
//
// Пакет отдаёт сайту данные — дерево ссылок с готовыми адресами (web.Link), — а как меню выглядит, решает шаблон
// сайта. Какие меню есть (Menu) и на какие разделы можно сослаться (Section), передаёт cmd/site.
package menu

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/keelkit/keelkit/reference/internal/content/pages"
	"github.com/keelkit/keelkit/reference/internal/web"
)

// Menu — меню из site.yaml (menus): имя в menu_items.menu и web.Site.Menus, как назвать его в админке и где оно
// выводится.
type Menu struct {
	Name  string // main, footer
	Title string // «Основное меню»
	Hint  string // «В шапке каждой страницы.»
}

// Section — раздел сайта, на который может вести пункт меню (лента блога). Key хранится в menu_items.section:
// раздел, которого больше нет в коде, на сайте не выводится, а в админке помечен ошибкой.
type Section struct {
	Key   string // blog
	Title string // название пункта по умолчанию и в выборе
	Path  string // адрес на сайте
	// ChildrenTitle — что выводится подпунктами из записей раздела, для подписи флажка («категории»);
	// пусто — у раздела их нет.
	ChildrenTitle string
	// Children — подпункты из записей раздела (видимые на сайте сейчас); nil, если ChildrenTitle пуст.
	Children func(ctx context.Context) ([]web.Link, error)
}

// Куда ведёт пункт (Item.Target): страница, раздел или свой адрес.
const (
	pagePrefix    = "page:"
	sectionPrefix = "section:"
	// URLTarget — свой адрес: путь на сайте или http(s).
	URLTarget = "url"
)

// PageTarget — пункт ведёт на страницу с этим id.
func PageTarget(id int64) string { return pagePrefix + strconv.FormatInt(id, 10) }

// SectionTarget — пункт ведёт на раздел с этим ключом.
func SectionTarget(key string) string { return sectionPrefix + key }

// Item — пункт меню в админке: элемент плоского списка формы, подпункт идёт сразу после своего пункта верхнего
// уровня (или другого подпункта того же пункта).
type Item struct {
	ID       string // id элемента формы (listfield): id строки или новый
	Child    bool   // подпункт ближайшего пункта верхнего уровня выше
	Target   string // PageTarget, SectionTarget или URLTarget; пусто — не выбрано
	Title    string // пусто — название страницы или раздела (у своего адреса обязательно)
	URL      string // только у URLTarget
	Children bool   // подпункты из записей раздела (только у раздела с ChildrenTitle, только верхний уровень)
}

// page — id страницы, если пункт ведёт на страницу.
func (it Item) page() (int64, bool) {
	s, ok := strings.CutPrefix(it.Target, pagePrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

// section — ключ раздела, если пункт ведёт на раздел.
func (it Item) section() (string, bool) {
	key, ok := strings.CutPrefix(it.Target, sectionPrefix)
	return key, ok && key != ""
}

// target — значение Item.Target для строки меню.
func target(pageID sql.NullInt64, url, section string) string {
	switch {
	case pageID.Valid:
		return PageTarget(pageID.Int64)
	case section != "":
		return SectionTarget(section)
	case url != "":
		return URLTarget
	}
	return ""
}

// Load — пункты меню name плоским списком в порядке формы: пункт верхнего уровня, за ним его подпункты.
func Load(ctx context.Context, q *Queries, name string) ([]Item, error) {
	rows, err := q.ListMenuItems(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("menu %s: %w", name, err)
	}
	children := map[int64][]Item{}
	var top []int64
	byID := map[int64]Item{}
	for _, r := range rows { // сначала пункты верхнего уровня, затем подпункты, каждые по порядку
		it := Item{
			ID: strconv.FormatInt(r.ID, 10), Child: r.ParentID.Valid, Target: target(r.PageID, r.Url, r.Section),
			Title: r.Title, URL: r.Url, Children: r.Children == 1,
		}
		if r.ParentID.Valid {
			children[r.ParentID.Int64] = append(children[r.ParentID.Int64], it)
			continue
		}
		top = append(top, r.ID)
		byID[r.ID] = it
	}
	items := make([]Item, 0, len(rows))
	for _, id := range top {
		items = append(items, byID[id])
		items = append(items, children[id]...)
	}
	return items, nil
}

// Save заменяет пункты меню name одной транзакцией. Пункты уже проверены (validate): первый — верхнего уровня,
// Target корректен.
func Save(ctx context.Context, db *sql.DB, name string, items []Item) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := New(tx)
	if err := q.DeleteMenuItems(ctx, name); err != nil {
		return err
	}
	var parent sql.NullInt64
	var pos, childPos int64
	for _, it := range items {
		p := InsertMenuItemParams{Menu: name, Title: it.Title}
		if id, ok := it.page(); ok {
			p.PageID = sql.NullInt64{Int64: id, Valid: true}
		} else if key, ok := it.section(); ok {
			p.Section = key
		} else {
			p.Url = it.URL
		}
		if it.Child {
			childPos++
			p.ParentID, p.Position = parent, childPos
		} else {
			pos++
			p.Position = pos
			if it.Children {
				p.Children = 1
			}
		}
		id, err := q.InsertMenuItem(ctx, p)
		if err != nil {
			return err
		}
		if !it.Child {
			parent, childPos = sql.NullInt64{Int64: id, Valid: true}, 0
		}
	}
	return tx.Commit()
}

// SeedIfEmpty — пункты меню name для dev-данных, только если меню пустое (seed идемпотентен).
func SeedIfEmpty(ctx context.Context, db *sql.DB, name string, items []Item) error {
	counts, err := New(db).CountMenuItems(ctx)
	if err != nil {
		return err
	}
	for _, c := range counts {
		if c.Menu == name && c.Items > 0 {
			return nil
		}
	}
	return Save(ctx, db, name, items)
}

// MenusUsingPage — имена меню, в которых есть пункт на страницу (её нельзя удалить).
func MenusUsingPage(ctx context.Context, q *Queries, pageID int64) ([]string, error) {
	return q.MenusUsingPage(ctx, sql.NullInt64{Int64: pageID, Valid: true})
}

// Site — меню для сайта по именам (web.Site.Menus): только то, что видно посетителю. Пункт на неопубликованную
// страницу или на раздел, которого нет в sections, не выводится вместе со своими подпунктами. Подпункты из записей
// раздела идут после заданных вручную; каждый раздел запрашивает их не больше раза.
func Site(ctx context.Context, q *Queries, sections []Section) (map[string][]web.Link, error) {
	rows, err := q.ListSiteMenuItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("site menus: %w", err)
	}
	secs := make(map[string]Section, len(sections))
	for _, s := range sections {
		secs[s.Key] = s
	}
	auto := map[string][]web.Link{}
	autoChildren := func(s Section) ([]web.Link, error) {
		if l, ok := auto[s.Key]; ok {
			return l, nil
		}
		l, err := s.Children(ctx)
		if err != nil {
			return nil, fmt.Errorf("menu section %s children: %w", s.Key, err)
		}
		auto[s.Key] = l
		return l, nil
	}

	// Строки идут по меню: сначала пункты верхнего уровня, затем подпункты; подпункт без видимого родителя
	// не выводится.
	menus := map[string][]web.Link{}
	index := map[int64]int{} // id видимого пункта верхнего уровня → индекс в menus[его меню]
	var withAuto []ListSiteMenuItemsRow
	for _, r := range rows {
		var l web.Link
		switch {
		case r.PageID.Valid:
			if r.PageSlug == "" {
				continue
			}
			l = web.Link{Title: cmp.Or(r.Title, r.PageTitle), URL: pages.PublicPath(r.PageSlug)}
		case r.Section != "":
			s, ok := secs[r.Section]
			if !ok {
				continue
			}
			l = web.Link{Title: cmp.Or(r.Title, s.Title), URL: s.Path}
		default:
			l = web.Link{Title: r.Title, URL: r.Url}
		}
		if r.ParentID.Valid {
			i, ok := index[r.ParentID.Int64]
			if !ok {
				continue
			}
			menus[r.Menu][i].Children = append(menus[r.Menu][i].Children, l)
			continue
		}
		index[r.ID] = len(menus[r.Menu])
		menus[r.Menu] = append(menus[r.Menu], l)
		if r.Children == 1 && r.Section != "" && secs[r.Section].Children != nil {
			withAuto = append(withAuto, r)
		}
	}
	for _, r := range withAuto {
		l, err := autoChildren(secs[r.Section])
		if err != nil {
			return nil, err
		}
		i := index[r.ID]
		menus[r.Menu][i].Children = append(menus[r.Menu][i].Children, l...)
	}
	return menus, nil
}

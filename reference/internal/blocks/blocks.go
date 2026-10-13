// Package blocks — поля типа blocks (site.yaml: { type: blocks, allow: [...] }): контент из готовых блоков.
//
// Набор блоков — дизайн сайта, поэтому поля блоков описывает этот пакет, а не спецификация. У каждого типа —
// Go-структура полей, проверка, вывод на сайте и фрагмент формы в админке (types.go, view.templ, form.templ).
// Блоки хранятся JSON-массивом в колонке (Marshal, Unmarshal); изображения — по id модуля images, адреса
// вариантов в данных не хранятся (меняются при повторной обработке). Пакет не знает о типах контента и маршрутах:
// адреса для htmx передаёт FormEnv.
package blocks

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/a-h/templ"
	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/listfield"
)

// MaxBlocks — предел числа блоков в одном поле.
const MaxBlocks = 100

// Block — один блок: общие поля и содержимое своего типа.
type Block struct {
	ID      string  // стабильный id: якорь на сайте (block-<ID>), имена полей формы, тесты
	Type    string  // имя типа: Kinds, Name
	Hidden  bool    // остаётся в данных и в админке, на сайт не выводится
	Content Content // *Text, *Image, *ImageText, *Quote, *Gallery, *Listing; *Unknown — тип, которого нет в коде
}

// Content — содержимое блока одного типа. Методы неэкспортируемые: типы блоков живут только в этом пакете.
type Content interface {
	// readForm читает поля блока из формы: f.Get(field) — значение поля block-<id>-<field>.
	readForm(f fields)
	// check нормализует поля и проверяет их; ошибки — поле → сообщение.
	check(res Resolved) map[string]string
	view(b Block, e viewEnv) templ.Component
	form(b Block, e FormEnv) templ.Component
	imageIDs() []string
	plainText() []string
}

// Kind — тип блока: имя (в JSON и в allow спецификации) и название в админке.
type Kind struct {
	Name  string
	Title string
	new   func() Content
}

// Kinds — все типы блоков, в порядке кнопок «Добавить блок». Новый тип — запись здесь, структура в types.go,
// вывод в view.templ, форма в form.templ и тест (AGENTS.md сайта, «Блоки»).
var Kinds = []Kind{
	{Name: "text", Title: "Текст", new: func() Content { return &Text{} }},
	{Name: "image", Title: "Изображение", new: func() Content { return &Image{} }},
	{Name: "image_text", Title: "Изображение с текстом", new: func() Content { return &ImageText{} }},
	{Name: "quote", Title: "Цитата", new: func() Content { return &Quote{} }},
	{Name: "gallery", Title: "Галерея", new: newGallery},
	{Name: "people", Title: "Люди", new: newListing},
}

func kind(name string) (Kind, bool) {
	i := slices.IndexFunc(Kinds, func(k Kind) bool { return k.Name == name })
	if i < 0 {
		return Kind{}, false
	}
	return Kinds[i], true
}

// ListKinds — имена блоков-списков (Listing): каждому нужен источник записей в ListSources.
func ListKinds() []string {
	var names []string
	for _, k := range Kinds {
		if _, ok := k.new().(*Listing); ok {
			names = append(names, k.Name)
		}
	}
	return names
}

// Known сообщает, что тип блока есть в коде.
func Known(name string) bool {
	_, ok := kind(name)
	return ok
}

// Title — название типа блока для админки.
func Title(name string) string {
	if k, ok := kind(name); ok {
		return k.Title
	}
	return "Неизвестный блок «" + name + "»"
}

// New — пустой блок типа name с новым id.
func New(name string) (Block, bool) {
	k, ok := kind(name)
	if !ok {
		return Block{}, false
	}
	return Block{ID: NewID(), Type: name, Content: k.new()}, true
}

// idPattern — id блока: строчная латиница, цифры, дефис (seed задаёт читаемые id, NewID — случайные).
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// NewID — случайный id блока: 12 символов base32 в нижнем регистре (60 бит).
func NewID() string { return strings.ToLower(rand.Text()[:12]) }

// header — общие поля блока в JSON.
type header struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Hidden bool   `json:"hidden,omitempty"`
}

// Marshal — блоки в JSON для колонки: массив объектов {id, type, hidden?, <поля типа>}.
// Ключи объекта отсортированы, поэтому одинаковые блоки дают одинаковую строку.
func Marshal(list []Block) (string, error) {
	out := make([]map[string]json.RawMessage, 0, len(list))
	for _, b := range list {
		data, err := json.Marshal(b.Content)
		if err != nil {
			return "", fmt.Errorf("marshal block %s: %w", b.ID, err)
		}
		m := map[string]json.RawMessage{}
		if err := json.Unmarshal(data, &m); err != nil {
			return "", fmt.Errorf("marshal block %s: content is not an object: %w", b.ID, err)
		}
		delete(m, "hidden")
		m["id"], _ = json.Marshal(b.ID)
		m["type"], _ = json.Marshal(b.Type)
		if b.Hidden {
			m["hidden"] = json.RawMessage("true")
		}
		out = append(out, m)
	}
	data, err := json.Marshal(out)
	return string(data), err
}

// Unmarshal читает блоки из колонки. Поле, которого нет в сохранённом блоке, получает нулевое значение
// (его смысл по умолчанию определяет тип: например, у ImageText пустое положение — слева). Блок неизвестного
// типа сохраняется как *Unknown: на сайт он не выводится, в админке остаётся как есть. Нет или неверный id
// (старые данные) — блок получает новый.
func Unmarshal(s string) ([]Block, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("unmarshal blocks: %w", err)
	}
	list := make([]Block, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for i, r := range raw {
		var h header
		if err := json.Unmarshal(r, &h); err != nil {
			return nil, fmt.Errorf("unmarshal block %d: %w", i, err)
		}
		if !idPattern.MatchString(h.ID) || seen[h.ID] {
			h.ID = NewID()
		}
		seen[h.ID] = true
		b := Block{ID: h.ID, Type: h.Type, Hidden: h.Hidden}
		if k, ok := kind(h.Type); ok {
			b.Content = k.new()
			if err := json.Unmarshal(r, b.Content); err != nil {
				return nil, fmt.Errorf("unmarshal block %d (%s): %w", i, h.Type, err)
			}
			if l, ok := b.Content.(itemList); ok {
				l.fixItemIDs()
			}
		} else {
			b.Content = &Unknown{Raw: string(r)}
		}
		list = append(list, b)
	}
	return list, nil
}

// FieldName — имя поля формы block-<id>-<field>; оно же — ключ ошибки в Validate.
func FieldName(id, field string) string { return "block-" + id + "-" + field }

// ErrForm — форма с блоками испорчена (неверный или повторный id, неизвестный тип): 400, а не ошибка у поля.
var ErrForm = errors.New("invalid blocks form")

// Parse читает блоки из формы в порядке полей block (каждое — id блока). Поля блока: block-<id>-type,
// block-<id>-hidden ("1" — скрыт) и поля его типа; у блока со списком — ещё элементы (items.go).
func Parse(form url.Values) ([]Block, error) {
	ids := form["block"]
	list := make([]Block, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !idPattern.MatchString(id) || seen[id] {
			return nil, fmt.Errorf("%w: block id %q", ErrForm, id)
		}
		seen[id] = true
		typ := form.Get(FieldName(id, "type"))
		b := Block{ID: id, Type: typ, Hidden: form.Get(FieldName(id, "hidden")) == "1"}
		if k, ok := kind(typ); ok {
			b.Content = k.new()
		} else if form.Has(FieldName(id, "raw")) {
			b.Content = &Unknown{}
		} else {
			return nil, fmt.Errorf("%w: block %s: unknown type %q", ErrForm, id, typ)
		}
		b.Content.readForm(fields{form: form, id: id})
		if l, ok := b.Content.(itemList); ok && !listfield.ValidIDs(l.itemIDs()) {
			return nil, fmt.Errorf("%w: block %s: item ids", ErrForm, id)
		}
		list = append(list, b)
	}
	return list, nil
}

// Apply выполняет действие кнопки блока: "up:<id>", "down:<id>", "hide:<id>" (скрыть или показать),
// "remove:<id>", "add:<тип>" (тип — из allow); действия над элементами списка — "item-…" (items.go).
// Неизвестное действие — ok == false. Входной список не меняется.
func Apply(list []Block, action string, allow []string) ([]Block, bool) {
	op, arg, _ := strings.Cut(action, ":")
	if itemOp, ok := strings.CutPrefix(op, "item-"); ok {
		return applyItemAction(list, itemOp, arg)
	}
	if op == "add" {
		if !slices.Contains(allow, arg) {
			return list, false
		}
		b, ok := New(arg)
		return append(slices.Clone(list), b), ok
	}
	i := slices.IndexFunc(list, func(b Block) bool { return b.ID == arg })
	if i < 0 {
		return list, false
	}
	list = slices.Clone(list)
	switch op {
	case "up":
		if i > 0 {
			list[i-1], list[i] = list[i], list[i-1]
		}
	case "down":
		if i < len(list)-1 {
			list[i], list[i+1] = list[i+1], list[i]
		}
	case "hide":
		list[i].Hidden = !list[i].Hidden
	case "remove":
		list = slices.Delete(list, i, i+1)
	default:
		return list, false
	}
	return list, true
}

// Images — то, что пакету нужно от модуля images (интерфейс на стороне потребителя).
type Images interface {
	Get(ctx context.Context, id string) (*images.Image, error)
}

// Resolved — изображения блоков по id. Изображения, которого нет в медиатеке, здесь нет.
type Resolved map[string]*images.Image

// Resolve загружает изображения, на которые ссылаются блоки.
func Resolve(ctx context.Context, src Images, list []Block) (Resolved, error) {
	res := Resolved{}
	for _, id := range ImageIDs(list) {
		img, err := src.Get(ctx, id)
		if errors.Is(err, images.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resolve image %s: %w", id, err)
		}
		res[id] = img
	}
	return res, nil
}

// Validate нормализует и проверяет блоки, в том числе скрытые. Ошибки — по имени поля формы (FieldName);
// превышение MaxBlocks — по ключу "blocks".
func Validate(list []Block, res Resolved) map[string]string {
	errs := map[string]string{}
	if len(list) > MaxBlocks {
		errs["blocks"] = fmt.Sprintf("Блоков больше %d: объедините часть из них.", MaxBlocks)
	}
	for _, b := range list {
		for field, msg := range b.Content.check(res) {
			errs[FieldName(b.ID, field)] = msg
		}
	}
	return errs
}

// ImageIDs — id изображений из всех блоков, включая скрытые (они тоже держат изображение), без повторов.
func ImageIDs(list []Block) []string {
	var ids []string
	for _, b := range list {
		for _, id := range b.Content.imageIDs() {
			if id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// FirstImage — первое изображение видимых блоков (картинка страницы для og:image); нет — nil.
func FirstImage(list []Block, res Resolved) *images.Image {
	for _, b := range list {
		if b.Hidden {
			continue
		}
		for _, id := range b.Content.imageIDs() {
			if img := res[id]; img != nil {
				return img
			}
		}
	}
	return nil
}

// PlainText — текст видимых блоков без разметки: для описания страницы, RSS и поиска.
func PlainText(list []Block) string {
	var parts []string
	for _, b := range list {
		if b.Hidden {
			continue
		}
		for _, p := range b.Content.plainText() {
			if p != "" {
				parts = append(parts, p)
			}
		}
	}
	return strings.Join(parts, " ")
}

// Summary — начало PlainText не длиннее n символов, по границе слова, с многоточием, если обрезано.
func Summary(list []Block, n int) string {
	s := []rune(PlainText(list))
	if len(s) <= n {
		return string(s)
	}
	cut := string(s[:n])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:—-") + "…"
}

// FormEnv — данные для формы блоков в админке.
type FormEnv struct {
	Errors   map[string]string // ошибки Validate
	Resolved Resolved          // выбранные изображения
	Library  []*images.Image   // выбор изображения: последние загруженные
	Allow    []string          // типы для «Добавить блок» (allow из site.yaml)
	// Адреса для htmx. PreviewURL — POST: превью Markdown одного поля (имя поля — в ?field=);
	// BlockURL — POST: фрагмент формы блока — нового (?type=) или существующего из формы (?id=).
	PreviewURL string
	BlockURL   string
	MediaURL   string      // медиатека: ссылка «загрузите в медиатеку»
	Lists      ListSources // блоки-списки: ссылка на раздел их записей в админке
}

func (e FormEnv) err(b Block, field string) string { return e.Errors[FieldName(b.ID, field)] }

// logUnknown — на сайт блок неизвестного типа не выводится; это видно в логе.
func logUnknown(ctx context.Context, b Block) {
	slog.WarnContext(ctx, "blocks: unknown block type skipped", "type", b.Type, "id", b.ID)
}

// ListSource — откуда блок-список берёт записи: вывод всех записей типа контента на сайте и раздел админки,
// где их правят. Даёт код типа (internal/content/people): блоки о типах контента не знают.
type ListSource struct {
	Render   func(ctx context.Context) (templ.Component, error)
	AdminURL string
}

// ListSources — источники записей по имени блока-списка; собирает cmd/site (blockLists).
type ListSources map[string]ListSource

// Lists — вывод записей видимых блоков-списков по имени блока (ListSources.Resolve).
type Lists map[string]templ.Component

// Resolve — вывод записей для видимых блоков-списков; у скрытых записи не загружаются.
// Блок-список без источника не выводится (на сайте — пусто, в лог — предупреждение при выводе).
func (s ListSources) Resolve(ctx context.Context, list []Block) (Lists, error) {
	lists := Lists{}
	for _, b := range list {
		if _, ok := b.Content.(*Listing); !ok || b.Hidden || lists[b.Type] != nil {
			continue
		}
		src, ok := s[b.Type]
		if !ok || src.Render == nil {
			continue
		}
		c, err := src.Render(ctx)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", b.Type, err)
		}
		lists[b.Type] = c
	}
	return lists, nil
}

// viewEnv — данные для вывода блоков на сайте.
type viewEnv struct {
	res   Resolved
	lists Lists
}

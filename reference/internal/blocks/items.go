package blocks

import (
	"net/url"
	"slices"
	"strings"
)

// Списки элементов внутри блока (галерея; так же — таймлайн, характеристики и другие повторяющиеся данные).
// У элемента стабильный id, как у блока: имена полей формы block-<блок>-i-<элемент>-<поле> (ItemField),
// порядок — поля block-<блок>-item. Кнопки элемента — те же submit с name="action" (Apply):
// item-up:<блок>:<элемент>, item-down:…, item-remove:…, item-add:<блок>.

// MaxItems — предел числа элементов в одном блоке.
const MaxItems = 50

// fields — поля одного блока в форме: Get(field) и All(field) читают block-<id>-<field>.
type fields struct {
	form url.Values
	id   string
}

func (f fields) Get(field string) string      { return f.form.Get(FieldName(f.id, field)) }
func (f fields) All(field string) []string    { return f.form[FieldName(f.id, field)] }
func (f fields) Has(field string) bool        { return f.form.Has(FieldName(f.id, field)) }
func (f fields) item(id, field string) string { return f.Get(ItemField(id, field)) }

// ItemField — поле элемента списка относительно блока: i-<элемент>-<поле>. Полное имя в форме —
// FieldName(блок, ItemField(элемент, поле)); оно же — ключ ошибки в Validate.
func ItemField(item, field string) string { return "i-" + item + "-" + field }

// itemList — содержимое блока со списком элементов.
type itemList interface {
	itemIDs() []string
	// fixItemIDs даёт новый id элементам без id или с повторным (старые данные).
	fixItemIDs()
	// applyItem — копия содержимого после действия над элементом: op — up, down, remove (над item) или add.
	applyItem(op, item string) (Content, bool)
}

// validItemIDs — id элементов из формы корректны и не повторяются.
func validItemIDs(ids []string) bool {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !idPattern.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// fixIDs — новые id элементам без корректного или с повторным id.
func fixIDs[T any](list []T, id func(*T) *string) {
	seen := make(map[string]bool, len(list))
	for i := range list {
		p := id(&list[i])
		if !idPattern.MatchString(*p) || seen[*p] {
			*p = NewID()
		}
		seen[*p] = true
	}
}

// applyTo — копия списка после действия над элементом item: up, down, remove; add — новый элемент в конец
// (не больше MaxItems). Неизвестный элемент или действие — ok == false.
func applyTo[T any](list []T, id func(T) string, op, item string, add func() T) ([]T, bool) {
	list = slices.Clone(list)
	if op == "add" {
		if len(list) < MaxItems {
			list = append(list, add())
		}
		return list, true
	}
	i := slices.IndexFunc(list, func(v T) bool { return id(v) == item })
	if i < 0 {
		return list, false
	}
	switch op {
	case "up":
		if i > 0 {
			list[i-1], list[i] = list[i], list[i-1]
		}
	case "down":
		if i < len(list)-1 {
			list[i], list[i+1] = list[i+1], list[i]
		}
	case "remove":
		list = slices.Delete(list, i, i+1)
	default:
		return list, false
	}
	return list, true
}

// applyItemAction выполняет действие над элементом: action без префикса «item-», arg — «<блок>:<элемент>»
// или «<блок>» для add.
func applyItemAction(list []Block, op, arg string) ([]Block, bool) {
	block, item, _ := strings.Cut(arg, ":")
	i := slices.IndexFunc(list, func(b Block) bool { return b.ID == block })
	if i < 0 {
		return list, false
	}
	l, ok := list[i].Content.(itemList)
	if !ok || (op == "add") != (item == "") {
		return list, false
	}
	c, ok := l.applyItem(op, item)
	if !ok {
		return list, false
	}
	list = slices.Clone(list)
	list[i].Content = c
	return list, true
}

// prefixErrors переносит ошибки элемента в ошибки блока: поле → i-<элемент>-<поле>.
func prefixErrors(dst, src map[string]string, item string) {
	for field, msg := range src {
		dst[ItemField(item, field)] = msg
	}
}

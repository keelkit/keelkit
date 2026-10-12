// Package listfield — поле записи со списком элементов (site.yaml: { type: list, fields: {…} }): соцсети
// в настройках, характеристики товара, этапы проекта. Общий код любого такого поля: id элементов, имена полей
// формы, действия кнопок «↑ ↓ Убрать / Добавить» (без JavaScript и с htmx), хранение JSON-массивом в колонке.
// Поля элемента задаёт тип записи: структура с полем ID, чтение формы, проверка и вывод — в его пакете.
//
// Те же операции над элементами использует список внутри блока (internal/blocks, галерея): Apply, FixIDs, ValidIDs.
//
// Форма поля <field>:
//   - <field>-item — id элементов по порядку (скрытые поля), Order;
//   - <field>-i-<элемент>-<поле> — поле элемента, Name; оно же — ключ ошибки;
//   - кнопки — submit с name="action": list-up:<field>:<элемент>, list-down:…, list-remove:…, list-add:<field>
//     (ActionValue, ParseAction); форма возвращается изменённой, без сохранения.
//
// Хранение — JSON-массив в колонке TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(…)): порядок меняется атомарно.
// Картинки элементов (поле image) — как у блоков: ещё и таблица ссылок <type>_images → images_files(id),
// перезаписываемая в той же транзакции, чтобы используемое изображение нельзя было удалить.
package listfield

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// MaxItems — предел числа элементов в одном списке.
const MaxItems = 50

// actionPrefix — префикс значения кнопки действия над списком поля записи.
const actionPrefix = "list-"

// idPattern — id элемента: строчные латинские буквы, цифры и «-» (входит в имена полей формы).
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// NewID — случайный id нового элемента.
func NewID() string { return strings.ToLower(rand.Text()[:12]) }

// ValidIDs — id элементов из формы корректны и не повторяются.
func ValidIDs(ids []string) bool {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !idPattern.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// FixIDs — новые id элементам без корректного или с повторным id (данные, записанные не через форму).
func FixIDs[T any](list []T, id func(*T) *string) {
	seen := make(map[string]bool, len(list))
	for i := range list {
		p := id(&list[i])
		if !idPattern.MatchString(*p) || seen[*p] {
			*p = NewID()
		}
		seen[*p] = true
	}
}

// Apply — копия списка после действия над элементом item: up, down, remove; add — новый элемент в конец
// (не больше MaxItems). Неизвестный элемент или действие — ok == false. Входной список не меняется.
func Apply[T any](list []T, id func(T) string, op, item string, add func() T) ([]T, bool) {
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

// Order — имя скрытого поля формы с id элементов по порядку.
func Order(field string) string { return field + "-item" }

// Name — имя поля элемента в форме (и ключ его ошибки): <field>-i-<элемент>-<поле>.
func Name(field, item, sub string) string { return Anchor(field, item) + "-" + sub }

// Anchor — id элемента в разметке формы: <field>-i-<элемент>.
func Anchor(field, item string) string { return field + "-i-" + item }

// IDs — id элементов поля из формы по порядку; некорректные или повторные — ошибка (форма подделана).
func IDs(form url.Values, field string) ([]string, error) {
	ids := form[Order(field)]
	if !ValidIDs(ids) {
		return nil, fmt.Errorf("list field %s: item ids", field)
	}
	return ids, nil
}

// Action — действие кнопки списка: Op — up, down, remove (над Item) или add.
type Action struct {
	Field, Op, Item string
}

// ActionValue — значение кнопки: list-<op>:<field>:<элемент>; для add item пустой — list-add:<field>.
func ActionValue(op, field, item string) string {
	v := actionPrefix + op + ":" + field
	if item != "" {
		v += ":" + item
	}
	return v
}

// ParseAction разбирает значение кнопки; ok == false — это не действие над списком (или оно неполное).
// Существование поля и элемента проверяет вызывающий: Apply вернёт ok == false для чужого элемента.
func ParseAction(action string) (Action, bool) {
	rest, ok := strings.CutPrefix(action, actionPrefix)
	if !ok {
		return Action{}, false
	}
	op, arg, _ := strings.Cut(rest, ":")
	field, item, _ := strings.Cut(arg, ":")
	if field == "" || (op == "add") != (item == "") {
		return Action{}, false
	}
	return Action{Field: field, Op: op, Item: item}, true
}

// Decode — элементы из колонки (JSON-массив); пустая строка — пустой список. Элементам без id даёт новые.
func Decode[T any](s string, id func(*T) *string) ([]T, error) {
	if s == "" {
		return nil, nil
	}
	var list []T
	if err := json.Unmarshal([]byte(s), &list); err != nil {
		return nil, fmt.Errorf("decode list: %w", err)
	}
	FixIDs(list, id)
	return list, nil
}

// Encode — значение колонки: JSON-массив (пустой список — "[]").
func Encode[T any](list []T) (string, error) {
	if list == nil {
		list = []T{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return "", fmt.Errorf("encode list: %w", err)
	}
	return string(b), nil
}

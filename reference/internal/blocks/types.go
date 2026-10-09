package blocks

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/voffkin/keelkit/reference/internal/richtext"
)

// Ограничения полей блоков.
const (
	maxMarkdown = 1 << 20 // байт Markdown в одном блоке
	maxShort    = 300     // символов: alt, подпись, автор
	maxQuote    = 2000    // символов цитаты
)

// Text — блок text: Markdown.
type Text struct {
	Body string `json:"body"`
}

func (t *Text) readForm(get func(string) string) { t.Body = get("body") }

func (t *Text) check(Resolved) map[string]string {
	t.Body = normalizeNewlines(t.Body)
	errs := map[string]string{}
	checkMarkdown(errs, "body", t.Body, true)
	return errs
}

func (t *Text) view(b Block, _ Resolved) templ.Component { return textView(b, t) }
func (t *Text) form(b Block, e FormEnv) templ.Component  { return textForm(b, t, e) }
func (t *Text) imageIDs() []string                       { return nil }
func (t *Text) plainText() []string                      { return []string{richtext.PlainText(t.Body)} }

// Image — блок image: изображение из медиатеки (id модуля images), alt обязателен, подпись — нет.
type Image struct {
	Image   string `json:"image"`
	Alt     string `json:"alt"`
	Caption string `json:"caption"`
}

func (m *Image) readForm(get func(string) string) {
	m.Image, m.Alt, m.Caption = get("image"), get("alt"), get("caption")
}

func (m *Image) check(res Resolved) map[string]string {
	errs := map[string]string{}
	checkImage(errs, &m.Image, &m.Alt, res)
	m.Caption = strings.TrimSpace(m.Caption)
	checkShort(errs, "caption", m.Caption, "Подпись")
	return errs
}

func (m *Image) view(b Block, res Resolved) templ.Component { return imageView(b, m, res[m.Image]) }
func (m *Image) form(b Block, e FormEnv) templ.Component    { return imageForm(b, m, e) }
func (m *Image) imageIDs() []string                         { return []string{m.Image} }
func (m *Image) plainText() []string                        { return []string{m.Caption} }

// ImageText — блок image_text: изображение и Markdown рядом. Position: "right" — картинка справа,
// иначе (в том числе пусто в старых данных) — слева.
type ImageText struct {
	Image    string `json:"image"`
	Alt      string `json:"alt"`
	Body     string `json:"body"`
	Position string `json:"position"`
}

// Положения картинки в ImageText.
const (
	PositionLeft  = "left"
	PositionRight = "right"
)

func (m *ImageText) readForm(get func(string) string) {
	m.Image, m.Alt, m.Body, m.Position = get("image"), get("alt"), get("body"), get("position")
}

func (m *ImageText) check(res Resolved) map[string]string {
	errs := map[string]string{}
	checkImage(errs, &m.Image, &m.Alt, res)
	m.Body = normalizeNewlines(m.Body)
	checkMarkdown(errs, "body", m.Body, true)
	if m.Position != PositionRight {
		m.Position = PositionLeft
	}
	return errs
}

func (m *ImageText) right() bool { return m.Position == PositionRight }

func (m *ImageText) view(b Block, res Resolved) templ.Component {
	return imageTextView(b, m, res[m.Image])
}
func (m *ImageText) form(b Block, e FormEnv) templ.Component { return imageTextForm(b, m, e) }
func (m *ImageText) imageIDs() []string                      { return []string{m.Image} }
func (m *ImageText) plainText() []string                     { return []string{richtext.PlainText(m.Body)} }

// Quote — блок quote: цитата и необязательный автор.
type Quote struct {
	Text   string `json:"text"`
	Author string `json:"author"`
}

func (q *Quote) readForm(get func(string) string) { q.Text, q.Author = get("text"), get("author") }

func (q *Quote) check(Resolved) map[string]string {
	errs := map[string]string{}
	q.Text = strings.TrimSpace(normalizeNewlines(q.Text))
	q.Author = strings.TrimSpace(q.Author)
	switch {
	case q.Text == "":
		errs["text"] = "Введите цитату или удалите блок."
	case utf8.RuneCountInString(q.Text) > maxQuote:
		errs["text"] = fmt.Sprintf("Цитата длиннее %d символов.", maxQuote)
	}
	checkShort(errs, "author", q.Author, "Автор")
	return errs
}

func (q *Quote) view(b Block, _ Resolved) templ.Component { return quoteView(b, q) }
func (q *Quote) form(b Block, e FormEnv) templ.Component  { return quoteForm(b, q, e) }
func (q *Quote) imageIDs() []string                       { return nil }
func (q *Quote) plainText() []string                      { return []string{q.Text, q.Author} }

// Unknown — блок типа, которого нет в коде (тип удалили или данные из более новой версии сайта).
// Сохраняется как есть (Raw — исходный JSON-объект), на сайт не выводится, в админке его можно только
// переставить, скрыть или удалить.
type Unknown struct {
	Raw string
}

func (u *Unknown) MarshalJSON() ([]byte, error) { return []byte(u.Raw), nil }

func (u *Unknown) readForm(get func(string) string) { u.Raw = get("raw") }

func (u *Unknown) check(Resolved) map[string]string {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(u.Raw), &m) != nil {
		return map[string]string{"raw": "Данные блока повреждены: удалите блок."}
	}
	return nil
}

func (u *Unknown) view(b Block, _ Resolved) templ.Component { return unknownView(b) }
func (u *Unknown) form(b Block, _ FormEnv) templ.Component {
	return unknownForm(b, u)
}

// imageIDs — изображения неизвестного блока не видны; внешний ключ защищает только известные блоки.
func (u *Unknown) imageIDs() []string  { return nil }
func (u *Unknown) plainText() []string { return nil }

// checkImage — выбранное изображение есть в медиатеке, alt заполнен.
func checkImage(errs map[string]string, id, alt *string, res Resolved) {
	*id = strings.TrimSpace(*id)
	*alt = strings.TrimSpace(*alt)
	switch {
	case *id == "":
		errs["image"] = "Выберите изображение."
	case res[*id] == nil:
		errs["image"] = "Изображения нет в медиатеке: выберите другое."
	}
	switch {
	case *alt == "":
		errs["alt"] = "Опишите изображение для тех, кто его не видит (alt)."
	case utf8.RuneCountInString(*alt) > maxShort:
		errs["alt"] = fmt.Sprintf("Описание длиннее %d символов.", maxShort)
	}
}

func checkMarkdown(errs map[string]string, field, s string, required bool) {
	switch {
	case required && strings.TrimSpace(s) == "":
		errs[field] = "Введите текст или удалите блок."
	case len(s) > maxMarkdown:
		errs[field] = fmt.Sprintf("Текст больше %d КБ.", maxMarkdown>>10)
	}
}

func checkShort(errs map[string]string, field, s, label string) {
	if utf8.RuneCountInString(s) > maxShort {
		errs[field] = fmt.Sprintf("%s длиннее %d символов.", label, maxShort)
	}
}

// normalizeNewlines — браузер отправляет переводы строк textarea как CRLF; храним LF.
func normalizeNewlines(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// Package richtext превращает поля типа richtext (Markdown) в безопасный HTML.
//
// Два рубежа: goldmark не пропускает сырой HTML из Markdown, bluemonday (политика UGC) чистит результат.
package richtext

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// HTML — очищенный HTML. Реализует templ.Component: в шаблоне выводится как @body.
// Получить значение можно только через Render, поэтому неочищенный HTML в шаблон не попадёт.
type HTML string

// Render пишет HTML как есть: он уже очищен.
func (h HTML) Render(_ context.Context, w io.Writer) error {
	_, err := io.WriteString(w, string(h))
	return err
}

// markdown — GFM (таблицы, зачёркивание, автоссылки, списки задач); сырой HTML не пропускается (нет html.WithUnsafe).
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

var policy = newPolicy()

func newPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// Язык блока кода (```go → class="language-go") — для подсветки на стороне CSS/JS.
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^language-[\w+#-]+$`)).OnElements("code")
	// nofollow — только на внешних ссылках: внутренние ссылки сайта индексируются.
	p.RequireNoFollowOnLinks(false)
	p.RequireNoFollowOnFullyQualifiedLinks(true)
	return p
}

// Render преобразует Markdown в очищенный HTML.
func Render(src string) (HTML, error) {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(src), &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return HTML(policy.SanitizeBytes(buf.Bytes())), nil
}

// strict убирает все теги: для PlainText.
var strict = bluemonday.StrictPolicy()

var blockEnd = regexp.MustCompile(`</(p|h[1-6]|li|blockquote|pre|td|th)>|<br\s*/?>|<hr\s*/?>`)

// PlainText — текст Markdown без разметки: для описания страницы, RSS и поиска. Абзацы и строки
// схлопываются в одну строку с одиночными пробелами.
func PlainText(src string) string {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(src), &buf); err != nil {
		return strings.Join(strings.Fields(src), " ")
	}
	// После блочных элементов — пробел, иначе слова соседних абзацев склеятся.
	spaced := blockEnd.ReplaceAllString(buf.String(), "$0 ")
	return strings.Join(strings.Fields(html.UnescapeString(strict.Sanitize(spaced))), " ")
}

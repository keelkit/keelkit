// Package testkit — контрактные тесты модуля richtext для сайтов.
//
// Сайт передаёт функцию, которой выводит поле richtext так же, как на настоящей странице (через свои шаблоны
// и компоненты: фрагмент или страница целиком). Тесты проверяют, что Markdown превращается в разметку и что
// XSS-примеры обезврежены — то есть сайт выводит richtext через Render и не обходит санитайзер.
//
//	func TestRichtextContract(t *testing.T) {
//		testkit.Run(t, func(t *testing.T, markdown string) string {
//			return renderTextBlock(t, markdown) // HTML, как его отдаёт сайт
//		})
//	}
package testkit

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Marker — метка в XSS-примерах: по ней проверка отличает вставленный код от собственных скриптов страницы.
const Marker = "keelkit-xss"

// payloads — XSS-примеры в Markdown: каждый пытается выполнить код с меткой Marker.
var payloads = []struct{ name, src string }{
	{"ScriptTag", "<script>alert('" + Marker + "')</script>"},
	{"ScriptInline", "текст <script>alert('" + Marker + "')</script> текст"},
	{"EventHandler", `<img src="x" onerror="alert('` + Marker + `')">`},
	{"SVGOnload", `<svg onload="alert('` + Marker + `')"></svg>`},
	{"Iframe", `<iframe src="javascript:alert('` + Marker + `')"></iframe>`},
	{"JavascriptLink", "[ссылка](javascript:alert('" + Marker + "'))"},
	{"JavascriptLinkTab", `<a href="jav&#x09;ascript:alert('` + Marker + `')">ссылка</a>`},
	{"JavascriptAutolink", "<javascript:alert('" + Marker + "')>"},
	{"JavascriptImage", "![картинка](javascript:alert('" + Marker + "'))"},
	{"DataLink", "[ссылка](data:text/html,<script>alert('" + Marker + "')</script>)"},
	{"StyleTag", "<style>body{background:url(javascript:alert('" + Marker + "'))}</style>"},
	{"StyleAttr", `<p style="background:url(javascript:alert('` + Marker + `'))">текст</p>`},
	{"CodeClass", "```x\" onclick=\"alert('" + Marker + "')\n1\n```"},
	{"FormAction", `<form action="javascript:alert('` + Marker + `')"><button>ok</button></form>`},
}

// Run запускает контрактные тесты модуля как подтесты t. render выводит Markdown так, как сайт выводит
// поле richtext, и возвращает получившийся HTML.
func Run(t *testing.T, render func(t *testing.T, markdown string) string) {
	t.Helper()
	t.Run("MarkdownRendered", func(t *testing.T) {
		got := render(t, "Текст с **жирным** и [ссылкой](/about).")
		for _, want := range []string{"<strong>жирным</strong>", `<a href="/about">ссылкой</a>`} {
			if !strings.Contains(got, want) {
				t.Errorf("Markdown не превратился в разметку: нет %s в\n%s", want, got)
			}
		}
	})
	for _, p := range payloads {
		t.Run("XSS/"+p.name, func(t *testing.T) {
			for _, problem := range Check(render(t, p.src)) {
				t.Errorf("%s: %s", problem, p.src)
			}
		})
	}
}

// Check ищет в HTML исполняемый код с меткой Marker: элементы script/style/iframe/object/embed,
// обработчики событий, атрибут style и адреса javascript:, vbscript: и data:. Возвращает найденное.
func Check(src string) []string {
	var problems []string
	z := html.NewTokenizer(strings.NewReader(src))
	var raw string // элемент, внутри которого текст исполняется (script, style)
	for {
		switch z.Next() {
		case html.ErrorToken:
			return problems
		case html.TextToken:
			if raw != "" && strings.Contains(string(z.Text()), Marker) {
				problems = append(problems, "код внутри <"+raw+">")
			}
		case html.EndTagToken:
			raw = ""
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			switch tok.Data {
			case "script", "style":
				raw = tok.Data
			case "iframe", "object", "embed", "frame":
				problems = append(problems, "элемент <"+tok.Data+">")
			}
			for _, a := range tok.Attr {
				if !strings.Contains(a.Val, Marker) {
					continue
				}
				switch {
				case strings.HasPrefix(a.Key, "on"):
					problems = append(problems, "обработчик "+a.Key+" у <"+tok.Data+">")
				case a.Key == "style":
					problems = append(problems, "атрибут style у <"+tok.Data+">")
				case dangerousURL(a.Val):
					problems = append(problems, "адрес "+a.Key+"="+a.Val+" у <"+tok.Data+">")
				}
			}
		}
	}
}

// dangerousURL сообщает, что адрес исполняет код. Браузер пропускает управляющие символы и пробелы
// внутри схемы («jav\tascript:»), поэтому они удаляются перед сравнением.
func dangerousURL(u string) bool {
	u = strings.Map(func(r rune) rune {
		if r <= ' ' {
			return -1
		}
		return r
	}, strings.ToLower(u))
	return strings.HasPrefix(u, "javascript:") || strings.HasPrefix(u, "vbscript:") || strings.HasPrefix(u, "data:")
}

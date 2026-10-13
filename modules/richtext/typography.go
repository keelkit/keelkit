package richtext

import (
	"bytes"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// nbsp — неразрывный пробел (U+00A0): браузер не переносит строку в этом месте.
const nbsp = '\u00a0'

// shortWords — предлоги, союзы и частицы, которые не должны оставаться в конце строки:
// пробел после них становится неразрывным. Сравнение без учёта регистра.
var shortWords = map[string]bool{}

func init() {
	for w := range strings.FieldsSeq(`
		а без в во да для до за и из или к ко на над не ни но о об обо от по под при про с со у`) {
		shortWords[w] = true
	}
}

// blockTags — элементы, на границе которых текст не продолжается: слово до них не связывается со словом после.
var blockTags = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true, "blockquote": true,
	"table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true, "td": true, "th": true,
	"div": true, "hr": true, "br": true, "img": true,
}

// verbatimTags — текст внутри не меняется: код показывается как есть.
var verbatimTags = map[string]bool{"pre": true, "code": true, "kbd": true, "samp": true}

// typography расставляет неразрывные пробелы в тексте очищенного HTML: после коротких предлогов и союзов
// и перед тире (« — », « – »). Теги, атрибуты и код не меняются. Слово, разбитое строчными тегами
// («<strong>в</strong> Москве»), рассматривается целиком.
func typography(src []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(src))
	z := html.NewTokenizer(bytes.NewReader(src))
	var word []rune // буквы в конце уже обработанного текста — слово, которое ещё может продолжаться
	verbatim := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return out.Bytes()
		case html.TextToken:
			if verbatim > 0 {
				word = word[:0]
				break
			}
			text := string(z.Text())
			var fixed string
			fixed, word = typographText(text, word)
			if fixed != text {
				out.WriteString(html.EscapeString(fixed))
				continue
			}
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			if verbatimTags[string(name)] {
				if tt == html.StartTagToken {
					verbatim++
				} else if tt == html.EndTagToken && verbatim > 0 {
					verbatim--
				}
			}
			if blockTags[string(name)] {
				word = word[:0]
			}
		}
		out.Write(z.Raw())
	}
}

// typographText обрабатывает один текстовый фрагмент. word — буквы в конце предыдущего фрагмента;
// возвращаются исправленный текст и буквы в его конце.
func typographText(text string, word []rune) (string, []rune) {
	rs := []rune(text)
	for i, r := range rs {
		switch {
		case unicode.IsLetter(r):
			word = append(word, r)
			continue
		case r == ' ' || r == '\n':
			if shortWords[strings.ToLower(string(word))] || isDashAt(rs, i+1) {
				rs[i] = nbsp
			}
		}
		word = word[:0]
	}
	return string(rs), word
}

// isDashAt сообщает, что с позиции i стоит тире, отбитое пробелом или концом фрагмента.
func isDashAt(rs []rune, i int) bool {
	if i >= len(rs) || (rs[i] != '—' && rs[i] != '–') {
		return false
	}
	return i+1 == len(rs) || unicode.IsSpace(rs[i+1])
}

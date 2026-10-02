// Package slug — адреса контента: генерация slug из заголовка (с транслитерацией кириллицы) и проверка формата.
//
// Транслитерация — своя таблица (решение в корневом AGENTS.md): упрощённая латиница без диакритики,
// как в привычных адресах: «Тестовая» → testovaya, «Щука» → shchuka. Буквы, которых нет в таблице
// (не латиница и не кириллица), становятся разделителями.
package slug

import (
	"regexp"
	"strings"
)

// MaxLen — максимальная длина slug в байтах. Make обрезает длиннее по границе слова.
const MaxLen = 80

var valid = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// translit — кириллица (русский, а также буквы украинского и белорусского алфавитов) → латиница.
// Только строчные: Make приводит текст к нижнему регистру до замены.
var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya",
	'і': "i", 'ї': "yi", 'є': "ye", 'ґ': "g", 'ў': "u",
}

// Make строит slug из произвольного текста: нижний регистр, транслитерация, всё прочее — дефисы.
// Пустой результат означает, что в тексте нет ни букв, ни цифр, из которых можно составить адрес.
func Make(s string) string {
	var b strings.Builder
	sep := false // нужен дефис перед следующим символом
	for _, r := range strings.ToLower(s) {
		var part string
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			part = string(r)
		case r == 'ъ' || r == 'ь':
			continue // знаки не дают букв, но и не разделяют слово: «объект» → obekt
		default:
			part = translit[r]
		}
		if part == "" {
			sep = b.Len() > 0
			continue
		}
		if sep {
			b.WriteByte('-')
			sep = false
		}
		b.WriteString(part)
	}
	return truncate(b.String())
}

// truncate обрезает slug до MaxLen, по возможности по последнему дефису.
func truncate(s string) string {
	if len(s) <= MaxLen {
		return s
	}
	s = s[:MaxLen]
	if i := strings.LastIndexByte(s, '-'); i > 0 {
		return s[:i]
	}
	return strings.TrimSuffix(s, "-")
}

// Valid сообщает, что s — готовый slug: латиница в нижнем регистре, цифры и одиночные дефисы между ними.
func Valid(s string) bool {
	return len(s) <= MaxLen && valid.MatchString(s)
}

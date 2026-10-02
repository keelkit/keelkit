package slug

import (
	"strings"
	"testing"
)

func TestMake(t *testing.T) {
	for in, want := range map[string]string{
		"Тестовая":                 "testovaya",
		"О проекте":                "o-proekte",
		"Щука, ёж и Хьюстон":       "shchuka-yozh-i-khyuston",
		"Объект съёмки":            "obekt-syomki",
		"Цены 2026: всё включено!": "tseny-2026-vsyo-vklyucheno",
		"Hello, World":             "hello-world",
		"  --уже-slug--  ":         "uzhe-slug",
		"Їжак і ґанок":             "yizhak-i-ganok",
		"café":                     "caf",
		"!!!":                      "",
		"":                         "",
		"Ъ":                        "",
		"a—b–c":                    "a-b-c",
		"Привет\tмир\nснова":       "privet-mir-snova",
		"COVID-19 и ИИ":            "covid-19-i-ii",
		"Шоссе № 5":                "shosse-5",
		"Съезд партии":             "sezd-partii",
		"Дом-2: продолжение (2026)": "dom-2-prodolzhenie-2026",
	} {
		if got := Make(in); got != want {
			t.Errorf("Make(%q) = %q, want %q", in, got, want)
		}
		if got := Make(in); got != "" && !Valid(got) {
			t.Errorf("Make(%q) = %q is not Valid", in, got)
		}
	}
}

func TestMakeTruncates(t *testing.T) {
	long := strings.Repeat("слово ", 30) // slovo-slovo-…
	got := Make(long)
	if len(got) > MaxLen || !Valid(got) || strings.HasSuffix(got, "-") {
		t.Fatalf("Make(long) = %q (len %d)", got, len(got))
	}
	if !strings.HasSuffix(got, "slovo") {
		t.Errorf("Make(long) = %q: cut inside a word", got)
	}

	// Одно длинное слово режется ровно по MaxLen.
	if got := Make(strings.Repeat("a", 100)); got != strings.Repeat("a", MaxLen) {
		t.Errorf("Make(100×a) = %q", got)
	}
}

func TestValid(t *testing.T) {
	for _, s := range []string{"a", "about", "o-proekte", "2026", "a-1-b"} {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false", s)
		}
	}
	for _, s := range []string{"", "-a", "a-", "a--b", "About", "о-проекте", "a b", "a_b", "a/b", "a.b", strings.Repeat("a", MaxLen+1)} {
		if Valid(s) {
			t.Errorf("Valid(%q) = true", s)
		}
	}
}

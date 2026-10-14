package menu

import (
	"strings"
	"testing"

	"github.com/keelkit/keelkit/reference/internal/listfield"
)

// items — пункты из строки «a b- c d-»: имя — id, «-» — подпункт.
func items(s string) []Item {
	var list []Item
	for _, f := range strings.Fields(s) {
		id, child := strings.CutSuffix(f, "-")
		list = append(list, Item{ID: id, Child: child})
	}
	return list
}

func str(list []Item) string {
	var out []string
	for _, it := range list {
		s := it.ID
		if it.Child {
			s += "-"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func TestApply(t *testing.T) {
	cases := []struct {
		in, op, id, want string
	}{
		// Пункт верхнего уровня переносится вместе с подпунктами.
		{"a b b1- b2- c", "up", "b", "b b1- b2- a c"},
		{"a b b1- b2- c", "down", "b", "a c b b1- b2-"},
		{"a a1- b b1-", "down", "a", "b b1- a a1-"},
		{"a b b1-", "up", "a", "a b b1-"},
		{"a b b1-", "down", "b", "a b b1-"},
		// Подпункт — только среди подпунктов своего пункта.
		{"a b b1- b2- c", "up", "b2", "a b b2- b1- c"},
		{"a b b1- b2- c", "up", "b1", "a b b1- b2- c"},
		{"a b b1- b2- c", "down", "b2", "a b b1- b2- c"},
		// Вложить — только пункт без подпунктов и не первый; вынести — подпункты ниже становятся его.
		{"a b c", "indent", "b", "a b- c"},
		{"a b c", "indent", "a", "a b c"},
		{"a b b1- c", "indent", "b", "a b b1- c"},
		{"a b b1- b2- b3-", "outdent", "b2", "a b b1- b2 b3-"},
		// Убрать пункт — вместе с подпунктами; первый подпункт без пункта становится пунктом.
		{"a b b1- b2- c", "remove", "b", "a c"},
		{"a b b1- c", "remove", "b1", "a b c"},
		{"a a1- b", "remove", "a", "b"},
	}
	for _, c := range cases {
		got, ok := apply(items(c.in), c.op, c.id)
		if !ok || str(got) != c.want {
			t.Errorf("%s %s on %q = %q, %v; want %q", c.op, c.id, c.in, str(got), ok, c.want)
		}
	}

	in := items("a b-")
	if _, ok := apply(in, "up", "zzz"); ok {
		t.Error("unknown item: ok")
	}
	if _, ok := apply(in, "hide", "a"); ok {
		t.Error("unknown op: ok")
	}
	if got, ok := apply(in, "add", ""); !ok || len(got) != 3 || got[2].Child || got[2].ID == "" {
		t.Errorf("add = %+v, %v", got, ok)
	}
	if str(in) != "a b-" {
		t.Errorf("apply changed its input: %q", str(in))
	}
	full := make([]Item, listfield.MaxItems)
	for i := range full {
		full[i].ID = listfield.NewID()
	}
	if got, _ := apply(full, "add", ""); len(got) != listfield.MaxItems {
		t.Errorf("add over MaxItems: %d items", len(got))
	}
}

func TestValidLink(t *testing.T) {
	for s, want := range map[string]bool{
		"/privacy":            true,
		"/blog?page=2":        true,
		"https://example.com": true,
		"http://example.com/": true,
		"//evil.example":      false,
		"/\\evil.example":     false,
		"javascript:alert(1)": false,
		"example.com":         false,
		"https://":            false,
		"/a b":                false,
		"":                    false,
	} {
		if got := validLink(s); got != want {
			t.Errorf("validLink(%q) = %v, want %v", s, got, want)
		}
	}
}

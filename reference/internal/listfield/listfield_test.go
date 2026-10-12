package listfield

import (
	"net/url"
	"slices"
	"strings"
	"testing"
)

type item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func itemID(it item) string      { return it.ID }
func itemIDPtr(it *item) *string { return &it.ID }

func ids(list []item) []string {
	var out []string
	for _, it := range list {
		out = append(out, it.ID)
	}
	return out
}

func TestApply(t *testing.T) {
	list := []item{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	add := func() item { return item{ID: "new"} }
	cases := []struct {
		op, item string
		want     []string
		ok       bool
	}{
		{"up", "b", []string{"b", "a", "c"}, true},
		{"up", "a", []string{"a", "b", "c"}, true},
		{"down", "b", []string{"a", "c", "b"}, true},
		{"down", "c", []string{"a", "b", "c"}, true},
		{"remove", "a", []string{"b", "c"}, true},
		{"add", "", []string{"a", "b", "c", "new"}, true},
		{"up", "zzz", nil, false},
		{"hide", "a", nil, false},
	}
	for _, c := range cases {
		got, ok := Apply(list, itemID, c.op, c.item, add)
		if ok != c.ok || (ok && !slices.Equal(ids(got), c.want)) {
			t.Errorf("%s %s = %v, %v; want %v, %v", c.op, c.item, ids(got), ok, c.want, c.ok)
		}
	}
	if !slices.Equal(ids(list), []string{"a", "b", "c"}) {
		t.Errorf("input changed: %v", ids(list))
	}

	full := make([]item, MaxItems)
	for i := range full {
		full[i].ID = NewID()
	}
	if got, ok := Apply(full, itemID, "add", "", add); !ok || len(got) != MaxItems {
		t.Errorf("add to full list: len %d, ok %v", len(got), ok)
	}
}

func TestAction(t *testing.T) {
	for _, a := range []Action{{"social", "up", "abc"}, {"social", "remove", "x-1"}, {"social", "add", ""}} {
		got, ok := ParseAction(ActionValue(a.Op, a.Field, a.Item))
		if !ok || got != a {
			t.Errorf("round trip %+v = %+v, %v", a, got, ok)
		}
	}
	if v := ActionValue("add", "social", ""); v != "list-add:social" {
		t.Errorf("add value = %q", v)
	}
	for _, s := range []string{"", "save", "up:abc", "item-up:b:i", "list-up:social", "list-add:social:abc", "list-up::abc"} {
		if a, ok := ParseAction(s); ok {
			t.Errorf("ParseAction(%q) = %+v, want not ok", s, a)
		}
	}
}

func TestIDs(t *testing.T) {
	form := url.Values{Order("social"): {"a1", "b2"}}
	if got, err := IDs(form, "social"); err != nil || !slices.Equal(got, []string{"a1", "b2"}) {
		t.Errorf("IDs = %v, %v", got, err)
	}
	if got, err := IDs(url.Values{}, "social"); err != nil || len(got) != 0 {
		t.Errorf("no items: %v, %v", got, err)
	}
	for _, bad := range [][]string{{"a1", "a1"}, {"Bad"}, {""}, {strings.Repeat("a", 33)}} {
		if _, err := IDs(url.Values{Order("social"): bad}, "social"); err == nil {
			t.Errorf("IDs(%q): no error", bad)
		}
	}
	if n := Name("social", "a1", "url"); n != "social-i-a1-url" {
		t.Errorf("Name = %q", n)
	}
}

func TestEncodeDecode(t *testing.T) {
	if s, err := Encode[item](nil); err != nil || s != "[]" {
		t.Errorf("Encode(nil) = %q, %v", s, err)
	}
	list := []item{{ID: "a", Name: "Telegram"}, {ID: "b", Name: "ВКонтакте"}}
	s, err := Encode(list)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(s, itemIDPtr)
	if err != nil || !slices.Equal(got, list) {
		t.Errorf("Decode(Encode) = %v, %v", got, err)
	}
	if got, err := Decode("", itemIDPtr); err != nil || got != nil {
		t.Errorf("Decode(\"\") = %v, %v", got, err)
	}
	if _, err := Decode("{", itemIDPtr); err == nil {
		t.Error("Decode of broken JSON: no error")
	}

	// Старые данные: нет id или повторный — новый, остальные не меняются.
	got, err = Decode(`[{"name":"x"},{"id":"a"},{"id":"a"},{"id":"BAD"}]`, itemIDPtr)
	if err != nil {
		t.Fatal(err)
	}
	if got[1].ID != "a" || got[0].ID == "" || got[2].ID == "a" || got[3].ID == "BAD" || !ValidIDs(ids(got)) {
		t.Errorf("fixed ids = %v", ids(got))
	}
}

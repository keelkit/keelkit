package blocks

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/voffkin/keelkit/modules/images"
)

func TestMarshalRoundTrip(t *testing.T) {
	list := []Block{
		{ID: "t1", Type: "text", Content: &Text{Body: "Привет, **мир**"}},
		{ID: "i1", Type: "image", Hidden: true, Content: &Image{Image: "img1", Alt: "Кот", Caption: "Подпись"}},
		{ID: "it1", Type: "image_text", Content: &ImageText{Image: "img2", Alt: "Пёс", Body: "Текст", Position: PositionRight}},
		{ID: "q1", Type: "quote", Content: &Quote{Text: "Писать код дёшево", Author: "Keelkit"}},
	}
	s, err := Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(s)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Marshal(got)
	if err != nil || again != s {
		t.Errorf("round trip changed JSON:\n%s\n%s (%v)", s, again, err)
	}
	if !got[1].Hidden || got[0].Hidden {
		t.Errorf("hidden flags: %v, %v", got[0].Hidden, got[1].Hidden)
	}
	if !strings.Contains(s, `"hidden":true`) || strings.Count(s, `"hidden"`) != 1 {
		t.Errorf("hidden is stored only when true: %s", s)
	}
}

// Сохранённый раньше JSON: нет поля position у image_text, нет id, повтор id, неизвестный тип.
func TestUnmarshalOldJSON(t *testing.T) {
	old := `[
		{"type":"text","body":"без id"},
		{"id":"a1","type":"image_text","image":"img","alt":"x","body":"y"},
		{"id":"a1","type":"quote","text":"повтор id"},
		{"id":"u1","type":"gallery","images":["i1","i2"],"hidden":true}
	]`
	list, err := Unmarshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("blocks = %d, want 4", len(list))
	}
	if list[0].ID == "" || !idPattern.MatchString(list[0].ID) {
		t.Errorf("block without id got %q", list[0].ID)
	}
	if list[2].ID == "a1" {
		t.Error("duplicate id kept")
	}
	it := list[1].Content.(*ImageText)
	if it.right() {
		t.Error("missing position must mean left")
	}
	if errs := Validate(list[1:2], Resolved{"img": &images.Image{ID: "img"}}); len(errs) != 0 || it.Position != PositionLeft {
		t.Errorf("validate old image_text: %v, position %q", errs, it.Position)
	}
	u, ok := list[3].Content.(*Unknown)
	if !ok {
		t.Fatalf("unknown type: content %T", list[3].Content)
	}
	// Неизвестный блок сохраняется как есть.
	s, err := Marshal(list[3:])
	if err != nil || !strings.Contains(s, `"images":["i1","i2"]`) || !strings.Contains(s, `"hidden":true`) {
		t.Errorf("unknown block marshal = %s, %v (raw %s)", s, err, u.Raw)
	}
	// На сайте не выводится и не роняет страницу.
	var buf bytes.Buffer
	list[3].Hidden = false
	if err := View(list[3:], nil).Render(context.Background(), &buf); err != nil || buf.Len() != 0 {
		t.Errorf("unknown block view = %q, %v", buf.String(), err)
	}
}

func TestUnmarshalInvalid(t *testing.T) {
	for _, s := range []string{`{}`, `[1]`, `not json`} {
		if _, err := Unmarshal(s); err == nil {
			t.Errorf("Unmarshal(%s): want error", s)
		}
	}
}

func TestParseAndValidate(t *testing.T) {
	form := url.Values{
		"block":              {"b2", "b1", "b3", "b4"},
		"block-b1-type":      {"text"},
		"block-b1-body":      {"строка\r\nвторая"},
		"block-b2-type":      {"image"},
		"block-b2-image":     {"missing"},
		"block-b2-hidden":    {"1"},
		"block-b3-type":      {"quote"},
		"block-b3-text":      {"  "},
		"block-b4-type":      {"image_text"},
		"block-b4-image":     {"img"},
		"block-b4-alt":       {"Описание"},
		"block-b4-body":      {"Текст"},
		"block-b4-position":  {"right"},
		"block-zz-type":      {"text"}, // нет в списке block — не блок
		"block-b3-unrelated": {"x"},
	}
	list, err := Parse(form)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 || list[0].ID != "b2" || list[1].ID != "b1" {
		t.Fatalf("order: %+v", list)
	}
	if !list[0].Hidden || list[1].Hidden {
		t.Error("hidden flags")
	}
	errs := Validate(list, Resolved{"img": &images.Image{ID: "img"}})
	want := []string{"block-b2-image", "block-b2-alt", "block-b3-text"}
	for _, k := range want {
		if errs[k] == "" {
			t.Errorf("no error for %s: %v", k, errs)
		}
	}
	if len(errs) != len(want) {
		t.Errorf("errors = %v, want only %v", errs, want)
	}
	if got := list[1].Content.(*Text).Body; got != "строка\nвторая" {
		t.Errorf("CRLF not normalized: %q", got)
	}
	if !list[3].Content.(*ImageText).right() {
		t.Error("position right lost")
	}
}

func TestParseRejectsTamperedForm(t *testing.T) {
	bad := []url.Values{
		{"block": {"Bad-ID"}, "block-Bad-ID-type": {"text"}},
		{"block": {"a", "a"}, "block-a-type": {"text"}},
		{"block": {"a"}, "block-a-type": {"script"}},
	}
	for _, f := range bad {
		if _, err := Parse(f); err == nil {
			t.Errorf("Parse(%v): want error", f)
		}
	}
}

func TestApply(t *testing.T) {
	list := []Block{
		{ID: "a", Type: "text", Content: &Text{}},
		{ID: "b", Type: "text", Content: &Text{}},
		{ID: "c", Type: "text", Content: &Text{}},
	}
	ids := func(l []Block) string {
		var s []string
		for _, b := range l {
			s = append(s, b.ID)
		}
		return strings.Join(s, ",")
	}
	allow := []string{"text", "quote"}
	tests := []struct {
		action, want string
		ok           bool
	}{
		{"up:b", "b,a,c", true},
		{"up:a", "a,b,c", true},
		{"down:b", "a,c,b", true},
		{"down:c", "a,b,c", true},
		{"remove:b", "a,c", true},
		{"hide:a", "a,b,c", true},
		{"up:zzz", "a,b,c", false},
		{"add:image", "a,b,c", false}, // нет в allow
		{"jump:a", "a,b,c", false},
	}
	for _, tt := range tests {
		got, ok := Apply(list, tt.action, allow)
		if ids(got) != tt.want || ok != tt.ok {
			t.Errorf("Apply(%s) = %s, %v; want %s, %v", tt.action, ids(got), ok, tt.want, tt.ok)
		}
	}
	if ids(list) != "a,b,c" || list[0].Hidden {
		t.Errorf("Apply changed its input: %s", ids(list))
	}
	got, _ := Apply(list, "hide:a", allow)
	if !got[0].Hidden {
		t.Error("hide did not hide")
	}
	got, ok := Apply(list, "add:quote", allow)
	if !ok || len(got) != 4 || got[3].Type != "quote" || got[3].ID == "" {
		t.Errorf("add: %+v", got)
	}
}

func TestImageIDsAndPlainText(t *testing.T) {
	list := []Block{
		{ID: "a", Type: "text", Content: &Text{Body: "Первый **абзац**"}},
		{ID: "b", Type: "image", Hidden: true, Content: &Image{Image: "i1", Caption: "скрытая подпись"}},
		{ID: "c", Type: "image_text", Content: &ImageText{Image: "i2", Body: "Рядом"}},
		{ID: "d", Type: "image", Content: &Image{Image: "i1", Caption: "Подпись"}},
		{ID: "e", Type: "quote", Content: &Quote{Text: "Цитата", Author: "Автор"}},
	}
	if got := strings.Join(ImageIDs(list), ","); got != "i1,i2" {
		t.Errorf("ImageIDs = %s, want i1,i2 (hidden blocks included, no duplicates)", got)
	}
	if got, want := PlainText(list), "Первый абзац Рядом Подпись Цитата Автор"; got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
	if got := Summary(list, 15); got != "Первый абзац…" {
		t.Errorf("Summary = %q", got)
	}
}

func TestViewSkipsHidden(t *testing.T) {
	list := []Block{
		{ID: "a", Type: "text", Content: &Text{Body: "видно"}},
		{ID: "b", Type: "quote", Hidden: true, Content: &Quote{Text: "не видно"}},
		{ID: "c", Type: "quote", Content: &Quote{Text: "абзац 1\n\nабзац 2", Author: "Автор"}},
	}
	var buf bytes.Buffer
	if err := View(list, nil).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{`id="block-a"`, "видно", `id="block-c"`, "<p>абзац 1</p>", "<p>абзац 2</p>", "<figcaption>Автор</figcaption>"} {
		if !strings.Contains(html, want) {
			t.Errorf("view has no %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "не видно") || strings.Contains(html, "block-b") {
		t.Errorf("hidden block rendered:\n%s", html)
	}
}

func TestKindsHaveTitles(t *testing.T) {
	for _, k := range Kinds {
		if k.Title == "" || !idPattern.MatchString(strings.ReplaceAll(k.Name, "_", "")) {
			t.Errorf("kind %+v", k)
		}
		b, ok := New(k.Name)
		if !ok || b.Content == nil {
			t.Errorf("New(%s) failed", k.Name)
		}
		// Пустой блок любого типа рендерится в форму и на сайт без паники.
		var buf bytes.Buffer
		if err := Form(b, FormEnv{}).Render(context.Background(), &buf); err != nil {
			t.Errorf("form %s: %v", k.Name, err)
		}
		if err := View([]Block{b}, nil).Render(context.Background(), &buf); err != nil {
			t.Errorf("view %s: %v", k.Name, err)
		}
	}
}

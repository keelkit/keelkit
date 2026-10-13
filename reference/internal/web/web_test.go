package web

import (
	"strings"
	"testing"
)

func TestTelURL(t *testing.T) {
	for in, want := range map[string]string{
		"+7 (495) 123-45-67": "tel:+74951234567",
		"8 800 555-35-35":    "tel:88005553535",
		" +44 20 7946 0958 ": "tel:+442079460958",
		"доб. 12":            "tel:12",
		"+":                  "",
		"звоните":            "",
		"":                   "",
	} {
		if got := TelURL(in); got != want {
			t.Errorf("TelURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocalPath(t *testing.T) {
	for in, want := range map[string]string{
		"/blog?page=2":         "/blog?page=2",
		"/":                    "/",
		"":                     "/",
		"blog":                 "/",
		"//evil.example/":      "/",
		"/\\evil.example/":     "/",
		"https://evil.example": "/",
	} {
		if got := localPath(in); got != want {
			t.Errorf("localPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMarkCurrent(t *testing.T) {
	links := []Link{
		{Title: "Главная", URL: "/"},
		{Title: "Блог", URL: "/blog", Children: []Link{{Title: "Новости", URL: "/blog/category/news"}}},
		{Title: "О нас", URL: "/about"},
		{Title: "Внешний", URL: "https://example.com/blog"},
	}
	for path, want := range map[string]string{
		"/":                       "Главная current",
		"/blog":                   "Блог current",
		"/blog/post-1":            "Блог active",
		"/blog/category/news":     "Блог active, Новости current",
		"/about":                  "О нас current",
		"/aboutus":                "",
		"/contacts":               "",
		"/blog/category/newsroom": "Блог active",
	} {
		var got []string
		var walk func([]Link)
		walk = func(ls []Link) {
			for _, l := range ls {
				switch {
				case l.Current:
					got = append(got, l.Title+" current")
				case l.Active:
					got = append(got, l.Title+" active")
				}
				walk(l.Children)
			}
		}
		marked := markCurrent(links, path)
		walk(marked)
		if s := strings.Join(got, ", "); s != want {
			t.Errorf("%s: %q, want %q", path, s, want)
		}
	}
	if links[1].Current || links[1].Children[0].Current {
		t.Error("markCurrent changed its input")
	}
}

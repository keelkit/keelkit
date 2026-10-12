package web

import "testing"

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

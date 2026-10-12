package settings

import "testing"

func TestVerification(t *testing.T) {
	for in, want := range map[string]string{
		"39d68ec668b4661d": "39d68ec668b4661d",
		"  abc_DEF-1  ":    "abc_DEF-1",
		`<meta name="yandex-verification" content="39d68ec668b4661d" />`: "39d68ec668b4661d",
		`<META NAME='google-site-verification' CONTENT=' x-Y_z '>`:       "x-Y_z",
		`<meta name="yandex-verification">`:                              `<meta name="yandex-verification">`,
		"":                                                               "",
	} {
		if got := verification(in); got != want {
			t.Errorf("verification(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidLink(t *testing.T) {
	for in, want := range map[string]bool{
		"/privacy":                true,
		"/docs/privacy.pdf?v=2":   true,
		"https://example.com/p":   true,
		"http://example.com":      true,
		"//example.com":           false,
		"/\\example.com":          false,
		"privacy":                 false,
		"javascript:alert(1)":     false,
		"mailto:info@example.com": false,
		"https://":                false,
		"/with space":             false,
	} {
		if got := validLink(in); got != want {
			t.Errorf("validLink(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestValidEmail(t *testing.T) {
	for in, want := range map[string]bool{
		"info@example.com":         true,
		"a.b+c@sub.example.ru":     true,
		"info@":                    false,
		"info example.com":         false,
		"Отдел <info@example.com>": false,
		"<info@example.com>":       false,
	} {
		if got := validEmail(in); got != want {
			t.Errorf("validEmail(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMultiLine(t *testing.T) {
	if got := multiLine(" Москва, \r\n\r\n  ул.  Примерная, 1 \n"); got != "Москва,\nул. Примерная, 1" {
		t.Errorf("multiLine = %q", got)
	}
}

package web

import (
	"net/http"
	"strings"
	"time"
)

// Баннер о cookie: «Понятно» отправляет форму на CookieNoticePath, ответ ставит cookie CookieNoticeName на год,
// и баннер больше не выводится. Работает без JavaScript (форма, 303 обратно на страницу), с htmx баннер просто
// исчезает.
const (
	CookieNoticePath = "/cookie-notice"
	CookieNoticeName = "cookie_notice"
	cookieNoticeAge  = 365 * 24 * time.Hour
)

func cookieAccepted(r *http.Request) bool {
	c, err := r.Cookie(CookieNoticeName)
	return err == nil && c.Value == "1"
}

// AcceptCookies — POST CookieNoticePath: запоминает согласие. Без htmx — 303 на страницу из поля back
// (только путь на этом сайте, иначе главная), с htmx — пустой ответ: баннер заменяется ничем.
// secure — флаг Secure у cookie (COOKIE_SECURE).
func AcceptCookies(secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure выключается только настройкой COOKIE_SECURE=false для разработки
			Name:     CookieNoticeName,
			Value:    "1",
			Path:     "/",
			MaxAge:   int(cookieNoticeAge.Seconds()),
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
		if r.Header.Get("HX-Request") == "true" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, localPath(r.PostFormValue("back")), http.StatusSeeOther) //nolint:gosec // localPath: только путь на этом сайте
	}
}

// localPath — путь на этом сайте или «/»: «//host» и «/\host» браузер понял бы как другой сайт.
func localPath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") {
		return "/"
	}
	return p
}

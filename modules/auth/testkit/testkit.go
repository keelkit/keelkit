// Package testkit — контрактные тесты модуля auth для сайтов.
//
// Сайт передаёт свой HTTP-обработчик (весь сервер) с настоящими страницами входа и админки, собранный
// с Config.Now = now, и пользователя, созданного через Service.CreateUser. Тесты ходят по сайту как браузер
// (HTTPS, cookie) и проверяют: без сессии — редирект на вход; вход и выход; cookie сессии HttpOnly, Secure,
// SameSite; форма входа без токена и POST без CSRF-токена сессии отклоняются (403); после неудачных попыток
// вход блокируется (429); сессия истекает.
//
// Что ожидается от сайта:
//
//   - GET LoginPath — форма с полем auth.CSRFField (скрытое поле с LoginCSRF); POST LoginPath принимает поля
//     email, password, auth.CSRFField и при успехе отвечает редиректом;
//
//   - GET ProtectedPath внутри RequireUser содержит форму с полем auth.CSRFField (например, кнопку выхода);
//
//   - POST LogoutPath внутри RequireUser вызывает Logout и отвечает редиректом;
//
//   - срок сессии (Config.SessionTTL) меньше года.
//
//     func TestAuthContract(t *testing.T) {
//     testkit.Run(t, func(t *testing.T, now func() time.Time) testkit.Site {
//     db := newTestDB(t) // миграции модуля и сайта применены
//     svc := auth.NewService(db, auth.Config{Now: now})
//     if _, err := svc.CreateUser(t.Context(), "admin@example.com", "correct horse"); err != nil {
//     t.Fatal(err)
//     }
//     return testkit.Site{Handler: newServer(db, svc), LoginPath: "/admin/login", ProtectedPath: "/admin",
//     LogoutPath: "/admin/logout", Email: "admin@example.com", Password: "correct horse"}
//     })
//     }
package testkit

import (
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keelkit/keelkit/modules/auth"
)

// Site — сайт, на котором идут контрактные тесты. Setup вызывается для каждого теста заново.
type Site struct {
	// Handler — HTTP-обработчик сайта, собранный с auth.Config{Now: now}.
	Handler http.Handler
	// LoginPath, ProtectedPath, LogoutPath — страница входа, страница внутри RequireUser, выход.
	LoginPath, ProtectedPath, LogoutPath string
	// Email и Password — существующий пользователь.
	Email, Password string
}

// maxAttempts — сколько неудачных попыток входа ждать блокировки.
const maxAttempts = 20

// sessionLifetimeBound — сдвиг часов, после которого сессия должна истечь.
const sessionLifetimeBound = 366 * 24 * time.Hour

// Run запускает контрактные тесты модуля как подтесты t.
func Run(t *testing.T, setup func(t *testing.T, now func() time.Time) Site) {
	t.Helper()
	t.Run("RedirectsToLogin", func(t *testing.T) { testRedirectsToLogin(newBrowser(t, setup)) })
	t.Run("LoginLogout", func(t *testing.T) { testLoginLogout(newBrowser(t, setup)) })
	t.Run("WrongPassword", func(t *testing.T) { testWrongPassword(newBrowser(t, setup)) })
	t.Run("LoginFormWithoutToken", func(t *testing.T) { testLoginFormWithoutToken(newBrowser(t, setup)) })
	t.Run("PostWithoutToken", func(t *testing.T) { testPostWithoutToken(newBrowser(t, setup)) })
	t.Run("RateLimit", func(t *testing.T) { testRateLimit(newBrowser(t, setup)) })
	t.Run("SessionExpires", func(t *testing.T) { testSessionExpires(newBrowser(t, setup)) })
}

func testRedirectsToLogin(b *browser) {
	resp, _ := b.get(b.site.ProtectedPath)
	b.wantRedirect(resp, b.site.LoginPath, "GET "+b.site.ProtectedPath+" without session")
	if cc := resp.header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		b.t.Errorf("GET %s: Cache-Control = %q, want no-store", b.site.ProtectedPath, cc)
	}
}

func testLoginLogout(b *browser) {
	resp := b.login(b.site.Password)
	if !isRedirect(resp.status) {
		b.t.Fatalf("login with correct password: status %d, want redirect", resp.status)
	}
	c := lastCookie(resp, auth.SessionCookie)
	if c == nil {
		b.t.Fatal("login: no session cookie")
	}
	if !c.HttpOnly || !c.Secure || (c.SameSite != http.SameSiteLaxMode && c.SameSite != http.SameSiteStrictMode) {
		b.t.Errorf("session cookie: HttpOnly %v, Secure %v, SameSite %v; want true, true, Lax or Strict",
			c.HttpOnly, c.Secure, c.SameSite)
	}

	resp, body := b.get(b.site.ProtectedPath)
	if resp.status != http.StatusOK {
		b.t.Fatalf("GET %s after login: status %d, want 200", b.site.ProtectedPath, resp.status)
	}
	token := b.csrfToken(body, b.site.ProtectedPath)
	resp, _ = b.post(b.site.LogoutPath, url.Values{auth.CSRFField: {token}})
	if !isRedirect(resp.status) {
		b.t.Fatalf("POST %s: status %d, want redirect", b.site.LogoutPath, resp.status)
	}
	resp, _ = b.get(b.site.ProtectedPath)
	b.wantRedirect(resp, b.site.LoginPath, "GET "+b.site.ProtectedPath+" after logout")
}

func testWrongPassword(b *browser) {
	resp := b.login(b.site.Password + "-wrong")
	if resp.status < 400 || resp.status >= 500 {
		b.t.Errorf("login with wrong password: status %d, want 4xx", resp.status)
	}
	if c := lastCookie(resp, auth.SessionCookie); c != nil && c.MaxAge >= 0 {
		b.t.Error("login with wrong password set a session cookie")
	}
	resp, _ = b.get(b.site.ProtectedPath)
	b.wantRedirect(resp, b.site.LoginPath, "GET "+b.site.ProtectedPath+" after wrong password")
}

func testLoginFormWithoutToken(b *browser) {
	b.get(b.site.LoginPath) // cookie формы есть, токена в запросе нет
	resp, _ := b.post(b.site.LoginPath, url.Values{"email": {b.site.Email}, "password": {b.site.Password}})
	if resp.status != http.StatusForbidden {
		b.t.Errorf("login form without token: status %d, want 403", resp.status)
	}
	resp, _ = b.get(b.site.ProtectedPath)
	b.wantRedirect(resp, b.site.LoginPath, "GET "+b.site.ProtectedPath+" after login without token")
}

func testPostWithoutToken(b *browser) {
	if resp := b.login(b.site.Password); !isRedirect(resp.status) {
		b.t.Fatalf("login: status %d, want redirect", resp.status)
	}
	for _, token := range []string{"", "wrong-token"} {
		form := url.Values{}
		if token != "" {
			form.Set(auth.CSRFField, token)
		}
		resp, _ := b.post(b.site.LogoutPath, form)
		if resp.status != http.StatusForbidden {
			b.t.Errorf("POST %s with token %q: status %d, want 403", b.site.LogoutPath, token, resp.status)
		}
	}
	// Отклонённый выход не завершил сессию.
	if resp, _ := b.get(b.site.ProtectedPath); resp.status != http.StatusOK {
		b.t.Errorf("GET %s after rejected POST: status %d, want 200", b.site.ProtectedPath, resp.status)
	}
}

func testRateLimit(b *browser) {
	for range maxAttempts {
		if resp := b.login(b.site.Password + "-wrong"); resp.status == http.StatusTooManyRequests {
			// Блокировка действует и на верный пароль.
			if resp := b.login(b.site.Password); resp.status != http.StatusTooManyRequests {
				b.t.Errorf("correct password while locked: status %d, want 429", resp.status)
			}
			return
		}
	}
	b.t.Errorf("no 429 after %d failed logins", maxAttempts)
}

func testSessionExpires(b *browser) {
	if resp := b.login(b.site.Password); !isRedirect(resp.status) {
		b.t.Fatalf("login: status %d, want redirect", resp.status)
	}
	b.clock.add(sessionLifetimeBound)
	resp, _ := b.get(b.site.ProtectedPath)
	b.wantRedirect(resp, b.site.LoginPath, "GET "+b.site.ProtectedPath+" after the session lifetime")
}

// clock — время сайта, которое тесты сдвигают.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// browser — клиент с cookie по HTTPS (cookie модуля — Secure); редиректам не следует.
type browser struct {
	t      *testing.T
	site   Site
	clock  *clock
	srv    *httptest.Server
	client *http.Client
}

func newBrowser(t *testing.T, setup func(t *testing.T, now func() time.Time) Site) *browser {
	t.Helper()
	c := &clock{now: time.Now()}
	site := setup(t, c.Now)
	srv := httptest.NewTLSServer(site.Handler)
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := srv.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &browser{t: t, site: site, clock: c, srv: srv, client: client}
}

// response — ответ с прочитанным телом.
type response struct {
	status  int
	header  http.Header
	cookies []*http.Cookie
}

func (b *browser) do(method, path string, form url.Values) (response, string) {
	b.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(b.t.Context(), method, b.srv.URL+path, body)
	if err != nil {
		b.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	return response{status: resp.StatusCode, header: resp.Header, cookies: resp.Cookies()}, string(data)
}

func (b *browser) get(path string) (response, string) {
	b.t.Helper()
	return b.do(http.MethodGet, path, nil)
}

func (b *browser) post(path string, form url.Values) (response, string) {
	b.t.Helper()
	return b.do(http.MethodPost, path, form)
}

// login открывает форму входа и отправляет её с паролем password.
func (b *browser) login(password string) response {
	b.t.Helper()
	resp, body := b.get(b.site.LoginPath)
	if resp.status != http.StatusOK {
		b.t.Fatalf("GET %s: status %d, want 200", b.site.LoginPath, resp.status)
	}
	resp, _ = b.post(b.site.LoginPath, url.Values{
		auth.CSRFField: {b.csrfToken(body, b.site.LoginPath)},
		"email":        {b.site.Email},
		"password":     {password},
	})
	return resp
}

var (
	inputTag  = regexp.MustCompile(`(?i)<input\b[^>]*>`)
	nameAttr  = regexp.MustCompile(`(?i)\bname\s*=\s*["']?` + regexp.QuoteMeta(auth.CSRFField) + `["'\s/>]`)
	valueAttr = regexp.MustCompile(`(?i)\bvalue\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

// csrfToken — значение первого поля auth.CSRFField на странице.
func (b *browser) csrfToken(page, path string) string {
	b.t.Helper()
	for _, tag := range inputTag.FindAllString(page, -1) {
		if !nameAttr.MatchString(tag) {
			continue
		}
		if m := valueAttr.FindStringSubmatch(tag); m != nil && m[1]+m[2] != "" {
			return html.UnescapeString(m[1] + m[2])
		}
	}
	b.t.Fatalf("GET %s: no <input name=%q> with a value", path, auth.CSRFField)
	return ""
}

// wantRedirect проверяет редирект на страницу с путём path (query не важен).
func (b *browser) wantRedirect(resp response, path, what string) {
	b.t.Helper()
	loc, err := url.Parse(resp.header.Get("Location"))
	if !isRedirect(resp.status) || err != nil || loc.Path != path {
		b.t.Errorf("%s: status %d, Location %q; want redirect to %s",
			what, resp.status, resp.header.Get("Location"), path)
	}
}

func isRedirect(status int) bool { return status >= 300 && status < 400 }

// lastCookie — последний Set-Cookie с именем name: браузер применяет их по порядку.
func lastCookie(resp response, name string) *http.Cookie {
	var found *http.Cookie
	for _, c := range resp.cookies {
		if c.Name == name {
			found = c
		}
	}
	return found
}

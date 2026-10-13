package main

// e2e админки: вход, выход, редирект без сессии, CSRF, ограничение попыток входа.

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// adminClient — «браузер»: хранит cookie и не следует редиректам, чтобы их можно было проверить.
func adminClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// post отправляет форму (и заголовки header) и возвращает ответ с прочитанным телом.
func post(t *testing.T, client *http.Client, u string, form url.Values, header http.Header) (response, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{StatusCode: resp.StatusCode, Header: resp.Header}, string(body)
}

func parse(t *testing.T, body string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// csrfToken — значение скрытого поля csrf_token в первой форме страницы.
func csrfToken(t *testing.T, doc *goquery.Document) string {
	t.Helper()
	v, ok := doc.Find(`input[name="csrf_token"]`).Attr("value")
	if !ok || v == "" {
		t.Fatal("no csrf_token in form")
	}
	return v
}

// openLogin открывает форму входа и возвращает её CSRF-токен.
func openLogin(t *testing.T, client *http.Client, srv *httptest.Server) string {
	t.Helper()
	resp, body := fetch(t, client, srv.URL+"/admin/login")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/login: status %d", resp.StatusCode)
	}
	return csrfToken(t, parse(t, body))
}

// login входит под email и password и возвращает ответ на POST /admin/login.
func login(t *testing.T, client *http.Client, srv *httptest.Server, email, password string) (response, string) {
	t.Helper()
	token := openLogin(t, client, srv)
	return post(t, client, srv.URL+"/admin/login", url.Values{
		"csrf_token": {token}, "email": {email}, "password": {password}, "next": {"/admin"},
	}, nil)
}

// loginOK входит под dev-пользователем из seed и возвращает CSRF-токен сессии со страницы дашборда.
func loginOK(t *testing.T, client *http.Client, srv *httptest.Server) string {
	t.Helper()
	resp, _ := login(t, client, srv, seedAdminEmail, seedAdminPassword)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin" {
		t.Fatalf("login: status %d, Location %q; want 303 to /admin", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, body := fetch(t, client, srv.URL+"/admin")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin after login: status %d", resp.StatusCode)
	}
	return csrfToken(t, parse(t, body))
}

func TestE2EAdminRequiresLogin(t *testing.T) {
	srv := e2eServer(t, true)
	client := adminClient(t)

	for path, want := range map[string]string{
		"/admin":       "/admin/login?next=%2Fadmin",
		"/admin/pages": "/admin/login?next=%2Fadmin%2Fpages",
	} {
		resp, _ := fetch(t, client, srv.URL+path)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Errorf("GET %s: status %d, Location %q; want 303 to %s", path, resp.StatusCode, resp.Header.Get("Location"), want)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("GET %s: Cache-Control %q, want no-store", path, cc)
		}
	}

	// POST без сессии не выполняется: редирект на вход.
	resp, _ := post(t, client, srv.URL+"/admin/pages", url.Values{"title": {"x"}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login" {
		t.Errorf("POST /admin/pages: status %d, Location %q; want 303 to /admin/login", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Форма входа не индексируется и не кэшируется.
	resp, body := fetch(t, client, srv.URL+"/admin/login")
	doc := parse(t, body)
	if robots, _ := doc.Find(`meta[name="robots"]`).Attr("content"); robots != "noindex, nofollow" {
		t.Errorf("robots = %q", robots)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("login Cache-Control %q, want no-store", cc)
	}
}

func TestE2EAdminLoginLogout(t *testing.T) {
	srv := e2eServer(t, true)
	client := adminClient(t)

	resp, _ := login(t, client, srv, " Admin@Example.com ", seedAdminPassword) // email нормализуется
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin" {
		t.Fatalf("login: status %d, Location %q; want 303 to /admin", resp.StatusCode, resp.Header.Get("Location"))
	}
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "session" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie")
	}
	if !session.HttpOnly || session.SameSite != http.SameSiteLaxMode || session.Secure {
		t.Errorf("session cookie: HttpOnly %v, SameSite %v, Secure %v; want true, Lax, false (dev)",
			session.HttpOnly, session.SameSite, session.Secure)
	}

	// Дашборд: счётчики контента, email пользователя, кнопка выхода.
	resp, body := fetch(t, client, srv.URL+"/admin")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin: status %d", resp.StatusCode)
	}
	doc := parse(t, body)
	if got, want := text(doc.Find(".counter").First()), "Страниц: "+strconv.Itoa(len(seedPages(nil))); got != want {
		t.Errorf("counter = %q, want %q", got, want)
	}
	if got := text(doc.Find(".admin-user")); got != seedAdminEmail {
		t.Errorf("user = %q", got)
	}
	token := csrfToken(t, doc)
	if hx, _ := doc.Find("body").Attr("hx-headers"); !strings.Contains(hx, token) {
		t.Errorf("hx-headers = %q: no CSRF token", hx)
	}

	// Вошедшего пользователя форма входа отправляет на дашборд.
	if resp, _ := fetch(t, client, srv.URL+"/admin/login"); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("GET /admin/login when signed in: status %d, want 303", resp.StatusCode)
	}

	// Неизвестный раздел — 404 админки.
	resp, body = fetch(t, client, srv.URL+"/admin/nope")
	if resp.StatusCode != http.StatusNotFound || text(parse(t, body).Find("main h1")) != "Не найдено" {
		t.Errorf("GET /admin/nope: status %d", resp.StatusCode)
	}

	// Выход: сессия удалена, cookie сброшена, админка снова требует входа.
	resp, _ = post(t, client, srv.URL+"/admin/logout", url.Values{"csrf_token": {token}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login" {
		t.Fatalf("logout: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := fetch(t, client, srv.URL+"/admin"); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("GET /admin after logout: status %d, want 303", resp.StatusCode)
	}
	// Старая cookie сессии больше не действует, даже если её сохранили.
	old := &http.Client{CheckRedirect: client.CheckRedirect}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/admin", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: session.Value})
	r, err := old.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusSeeOther {
		t.Errorf("GET /admin with old session cookie: status %d, want 303", r.StatusCode)
	}
}

func TestE2EAdminLoginNext(t *testing.T) {
	srv := e2eServer(t, true)
	client := adminClient(t)

	resp, body := fetch(t, client, srv.URL+"/admin/login?next=%2Fadmin%2Fnope")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	doc := parse(t, body)
	next, _ := doc.Find(`input[name="next"]`).Attr("value")
	resp, _ = post(t, client, srv.URL+"/admin/login", url.Values{
		"csrf_token": {csrfToken(t, doc)}, "email": {seedAdminEmail}, "password": {seedAdminPassword}, "next": {next},
	}, nil)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/admin/nope" {
		t.Errorf("login with next: status %d, Location %q; want 303 to /admin/nope", resp.StatusCode, loc)
	}

	// Внешний адрес в next игнорируется.
	client = adminClient(t)
	token := openLogin(t, client, srv)
	resp, _ = post(t, client, srv.URL+"/admin/login", url.Values{
		"csrf_token": {token}, "email": {seedAdminEmail}, "password": {seedAdminPassword}, "next": {"//evil.example/"},
	}, nil)
	if loc := resp.Header.Get("Location"); loc != "/admin" {
		t.Errorf("login with external next: Location %q, want /admin", loc)
	}
}

func TestE2EAdminWrongPassword(t *testing.T) {
	srv := e2eServer(t, true)
	client := adminClient(t)

	for _, email := range []string{seedAdminEmail, "nobody@example.com"} {
		resp, body := login(t, client, srv, email, "wrong")
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status %d, want 422", email, resp.StatusCode)
		}
		doc := parse(t, body)
		// Сообщение одинаковое: по нему нельзя узнать, есть ли пользователь.
		if got := text(doc.Find(".form-error")); got != "Неверный email или пароль." {
			t.Errorf("%s: error = %q", email, got)
		}
		if got, _ := doc.Find(`input[name="email"]`).Attr("value"); got != email {
			t.Errorf("%s: email not kept in form: %q", email, got)
		}
		if doc.Find(`input[name="password"][value]`).Length() != 0 {
			t.Errorf("%s: password is echoed back", email)
		}
		for _, c := range resp.Cookies() {
			if c.Name == "session" {
				t.Errorf("%s: session cookie set on failed login", email)
			}
		}
	}
}

func TestE2EAdminLoginLockout(t *testing.T) {
	srv := e2eServer(t, true)
	client := adminClient(t)

	for i := range 5 {
		if resp, _ := login(t, client, srv, seedAdminEmail, "wrong"); resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d: status %d, want 422", i+1, resp.StatusCode)
		}
	}
	// После 5 неудач вход заблокирован, даже с верным паролем.
	resp, body := login(t, client, srv, seedAdminEmail, seedAdminPassword)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after 5 failures: status %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
	if got := text(parse(t, body).Find(".form-error")); !strings.HasPrefix(got, "Слишком много неудачных попыток") {
		t.Errorf("error = %q", got)
	}
	// Другой email блокировкой не затронут.
	if resp, _ := login(t, client, srv, "other@example.com", "wrong"); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("other email: status %d, want 422", resp.StatusCode)
	}
}

func TestE2EAdminCSRF(t *testing.T) {
	srv := e2eServer(t, true)

	// Вход без токена или с чужим токеном — 403, сессии нет.
	client := adminClient(t)
	openLogin(t, client, srv)
	for _, token := range []string{"", "forged"} {
		resp, body := post(t, client, srv.URL+"/admin/login", url.Values{
			"csrf_token": {token}, "email": {seedAdminEmail}, "password": {seedAdminPassword},
		}, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("login with token %q: status %d, want 403", token, resp.StatusCode)
		}
		if got := text(parse(t, body).Find(".form-error")); got == "" {
			t.Errorf("login with token %q: no error message", token)
		}
	}

	// Межсайтовый запрос отклоняется по заголовкам браузера ещё до проверки токена.
	token := openLogin(t, client, srv)
	resp, _ := post(t, client, srv.URL+"/admin/login", url.Values{
		"csrf_token": {token}, "email": {seedAdminEmail}, "password": {seedAdminPassword},
	}, http.Header{"Sec-Fetch-Site": {"cross-site"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site login: status %d, want 403", resp.StatusCode)
	}

	// Вошедший пользователь: POST без токена — 403, сессия при этом жива.
	sessionToken := loginOK(t, client, srv)
	for _, form := range []url.Values{{}, {"csrf_token": {"forged"}}, {"csrf_token": {token}}} {
		resp, body := post(t, client, srv.URL+"/admin/logout", form, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("logout with %v: status %d, want 403", form, resp.StatusCode)
		}
		if got := text(parse(t, body).Find("main h1")); got != "Доступ запрещён" {
			t.Errorf("logout with %v: h1 = %q", form, got)
		}
	}
	if resp, _ := fetch(t, client, srv.URL+"/admin"); resp.StatusCode != http.StatusOK {
		t.Errorf("GET /admin after rejected logout: status %d, want 200", resp.StatusCode)
	}

	// Токен в заголовке (так его передаёт htmx) принимается.
	resp, _ = post(t, client, srv.URL+"/admin/logout", nil, http.Header{"X-Csrf-Token": {sessionToken}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("logout with header token: status %d, want 303", resp.StatusCode)
	}
}

// Вне локальной разработки cookie получают флаг Secure.
func TestE2EAdminSecureCookies(t *testing.T) {
	srv := httptest.NewServer(migratedSite(t, true).handler(config{CookieSecure: true}))
	t.Cleanup(srv.Close)

	resp, _ := fetch(t, adminClient(t), srv.URL+"/admin/login")
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "admin_csrf" {
		t.Fatalf("cookies = %v, want admin_csrf", cookies)
	}
	if c := cookies[0]; !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("admin_csrf: Secure %v, HttpOnly %v, SameSite %v; want true, true, Lax", c.Secure, c.HttpOnly, c.SameSite)
	}
}

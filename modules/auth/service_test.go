package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/keelkit/keelkit/modules/auth/migrations"
)

// openDB — пустая база с прагмами, как у сайта.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "site.db")+
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func migrate(t *testing.T, db *sql.DB) {
	t.Helper()
	p, err := goose.NewProvider(goose.DialectSQLite3, db, migrations.FS,
		goose.WithTableName(migrations.Table), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// clock — управляемое время для Config.Now.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

const (
	testEmail    = "admin@example.com"
	testPassword = "correct horse"
)

type env struct {
	db    *sql.DB
	svc   *Service
	clock *clock
	srv   *httptest.Server
}

// newEnv — сервис с пользователем testEmail и сервер с маршрутами, как у сайта.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := openDB(t)
	migrate(t, db)
	c := &clock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	svc := NewService(db, Config{Now: c.Now})
	if _, err := svc.CreateUser(context.Background(), testEmail, testPassword); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, svc.LoginCSRF(w, r))
	})
	mux.HandleFunc("POST /admin/login", func(w http.ResponseWriter, r *http.Request) {
		_, err := svc.Login(w, r, r.PostFormValue("email"), r.PostFormValue("password"))
		var locked *LockedError
		switch {
		case err == nil:
			http.Redirect(w, r, SafeNext(r.PostFormValue("next"), "/admin"), http.StatusSeeOther)
		case errors.Is(err, ErrLoginCSRF):
			http.Error(w, "stale form", http.StatusForbidden)
		case errors.As(err, &locked):
			http.Error(w, "locked", http.StatusTooManyRequests)
		case errors.Is(err, ErrBadCredentials):
			http.Error(w, "bad credentials", http.StatusUnprocessableEntity)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	admin := func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		_, _ = fmt.Fprintf(w, "%s %s %s", u.Email, u.Role, CSRFToken(r.Context()))
	}
	mux.Handle("GET /admin", svc.RequireUser(http.HandlerFunc(admin)))
	mux.Handle("POST /admin/save", svc.RequireUser(http.HandlerFunc(admin)))
	mux.Handle("POST /admin/logout", svc.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := svc.Logout(w, r); err != nil {
			t.Error(err)
		}
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	})))

	// TLS: cookie модуля — Secure, по http клиент их не отправит.
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return &env{db: db, svc: svc, clock: c, srv: srv}
}

// client — браузер: хранит cookie, не следует редиректам.
func (e *env) client(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := *e.srv.Client() // копия: у каждого браузера свои cookie
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// reply — ответ с уже прочитанным и закрытым телом.
type reply struct {
	StatusCode int
	Header     http.Header
	cookies    []*http.Cookie
}

func (r reply) Cookies() []*http.Cookie { return r.cookies }

func do(t *testing.T, c *http.Client, method, url string, form url.Values, header http.Header) (reply, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	maps.Copy(req.Header, header)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, cookies: resp.Cookies()}, string(body)
}

// login открывает форму входа и отправляет её.
func (e *env) login(t *testing.T, c *http.Client, email, password string) reply {
	t.Helper()
	_, token := do(t, c, http.MethodGet, e.srv.URL+"/admin/login", nil, nil)
	resp, _ := do(t, c, http.MethodPost, e.srv.URL+"/admin/login", url.Values{
		CSRFField: {token}, "email": {email}, "password": {password}, "next": {"/admin?x=1"},
	}, nil)
	return resp
}

// cookieNamed — последний Set-Cookie с именем name: браузер применяет их по порядку
// (вход сначала удаляет cookie прежней сессии, затем ставит новую).
func cookieNamed(resp reply, name string) *http.Cookie {
	var found *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == name {
			found = c
		}
	}
	return found
}

func TestLoginAndLogout(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)

	resp, _ := do(t, c, http.MethodGet, e.srv.URL+"/admin?x=1", nil, nil)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/admin/login?next=%2Fadmin%3Fx%3D1" {
		t.Fatalf("without session: %d %q; want 303 to login with next", resp.StatusCode, loc)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}

	resp = e.login(t, c, " Admin@Example.com ", testPassword) // email нормализуется
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/admin?x=1" {
		t.Fatalf("login: %d %q; want 303 to /admin?x=1", resp.StatusCode, loc)
	}
	sc := cookieNamed(resp, SessionCookie)
	if sc == nil || !sc.HttpOnly || !sc.Secure || sc.SameSite != http.SameSiteLaxMode || sc.MaxAge != int(DefaultSessionTTL/time.Second) {
		t.Errorf("session cookie = %+v; want HttpOnly, Secure, SameSite=Lax, MaxAge 14 days", sc)
	}
	if lc := cookieNamed(resp, loginCSRFCookie); lc == nil || lc.MaxAge >= 0 {
		t.Errorf("login csrf cookie = %+v; want deleted", lc)
	}

	_, body := do(t, c, http.MethodGet, e.srv.URL+"/admin", nil, nil)
	fields := strings.Fields(body)
	if len(fields) != 3 || fields[0] != testEmail || fields[1] != "admin" || fields[2] == "" {
		t.Fatalf("admin page = %q; want email, role, csrf token", body)
	}
	csrf := fields[2]

	// POST без токена, с чужим токеном — 403; с токеном в поле или заголовке — проходит.
	if resp, _ := do(t, c, http.MethodPost, e.srv.URL+"/admin/save", url.Values{}, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST without csrf: %d, want 403", resp.StatusCode)
	}
	if resp, _ := do(t, c, http.MethodPost, e.srv.URL+"/admin/save", url.Values{CSRFField: {"wrong"}}, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST with wrong csrf: %d, want 403", resp.StatusCode)
	}
	if resp, _ := do(t, c, http.MethodPost, e.srv.URL+"/admin/save", url.Values{CSRFField: {csrf}}, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("POST with csrf field: %d, want 200", resp.StatusCode)
	}
	h := http.Header{CSRFHeader: {csrf}}
	if resp, _ := do(t, c, http.MethodPost, e.srv.URL+"/admin/save", url.Values{}, h); resp.StatusCode != http.StatusOK {
		t.Errorf("POST with csrf header: %d, want 200", resp.StatusCode)
	}

	resp, _ = do(t, c, http.MethodPost, e.srv.URL+"/admin/logout", url.Values{CSRFField: {csrf}}, nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if n := countSessions(t, e.db); n != 0 {
		t.Errorf("sessions after logout = %d, want 0", n)
	}
	if resp, _ := do(t, c, http.MethodGet, e.srv.URL+"/admin", nil, nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("after logout: %d, want 303", resp.StatusCode)
	}
}

func countSessions(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRequireUserPOSTWithoutSession(t *testing.T) {
	e := newEnv(t)
	resp, _ := do(t, e.client(t), http.MethodPost, e.srv.URL+"/admin/save", url.Values{}, nil)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/admin/login" {
		t.Errorf("POST without session: %d %q; want 303 to /admin/login without next", resp.StatusCode, loc)
	}
}

func TestLoginCSRF(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	form := url.Values{"email": {testEmail}, "password": {testPassword}}

	// Без cookie формы (чужой сайт отправляет форму) — отказ, даже с токеном в поле.
	form.Set(CSRFField, "forged")
	if resp, _ := do(t, c, http.MethodPost, e.srv.URL+"/admin/login", form, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("login without csrf cookie: %d, want 403", resp.StatusCode)
	}
	// Cookie есть, токен в поле другой.
	do(t, c, http.MethodGet, e.srv.URL+"/admin/login", nil, nil)
	if resp, _ := do(t, c, http.MethodPost, e.srv.URL+"/admin/login", form, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("login with wrong csrf: %d, want 403", resp.StatusCode)
	}
	if n := countSessions(t, e.db); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)

	for i := range DefaultMaxFailures {
		if resp := e.login(t, c, testEmail, "wrong password"); resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("failure %d: %d, want 422", i+1, resp.StatusCode)
		}
	}
	// Заблокирован даже верный пароль; Retry-After — до конца окна.
	resp := e.login(t, c, testEmail, testPassword)
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "900" {
		t.Fatalf("locked: %d, Retry-After %q; want 429, 900", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	// Неизвестный email блокируется отдельно: другой пользователь входит.
	if _, err := e.svc.CreateUser(context.Background(), "editor@example.com", testPassword); err != nil {
		t.Fatal(err)
	}
	if resp := e.login(t, e.client(t), "editor@example.com", testPassword); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("other user while first is locked: %d, want 303", resp.StatusCode)
	}

	e.clock.Add(DefaultFailureWindow)
	if resp := e.login(t, c, testEmail, testPassword); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("after the window: %d, want 303", resp.StatusCode)
	}
}

func TestSessionExpires(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	if resp := e.login(t, c, testEmail, testPassword); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	e.clock.Add(DefaultSessionTTL - time.Second)
	if resp, _ := do(t, c, http.MethodGet, e.srv.URL+"/admin", nil, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("before expiry: %d, want 200", resp.StatusCode)
	}
	e.clock.Add(time.Second)
	if resp, _ := do(t, c, http.MethodGet, e.srv.URL+"/admin", nil, nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("after expiry: %d, want 303", resp.StatusCode)
	}

	// Следующий вход удаляет истёкшие сессии.
	if resp := e.login(t, e.client(t), testEmail, testPassword); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	if n := countSessions(t, e.db); n != 1 {
		t.Errorf("sessions = %d, want 1 (expired removed)", n)
	}
}

// Вход с действующей сессией заменяет её новой (защита от фиксации сессии).
func TestLoginReplacesSession(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	first := cookieNamed(e.login(t, c, testEmail, testPassword), SessionCookie)
	second := cookieNamed(e.login(t, c, testEmail, testPassword), SessionCookie)
	if first == nil || second == nil || first.Value == second.Value {
		t.Fatalf("session tokens %v, %v; want two different", first, second)
	}
	if n := countSessions(t, e.db); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
	var stored string
	if err := e.db.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != hashToken(second.Value) || strings.Contains(stored, second.Value) {
		t.Error("sessions stores not the SHA-256 of the current token")
	}
}

// Хеш со слабыми параметрами пересчитывается при входе.
func TestLoginRehash(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	weak := argonParams{memory: 8 * 1024, time: 1, threads: 1, saltLen: 16, keyLen: 32}
	hash, err := hashPassword(ctx, testPassword, weak)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE users SET password_hash = ? WHERE email = ?`, hash, testEmail); err != nil {
		t.Fatal(err)
	}
	if resp := e.login(t, e.client(t), testEmail, testPassword); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	if err := e.db.QueryRow(`SELECT password_hash FROM users WHERE email = ?`, testEmail).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("hash after login = %q, want default params", hash)
	}
}

func TestCurrentUser(t *testing.T) {
	e := newEnv(t)
	c := e.client(t)
	e.login(t, c, testEmail, testPassword)
	u, _ := url.Parse(e.srv.URL + "/admin/login")
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	for _, ck := range c.Jar.Cookies(u) {
		req.AddCookie(ck)
	}
	user, ok, err := e.svc.CurrentUser(req)
	if err != nil || !ok || user.Email != testEmail {
		t.Errorf("CurrentUser = %+v, %v, %v; want %s", user, ok, err, testEmail)
	}
	if _, ok, err := e.svc.CurrentUser(httptest.NewRequest(http.MethodGet, "/", nil)); ok || err != nil {
		t.Errorf("CurrentUser without cookie = %v, %v; want false, nil", ok, err)
	}
}

func TestCreateUser(t *testing.T) {
	db := openDB(t)
	migrate(t, db)
	svc := NewService(db, Config{})
	ctx := context.Background()

	u, err := svc.CreateUser(ctx, "Editor@Example.com", testPassword)
	if err != nil || u.ID == 0 || u.Email != "editor@example.com" || u.Role != "admin" {
		t.Fatalf("CreateUser = %+v, %v", u, err)
	}
	if _, err := svc.CreateUser(ctx, "editor@example.COM", testPassword); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate: %v, want ErrUserExists", err)
	}
	for _, email := range []string{"", "not-an-email", "Name <a@example.com>", "a@example.com, b@example.com"} {
		if _, err := svc.CreateUser(ctx, email, testPassword); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("CreateUser(%q): %v, want ErrInvalidEmail", email, err)
		}
	}
	if _, err := svc.CreateUser(ctx, "short@example.com", "short"); !errors.Is(err, ErrPasswordLength) {
		t.Errorf("short password: %v, want ErrPasswordLength", err)
	}
}

// Смена пароля: новый пароль входит, старый — нет, сессии пользователя удалены, чужие — нет.
func TestSetPassword(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.CreateUser(ctx, "editor@example.com", testPassword); err != nil {
		t.Fatal(err)
	}
	admin, editor := e.client(t), e.client(t)
	e.login(t, admin, testEmail, testPassword)
	editorResp := e.login(t, editor, "editor@example.com", testPassword)
	if editorResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("editor login: %d", editorResp.StatusCode)
	}

	const newPassword = "battery staple"
	if err := e.svc.SetPassword(ctx, " Admin@Example.com ", newPassword); err != nil {
		t.Fatal(err)
	}
	if resp, _ := do(t, admin, http.MethodGet, e.srv.URL+"/admin", nil, nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("admin session after password change: %d, want redirect to login", resp.StatusCode)
	}
	if resp, _ := do(t, editor, http.MethodGet, e.srv.URL+"/admin", nil, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("other user's session after password change: %d, want 200", resp.StatusCode)
	}
	if resp := e.login(t, e.client(t), testEmail, testPassword); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("old password: %d, want 422", resp.StatusCode)
	}
	if resp := e.login(t, e.client(t), testEmail, newPassword); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("new password: %d, want 303", resp.StatusCode)
	}

	if err := e.svc.SetPassword(ctx, testEmail, "short"); !errors.Is(err, ErrPasswordLength) {
		t.Errorf("short password: %v, want ErrPasswordLength", err)
	}
	if err := e.svc.SetPassword(ctx, "nobody@example.com", newPassword); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown email: %v, want ErrUserNotFound", err)
	}
}

// Удаление пользователя удаляет его сессии (внешний ключ ON DELETE CASCADE).
func TestDeleteUserCascadesSessions(t *testing.T) {
	e := newEnv(t)
	e.login(t, e.client(t), testEmail, testPassword)
	if _, err := e.db.Exec(`DELETE FROM users WHERE email = ?`, testEmail); err != nil {
		t.Fatal(err)
	}
	if n := countSessions(t, e.db); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

// База, где users и sessions созданы миграцией сайта до выноса авторизации в модуль:
// миграция модуля проходит и ничего не меняет, данные на месте.
func TestMigrateExistingTables(t *testing.T) {
	db := openDB(t)
	sqlText, err := migrations.FS.ReadFile("00001_auth.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Та же схема без IF NOT EXISTS — как в миграции сайта 00003.
	legacy := strings.ReplaceAll(string(sqlText), " IF NOT EXISTS", "")
	if _, err := db.Exec(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (email, password_hash) VALUES ('old@example.com', 'x')`); err != nil {
		t.Fatal(err)
	}
	migrate(t, db)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil || n != 1 {
		t.Errorf("users after migrate = %d, %v; want 1", n, err)
	}
}

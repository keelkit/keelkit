package testkit_test

import (
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/voffkin/keelkit/modules/auth"
	"github.com/voffkin/keelkit/modules/auth/migrations"
	"github.com/voffkin/keelkit/modules/auth/testkit"
)

// Мини-сайт, устроенный как эталонный: страница входа и админка рисуются сайтом, вход — через Service.
func TestRun(t *testing.T) {
	testkit.Run(t, func(t *testing.T, now func() time.Time) testkit.Site {
		db := newDB(t)
		svc := auth.NewService(db, auth.Config{Now: now})
		if _, err := svc.CreateUser(t.Context(), "admin@example.com", "correct horse"); err != nil {
			t.Fatal(err)
		}
		return testkit.Site{
			Handler:       newSite(svc),
			LoginPath:     "/admin/login",
			ProtectedPath: "/admin",
			LogoutPath:    "/admin/logout",
			Email:         "admin@example.com",
			Password:      "correct horse",
		}
	})
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "site.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p, err := goose.NewProvider(goose.DialectSQLite3, db, migrations.FS,
		goose.WithTableName(migrations.Table), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func newSite(svc *auth.Service) http.Handler {
	loginPage := func(w http.ResponseWriter, r *http.Request, status int, msg string) {
		token := svc.LoginCSRF(w, r) // ставит cookie: до WriteHeader
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `<p>%s</p><form method="post"><input type="hidden" name="%s" value="%s">`+
			`<input name="email"><input type="password" name="password"></form>`,
			html.EscapeString(msg), auth.CSRFField, html.EscapeString(token))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/login", func(w http.ResponseWriter, r *http.Request) {
		loginPage(w, r, http.StatusOK, "")
	})
	mux.HandleFunc("POST /admin/login", func(w http.ResponseWriter, r *http.Request) {
		_, err := svc.Login(w, r, r.PostFormValue("email"), r.PostFormValue("password"))
		var locked *auth.LockedError
		switch {
		case err == nil:
			http.Redirect(w, r, auth.SafeNext(r.PostFormValue("next"), "/admin"), http.StatusSeeOther)
		case errors.Is(err, auth.ErrLoginCSRF):
			loginPage(w, r, http.StatusForbidden, "Форма устарела")
		case errors.As(err, &locked):
			loginPage(w, r, http.StatusTooManyRequests, "Слишком много попыток")
		case errors.Is(err, auth.ErrBadCredentials):
			loginPage(w, r, http.StatusUnprocessableEntity, "Неверный email или пароль")
		default:
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
	mux.Handle("GET /admin", svc.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `<form method="post" action="/admin/logout"><input type="hidden" name="%s" value="%s">`+
			`<button>Выйти</button></form>`, auth.CSRFField, html.EscapeString(auth.CSRFToken(r.Context())))
	})))
	mux.Handle("POST /admin/logout", svc.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := svc.Logout(w, r); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	})))
	return http.NewCrossOriginProtection().Handler(mux)
}

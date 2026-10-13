// Package admin — оболочка админки сайта: страница входа, меню, дашборд, страницы ошибок.
//
// Пользователи, пароли, сессии и CSRF-токены — модуль auth (github.com/voffkin/keelkit/modules/auth):
// пакет рисует форму входа и вызывает его Service. «Простой режим»: любой пользователь — администратор.
//
// Пакет не знает о типах контента: счётчики дашборда передаёт cmd/site через Options.Counters.
package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/a-h/templ"

	"github.com/voffkin/keelkit/modules/auth"

	"github.com/voffkin/keelkit/reference/internal/web"
)

// Пути админки.
const (
	dashboardPath = "/admin"
	loginPath     = "/admin/login"
	logoutPath    = "/admin/logout"
)

// Counter — счётчик на дашборде: «Страниц: 3». URL — ссылка на раздел (необязательно).
type Counter struct {
	Label string
	Value int64
	URL   string
}

// Section — раздел админки в меню: «Страницы» → /admin/pages. Пункт подсвечивается на URL и вложенных путях.
// Icon — иконка пункта: "pages", "posts", "tags", "people", "image", "files", "settings", "menu"; пусто или неизвестное имя — общая иконка папки.
type Section struct {
	Title string
	URL   string
	Icon  string
}

// Options — настройки админки.
type Options struct {
	// SecureCookies — флаг Secure у cookie. Выключается только для локальной разработки по http.
	SecureCookies bool
	// Now — текущее время для модуля auth (срок сессии, блокировка входа); для тестов. По умолчанию time.Now.
	Now func() time.Time
	// Counters — счётчики контента для дашборда.
	Counters func(context.Context) ([]Counter, error)
	// Sections — разделы типов контента в меню, по порядку.
	Sections []Section
	// SiteSections — разделы всего сайта (настройки, позже меню) в группе «Сайт» под контентом.
	SiteSections []Section
}

// Handler — хендлеры и middleware админки.
type Handler struct {
	auth *auth.Service
	opts Options
}

func NewHandler(db *sql.DB, opts Options) *Handler {
	h := &Handler{opts: opts}
	h.auth = auth.NewService(db, auth.Config{
		LoginPath:       loginPath,
		InsecureCookies: !opts.SecureCookies,
		Forbidden: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.render(w, r, http.StatusForbidden, ForbiddenPage())
		}),
		ServerError: ServerError,
		Now:         opts.Now,
	})
	return h
}

// shell — данные оболочки для меню: разделы и путь запроса (текущий пункт).
type shell struct {
	sections []Section // разделы меню (Options.Sections)
	site     []Section // разделы группы «Сайт» (Options.SiteSections)
	path     string
}

type shellKey struct{}

func shellFrom(ctx context.Context) shell {
	s, _ := ctx.Value(shellKey{}).(shell)
	return s
}

// RequireUser пропускает только вошедших пользователей (auth.Service.RequireUser: сессия, CSRF-токен
// для небезопасных методов) и кладёт в контекст данные меню оболочки.
func (h *Handler) RequireUser(next http.Handler) http.Handler {
	return h.auth.RequireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := shell{sections: h.opts.Sections, site: h.opts.SiteSections, path: r.URL.Path}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), shellKey{}, s)))
	}))
}

// loginForm — данные формы входа.
type loginForm struct {
	Email string
	Next  string
	CSRF  string
	Error string
}

// LoginForm — GET /admin/login. Вошедшего пользователя отправляет на дашборд.
func (h *Handler) LoginForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok, err := h.auth.CurrentUser(r); err != nil {
		h.serverError(w, r, err)
		return
	} else if ok {
		http.Redirect(w, r, dashboardPath, http.StatusSeeOther)
		return
	}
	form := loginForm{CSRF: h.auth.LoginCSRF(w, r), Next: auth.SafeNext(r.URL.Query().Get("next"), dashboardPath)}
	h.render(w, r, http.StatusOK, LoginPage(form))
}

// Login — POST /admin/login: проверки и сессия — auth.Service.Login, здесь — ответ формой или редирект.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	form := loginForm{
		Email: auth.NormalizeEmail(r.PostFormValue("email")),
		Next:  auth.SafeNext(r.PostFormValue("next"), dashboardPath),
	}

	_, err := h.auth.Login(w, r, form.Email, r.PostFormValue("password"))
	var (
		locked *auth.LockedError
		status int
	)
	switch {
	case err == nil:
		http.Redirect(w, r, form.Next, http.StatusSeeOther)
		return
	case errors.Is(err, auth.ErrLoginCSRF):
		status, form.Error = http.StatusForbidden, "Форма устарела. Отправьте её ещё раз."
	case errors.As(err, &locked):
		status = http.StatusTooManyRequests
		form.Error = fmt.Sprintf("Слишком много неудачных попыток входа. Попробуйте через %d мин.",
			int(math.Ceil(locked.RetryAfter.Minutes())))
	case errors.Is(err, auth.ErrBadCredentials):
		status, form.Error = http.StatusUnprocessableEntity, "Неверный email или пароль."
	default:
		h.serverError(w, r, err)
		return
	}
	form.CSRF = h.auth.LoginCSRF(w, r)
	h.render(w, r, status, LoginPage(form))
}

// Logout — POST /admin/logout (внутри RequireUser: CSRF-токен уже проверен).
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.auth.Logout(w, r); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, loginPath, http.StatusSeeOther)
}

// Dashboard — GET /admin: счётчики контента.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	var counters []Counter
	if h.opts.Counters != nil {
		var err error
		if counters, err = h.opts.Counters(r.Context()); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	h.render(w, r, http.StatusOK, DashboardPage(counters))
}

// NotFound — неизвестный путь внутри админки (внутри RequireUser).
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	NotFound(w, r)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	web.Render(w, r, status, c)
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	ServerError(w, r, err)
}

// NotFound — 404 админки: для разделов типов контента, когда запись не найдена.
func NotFound(w http.ResponseWriter, r *http.Request) {
	web.Render(w, r, http.StatusNotFound, NotFoundPage())
}

// Redirect — после успешного POST в разделе админки: обычной форме 303, htmx — 204 и заголовок HX-Redirect
// (полный переход на адрес). target — путь внутри админки, собранный из констант и id, не из запроса.
func Redirect(w http.ResponseWriter, r *http.Request, target string) {
	if IsHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // target не берётся из запроса
}

// IsHTMX — запрос отправил htmx: в ответ на него отдают фрагмент, а не страницу целиком.
func IsHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// ServerError логирует ошибку и отдаёт страницу 500 админки без подробностей.
func ServerError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("admin request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	web.Render(w, r, http.StatusInternalServerError, ErrorPage())
}

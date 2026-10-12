// Package admin — админка сайта: вход по паролю, сессии, CSRF-защита, оболочка (меню, дашборд).
//
// «Простой режим»: любой пользователь из таблицы users — администратор с полным доступом;
// поле role зарезервировано под роли. Авторизация живёт здесь до выноса в модуль (после второго сайта).
//
// Пакет не знает о типах контента: счётчики дашборда передаёт cmd/site через Options.Counters.
package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"

	"github.com/voffkin/keelkit/reference/internal/web"
)

// Пути админки.
const (
	dashboardPath = "/admin"
	loginPath     = "/admin/login"
	logoutPath    = "/admin/logout"
)

// Ограничение попыток входа: после loginMaxFailures неудач за loginWindow вход для email блокируется
// до конца окна.
const (
	loginMaxFailures = 5
	loginWindow      = 15 * time.Minute
)

// Counter — счётчик на дашборде: «Страниц: 3». URL — ссылка на раздел (необязательно).
type Counter struct {
	Label string
	Value int64
	URL   string
}

// Section — раздел админки в меню: «Страницы» → /admin/pages. Пункт подсвечивается на URL и вложенных путях.
// Icon — иконка пункта: "pages", "posts", "tags", "image", "settings"; пусто или неизвестное имя — общая иконка папки.
type Section struct {
	Title string
	URL   string
	Icon  string
}

// Options — настройки админки.
type Options struct {
	// SecureCookies — флаг Secure у cookie. Выключается только для локальной разработки по http.
	SecureCookies bool
	// Counters — счётчики контента для дашборда.
	Counters func(context.Context) ([]Counter, error)
	// Sections — разделы типов контента в меню, по порядку.
	Sections []Section
	// SiteSections — разделы всего сайта (настройки, позже меню) в группе «Сайт» под контентом.
	SiteSections []Section
}

// Handler — хендлеры и middleware админки.
type Handler struct {
	q       *Queries
	opts    Options
	limiter *limiter
	now     func() time.Time
}

func NewHandler(db DBTX, opts Options) *Handler {
	return &Handler{
		q:       New(db),
		opts:    opts,
		limiter: newLimiter(loginMaxFailures, loginWindow),
		now:     time.Now,
	}
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
	if _, ok, err := h.currentSession(r); err != nil {
		h.serverError(w, r, err)
		return
	} else if ok {
		http.Redirect(w, r, dashboardPath, http.StatusSeeOther)
		return
	}
	form := loginForm{CSRF: h.loginCSRF(w, r), Next: safeNext(r.URL.Query().Get("next"))}
	h.render(w, r, http.StatusOK, LoginPage(form))
}

// Login — POST /admin/login: проверка CSRF-токена, ограничения попыток и пароля, создание сессии.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	form := loginForm{
		Email: NormalizeEmail(r.PostFormValue("email")),
		Next:  safeNext(r.PostFormValue("next")),
		CSRF:  h.loginCSRF(w, r),
	}

	// Токен формы должен совпасть с cookie: чужой сайт не может отправить форму входа от имени браузера.
	if !tokensEqual(form.CSRF, r.PostFormValue(csrfField)) {
		slog.Warn("login: csrf token mismatch")
		form.Error = "Форма устарела. Отправьте её ещё раз."
		h.render(w, r, http.StatusForbidden, LoginPage(form))
		return
	}

	if left, locked := h.limiter.blocked(form.Email); locked {
		minutes := int(math.Ceil(left.Minutes()))
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(left.Seconds()))))
		form.Error = fmt.Sprintf("Слишком много неудачных попыток входа. Попробуйте через %d мин.", minutes)
		h.render(w, r, http.StatusTooManyRequests, LoginPage(form))
		return
	}

	user, err := h.authenticate(r.Context(), form.Email, r.PostFormValue("password"))
	if errors.Is(err, errBadCredentials) {
		h.limiter.fail(form.Email)
		slog.Warn("login failed", "email", form.Email, "remote_addr", r.RemoteAddr)
		form.Error = "Неверный email или пароль."
		h.render(w, r, http.StatusUnprocessableEntity, LoginPage(form))
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.limiter.reset(form.Email)

	if err := h.endSession(w, r); err != nil {
		h.serverError(w, r, err)
		return
	}
	if err := h.startSession(r.Context(), w, user.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.SetCookie(w, h.cookie(loginCSRFCookie, "", loginPath, -1))
	slog.Info("login", "user_id", user.ID, "remote_addr", r.RemoteAddr)
	http.Redirect(w, r, form.Next, http.StatusSeeOther)
}

// Logout — POST /admin/logout (внутри RequireUser: CSRF-токен уже проверен).
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.endSession(w, r); err != nil {
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

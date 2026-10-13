// Package auth — вход в админку сайтов Keelkit: пользователи, пароли, сессии, CSRF-токены,
// ограничение попыток входа.
//
// «Простой режим»: любой пользователь из таблицы users — администратор с полным доступом;
// поле Role зарезервировано под роли.
//
// Модуль не знает о вёрстке. Сайт рисует страницу входа и оболочку админки сам и вызывает Service:
//   - GET страницы входа: CurrentUser (вошедшего — на дашборд), LoginCSRF — токен для скрытого поля CSRFField;
//   - POST входа: Login(w, r, email, password); ошибки ErrLoginCSRF, ErrBadCredentials, *LockedError
//     сайт показывает в форме, при успехе — редирект на SafeNext(next, base);
//   - всё остальное в админке — внутри RequireUser: без сессии — редирект на Config.LoginPath,
//     небезопасный метод без CSRF-токена сессии (поле CSRFField или заголовок CSRFHeader) — Config.Forbidden;
//     в обработчиках — UserFrom и CSRFToken;
//   - выход — POST внутри RequireUser: Logout.
//
// Защита от межсайтовых запросов на уровне браузера (заголовки Sec-Fetch-Site, Origin) — забота сайта:
// http.CrossOriginProtection стандартной библиотеки вокруг маршрутов админки. Токены модуля — второй рубеж.
//
// Таблицы users и sessions — в базе сайта (SQLite), миграции — в пакете migrations; сайт обращается
// к ним только через Service.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Поля запроса с CSRF-токеном: скрытое поле формы или заголовок (для htmx).
const (
	CSRFField  = "csrf_token"
	CSRFHeader = "X-CSRF-Token"
)

// Значения Config по умолчанию.
const (
	DefaultLoginPath     = "/admin/login"
	DefaultSessionTTL    = 14 * 24 * time.Hour
	DefaultMaxFailures   = 5
	DefaultFailureWindow = 15 * time.Minute
)

// Config — настройки Service. Нулевые значения заменяются значениями по умолчанию.
type Config struct {
	// LoginPath — страница входа: туда RequireUser отправляет без сессии; путь cookie токена формы входа.
	LoginPath string
	// InsecureCookies снимает флаг Secure с cookie. Только для локальной разработки по http.
	InsecureCookies bool
	// SessionTTL — срок жизни сессии с момента входа.
	SessionTTL time.Duration
	// MaxFailures неудачных попыток входа за FailureWindow блокируют вход для email до конца окна.
	MaxFailures   int
	FailureWindow time.Duration
	// Forbidden отвечает на небезопасный запрос без верного CSRF-токена. По умолчанию — текст 403.
	Forbidden http.Handler
	// ServerError отвечает на внутреннюю ошибку в RequireUser. По умолчанию — запись в лог и текст 500.
	ServerError func(w http.ResponseWriter, r *http.Request, err error)
	// Now — текущее время; для тестов (срок сессии, окно блокировки). По умолчанию time.Now.
	Now func() time.Time
}

// User — пользователь админки.
type User struct {
	ID    int64
	Email string
	Role  string
}

// Service — пользователи и сессии. Безопасен для одновременного использования.
type Service struct {
	db      *sql.DB
	cfg     Config
	limiter *limiter
}

// NewService создаёт сервис поверх базы сайта, в которой применены миграции модуля.
func NewService(db *sql.DB, cfg Config) *Service {
	if cfg.LoginPath == "" {
		cfg.LoginPath = DefaultLoginPath
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = DefaultSessionTTL
	}
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = DefaultMaxFailures
	}
	if cfg.FailureWindow <= 0 {
		cfg.FailureWindow = DefaultFailureWindow
	}
	if cfg.Forbidden == nil {
		cfg.Forbidden = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		})
	}
	if cfg.ServerError == nil {
		cfg.ServerError = func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("auth: request failed", "method", r.Method, "path", r.URL.Path, "err", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	l := newLimiter(cfg.MaxFailures, cfg.FailureWindow)
	l.now = cfg.Now
	return &Service{db: db, cfg: cfg, limiter: l}
}

// Ошибки входа и создания пользователя.
var (
	// ErrLoginCSRF — токен формы входа не совпал с cookie: форма устарела или отправлена с чужого сайта.
	ErrLoginCSRF = errors.New("auth: login form token mismatch")
	// ErrBadCredentials — неверный email или пароль (что именно — не сообщается).
	ErrBadCredentials = errors.New("auth: bad credentials")
	// ErrInvalidEmail — строка не похожа на адрес электронной почты.
	ErrInvalidEmail = errors.New("некорректный email")
	// ErrUserExists — пользователь с таким email уже есть.
	ErrUserExists = errors.New("пользователь с таким email уже существует")
)

// LockedError — вход для email временно заблокирован после неудачных попыток.
type LockedError struct {
	RetryAfter time.Duration // сколько осталось ждать
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("auth: too many failed logins, retry after %s", e.RetryAfter)
}

type sessionKey struct{}

// session — сессия текущего запроса внутри RequireUser.
type session struct {
	user User
	csrf string
}

func sessionFrom(ctx context.Context) (session, bool) {
	s, ok := ctx.Value(sessionKey{}).(session)
	return s, ok
}

// UserFrom возвращает вошедшего пользователя (только внутри RequireUser).
func UserFrom(ctx context.Context) (User, bool) {
	s, ok := sessionFrom(ctx)
	return s.user, ok
}

// CSRFToken — токен для форм и htmx-запросов текущей сессии (только внутри RequireUser).
func CSRFToken(ctx context.Context) string {
	s, _ := sessionFrom(ctx)
	return s.csrf
}

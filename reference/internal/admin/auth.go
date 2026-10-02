package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	sessionCookie = "session"
	sessionTTL    = 14 * 24 * time.Hour

	// loginCSRFCookie — токен формы входа, пока сессии ещё нет (double submit cookie).
	loginCSRFCookie = "admin_csrf"

	// csrfField и csrfHeader — где запрос передаёт CSRF-токен: поле формы или заголовок (для htmx).
	csrfField  = "csrf_token"
	csrfHeader = "X-CSRF-Token"

	// timeFormat — формат времени в БД: UTC RFC 3339 без долей секунды, сравнивается как строка.
	timeFormat = "2006-01-02T15:04:05Z"
)

// User — вошедший пользователь.
type User struct {
	ID    int64
	Email string
	Role  string
}

// session — сессия текущего запроса.
type session struct {
	user User
	csrf string
}

type sessionKey struct{}

func sessionFrom(ctx context.Context) (session, bool) {
	s, ok := ctx.Value(sessionKey{}).(session)
	return s, ok
}

// UserFrom возвращает вошедшего пользователя (только внутри RequireUser).
func UserFrom(ctx context.Context) (User, bool) {
	s, ok := sessionFrom(ctx)
	return s.user, ok
}

// CSRFToken — токен для форм текущей сессии (только внутри RequireUser).
func CSRFToken(ctx context.Context) string {
	s, _ := sessionFrom(ctx)
	return s.csrf
}

// NormalizeEmail приводит email к виду, в котором он хранится в users.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// hashToken — SHA-256 токена сессии: в БД хранится только он.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// tokensEqual сравнивает токены за постоянное время; пустой токен не равен ничему.
func tokensEqual(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// errBadCredentials — неверный email или пароль (что именно — не сообщаем).
var errBadCredentials = errors.New("bad credentials")

// authenticate проверяет email и пароль. Для неизвестного email пароль всё равно сверяется
// с dummyHash, чтобы время ответа не выдавало, существует ли пользователь.
func (h *Handler) authenticate(ctx context.Context, email, password string) (User, error) {
	u, err := h.q.GetUserByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		dummy, err := dummyHash()
		if err != nil {
			return User{}, err
		}
		if _, _, err := VerifyPassword(ctx, dummy, password); err != nil {
			return User{}, err
		}
		return User{}, errBadCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}

	ok, rehash, err := VerifyPassword(ctx, u.PasswordHash, password)
	if err != nil {
		return User{}, fmt.Errorf("verify password of user %d: %w", u.ID, err)
	}
	if !ok {
		return User{}, errBadCredentials
	}
	if rehash {
		// Параметры хеширования усилены: пересчитываем хеш, пока знаем пароль. Ошибка не мешает входу.
		if err := h.rehash(ctx, u.ID, password); err != nil {
			slog.Error("rehash password", "user_id", u.ID, "err", err)
		}
	}
	return User{ID: u.ID, Email: u.Email, Role: u.Role}, nil
}

func (h *Handler) rehash(ctx context.Context, userID int64, password string) error {
	hash, err := HashPassword(ctx, password)
	if err != nil {
		return err
	}
	return h.q.UpdatePasswordHash(ctx, UpdatePasswordHashParams{PasswordHash: hash, ID: userID})
}

// currentSession находит действующую сессию по cookie.
func (h *Handler) currentSession(r *http.Request) (session, bool, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return session{}, false, nil
	}
	row, err := h.q.GetSession(r.Context(), GetSessionParams{
		TokenHash: hashToken(c.Value),
		Now:       h.now().UTC().Format(timeFormat),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return session{}, false, nil
	}
	if err != nil {
		return session{}, false, fmt.Errorf("get session: %w", err)
	}
	return session{user: User{ID: row.UserID, Email: row.Email, Role: row.Role}, csrf: row.CsrfToken}, true, nil
}

// startSession создаёт сессию и ставит cookie. Заодно удаляет истёкшие сессии.
// Токен всегда новый: сессия, существовавшая до входа, не продолжается (защита от фиксации сессии).
func (h *Handler) startSession(ctx context.Context, w http.ResponseWriter, userID int64) error {
	now := h.now().UTC()
	if err := h.q.DeleteExpiredSessions(ctx, now.Format(timeFormat)); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	token := rand.Text()
	expires := now.Add(sessionTTL)
	err := h.q.CreateSession(ctx, CreateSessionParams{
		TokenHash: hashToken(token),
		UserID:    userID,
		CsrfToken: rand.Text(),
		ExpiresAt: expires.Format(timeFormat),
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	http.SetCookie(w, h.cookie(sessionCookie, token, "/", int(sessionTTL/time.Second)))
	return nil
}

// endSession удаляет сессию из БД и cookie из браузера.
func (h *Handler) endSession(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := h.q.DeleteSession(r.Context(), hashToken(c.Value)); err != nil {
			return fmt.Errorf("delete session: %w", err)
		}
	}
	http.SetCookie(w, h.cookie(sessionCookie, "", "/", -1))
	return nil
}

// cookie — cookie с флагами админки: HttpOnly, SameSite=Lax, Secure вне локальной разработки.
// maxAge < 0 удаляет cookie, 0 — cookie до закрытия браузера.
func (h *Handler) cookie(name, value, path string, maxAge int) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure выключается только настройкой COOKIE_SECURE=false для разработки
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.opts.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	}
}

// loginCSRF возвращает токен формы входа из cookie или ставит новый.
func (h *Handler) loginCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(loginCSRFCookie); err == nil && c.Value != "" {
		return c.Value
	}
	token := rand.Text()
	http.SetCookie(w, h.cookie(loginCSRFCookie, token, loginPath, 0))
	return token
}

// csrfFromRequest — токен из заголовка (htmx) или из поля формы.
func csrfFromRequest(r *http.Request) string {
	if t := r.Header.Get(csrfHeader); t != "" {
		return t
	}
	return r.PostFormValue(csrfField)
}

// RequireUser пропускает только вошедших пользователей; остальных отправляет на страницу входа.
// Для небезопасных методов (POST, PUT, DELETE…) требует CSRF-токен сессии, иначе — 403.
// Ответы админки не кэшируются.
func (h *Handler) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		s, ok, err := h.currentSession(r)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		if !ok {
			target := loginPath
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				target += "?next=" + url.QueryEscape(r.URL.RequestURI())
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		if !safeMethod(r.Method) && !tokensEqual(s.csrf, csrfFromRequest(r)) {
			slog.Warn("csrf token mismatch", "method", r.Method, "path", r.URL.Path, "user_id", s.user.ID)
			h.render(w, r, http.StatusForbidden, ForbiddenPage())
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, s)))
	})
}

// safeNext — адрес возврата после входа: только путь внутри админки, иначе дашборд.
// Защищает от открытого редиректа (next=https://evil.example, next=//evil.example).
func safeNext(next string) string {
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || strings.HasPrefix(next, "//") || strings.Contains(next, `\`) {
		return dashboardPath
	}
	if u.Path != dashboardPath && !strings.HasPrefix(u.Path, dashboardPath+"/") {
		return dashboardPath
	}
	return next
}

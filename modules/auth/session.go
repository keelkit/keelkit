package auth

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
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookie = "session"

	// loginCSRFCookie — токен формы входа, пока сессии ещё нет (double submit cookie).
	loginCSRFCookie = "admin_csrf"

	// timeFormat — формат времени в БД: UTC RFC 3339 без долей секунды, сравнивается как строка.
	timeFormat = "2006-01-02T15:04:05Z"
)

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

// csrfFromRequest — токен из заголовка (htmx) или из поля формы.
func csrfFromRequest(r *http.Request) string {
	if t := r.Header.Get(CSRFHeader); t != "" {
		return t
	}
	return r.PostFormValue(CSRFField)
}

// LoginCSRF возвращает токен формы входа из cookie или ставит новый. Сайт кладёт его в скрытое поле CSRFField.
func (s *Service) LoginCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(loginCSRFCookie); err == nil && c.Value != "" {
		return c.Value
	}
	token := rand.Text()
	http.SetCookie(w, s.cookie(loginCSRFCookie, token, s.cfg.LoginPath, 0))
	return token
}

// Login проверяет токен формы входа, ограничение попыток и пароль и начинает новую сессию (cookie в w).
// Сайт заранее ограничивает размер тела запроса и разбирает форму; email нормализуется здесь.
//
// Ошибки для формы: ErrLoginCSRF (403), *LockedError (429; заголовок Retry-After уже выставлен),
// ErrBadCredentials (422). Остальные ошибки — внутренние (500).
func (s *Service) Login(w http.ResponseWriter, r *http.Request, email, password string) (User, error) {
	email = NormalizeEmail(email)

	// Токен формы должен совпасть с cookie: чужой сайт не может отправить форму входа от имени браузера.
	c, err := r.Cookie(loginCSRFCookie)
	if err != nil || !tokensEqual(c.Value, csrfFromRequest(r)) {
		slog.Warn("login: csrf token mismatch", "remote_addr", r.RemoteAddr)
		return User{}, ErrLoginCSRF
	}

	if left, locked := s.limiter.blocked(email); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(left.Seconds()))))
		return User{}, &LockedError{RetryAfter: left}
	}

	user, err := s.authenticate(r.Context(), email, password)
	if errors.Is(err, ErrBadCredentials) {
		s.limiter.fail(email)
		slog.Warn("login failed", "email", email, "remote_addr", r.RemoteAddr)
		return User{}, err
	}
	if err != nil {
		return User{}, err
	}
	s.limiter.reset(email)

	// Сессия, существовавшая до входа, не продолжается (защита от фиксации сессии).
	if err := s.Logout(w, r); err != nil {
		return User{}, err
	}
	if err := s.startSession(r.Context(), w, user.ID); err != nil {
		return User{}, err
	}
	http.SetCookie(w, s.cookie(loginCSRFCookie, "", s.cfg.LoginPath, -1))
	slog.Info("login", "user_id", user.ID, "remote_addr", r.RemoteAddr)
	return user, nil
}

// Logout удаляет сессию запроса из БД и cookie из браузера. Вызывается из POST внутри RequireUser
// (CSRF-токен уже проверен).
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if _, err := s.db.ExecContext(r.Context(),
			`DELETE FROM sessions WHERE token_hash = ?`, hashToken(c.Value)); err != nil {
			return fmt.Errorf("auth: delete session: %w", err)
		}
	}
	http.SetCookie(w, s.cookie(sessionCookie, "", "/", -1))
	return nil
}

// CurrentUser находит пользователя действующей сессии по cookie — например, чтобы со страницы входа
// отправить уже вошедшего на дашборд.
func (s *Service) CurrentUser(r *http.Request) (User, bool, error) {
	sess, ok, err := s.currentSession(r)
	return sess.user, ok, err
}

// RequireUser пропускает только вошедших пользователей; остальных отправляет на страницу входа
// (GET и HEAD — с ?next=<адрес запроса>). Для небезопасных методов (POST, PUT, DELETE…) требует
// CSRF-токен сессии, иначе отвечает Config.Forbidden. Ответы не кэшируются.
func (s *Service) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		sess, ok, err := s.currentSession(r)
		if err != nil {
			s.cfg.ServerError(w, r, err)
			return
		}
		if !ok {
			target := s.cfg.LoginPath
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				target += "?next=" + url.QueryEscape(r.URL.RequestURI())
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		if !safeMethod(r.Method) && !tokensEqual(sess.csrf, csrfFromRequest(r)) {
			slog.Warn("csrf token mismatch", "method", r.Method, "path", r.URL.Path, "user_id", sess.user.ID)
			s.cfg.Forbidden.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, sess)))
	})
}

// SafeNext — адрес возврата после входа: только путь внутри base (например, "/admin"), иначе base.
// Защищает от открытого редиректа (next=https://evil.example, next=//evil.example).
func SafeNext(next, base string) string {
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || strings.HasPrefix(next, "//") || strings.Contains(next, `\`) {
		return base
	}
	if u.Path != base && !strings.HasPrefix(u.Path, strings.TrimSuffix(base, "/")+"/") {
		return base
	}
	return next
}

// authenticate проверяет email и пароль. Для неизвестного email пароль всё равно сверяется
// с dummyHash, чтобы время ответа не выдавало, существует ли пользователь.
func (s *Service) authenticate(ctx context.Context, email, password string) (User, error) {
	var (
		u    User
		hash string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, role FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &hash, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		dummy, err := dummyHash()
		if err != nil {
			return User{}, err
		}
		if _, _, err := verifyPassword(ctx, dummy, password); err != nil {
			return User{}, err
		}
		return User{}, ErrBadCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get user: %w", err)
	}

	ok, rehash, err := verifyPassword(ctx, hash, password)
	if err != nil {
		return User{}, fmt.Errorf("auth: verify password of user %d: %w", u.ID, err)
	}
	if !ok {
		return User{}, ErrBadCredentials
	}
	if rehash {
		// Параметры хеширования усилены: пересчитываем хеш, пока знаем пароль. Ошибка не мешает входу.
		if err := s.setPassword(ctx, u.ID, password); err != nil {
			slog.Error("rehash password", "user_id", u.ID, "err", err)
		}
	}
	return u, nil
}

func (s *Service) setPassword(ctx context.Context, userID int64, password string) error {
	hash, err := hashPassword(ctx, password, defaultParams)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
		hash, userID)
	if err != nil {
		return fmt.Errorf("auth: update password hash: %w", err)
	}
	return nil
}

// currentSession находит действующую сессию по cookie.
func (s *Service) currentSession(r *http.Request) (session, bool, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return session{}, false, nil
	}
	var sess session
	err = s.db.QueryRowContext(r.Context(), `
		SELECT u.id, u.email, u.role, s.csrf_token
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`,
		hashToken(c.Value), s.cfg.Now().UTC().Format(timeFormat),
	).Scan(&sess.user.ID, &sess.user.Email, &sess.user.Role, &sess.csrf)
	if errors.Is(err, sql.ErrNoRows) {
		return session{}, false, nil
	}
	if err != nil {
		return session{}, false, fmt.Errorf("auth: get session: %w", err)
	}
	return sess, true, nil
}

// startSession создаёт сессию с новым токеном и ставит cookie. Заодно удаляет истёкшие сессии.
func (s *Service) startSession(ctx context.Context, w http.ResponseWriter, userID int64) error {
	now := s.cfg.Now().UTC()
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, now.Format(timeFormat)); err != nil {
		return fmt.Errorf("auth: delete expired sessions: %w", err)
	}
	token := rand.Text()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(token), userID, rand.Text(), now.Add(s.cfg.SessionTTL).Format(timeFormat))
	if err != nil {
		return fmt.Errorf("auth: create session: %w", err)
	}
	http.SetCookie(w, s.cookie(sessionCookie, token, "/", int(s.cfg.SessionTTL/time.Second)))
	return nil
}

// cookie — cookie модуля: HttpOnly, SameSite=Lax, Secure (кроме Config.InsecureCookies).
// maxAge < 0 удаляет cookie, 0 — cookie до закрытия браузера.
func (s *Service) cookie(name, value, path string, maxAge int) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure снимается только настройкой InsecureCookies для разработки
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !s.cfg.InsecureCookies,
		SameSite: http.SameSiteLaxMode,
	}
}

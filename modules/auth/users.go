package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// NormalizeEmail приводит email к виду, в котором он хранится в users.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CheckEmail проверяет email нового пользователя: только адрес, без имени («a@example.com»,
// не «A <a@example.com>»). Ошибка — ErrInvalidEmail.
func CheckEmail(email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != strings.TrimSpace(email) {
		return fmt.Errorf("%w: %q", ErrInvalidEmail, email)
	}
	return nil
}

// CreateUser создаёт пользователя; email проходит CheckEmail, пароль — CheckPasswordPolicy.
// Ошибки: ErrInvalidEmail, ErrPasswordLength, ErrUserExists.
func (s *Service) CreateUser(ctx context.Context, email, password string) (User, error) {
	if err := CheckEmail(email); err != nil {
		return User{}, err
	}
	email = NormalizeEmail(email)
	if err := CheckPasswordPolicy(password); err != nil {
		return User{}, err
	}
	hash, err := hashPassword(ctx, password, defaultParams)
	if err != nil {
		return User{}, err
	}
	u := User{Email: email}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash) VALUES (?, ?)
		ON CONFLICT (email) DO NOTHING
		RETURNING id, role`, email, hash,
	).Scan(&u.ID, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("%w: %s", ErrUserExists, email)
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: create user: %w", err)
	}
	return u, nil
}

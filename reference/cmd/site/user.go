package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/voffkin/keelkit/reference/internal/admin"
)

// errUserExists — пользователь с таким email уже есть.
var errUserExists = errors.New("пользователь с таким email уже существует")

// userCreate — site user create EMAIL: создаёт пользователя админки. Пароль спрашивается дважды
// через readPassword (в терминале — без эха).
func userCreate(ctx context.Context, cfg config, email string, readPassword func(prompt string) (string, error)) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != strings.TrimSpace(email) {
		return fmt.Errorf("некорректный email %q", email)
	}
	email = admin.NormalizeEmail(addr.Address)

	password, err := readPassword("Пароль: ")
	if err != nil {
		return err
	}
	if err := admin.CheckPasswordPolicy(password); err != nil {
		return err
	}
	again, err := readPassword("Повторите пароль: ")
	if err != nil {
		return err
	}
	if again != password {
		return errors.New("пароли не совпадают")
	}

	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	q := admin.New(db)
	switch _, err := q.GetUserByEmail(ctx, email); {
	case err == nil:
		return fmt.Errorf("%w: %s", errUserExists, email)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("get user (выполнена ли make migrate?): %w", err)
	}
	hash, err := admin.HashPassword(ctx, password)
	if err != nil {
		return err
	}
	id, err := q.CreateUser(ctx, admin.CreateUserParams{Email: email, PasswordHash: hash})
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	slog.Info("user created", "id", id, "email", email)
	return nil
}

// stdinLines — построчное чтение stdin, когда он не терминал (пароль передан через пайп).
var stdinLines = bufio.NewReader(os.Stdin)

// readPasswordTerminal читает пароль из терминала без эха; если stdin не терминал — строку из stdin.
func readPasswordTerminal(prompt string) (string, error) {
	fd := int(os.Stdin.Fd()) //nolint:gosec // дескриптор stdin помещается в int
	if !term.IsTerminal(fd) {
		line, err := stdinLines.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || line == "") {
			return "", fmt.Errorf("read password: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(b), nil
}

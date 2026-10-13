package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/voffkin/keelkit/modules/auth"
)

// userCreate — site user create EMAIL: создаёт пользователя админки. Пароль спрашивается дважды
// через readPassword (в терминале — без эха).
func userCreate(ctx context.Context, cfg config, email string, readPassword func(prompt string) (string, error)) error {
	if err := auth.CheckEmail(email); err != nil {
		return err
	}

	password, err := readPassword("Пароль: ")
	if err != nil {
		return err
	}
	if err := auth.CheckPasswordPolicy(password); err != nil {
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

	u, err := auth.NewService(db, auth.Config{}).CreateUser(ctx, email, password)
	if errors.Is(err, auth.ErrUserExists) {
		return err
	}
	if err != nil {
		return fmt.Errorf("create user (выполнена ли make migrate?): %w", err)
	}
	slog.Info("user created", "id", u.ID, "email", u.Email)
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

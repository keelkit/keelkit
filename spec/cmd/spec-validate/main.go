// Команда spec-validate проверяет спецификацию сайта (site.yaml).
//
//	spec-validate site.yaml [other.yaml ...]
//
// Код выхода: 0 — все файлы валидны, 1 — есть ошибки, 2 — неверный вызов.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/keelkit/keelkit/spec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: spec-validate FILE [FILE ...]")
		os.Exit(2)
	}
	failed := false
	for _, name := range os.Args[1:] {
		if !check(name) {
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func check(name string) bool {
	data, err := os.ReadFile(name) //nolint:gosec // CLI: путь к файлу передаёт пользователь
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return false
	}
	err = spec.Validate(data)
	var ve *spec.ValidationError
	switch {
	case err == nil:
		fmt.Printf("%s: OK\n", name)
		return true
	case errors.As(err, &ve):
		for _, is := range ve.Issues {
			fmt.Fprintf(os.Stderr, "%s:%s\n", name, is)
		}
	default:
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
	}
	return false
}

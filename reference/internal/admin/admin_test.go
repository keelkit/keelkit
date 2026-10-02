package admin

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestHashPassword(t *testing.T) {
	ctx := context.Background()
	hash, err := HashPassword(ctx, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("hash = %q, want PHC argon2id with default params", hash)
	}
	if other, _ := HashPassword(ctx, "correct horse"); other == hash {
		t.Error("two hashes of one password are equal: salt is not random")
	}

	ok, rehash, err := VerifyPassword(ctx, hash, "correct horse")
	if err != nil || !ok || rehash {
		t.Errorf("verify correct = %v, rehash %v, err %v; want true, false, nil", ok, rehash, err)
	}
	if ok, _, err := VerifyPassword(ctx, hash, "wrong horse"); err != nil || ok {
		t.Errorf("verify wrong = %v, err %v; want false, nil", ok, err)
	}
	if ok, _, _ := VerifyPassword(ctx, hash, strings.Repeat("x", maxPasswordLen+1)); ok {
		t.Error("too long password verified")
	}
}

// Хеш со слабыми параметрами проверяется, но требует пересчёта.
func TestVerifyPasswordRehash(t *testing.T) {
	ctx := context.Background()
	weak := argonParams{memory: 8 * 1024, time: 1, threads: 1, saltLen: 16, keyLen: 32}
	hash, err := hashPassword(ctx, "secret", weak)
	if err != nil {
		t.Fatal(err)
	}
	ok, rehash, err := VerifyPassword(ctx, hash, "secret")
	if err != nil || !ok || !rehash {
		t.Errorf("verify = %v, rehash %v, err %v; want true, true, nil", ok, rehash, err)
	}
}

func TestVerifyPasswordBadHash(t *testing.T) {
	for _, hash := range []string{
		"",
		"plain",
		"$2a$10$abcdefghijklmnopqrstuv", // bcrypt
		"$argon2i$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=16$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=0,t=3,p=4$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=4194304,t=3,p=4$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=65536,t=3,p=4$!!!$a2V5a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0$a2V5",
	} {
		if _, _, err := VerifyPassword(context.Background(), hash, "x"); err == nil {
			t.Errorf("VerifyPassword(%q): no error", hash)
		}
	}
}

func TestCheckPasswordPolicy(t *testing.T) {
	tests := []struct {
		password string
		ok       bool
	}{
		{"short", false},
		{"12345678", true},
		{"пароль!!", true}, // 8 символов, 15 байт
		{"пароль!", false},
		{strings.Repeat("x", maxPasswordLen), true},
		{strings.Repeat("x", maxPasswordLen+1), false},
	}
	for _, tt := range tests {
		if err := CheckPasswordPolicy(tt.password); (err == nil) != tt.ok {
			t.Errorf("CheckPasswordPolicy(%q) = %v, want ok %v", tt.password, err, tt.ok)
		}
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := newLimiter(3, 10*time.Minute)
	l.now = func() time.Time { return now }

	for i := range 3 {
		if _, locked := l.blocked("a"); locked {
			t.Fatalf("locked after %d failures", i)
		}
		l.fail("a")
	}
	left, locked := l.blocked("a")
	if !locked || left != 10*time.Minute {
		t.Fatalf("after 3 failures: locked %v, left %v; want true, 10m", locked, left)
	}
	if _, locked := l.blocked("b"); locked {
		t.Error("other key is locked")
	}

	// Окно истекло — блокировка снята.
	now = now.Add(10 * time.Minute)
	if _, locked := l.blocked("a"); locked {
		t.Error("still locked after the window")
	}

	// Успешный вход сбрасывает счётчик.
	l.fail("a")
	l.fail("a")
	l.reset("a")
	l.fail("a")
	l.fail("a")
	if _, locked := l.blocked("a"); locked {
		t.Error("locked: reset did not clear failures")
	}
}

func TestSafeNext(t *testing.T) {
	tests := map[string]string{
		"":                           "/admin",
		"/admin":                     "/admin",
		"/admin/pages?x=1":           "/admin/pages?x=1",
		"/":                          "/admin",
		"/administrator":             "/admin",
		"https://evil.example/":      "/admin",
		"//evil.example/admin":       "/admin",
		`/admin\..\evil`:             "/admin",
		"javascript:alert(1)":        "/admin",
		"https://evil.example/admin": "/admin",
	}
	for in, want := range tests {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

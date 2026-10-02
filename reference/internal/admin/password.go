package admin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Пароли хешируются argon2id (решение — в AGENTS.md монорепо). Хеш хранится в формате PHC:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<соль base64>$<ключ base64>
//
// Параметры записаны в самом хеше, поэтому их можно усилить без миграции: при входе хеш со старыми
// параметрами пересчитывается (VerifyPassword возвращает rehash = true).

// argonParams — параметры argon2id.
type argonParams struct {
	memory  uint32 // КиБ
	time    uint32
	threads uint8
	saltLen int
	keyLen  uint32
}

// defaultParams — текущие параметры: m=64 МиБ, t=3, p=4, соль 16 байт, ключ 32 байта.
var defaultParams = argonParams{memory: 64 * 1024, time: 3, threads: 4, saltLen: 16, keyLen: 32}

// Ограничения на длину пароля. Минимум проверяется только при создании пользователя;
// максимум защищает от огромных строк в форме входа.
const (
	MinPasswordLen = 8    // символов
	maxPasswordLen = 1024 // байт
)

// ErrPasswordLength — пароль короче MinPasswordLen или длиннее maxPasswordLen.
var ErrPasswordLength = fmt.Errorf("пароль должен быть от %d символов до %d байт", MinPasswordLen, maxPasswordLen)

// errBadHash — строка не похожа на хеш argon2id в формате PHC.
var errBadHash = errors.New("password hash: unsupported format")

// hashSlots ограничивает число одновременных вычислений argon2id: каждое занимает 64 МиБ памяти,
// и поток запросов на вход не должен исчерпать память сервера.
var hashSlots = make(chan struct{}, 2)

func withHashSlot(ctx context.Context, fn func()) error {
	select {
	case hashSlots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-hashSlots }()
	fn()
	return nil
}

// CheckPasswordPolicy проверяет пароль нового пользователя.
func CheckPasswordPolicy(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLen || len(password) > maxPasswordLen {
		return ErrPasswordLength
	}
	return nil
}

// HashPassword возвращает хеш argon2id с текущими параметрами и случайной солью.
func HashPassword(ctx context.Context, password string) (string, error) {
	return hashPassword(ctx, password, defaultParams)
}

func hashPassword(ctx context.Context, password string, p argonParams) (string, error) {
	if len(password) > maxPasswordLen {
		return "", ErrPasswordLength
	}
	salt := make([]byte, p.saltLen)
	_, _ = rand.Read(salt) // crypto/rand.Read не возвращает ошибок
	var key []byte
	err := withHashSlot(ctx, func() {
		key = argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, p.keyLen)
	})
	if err != nil {
		return "", err
	}
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.time, p.threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword сравнивает пароль с хешем за постоянное время.
// rehash = true, если пароль верен, но хеш посчитан с устаревшими параметрами.
func VerifyPassword(ctx context.Context, encoded, password string) (ok, rehash bool, err error) {
	p, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false, false, err
	}
	if len(password) > maxPasswordLen {
		return false, false, nil
	}
	var got []byte
	err = withHashSlot(ctx, func() {
		got = argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, p.keyLen)
	})
	if err != nil {
		return false, false, err
	}
	if subtle.ConstantTimeCompare(got, key) != 1 {
		return false, false, nil
	}
	return true, p != defaultParams, nil
}

func decodeHash(encoded string) (p argonParams, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=…,t=…,p=…", соль, ключ
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, nil, nil, errBadHash
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return p, nil, nil, errBadHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return p, nil, nil, errBadHash
	}
	// Защита от испорченного хеша: нулевые параметры или память больше 1 ГиБ.
	if p.memory == 0 || p.memory > 1<<20 || p.time == 0 || p.threads == 0 {
		return p, nil, nil, errBadHash
	}
	b64 := base64.RawStdEncoding
	if salt, err = b64.DecodeString(parts[4]); err != nil || len(salt) < 8 {
		return p, nil, nil, errBadHash
	}
	if key, err = b64.DecodeString(parts[5]); err != nil || len(key) < 16 || len(key) > 64 {
		return p, nil, nil, errBadHash
	}
	p.saltLen, p.keyLen = len(salt), uint32(len(key)) //nolint:gosec // длина ключа проверена выше: 16..64
	return p, salt, key, nil
}

// dummyHash — хеш случайного пароля. С ним сверяется пароль, когда email не найден:
// ответ занимает столько же времени, и по нему нельзя узнать, есть ли такой пользователь.
var dummyHash = sync.OnceValues(func() (string, error) {
	return HashPassword(context.Background(), rand.Text())
})

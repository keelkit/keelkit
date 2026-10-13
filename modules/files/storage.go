package files

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// ErrNotFound — в хранилище нет файла с таким ключом.
var ErrNotFound = errors.New("files: not found")

// Storage — хранилище файлов. Ключ — относительный путь через «/» без «.» и «..» (как в io/fs),
// например «k3x…/9f2c….pdf». Реализации: DiskStorage; S3-совместимая — позже, за этим же интерфейсом.
// Интерфейс совпадает с images.Storage, но объявлен здесь: модули не зависят друг от друга.
//
// Handler отдаёт файл частями (Range), если Get возвращает io.ReadSeeker (DiskStorage — *os.File);
// иначе — целиком.
//
// Put перезаписывает существующий файл; читатель никогда не видит наполовину записанный файл.
// Get возвращает ошибку, оборачивающую ErrNotFound, если файла нет. Delete отсутствующего файла — не ошибка:
// удаление и уборка корзины должны быть безопасны для повторного запуска.
type Storage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// DiskStorage — хранилище в каталоге локального диска. Доступ идёт через os.Root:
// ключ не может выйти за пределы каталога, в том числе через символические ссылки.
type DiskStorage struct {
	root *os.Root
}

var _ Storage = (*DiskStorage)(nil)

// NewDiskStorage открывает (и при необходимости создаёт) каталог dir. Закрывается через Close.
func NewDiskStorage(dir string) (*DiskStorage, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("files: storage dir: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("files: storage dir: %w", err)
	}
	return &DiskStorage{root: root}, nil
}

// Close освобождает дескриптор каталога.
func (s *DiskStorage) Close() error { return s.root.Close() }

// Put пишет во временный файл рядом и переименовывает его: rename атомарен в пределах каталога.
// Временный файл, оставшийся после падения процесса, имеет суффикс «.tmp-<случайное>» и безопасен для удаления.
func (s *DiskStorage) Put(ctx context.Context, key string, r io.Reader) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if dir := path.Dir(key); dir != "." {
		if err := s.root.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("files: put %s: %w", key, err)
		}
	}
	tmp := key + ".tmp-" + randomSuffix()
	f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("files: put %s: %w", key, err)
	}
	_, err = io.Copy(f, r)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = s.root.Rename(tmp, key)
	}
	if err != nil {
		_ = s.root.Remove(tmp)
		return fmt.Errorf("files: put %s: %w", key, err)
	}
	return nil
}

// Get открывает файл на чтение; вызывающий закрывает его.
func (s *DiskStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := s.root.Open(key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return nil, fmt.Errorf("files: get %s: %w", key, err)
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return f, nil
}

// Delete удаляет файл; отсутствие файла ошибкой не считается.
func (s *DiskStorage) Delete(ctx context.Context, key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("files: delete %s: %w", key, err)
	}
	return nil
}

// checkKey допускает только канонические относительные пути: без «..», «.», пустых сегментов и «\».
func checkKey(key string) error {
	if key == "." || !fs.ValidPath(key) || strings.Contains(key, `\`) {
		return fmt.Errorf("files: invalid storage key %q", key)
	}
	return nil
}

func randomSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read не возвращает ошибок
	return hex.EncodeToString(b[:])
}

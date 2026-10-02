package images

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newDisk(t *testing.T) (*DiskStorage, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := NewDiskStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, dir
}

func get(t *testing.T, st Storage, key string) string {
	t.Helper()
	rc, err := st.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDiskStorage(t *testing.T) {
	ctx := context.Background()
	st, dir := newDisk(t)

	if err := st.Put(ctx, "a/b/c.txt", strings.NewReader("one")); err != nil {
		t.Fatal(err)
	}
	if got := get(t, st, "a/b/c.txt"); got != "one" {
		t.Errorf("got %q", got)
	}
	if err := st.Put(ctx, "a/b/c.txt", strings.NewReader("two")); err != nil {
		t.Fatal("overwrite:", err)
	}
	if got := get(t, st, "a/b/c.txt"); got != "two" {
		t.Errorf("after overwrite got %q", got)
	}

	if err := st.Delete(ctx, "a/b/c.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(ctx, "a/b/c.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get after delete: %v, want ErrNotFound", err)
	}
	if err := st.Delete(ctx, "a/b/c.txt"); err != nil {
		t.Errorf("delete of a missing file must succeed: %v", err)
	}
	if _, err := st.Get(ctx, "a/b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get of a directory: %v, want ErrNotFound", err)
	}

	// Временных файлов не остаётся.
	_ = filepath.WalkDir(dir, func(p string, _ fs.DirEntry, _ error) error {
		if strings.Contains(p, ".tmp-") {
			t.Errorf("temporary file left: %s", p)
		}
		return nil
	})
}

func TestDiskStorageFailedPut(t *testing.T) {
	ctx := context.Background()
	st, dir := newDisk(t)
	if err := st.Put(ctx, "x.txt", strings.NewReader("old")); err != nil {
		t.Fatal(err)
	}
	// Ошибка чтения посередине: старый файл цел, временного нет.
	err := st.Put(ctx, "x.txt", io.MultiReader(strings.NewReader("partial"), errReader{}))
	if err == nil {
		t.Fatal("want error")
	}
	if got := get(t, st, "x.txt"); got != "old" {
		t.Errorf("got %q after failed put, want old content", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want only x.txt", len(entries))
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestDiskStorageKeys(t *testing.T) {
	ctx := context.Background()
	st, dir := newDisk(t)
	// Файл рядом с хранилищем, до которого ключ не должен дотянуться.
	outside := filepath.Join(filepath.Dir(dir), "outside-"+filepath.Base(dir))
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if err := os.Symlink(filepath.Dir(dir), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	bad := []string{"", ".", "..", "../x", "a/../../x", "/abs", "a//b", "a/./b", `a\b`, "a/", "../outside-" + filepath.Base(dir)}
	for _, key := range bad {
		if err := st.Put(ctx, key, strings.NewReader("x")); err == nil {
			t.Errorf("put %q: want error", key)
		}
		if _, err := st.Get(ctx, key); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("get %q: %v, want invalid key error", key, err)
		}
		if err := st.Delete(ctx, key); err == nil {
			t.Errorf("delete %q: want error", key)
		}
	}
	// Символическая ссылка наружу не открывает доступ за пределы каталога.
	if _, err := st.Get(ctx, "link/outside-"+filepath.Base(dir)); err == nil {
		t.Error("get through a symlink escaping the root must fail")
	}
}

func TestSave(t *testing.T) {
	ctx := context.Background()
	st, _ := newDisk(t)
	res := mustProcess(t, "transparent.png")
	if err := res.Save(ctx, st, "2026/abc"); err != nil {
		t.Fatal(err)
	}
	keys := []string{"2026/abc/original.png", "2026/abc/480.webp", "2026/abc/480.jpg", "2026/abc/800.webp", "2026/abc/800.jpg"}
	for _, k := range keys {
		rc, err := st.Get(ctx, k)
		if err != nil {
			t.Errorf("%s: %v", k, err)
			continue
		}
		_ = rc.Close()
	}
	if got := get(t, st, "2026/abc/original.png"); !bytes.Equal([]byte(got), res.Original.Data) {
		t.Error("stored original differs from the upload")
	}
}

func TestDiskStorageCanceled(t *testing.T) {
	st, _ := newDisk(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := st.Put(ctx, "x", strings.NewReader("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("put: %v", err)
	}
}

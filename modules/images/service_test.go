package images

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // драйвер "sqlite" без CGO

	"github.com/voffkin/keelkit/modules/images/migrations"
)

// newDB — файл SQLite в t.TempDir() с миграциями модуля, применёнными goose так же, как это делает сайт.
func newDB(t *testing.T, migrate bool) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "site.db")+
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if !migrate {
		return db
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, migrations.FS,
		goose.WithTableName(migrations.Table), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

type testEnv struct {
	db  *sql.DB
	st  *DiskStorage
	dir string
}

func newEnv(t *testing.T) testEnv {
	t.Helper()
	st, dir := newDisk(t)
	return testEnv{db: newDB(t, true), st: st, dir: dir}
}

// service — сервис, строящий варианты профиля profile (в тестах повторной обработки — «старого»).
func (e testEnv) service(profile int, st Storage) *Service {
	if st == nil {
		st = e.st
	}
	s := NewService(e.db, st, Config{})
	s.profile = profile
	return s
}

// files — все обычные файлы хранилища, относительные пути через «/».
func (e testEnv) files(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(e.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(e.dir, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// expectedFiles — оригиналы и варианты по метаданным в БД.
func (e testEnv) expectedFiles(t *testing.T) []string {
	t.Helper()
	keys, err := queryStrings(t.Context(), e.db,
		`SELECT original_key FROM images_files UNION ALL SELECT key FROM images_variants`)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	return keys
}

func uploadFile(t *testing.T, s *Service, name string) *Image {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	img, err := s.Upload(t.Context(), bytes.NewReader(data), name)
	if err != nil {
		t.Fatalf("Upload(%s): %v", name, err)
	}
	return img
}

func TestUploadGetList(t *testing.T) {
	e := newEnv(t)
	s := e.service(ProfileVersion, nil)
	ctx := t.Context()

	photo := uploadFile(t, s, "photo.jpg")
	small := uploadFile(t, s, "small.jpg")
	if len(photo.ID) != 26 || strings.ToLower(photo.ID) != photo.ID || photo.ID == small.ID {
		t.Errorf("ids %q, %q: want distinct 26 lowercase chars", photo.ID, small.ID)
	}
	for _, v := range photo.Variants {
		if !strings.HasPrefix(v.Path, photo.ID+"/v1/") {
			t.Errorf("variant path %q, want prefix %s/v1/", v.Path, photo.ID)
		}
	}

	got, err := s.Get(ctx, photo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "photo.jpg" || got.Bytes != photo.Bytes || !got.CreatedAt.Equal(photo.CreatedAt) ||
		!slices.Equal(got.Variants, photo.Variants) {
		t.Errorf("Get = %+v\nwant %+v", got, photo)
	}
	if _, err := s.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(nope) = %v, want ErrNotFound", err)
	}

	all, err := s.List(ctx, 10, 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("List = %d, %v; want 2", len(all), err)
	}
	for _, img := range all {
		if len(img.Variants) == 0 {
			t.Errorf("List: %s without variants", img.ID)
		}
	}
	page, err := s.List(ctx, 1, 1)
	if err != nil || len(page) != 1 || page[0].ID != all[1].ID {
		t.Errorf("List(1, 1) = %v, %v; want [%s]", page, err, all[1].ID)
	}
	if !slices.Equal(e.files(t), e.expectedFiles(t)) {
		t.Errorf("files %v\nwant %v", e.files(t), e.expectedFiles(t))
	}
}

func TestUploadRemovesFilesOnDBError(t *testing.T) {
	st, dir := newDisk(t)
	s := NewService(newDB(t, false), st, Config{}) // таблиц нет: вставка метаданных упадёт
	data, err := os.ReadFile("testdata/small.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upload(t.Context(), bytes.NewReader(data), "small.jpg"); err == nil {
		t.Fatal("Upload without tables succeeded")
	}
	if files := (testEnv{dir: dir}).files(t); len(files) != 0 {
		t.Errorf("files left after failed upload: %v", files)
	}
}

func TestUploadWaitsForSlot(t *testing.T) {
	e := newEnv(t)
	s := NewService(e.db, e.st, Config{MaxConcurrent: 1})
	s.sem <- struct{}{} // единственный слот занят «другой обработкой»
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	data, _ := os.ReadFile("testdata/small.jpg")
	if _, err := s.Upload(ctx, bytes.NewReader(data), ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Upload with busy slot = %v, want DeadlineExceeded", err)
	}
	<-s.sem
	if _, err := s.Upload(t.Context(), bytes.NewReader(data), ""); err != nil {
		t.Errorf("Upload with free slot: %v", err)
	}
}

func TestDelete(t *testing.T) {
	e := newEnv(t)
	s := e.service(ProfileVersion, nil)
	keep := uploadFile(t, s, "small.jpg")
	gone := uploadFile(t, s, "transparent.png")

	if err := s.Delete(t.Context(), gone.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(t.Context(), gone.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
	if !slices.Equal(e.files(t), e.expectedFiles(t)) {
		t.Errorf("files %v\nwant %v", e.files(t), e.expectedFiles(t))
	}
	if _, err := s.Get(t.Context(), keep.ID); err != nil {
		t.Errorf("other image: %v", err)
	}
}

// TestDeleteReferenced: если на изображение ссылается контент сайта, Delete откатывается и файлы целы.
func TestDeleteReferenced(t *testing.T) {
	e := newEnv(t)
	s := e.service(ProfileVersion, nil)
	img := uploadFile(t, s, "small.jpg")
	if _, err := e.db.Exec(`CREATE TABLE posts (cover TEXT REFERENCES images_files (id));
		INSERT INTO posts VALUES (?)`, img.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(t.Context(), img.ID); err == nil {
		t.Fatal("Delete of referenced image succeeded")
	}
	if _, err := s.Get(t.Context(), img.ID); err != nil {
		t.Errorf("Get after failed Delete: %v", err)
	}
	if !slices.Equal(e.files(t), e.expectedFiles(t)) {
		t.Errorf("files %v\nwant %v", e.files(t), e.expectedFiles(t))
	}
}

func TestReprocess(t *testing.T) {
	e := newEnv(t)
	old := e.service(0, nil)
	var ids []string
	for _, name := range []string{"photo.jpg", "small.jpg", "transparent.png", "orientation-6.webp"} {
		ids = append(ids, uploadFile(t, old, name).ID)
	}
	s := e.service(ProfileVersion, nil)

	if st, err := s.Reprocess(t.Context(), 0); err != nil || st.Processed != 0 {
		t.Errorf("Reprocess(0) = %+v, %v; want nothing to do", st, err)
	}
	st, err := s.Reprocess(t.Context(), ProfileVersion+5) // выше текущей версии — то же, что текущая
	if err != nil || st != (ReprocessStats{Processed: len(ids)}) {
		t.Fatalf("Reprocess = %+v, %v; want %d processed", st, err, len(ids))
	}
	checkAllCurrent(t, e, s, ids)

	st, err = s.Reprocess(t.Context(), ProfileVersion+5)
	if err != nil || st != (ReprocessStats{}) {
		t.Errorf("second Reprocess = %+v, %v; want nothing to do", st, err)
	}
}

// checkAllCurrent: все изображения на текущем профиле, в хранилище ровно оригиналы и текущие варианты.
func checkAllCurrent(t *testing.T, e testEnv, s *Service, ids []string) {
	t.Helper()
	for _, id := range ids {
		img, err := s.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if img.ProfileVersion != ProfileVersion || len(img.Variants) == 0 {
			t.Errorf("%s: profile %d with %d variants, want %d", id, img.ProfileVersion, len(img.Variants), ProfileVersion)
		}
		for _, v := range img.Variants {
			if !strings.HasPrefix(v.Path, id+"/v1/") {
				t.Errorf("%s: variant %s, want under v1/", id, v.Path)
			}
		}
	}
	if files, want := e.files(t), e.expectedFiles(t); !slices.Equal(files, want) {
		t.Errorf("files %v\nwant %v", files, want)
	}
}

// cancelingStorage отменяет контекст после n-й записи — как Ctrl+C посреди повторной обработки.
type cancelingStorage struct {
	Storage
	n      atomic.Int32
	cancel context.CancelFunc
}

func (c *cancelingStorage) Put(ctx context.Context, key string, r io.Reader) error {
	if c.n.Add(-1) == 0 {
		c.cancel()
	}
	return c.Storage.Put(ctx, key, r)
}

func TestReprocessInterrupted(t *testing.T) {
	e := newEnv(t)
	old := e.service(0, nil)
	var ids []string
	for _, name := range []string{"photo.jpg", "small.jpg", "transparent.png", "orientation-3.jpg"} {
		ids = append(ids, uploadFile(t, old, name).ID)
	}

	// Прерываем на каждой записи по очереди: каждый раз — в новом месте процесса.
	for n := int32(1); ; n++ {
		ctx, cancel := context.WithCancel(t.Context())
		cs := &cancelingStorage{Storage: e.st, cancel: cancel}
		cs.n.Store(n)
		st, err := e.service(ProfileVersion, cs).Reprocess(ctx, ProfileVersion)
		cancel()
		if err == nil {
			t.Logf("finished after %d interruptions, last run: %+v", n-1, st)
			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupted Reprocess: %v", err)
		}
		// После каждого прерывания каждое изображение целиком на одном из профилей и все его файлы на месте.
		for _, id := range ids {
			img, err := old.Get(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(old.Handler())
			for _, v := range img.Variants {
				resp, err := http.Get(srv.URL + "/" + v.Path)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("after %d writes: %s = %d", n, v.Path, resp.StatusCode)
				}
			}
			srv.Close()
		}
		if n > 200 {
			t.Fatal("Reprocess never finished")
		}
	}
	checkAllCurrent(t, e, e.service(ProfileVersion, nil), ids)
}

func TestReprocessMissingOriginal(t *testing.T) {
	e := newEnv(t)
	old := e.service(0, nil)
	broken := uploadFile(t, old, "small.jpg")
	ok := uploadFile(t, old, "transparent.png")
	if err := os.Remove(filepath.Join(e.dir, broken.ID, "original.jpg")); err != nil {
		t.Fatal(err)
	}

	s := e.service(ProfileVersion, nil)
	st, err := s.Reprocess(t.Context(), ProfileVersion)
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), broken.ID) {
		t.Errorf("Reprocess error = %v, want ErrNotFound for %s", err, broken.ID)
	}
	if st != (ReprocessStats{Processed: 1, Failed: 1}) {
		t.Errorf("Reprocess = %+v, want 1 processed, 1 failed", st)
	}
	if img, _ := s.Get(t.Context(), ok.ID); img.ProfileVersion != ProfileVersion {
		t.Errorf("good image: profile %d", img.ProfileVersion)
	}
	if img, _ := s.Get(t.Context(), broken.ID); img.ProfileVersion != 0 || len(img.Variants) == 0 {
		t.Errorf("broken image: profile %d with %d variants, want old variants kept", img.ProfileVersion, len(img.Variants))
	}
}

// TestReprocessDeletedMeanwhile: изображение удалили, пока строились новые варианты, — новые файлы не остаются.
func TestReprocessDeletedMeanwhile(t *testing.T) {
	e := newEnv(t)
	img := uploadFile(t, e.service(0, nil), "small.jpg")
	s := e.service(ProfileVersion, nil)
	res, err := s.processStored(t.Context(), img.ID+"/original.jpg")
	if err != nil {
		t.Fatal(err)
	}
	vs, err := s.putVariants(t.Context(), img.ID, res.Variants)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(t.Context(), img.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.commitReprocess(t.Context(), img.ID, 0, vs); err != nil {
		t.Fatal(err)
	}
	if err := s.emptyTrash(t.Context()); err != nil {
		t.Fatal(err)
	}
	if files := e.files(t); len(files) != 0 {
		t.Errorf("files left: %v", files)
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"photo.jpg":              "photo.jpg",
		`C:\Users\me\Фото 1.jpg`: "Фото 1.jpg",
		"../../etc/passwd":       "passwd",
		"":                       "",
		"/":                      "",
		"a\x00b\nc.png":          "abc.png",
		strings.Repeat("я", 200): strings.Repeat("я", 127),
		"bad\xffutf8.jpg":        "badutf8.jpg",
		"  spaced.jpg  ":         "spaced.jpg",
	} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

// failingDeleteStorage не удаляет файлы — как падение процесса сразу после транзакции.
type failingDeleteStorage struct{ Storage }

func (failingDeleteStorage) Delete(context.Context, string) error { return errors.New("disk on fire") }

// TestReprocessCrashAfterCommit: старые варианты, не удалённые после транзакции, удаляет следующий запуск.
func TestReprocessCrashAfterCommit(t *testing.T) {
	e := newEnv(t)
	ids := []string{uploadFile(t, e.service(0, nil), "small.jpg").ID}

	st, err := e.service(ProfileVersion, failingDeleteStorage{e.st}).Reprocess(t.Context(), ProfileVersion)
	if err != nil || st.Processed != 1 {
		t.Fatalf("Reprocess = %+v, %v", st, err)
	}
	if files, want := e.files(t), e.expectedFiles(t); len(files) <= len(want) {
		t.Fatalf("old variants should still be on disk: %v", files)
	}
	st, err = e.service(ProfileVersion, nil).Reprocess(t.Context(), ProfileVersion)
	if err != nil || st.Processed != 0 {
		t.Fatalf("second Reprocess = %+v, %v", st, err)
	}
	checkAllCurrent(t, e, e.service(ProfileVersion, nil), ids)
}

package files

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // драйвер "sqlite" без CGO

	"github.com/keelkit/keelkit/modules/files/migrations"
)

type testEnv struct {
	db  *sql.DB
	svc *Service
	dir string
}

// newEnv — БД с миграциями модуля (как их применяет сайт) и таблицей сайта, ссылающейся на файлы
// внешним ключом без ON DELETE; хранилище во временном каталоге; лимит — 1 МБ.
func newEnv(t *testing.T) testEnv {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "site.db")+
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p, err := goose.NewProvider(goose.DialectSQLite3, db, migrations.FS,
		goose.WithTableName(migrations.Table), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE site_docs (file_id TEXT NOT NULL REFERENCES files_files (id)) STRICT`); err != nil {
		t.Fatal(err)
	}
	st, dir := newDisk(t)
	return testEnv{db: db, svc: NewService(db, st, Config{MaxBytes: 1 << 20}), dir: dir}
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
	return out
}

func (e testEnv) upload(t *testing.T, data string) *File {
	t.Helper()
	f, err := e.svc.Upload(t.Context(), strings.NewReader(data), "doc.pdf")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	return f
}

func (e testEnv) serve(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.svc.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+path, nil))
	return rec
}

const pdf = "%PDF-1.4\n%%EOF\n"

func TestDefaultMaxBytes(t *testing.T) {
	if got := NewService(nil, nil, Config{}).MaxBytes(); got != DefaultMaxBytes || DefaultMaxBytes != 50<<20 {
		t.Errorf("MaxBytes() = %d, DefaultMaxBytes = %d; want 50 MB", got, DefaultMaxBytes)
	}
}

// Отклонённая загрузка ничего не оставляет ни в БД, ни в хранилище.
func TestRejectedUploadLeavesNothing(t *testing.T) {
	e := newEnv(t)
	for _, r := range []io.Reader{
		io.MultiReader(strings.NewReader("%PDF-1.4\n"), bytes.NewReader(make([]byte, 1<<20))),
		strings.NewReader("<html></html>"),
		io.MultiReader(strings.NewReader("%PDF-1.4\n"), failingReader{}),
	} {
		if _, err := e.svc.Upload(t.Context(), r, "x.pdf"); err == nil {
			t.Error("Upload succeeded, want an error")
		}
	}
	if got := e.files(t); len(got) != 0 {
		t.Errorf("storage after rejected uploads: %v, want empty", got)
	}
	if list, _ := e.svc.List(t.Context(), 10, 0); len(list) != 0 {
		t.Errorf("List: %d files, want 0", len(list))
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// Замена оставляет в хранилище только новый файл; прежний уходит через корзину.
func TestReplaceKeepsOneFile(t *testing.T) {
	e := newEnv(t)
	f := e.upload(t, pdf)
	before := e.files(t)
	g, err := e.svc.Replace(t.Context(), f.ID, strings.NewReader(pdf+"v2"), "new.pdf")
	if err != nil {
		t.Fatal(err)
	}
	after := e.files(t)
	if len(after) != 1 || slices.Equal(after, before) || !strings.HasPrefix(after[0], f.ID+"/") {
		t.Errorf("storage: %v before, %v after Replace; want one new file under %s/", before, after, f.ID)
	}
	if !g.UpdatedAt.After(f.CreatedAt.Add(-1)) || g.CreatedAt != f.CreatedAt || g.URLName != "doc.pdf" {
		t.Errorf("Replace = %+v, want the same CreatedAt and URLName, a fresh UpdatedAt", g)
	}
	if rec := e.serve(f.Path); rec.Body.String() != pdf+"v2" {
		t.Errorf("GET after Replace = %q, want the new content", rec.Body.String())
	}
	var trash int
	if err := e.db.QueryRow(`SELECT count(*) FROM files_trash`).Scan(&trash); err != nil || trash != 0 {
		t.Errorf("files_trash: %d rows, %v; want empty", trash, err)
	}
}

// Используемый файл (внешний ключ сайта без ON DELETE) не удаляется: ошибка драйвера, файл на месте.
func TestDeleteUsedFile(t *testing.T) {
	e := newEnv(t)
	f := e.upload(t, pdf)
	if _, err := e.db.Exec(`INSERT INTO site_docs (file_id) VALUES (?)`, f.ID); err != nil {
		t.Fatal(err)
	}
	err := e.svc.Delete(t.Context(), f.ID)
	if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("Delete of a used file: %v, want a foreign key error", err)
	}
	if rec := e.serve(f.Path); rec.Code != http.StatusOK || len(e.files(t)) != 1 {
		t.Errorf("after a refused Delete: GET %d, %d files in storage; want 200 and the file kept", rec.Code, len(e.files(t)))
	}
	if _, err := e.db.Exec(`DELETE FROM site_docs`); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(t.Context(), f.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := e.files(t); len(got) != 0 {
		t.Errorf("storage after Delete: %v, want empty", got)
	}
}

// Корзина, оставшаяся после прерванного процесса, разбирается следующим удалением.
func TestTrashLeftoversRemoved(t *testing.T) {
	e := newEnv(t)
	f := e.upload(t, pdf)
	g := e.upload(t, pdf)
	if _, err := e.db.Exec(`INSERT INTO files_trash (key) SELECT key FROM files_files WHERE id = ?`, g.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(t.Context(), f.ID); err != nil {
		t.Fatal(err)
	}
	// Ключ g всё ещё используется — файл остался, запись корзины убрана.
	if got := e.files(t); len(got) != 1 || !strings.HasPrefix(got[0], g.ID+"/") {
		t.Errorf("storage: %v, want only %s", got, g.ID)
	}
	if rec := e.serve(g.Path); rec.Code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", g.Path, rec.Code)
	}
}

func TestHandlerMethods(t *testing.T) {
	e := newEnv(t)
	f := e.upload(t, pdf)
	rec := httptest.NewRecorder()
	e.svc.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+f.Path, nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST = %d, Allow %q; want 405, GET, HEAD", rec.Code, rec.Header().Get("Allow"))
	}
	// Файл пропал из хранилища (восстановили старую БД без загрузок) — 404, а не 500.
	if err := e.svc.st.Delete(t.Context(), mustKey(t, e.db, f.ID)); err != nil {
		t.Fatal(err)
	}
	if rec := e.serve(f.Path); rec.Code != http.StatusNotFound {
		t.Errorf("GET of a file missing in storage = %d, want 404", rec.Code)
	}
}

func mustKey(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var key string
	if err := db.QueryRow(`SELECT key FROM files_files WHERE id = ?`, id).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key
}

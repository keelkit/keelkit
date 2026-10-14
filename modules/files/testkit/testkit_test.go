package testkit_test

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // драйвер "sqlite" без CGO

	"github.com/keelkit/keelkit/modules/files"
	"github.com/keelkit/keelkit/modules/files/migrations"
	"github.com/keelkit/keelkit/modules/files/testkit"
)

// TestRun — контрактные тесты на минимальной интеграции, как её собрал бы сайт.
func TestRun(t *testing.T) {
	testkit.Run(t, func(t *testing.T) testkit.Env {
		dir := t.TempDir()
		db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "site.db")+"?_pragma=foreign_keys(1)")
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
		st, err := files.NewDiskStorage(filepath.Join(dir, "files"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		svc := files.NewService(db, st, files.Config{MaxBytes: 1 << 20})

		mux := http.NewServeMux()
		mux.Handle("GET /files/", http.StripPrefix("/files", svc.Handler()))
		return testkit.Env{Service: svc, Handler: mux, URL: func(p string) string { return "/files/" + p }}
	})
}

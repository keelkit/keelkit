package testkit_test

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // драйвер "sqlite" без CGO

	"github.com/voffkin/keelkit/modules/images"
	"github.com/voffkin/keelkit/modules/images/migrations"
	"github.com/voffkin/keelkit/modules/images/testkit"
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
		st, err := images.NewDiskStorage(filepath.Join(dir, "uploads"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		svc := images.NewService(db, st, images.Config{})

		mux := http.NewServeMux()
		mux.Handle("/media/", http.StripPrefix("/media/", svc.Handler()))
		return testkit.Env{Service: svc, Handler: mux, URL: func(p string) string { return "/media/" + p }}
	})
}

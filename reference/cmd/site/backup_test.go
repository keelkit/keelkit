package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keelkit/keelkit/modules/files"
	"github.com/keelkit/keelkit/modules/files/testkit"

	"github.com/keelkit/keelkit/reference/internal/web"
)

// TestBackupRestore: из снимка и архива site backup поднимается рабочий сайт — изображения и файлы отдаются.
func TestBackupRestore(t *testing.T) {
	ctx := context.Background()
	s := migratedSite(t, false)
	img, err := s.media.Upload(ctx, bytes.NewReader(encodeJPEG(t, 600, 400)), "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	pdf := testkit.Sample(files.PDF, 2000)
	f, err := s.files.Upload(ctx, bytes.NewReader(pdf), "report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	// Недописанный файл хранилища в архив не попадает.
	if err := os.WriteFile(filepath.Join(s.cfg.uploadsDir(), "x.jpg.tmp-abc"), []byte("partial"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := backup(ctx, s.cfg); err != nil {
		t.Fatalf("backup: %v", err)
	}
	archives, _ := filepath.Glob(filepath.Join(s.cfg.BackupDir, "*.tar.gz"))
	if len(archives) != 1 {
		t.Fatalf("archives = %v, want one", archives)
	}
	snapshot := strings.TrimSuffix(archives[0], ".tar.gz") + ".db"
	for _, p := range []string{snapshot, archives[0]} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, want 0600", p, fi.Mode().Perm())
		}
	}
	if tmp, _ := filepath.Glob(filepath.Join(s.cfg.BackupDir, ".backup-*")); len(tmp) > 0 {
		t.Errorf("temporary files left: %v", tmp)
	}

	// Восстановление: архив распаковывается в новый DATA_DIR, снимок становится site.db.
	restored := testConfig(t)
	names := extractTarGz(t, archives[0], restored.DataDir)
	if names["uploads/x.jpg.tmp-abc"] {
		t.Error("archive contains temporary storage file")
	}
	data, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(restored.dbPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(ctx, restored)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	media, closeMedia, err := openMedia(db, restored)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeMedia)
	fls, closeFiles, err := openFiles(db, restored)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFiles)
	r := testSite{cfg: restored, db: db, media: media, files: fls}

	got, err := r.media.Get(ctx, img.ID)
	if err != nil {
		t.Fatalf("restored image: %v", err)
	}
	for _, v := range got.Variants {
		if rec := get(t, r, web.MediaPrefix+v.Path); rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d", v.Path, rec.Code)
		}
	}
	rec := get(t, r, web.FilesPrefix+f.Path)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pdf) {
		t.Errorf("GET %s: status %d, body equal %v", f.Path, rec.Code, bytes.Equal(rec.Body.Bytes(), pdf))
	}
}

// TestBackupRequiresDatabase: при неверном DATA_DIR бэкап — ошибка, а не снимок пустой БД.
func TestBackupRequiresDatabase(t *testing.T) {
	cfg := testConfig(t)
	if err := backup(context.Background(), cfg); err == nil {
		t.Fatal("backup without database: no error")
	}
	if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
		t.Errorf("DATA_DIR created: %v", err)
	}
}

// extractTarGz распаковывает архив в dir и возвращает имена записей.
func extractTarGz(t *testing.T, archive, dir string) map[string]bool {
	t.Helper()
	af, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = af.Close() }()
	gz, err := gzip.NewReader(af)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names[hdr.Name] = true
		p := filepath.Join(dir, filepath.FromSlash(hdr.Name))
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(p, 0o750); err != nil {
				t.Fatal(err)
			}
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o640); err != nil {
			t.Fatal(err)
		}
	}
}

package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/keelkit/keelkit/reference/internal/database"
)

// backup — site backup: снимок БД (VACUUM INTO) и архив загрузок в BACKUP_DIR с одной меткой времени:
// <время>.db и <время>.tar.gz (каталоги uploads/ и files/ из DATA_DIR). Сервер может работать.
//
// Сначала снимок, потом архив: файлы пишутся в хранилище до строки в БД, а удаляются после неё, поэтому всё, на что
// ссылается снимок, уже лежит на диске. Пропасть может только файл, удалённый, пока идёт архив (редактор удалил
// изображение или идёт reprocess-images), — такие файлы перечисляются в логе. Лишние файлы в архиве безвредны.
func backup(ctx context.Context, cfg config) error {
	// Не openDB: тот создал бы пустую БД, и бэкап «прошёл» бы на неверном DATA_DIR.
	if _, err := os.Stat(cfg.dbPath()); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	db, err := database.Open(ctx, cfg.dbPath())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	snapshot, err := database.Snapshot(ctx, db, cfg.BackupDir)
	if err != nil {
		return err
	}
	archive := strings.TrimSuffix(snapshot, ".db") + ".tar.gz"
	n, missing, err := archiveDirs(ctx, archive, cfg.DataDir, "uploads", "files")
	if err != nil {
		return fmt.Errorf("archive uploads (snapshot %s saved): %w", snapshot, err)
	}
	for _, name := range missing {
		slog.Warn("file deleted during backup, not archived", "path", name)
	}
	slog.Info("backup saved", "db", snapshot, "archive", archive, "files", n)
	return nil
}

// archiveDirs пишет каталоги dirs из base в tar.gz по пути dst; пути в архиве — относительно base
// (uploads/…, files/…). Архив появляется под именем dst только целиком: до этого он пишется во временный файл.
// Отсутствующий каталог пропускается (загрузок ещё не было). Возвращает число файлов и файлы, исчезнувшие во время записи.
func archiveDirs(ctx context.Context, dst, base string, dirs ...string) (n int, missing []string, err error) {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".backup-*.tmp")
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	// Изображения, PDF и видео уже сжаты: быстрое сжатие почти не уступает в размере, а CPU тратит в разы меньше.
	gz, err := gzip.NewWriterLevel(tmp, gzip.BestSpeed)
	if err != nil {
		return 0, nil, err
	}
	tw := tar.NewWriter(gz)
	for _, dir := range dirs {
		err = filepath.WalkDir(filepath.Join(base, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					if p != filepath.Join(base, dir) {
						missing = append(missing, p)
					}
					return nil
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			// Недописанные файлы хранилища (Put пишет в «<ключ>.tmp-<случайное>» и переименовывает).
			if strings.Contains(d.Name(), ".tmp-") {
				return nil
			}
			ok, err := addToTar(tw, base, p, d)
			if errors.Is(err, fs.ErrNotExist) {
				missing = append(missing, p)
				return nil
			}
			if ok {
				n++
			}
			return err
		})
		if err != nil {
			return 0, nil, err
		}
	}
	if err = tw.Close(); err != nil {
		return 0, nil, err
	}
	if err = gz.Close(); err != nil {
		return 0, nil, err
	}
	if err = tmp.Sync(); err != nil {
		return 0, nil, err
	}
	if err = tmp.Close(); err != nil {
		return 0, nil, err
	}
	if err = os.Rename(tmp.Name(), dst); err != nil {
		return 0, nil, err
	}
	return n, missing, nil
}

// addToTar добавляет каталог или обычный файл p; isFile — добавлен файл. Другие типы (ссылки, устройства)
// хранилище не создаёт — ошибка, чтобы бэкап не пропустил что-то молча.
func addToTar(tw *tar.Writer, base, p string, d fs.DirEntry) (isFile bool, err error) {
	info, err := d.Info()
	if err != nil {
		return false, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s: not a regular file", p)
	}
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return false, err
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return false, err
	}
	hdr.Name = filepath.ToSlash(rel)
	if info.IsDir() {
		hdr.Name += "/"
		return false, tw.WriteHeader(hdr)
	}
	f, err := os.Open(p) //nolint:gosec // p — из обхода DATA_DIR, не из запроса
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	if err := tw.WriteHeader(hdr); err != nil {
		return false, err
	}
	// Ровно hdr.Size байт: файлы хранилища не меняются на месте, а укоротившийся файл — ошибка, а не битый архив.
	if _, err := io.CopyN(tw, f, hdr.Size); err != nil {
		return false, fmt.Errorf("%s: %w", p, err)
	}
	return true, nil
}

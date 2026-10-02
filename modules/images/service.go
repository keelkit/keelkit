package images

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// DefaultMaxConcurrent — сколько изображений Service обрабатывает одновременно, если в Config не задано иное.
// Фото 40 Мп при обработке занимает ~400 МБ памяти.
const DefaultMaxConcurrent = 2

// maxNameBytes — предел длины исходного имени файла, которое хранится для админки.
const maxNameBytes = 255

// Config — настройки Service.
type Config struct {
	// MaxConcurrent — сколько изображений обрабатывается одновременно (загрузки и повторная обработка вместе);
	// остальные ждут очереди. 0 — DefaultMaxConcurrent.
	MaxConcurrent int
}

// Service — загрузка, хранение и отдача изображений. Метаданные — в таблицах images_* базы сайта
// (SQLite, миграции из пакета migrations), файлы — в Storage. Безопасен для одновременного использования.
//
// Раскладка файлов в хранилище: оригинал — «<id>/original.<ext>», варианты — «<id>/v<профиль>/<ширина>.<ext>».
// Версия профиля в пути вариантов меняет их адрес при повторной обработке, поэтому Handler отдаёт их
// с бессрочным кэшем (immutable).
type Service struct {
	db      *sql.DB
	st      Storage
	sem     chan struct{}
	profile int // версия профиля новых вариантов; в тестах модуля — меньше ProfileVersion
}

// NewService создаёт сервис поверх БД сайта с применёнными миграциями модуля и хранилища st.
func NewService(db *sql.DB, st Storage, cfg Config) *Service {
	n := cfg.MaxConcurrent
	if n <= 0 {
		n = DefaultMaxConcurrent
	}
	return &Service{db: db, st: st, sem: make(chan struct{}, n), profile: ProfileVersion}
}

// Image — загруженное изображение.
type Image struct {
	ID             string
	Name           string // исходное имя файла (без каталогов), может быть пустым
	Format         Format // формат оригинала
	Width, Height  int    // размеры оригинала с учётом ориентации EXIF
	Bytes          int64  // размер оригинала
	ProfileVersion int    // версия профиля, которым построены Variants
	CreatedAt      time.Time
	Variants       []StoredVariant // по возрастанию ширины; для каждой ширины — WebP, затем JPEG
}

// StoredVariant — вариант изображения в хранилище.
type StoredVariant struct {
	Format        Format
	Width, Height int
	Bytes         int64
	// Path — путь варианта относительно корня Handler (он же ключ в Storage), например «k3x…/v1/960.webp».
	// Сайт строит адрес как <префикс, под которым смонтирован Handler> + Path.
	Path string
}

// Upload проверяет и обрабатывает файл (см. Process), сохраняет оригинал и варианты и записывает метаданные.
// name — исходное имя файла, только для показа в админке. Ошибки проверки оборачивают ErrTooLarge,
// ErrTooManyPixels, ErrUnsupportedFormat или ErrInvalidImage. Пока очередь на обработку занята
// (Config.MaxConcurrent), Upload ждёт или возвращает ошибку ctx.
func (s *Service) Upload(ctx context.Context, r io.Reader, name string) (*Image, error) {
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	res, err := Process(ctx, r)
	s.release()
	if err != nil {
		return nil, err
	}

	id := newID()
	img := &Image{
		ID:             id,
		Name:           cleanName(name),
		Format:         res.Original.Format,
		Width:          res.Original.Width,
		Height:         res.Original.Height,
		Bytes:          int64(len(res.Original.Data)),
		ProfileVersion: s.profile,
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
	}
	origKey := id + "/" + res.Original.Name()
	if err := putBytes(ctx, s.st, origKey, res.Original.Data); err != nil {
		return nil, err
	}
	written := []string{origKey}
	img.Variants, err = s.putVariants(ctx, id, res.Variants)
	for _, v := range img.Variants {
		written = append(written, v.Path)
	}
	if err == nil {
		err = s.insert(ctx, img, origKey)
	}
	if err != nil {
		s.deleteFiles(written)
		return nil, err
	}
	return img, nil
}

func (s *Service) insert(ctx context.Context, img *Image, origKey string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("images: upload: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO images_files
		(id, name, format, width, height, bytes, original_key, profile_version, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		img.ID, img.Name, string(img.Format), img.Width, img.Height, img.Bytes, origKey, img.ProfileVersion,
		img.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("images: upload: %w", err)
	}
	if err := insertVariants(ctx, tx, img.ID, img.Variants); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("images: upload: %w", err)
	}
	return nil
}

// Get возвращает изображение с вариантами; если его нет — ошибку, оборачивающую ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (*Image, error) {
	imgs, err := s.query(ctx, `WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(imgs) == 0 {
		return nil, fmt.Errorf("%w: image %q", ErrNotFound, id)
	}
	return imgs[0], nil
}

// List возвращает изображения от новых к старым, не больше limit начиная с offset.
func (s *Service) List(ctx context.Context, limit, offset int) ([]*Image, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.query(ctx, `ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, max(offset, 0))
}

// query выбирает изображения условием tail (WHERE/ORDER/LIMIT по images_files) и их варианты одним снимком БД.
func (s *Service) query(ctx context.Context, tail string, args ...any) ([]*Image, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("images: query: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// #nosec G202 -- tail — строковая константа из Get/List, значения передаются параметрами
	rows, err := tx.QueryContext(ctx, `SELECT id, name, format, width, height, bytes, profile_version, created_at
		FROM images_files `+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("images: query: %w", err)
	}
	var imgs []*Image
	byID := map[string]*Image{}
	for rows.Next() {
		var img Image
		var format, created string
		if err := rows.Scan(&img.ID, &img.Name, &format, &img.Width, &img.Height, &img.Bytes,
			&img.ProfileVersion, &created); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("images: query: %w", err)
		}
		img.Format = Format(format)
		if img.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("images: query: created_at of %s: %w", img.ID, err)
		}
		imgs = append(imgs, &img)
		byID[img.ID] = &img
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, fmt.Errorf("images: query: %w", err)
	}
	if len(imgs) == 0 {
		return nil, nil
	}

	// Варианты — тем же условием, что и изображения, в одном запросе.
	// #nosec G202 -- см. выше
	rows, err = tx.QueryContext(ctx, `SELECT image_id, format, width, height, bytes, key FROM images_variants
		WHERE image_id IN (SELECT id FROM images_files `+tail+`)
		ORDER BY image_id, width, format DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("images: query variants: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, format string
		var v StoredVariant
		if err := rows.Scan(&id, &format, &v.Width, &v.Height, &v.Bytes, &v.Path); err != nil {
			return nil, fmt.Errorf("images: query variants: %w", err)
		}
		v.Format = Format(format)
		if img := byID[id]; img != nil {
			img.Variants = append(img.Variants, v)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("images: query variants: %w", err)
	}
	return imgs, nil
}

// Delete удаляет изображение: метаданные — в транзакции, файлы — после неё. Если изображения нет —
// ошибка, оборачивающая ErrNotFound. Если на изображение ссылается контент сайта (внешний ключ без
// ON DELETE), транзакция откатывается и файлы остаются на месте. Файлы, которые не удалось удалить,
// удалит следующий вызов Delete или Reprocess.
func (s *Service) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("images: delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Сначала запись: транзакция сразу берёт блокировку на запись, без конфликта «чтение → запись».
	keys, err := queryStrings(ctx, tx, `DELETE FROM images_variants WHERE image_id = ? RETURNING key`, id)
	if err != nil {
		return fmt.Errorf("images: delete: %w", err)
	}
	orig, err := queryStrings(ctx, tx, `DELETE FROM images_files WHERE id = ? RETURNING original_key`, id)
	if err != nil {
		return fmt.Errorf("images: delete: %w", err)
	}
	if len(orig) == 0 {
		return fmt.Errorf("%w: image %q", ErrNotFound, id)
	}
	if err := trash(ctx, tx, append(keys, orig...)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("images: delete: %w", err)
	}
	_ = s.emptyTrash(ctx) // остатки удалит следующий вызов
	return nil
}

// putVariants кладёт варианты в «<id>/v<профиль>/…» и возвращает их описание; при ошибке — уже записанные.
func (s *Service) putVariants(ctx context.Context, id string, vs []Variant) ([]StoredVariant, error) {
	out := make([]StoredVariant, 0, len(vs))
	for _, v := range vs {
		key := id + "/v" + strconv.Itoa(s.profile) + "/" + v.Name()
		if err := putBytes(ctx, s.st, key, v.Data); err != nil {
			return out, err
		}
		out = append(out, StoredVariant{
			Format: v.Format, Width: v.Width, Height: v.Height, Bytes: int64(len(v.Data)), Path: key,
		})
	}
	return out, nil
}

func insertVariants(ctx context.Context, tx *sql.Tx, id string, vs []StoredVariant) error {
	for _, v := range vs {
		_, err := tx.ExecContext(ctx, `INSERT INTO images_variants (image_id, format, width, height, bytes, key)
			VALUES (?, ?, ?, ?, ?, ?)`, id, string(v.Format), v.Width, v.Height, v.Bytes, v.Path)
		if err != nil {
			return fmt.Errorf("images: insert variant %s: %w", v.Path, err)
		}
	}
	return nil
}

// trash отмечает файлы на удаление; ключи, на которые всё ещё ссылаются строки, пропускаются.
func trash(ctx context.Context, tx *sql.Tx, keys []string) error {
	for _, k := range keys {
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO images_trash (key) SELECT ?1
			WHERE NOT EXISTS (SELECT 1 FROM images_variants WHERE key = ?1)
			AND NOT EXISTS (SELECT 1 FROM images_files WHERE original_key = ?1)`, k)
		if err != nil {
			return fmt.Errorf("images: trash %s: %w", k, err)
		}
	}
	return nil
}

// emptyTrash удаляет файлы из images_trash. Ключ, на который снова ссылаются строки, из корзины
// убирается без удаления файла.
func (s *Service) emptyTrash(ctx context.Context) error {
	for {
		keys, err := queryStrings(ctx, s.db, `SELECT key FROM images_trash ORDER BY key LIMIT 100`)
		if err != nil {
			return fmt.Errorf("images: empty trash: %w", err)
		}
		if len(keys) == 0 {
			return nil
		}
		for _, k := range keys {
			var used bool
			err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM images_variants WHERE key = ?1)
				OR EXISTS (SELECT 1 FROM images_files WHERE original_key = ?1)`, k).Scan(&used)
			if err != nil {
				return fmt.Errorf("images: empty trash: %w", err)
			}
			if !used {
				if err := s.st.Delete(ctx, k); err != nil {
					return err
				}
			}
			if _, err := s.db.ExecContext(ctx, `DELETE FROM images_trash WHERE key = ?`, k); err != nil {
				return fmt.Errorf("images: empty trash: %w", err)
			}
		}
	}
}

// deleteFiles убирает файлы незавершённой загрузки; ошибки игнорируются — это уборка после другой ошибки.
func (s *Service) deleteFiles(keys []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, k := range keys {
		_ = s.st.Delete(ctx, k)
	}
}

func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) release() { <-s.sem }

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryStrings(ctx context.Context, q querier, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func putBytes(ctx context.Context, st Storage, key string, data []byte) error {
	return st.Put(ctx, key, bytes.NewReader(data))
}

// newID — 128 случайных бит в base32 строчными буквами (26 символов): адрес изображения нельзя угадать.
func newID() string { return strings.ToLower(rand.Text()) }

// cleanName оставляет от имени файла последний элемент пути, без управляющих символов, не длиннее maxNameBytes.
func cleanName(name string) string {
	name = strings.ToValidUTF8(name, "")
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == "/" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	for len(name) > maxNameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return strings.TrimSpace(name)
}

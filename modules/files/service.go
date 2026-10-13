package files

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Пределы имён.
const (
	maxNameBytes   = 255 // исходное имя файла, которое хранится для админки
	maxURLNameRune = 80  // имя в адресе без расширения
)

// Config — настройки Service.
type Config struct {
	// MaxBytes — наибольший размер файла в байтах; 0 — DefaultMaxBytes.
	MaxBytes int64
}

// Service — загрузка, замена, хранение и отдача файлов. Метаданные — в таблицах files_* базы сайта
// (SQLite, миграции из пакета migrations), файлы — в Storage. Безопасен для одновременного использования.
//
// Раскладка в хранилище: «<id>/<случайное>.<ext>»; замена пишет файл под новым ключом и переключает на него
// строку в транзакции, старый удаляется после неё. Публичный адрес («<id>/<имя>.<ext>», File.Path) от ключа
// не зависит и при замене не меняется.
type Service struct {
	db       *sql.DB
	st       Storage
	maxBytes int64
}

// NewService создаёт сервис поверх БД сайта с применёнными миграциями модуля и хранилища st.
func NewService(db *sql.DB, st Storage, cfg Config) *Service {
	n := cfg.MaxBytes
	if n <= 0 {
		n = DefaultMaxBytes
	}
	return &Service{db: db, st: st, maxBytes: n}
}

// MaxBytes — лимит размера файла (Config.MaxBytes или DefaultMaxBytes). Сайт ограничивает тело запроса
// загрузки чуть больше него, чтобы пользователь увидел ErrTooLarge, а не обрыв соединения.
func (s *Service) MaxBytes() int64 { return s.maxBytes }

// File — загруженный файл.
type File struct {
	ID      string
	Name    string // исходное имя последней загрузки (без каталогов), может быть пустым
	Format  Format
	Bytes   int64
	URLName string // имя в адресе, с расширением: задаётся при загрузке и не меняется
	// Path — путь файла относительно корня Handler, уже экранированный для URL: «<id>/<URLName>».
	// Сайт строит адрес как <префикс, под которым смонтирован Handler> + Path.
	Path      string
	CreatedAt time.Time
	UpdatedAt time.Time // время последней замены (или загрузки)
}

// Upload проверяет формат и размер файла, сохраняет его и записывает метаданные. name — исходное имя файла:
// из него составляется имя в адресе и оно показывается в админке. Ошибки проверки оборачивают
// ErrTooLarge, ErrUnsupportedFormat или ErrInvalidFile.
func (s *Service) Upload(ctx context.Context, r io.Reader, name string) (*File, error) {
	id := newID()
	format, key, n, err := s.put(ctx, id, r)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	f := &File{ID: id, Name: cleanName(name), Format: format, Bytes: n, CreatedAt: now, UpdatedAt: now}
	f.URLName = urlName(f.Name, format)
	f.Path = filePath(id, f.URLName)
	_, err = s.db.ExecContext(ctx, `INSERT INTO files_files (id, name, url_name, format, bytes, key, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.Name, f.URLName, string(f.Format), f.Bytes, key, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		s.deleteFile(key)
		return nil, fmt.Errorf("files: upload: %w", err)
	}
	return f, nil
}

// Replace заменяет содержимое файла id новым, того же формата: адрес (Path) не меняется, Name и Bytes —
// от новой загрузки. Нет файла — ошибка, оборачивающая ErrNotFound; другой формат — ErrFormatMismatch;
// ошибки проверки — как у Upload. При любой ошибке остаётся прежний файл.
func (s *Service) Replace(ctx context.Context, id string, r io.Reader, name string) (*File, error) {
	old, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	format, key, n, err := s.put(ctx, id, r)
	if err != nil {
		return nil, err
	}
	if format != old.Format {
		s.deleteFile(key)
		return nil, fmt.Errorf("%w: %s instead of %s", ErrFormatMismatch, format, old.Format)
	}
	f, err := s.swap(ctx, id, key, cleanName(name), n)
	if err != nil {
		s.deleteFile(key)
		return nil, err
	}
	_ = s.emptyTrash(ctx) // остатки удалит следующий вызов
	return f, nil
}

// swap переключает строку файла на новый ключ, а прежний ключ отправляет в корзину — одной транзакцией.
func (s *Service) swap(ctx context.Context, id, key, name string, n int64) (*File, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("files: replace: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Сначала запись: транзакция сразу берёт блокировку на запись, без конфликта «чтение → запись».
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO files_trash (key) SELECT key FROM files_files WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("files: replace: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	res, err := tx.ExecContext(ctx, `UPDATE files_files SET key = ?, name = ?, bytes = ?, updated_at = ? WHERE id = ?`,
		key, name, n, now.Format(time.RFC3339), id)
	if err != nil {
		return nil, fmt.Errorf("files: replace: %w", err)
	}
	if rows, err := res.RowsAffected(); err != nil || rows == 0 {
		return nil, errors.Join(fmt.Errorf("%w: file %q", ErrNotFound, id), err)
	}
	f, err := s.get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("files: replace: %w", err)
	}
	return f, nil
}

// put определяет формат по началу r и пишет файл под новым ключом «<id>/<случайное>.<ext>», не больше
// s.maxBytes. Возвращает формат, ключ и размер; при ошибке ничего не остаётся.
func (s *Service) put(ctx context.Context, id string, r io.Reader) (Format, string, int64, error) {
	format, all, err := sniff(r)
	if err != nil {
		return "", "", 0, err
	}
	key := id + "/" + randomSuffix() + "." + format.Ext()
	lr := &limitReader{r: all, left: s.maxBytes}
	if err := s.st.Put(ctx, key, lr); err != nil {
		s.deleteFile(key)
		if lr.over {
			return "", "", 0, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, s.maxBytes)
		}
		return "", "", 0, err
	}
	return format, key, s.maxBytes - lr.left, nil
}

// errOverLimit прерывает запись в хранилище, когда файл превысил лимит.
var errOverLimit = errors.New("files: over the size limit")

// limitReader читает не больше left байт; попытка прочитать сверх лимита — ошибка (запись прерывается).
type limitReader struct {
	r    io.Reader
	left int64
	over bool
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		// Ровно на лимите: файл может и закончиться — проверяем одним байтом.
		var b [1]byte
		n, err := l.r.Read(b[:])
		if n > 0 {
			l.over = true
			return 0, errOverLimit
		}
		return 0, err
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}

// Get возвращает файл; если его нет — ошибку, оборачивающую ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (*File, error) {
	return s.get(ctx, s.db, id)
}

func (s *Service) get(ctx context.Context, q querier, id string) (*File, error) {
	files, err := s.query(ctx, q, `WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: file %q", ErrNotFound, id)
	}
	return files[0], nil
}

// List возвращает файлы от новых к старым, не больше limit начиная с offset.
func (s *Service) List(ctx context.Context, limit, offset int) ([]*File, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.query(ctx, s.db, `ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, max(offset, 0))
}

// query выбирает файлы условием tail (WHERE/ORDER/LIMIT по files_files).
func (s *Service) query(ctx context.Context, q querier, tail string, args ...any) ([]*File, error) {
	// #nosec G202 -- tail — строковая константа из Get/List, значения передаются параметрами
	rows, err := q.QueryContext(ctx, `SELECT id, name, url_name, format, bytes, created_at, updated_at
		FROM files_files `+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("files: query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*File
	for rows.Next() {
		var f File
		var format, created, updated string
		if err := rows.Scan(&f.ID, &f.Name, &f.URLName, &format, &f.Bytes, &created, &updated); err != nil {
			return nil, fmt.Errorf("files: query: %w", err)
		}
		f.Format = Format(format)
		f.Path = filePath(f.ID, f.URLName)
		if f.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, fmt.Errorf("files: query: created_at of %s: %w", f.ID, err)
		}
		if f.UpdatedAt, err = time.Parse(time.RFC3339, updated); err != nil {
			return nil, fmt.Errorf("files: query: updated_at of %s: %w", f.ID, err)
		}
		out = append(out, &f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("files: query: %w", err)
	}
	return out, nil
}

// Delete удаляет файл: метаданные — в транзакции, сам файл — после неё. Если файла нет — ошибка,
// оборачивающая ErrNotFound. Если на файл ссылается контент сайта (внешний ключ без ON DELETE),
// транзакция откатывается, файл остаётся на месте, а ошибка оборачивает ошибку драйвера.
func (s *Service) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("files: delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	keys, err := queryStrings(ctx, tx, `DELETE FROM files_files WHERE id = ? RETURNING key`, id)
	if err != nil {
		return fmt.Errorf("files: delete: %w", err)
	}
	if len(keys) == 0 {
		return fmt.Errorf("%w: file %q", ErrNotFound, id)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO files_trash (key) VALUES (?)`, keys[0]); err != nil {
		return fmt.Errorf("files: delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("files: delete: %w", err)
	}
	_ = s.emptyTrash(ctx) // остатки удалит следующий вызов
	return nil
}

// emptyTrash удаляет файлы из files_trash. Ключ, на который снова ссылается строка, из корзины убирается
// без удаления файла.
func (s *Service) emptyTrash(ctx context.Context) error {
	for {
		keys, err := queryStrings(ctx, s.db, `SELECT key FROM files_trash ORDER BY key LIMIT 100`)
		if err != nil {
			return fmt.Errorf("files: empty trash: %w", err)
		}
		if len(keys) == 0 {
			return nil
		}
		for _, k := range keys {
			var used bool
			if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM files_files WHERE key = ?)`, k).Scan(&used); err != nil {
				return fmt.Errorf("files: empty trash: %w", err)
			}
			if !used {
				if err := s.st.Delete(ctx, k); err != nil {
					return err
				}
			}
			if _, err := s.db.ExecContext(ctx, `DELETE FROM files_trash WHERE key = ?`, k); err != nil {
				return fmt.Errorf("files: empty trash: %w", err)
			}
		}
	}
}

// deleteFile убирает файл незавершённой загрузки; ошибка игнорируется — это уборка после другой ошибки.
func (s *Service) deleteFile(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = s.st.Delete(ctx, key)
}

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

// newID — 128 случайных бит в base32 строчными буквами (26 символов): адрес файла нельзя угадать.
func newID() string { return strings.ToLower(rand.Text()) }

// filePath — путь файла для адреса: «<id>/<имя>», имя экранировано.
func filePath(id, urlName string) string { return id + "/" + url.PathEscape(urlName) }

// urlName — имя файла в адресе: из исходного имени без расширения — буквы и цифры (в нижнем регистре),
// остальное — дефисами, не длиннее maxURLNameRune; расширение — по формату. Пустое — «file».
func urlName(name string, f Format) string {
	base := strings.TrimSuffix(name, path.Ext(name))
	var b strings.Builder
	dash, n := false, 0
	for _, r := range strings.ToLower(base) {
		if n >= maxURLNameRune {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
				n++
			}
			b.WriteRune(r)
			dash = false
			n++
			continue
		}
		dash = true
	}
	if b.Len() == 0 {
		b.WriteString("file")
	}
	return b.String() + "." + f.Ext()
}

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

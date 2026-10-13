// Команда site — эталонный сайт Keelkit.
//
// Подкоманды:
//
//	site serve    HTTP-сервер; не стартует при непримененных миграциях
//	site migrate  снимок БД и применение миграций (модули, затем сайт)
//	site seed     dev-данные, идемпотентно
//	site user create EMAIL  пользователь админки; пароль спрашивается интерактивно
//	site reprocess-images -below N  повторная обработка изображений с версией профиля ниже N
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	authmigrations "github.com/voffkin/keelkit/modules/auth/migrations"
	"github.com/voffkin/keelkit/modules/files"
	filesmigrations "github.com/voffkin/keelkit/modules/files/migrations"
	"github.com/voffkin/keelkit/modules/images"
	imagesmigrations "github.com/voffkin/keelkit/modules/images/migrations"

	"github.com/voffkin/keelkit/reference/db/migrations"
	"github.com/voffkin/keelkit/reference/internal/database"
)

// version вшивается при сборке: -ldflags "-X main.version=<тег или хеш>".
var version = "dev"

// migrationSources — миграции в порядке применения: модули в порядке из site.yaml, затем сайт.
var migrationSources = []database.Source{
	{Name: "images", FS: imagesmigrations.FS, Table: imagesmigrations.Table},
	{Name: "files", FS: filesmigrations.FS, Table: filesmigrations.Table},
	{Name: "auth", FS: authmigrations.FS, Table: authmigrations.Table},
	{Name: "site", FS: migrations.FS, Table: "goose_site_version"},
}

// config — настройки сайта из переменных окружения.
type config struct {
	Addr      string // ADDR: адрес HTTP-сервера
	DataDir   string // DATA_DIR: каталог с БД и загрузками
	BackupDir string // BACKUP_DIR: каталог снимков БД
	// COOKIE_SECURE: флаг Secure у cookie админки. По умолчанию true; false — только для локальной
	// разработки по http (make run и make dev выставляют его сами).
	CookieSecure bool
	// BASE_URL: адрес сайта (схема и хост, без слеша в конце) для canonical, Open Graph, sitemap, RSS и robots.txt.
	// Обязателен для serve: адрес из заголовка Host подделывается и попал бы в кэши поисковиков.
	BaseURL string

	// now — текущее время для входа и сессий (модуль auth); только для тестов, по умолчанию time.Now.
	now func() time.Time
}

func loadConfig() (config, error) {
	cookieSecure, err := strconv.ParseBool(envOr("COOKIE_SECURE", "true"))
	if err != nil {
		return config{}, fmt.Errorf("COOKIE_SECURE: %w", err)
	}
	baseURL := os.Getenv("BASE_URL")
	if baseURL != "" {
		if baseURL, err = parseBaseURL(baseURL); err != nil {
			return config{}, fmt.Errorf("BASE_URL: %w", err)
		}
	}
	return config{
		Addr:         envOr("ADDR", ":8080"),
		DataDir:      envOr("DATA_DIR", "data"),
		BackupDir:    envOr("BACKUP_DIR", "backups"),
		CookieSecure: cookieSecure,
		BaseURL:      baseURL,
	}, nil
}

// parseBaseURL проверяет BASE_URL: http или https, хост, без пути (кроме «/»), запроса и фрагмента.
// Возвращает адрес без слеша в конце: к нему приписываются пути сайта.
func parseBaseURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q: нужен адрес сайта вида https://example.com, без пути", s)
	}
	return u.Scheme + "://" + u.Host, nil
}

func (c config) dbPath() string { return filepath.Join(c.DataDir, "site.db") }

// uploadsDir — загрузки модуля images: оригиналы и варианты. Входит в бэкап вместе с БД.
func (c config) uploadsDir() string { return filepath.Join(c.DataDir, "uploads") }

// filesDir — файлы модуля files (документы, видео, SVG). Входит в бэкап вместе с БД.
func (c config) filesDir() string { return filepath.Join(c.DataDir, "files") }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

const usage = "usage: site <serve|migrate|seed> | site user create EMAIL | site reprocess-images -below N"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(2)
	}
	cmd := args[0]
	switch {
	case cmd == "serve" && len(args) == 1:
		err = serve(ctx, cfg)
	case cmd == "migrate" && len(args) == 1:
		err = migrate(ctx, cfg)
	case cmd == "seed" && len(args) == 1:
		err = seed(ctx, cfg)
	case cmd == "user" && len(args) == 3 && args[1] == "create":
		err = userCreate(ctx, cfg, args[2], readPasswordTerminal)
	case cmd == "reprocess-images":
		err = reprocessImages(ctx, cfg, args[1:])
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("command failed", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

// openDB создаёт каталог данных и открывает БД сайта.
func openDB(ctx context.Context, cfg config) (*sql.DB, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return database.Open(ctx, cfg.dbPath())
}

// openMedia открывает хранилище загрузок и сервис изображений над БД сайта. close закрывает хранилище.
func openMedia(db *sql.DB, cfg config) (svc *images.Service, closeFn func(), err error) {
	st, err := images.NewDiskStorage(cfg.uploadsDir())
	if err != nil {
		return nil, nil, fmt.Errorf("uploads: %w", err)
	}
	return images.NewService(db, st, images.Config{}), func() { _ = st.Close() }, nil
}

// filesConfig — настройки модуля files. Лимит по умолчанию — files.DefaultMaxBytes (50 МБ); сайт с видео
// поднимает его здесь (MaxBytes), тогда растёт и предел тела запроса в разделе «Файлы».
var filesConfig = files.Config{}

// openFiles открывает хранилище файлов и сервис файлов над БД сайта. close закрывает хранилище.
func openFiles(db *sql.DB, cfg config) (svc *files.Service, closeFn func(), err error) {
	st, err := files.NewDiskStorage(cfg.filesDir())
	if err != nil {
		return nil, nil, fmt.Errorf("files: %w", err)
	}
	return files.NewService(db, st, filesConfig), func() { _ = st.Close() }, nil
}

// errPendingMigrations — отказ старта: схема БД отстаёт от кода.
var errPendingMigrations = errors.New("есть непримененные миграции")

// errNoBaseURL — отказ старта: без адреса сайта не построить canonical, sitemap и RSS.
var errNoBaseURL = errors.New("BASE_URL не задан: укажите адрес сайта, например BASE_URL=https://example.com")

func serve(ctx context.Context, cfg config) error {
	if cfg.BaseURL == "" {
		return errNoBaseURL
	}
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	schemas, err := database.Status(ctx, db, migrationSources)
	if err != nil {
		return err
	}
	if n := database.Pending(schemas); n > 0 {
		return fmt.Errorf("%w (%d), выполните make migrate", errPendingMigrations, n)
	}

	media, closeMedia, err := openMedia(db, cfg)
	if err != nil {
		return err
	}
	defer closeMedia()
	fls, closeFiles, err := openFiles(db, cfg)
	if err != nil {
		return err
	}
	defer closeFiles()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newMux(db, media, fls, cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("site started", "addr", cfg.Addr, "base_url", cfg.BaseURL, "data_dir", cfg.DataDir, "version", version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func migrate(ctx context.Context, cfg config) error {
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	snapshot, err := database.Migrate(ctx, db, migrationSources, cfg.BackupDir)
	if err != nil {
		return err
	}
	if snapshot == "" {
		slog.Info("no pending migrations")
		return nil
	}
	schemas, err := database.Status(ctx, db, migrationSources)
	if err != nil {
		return err
	}
	for _, s := range schemas {
		slog.Info("schema migrated", "name", s.Name, "version", s.Version, "pending", s.Pending)
	}
	slog.Info("snapshot saved", "path", snapshot)
	return nil
}

// reprocessImages — site reprocess-images -below N: варианты изображений с версией профиля ниже N строятся заново
// текущим профилем. Идемпотентна и безопасна для прерывания: повторный запуск доделывает оставшееся.
// Сервер при этом может работать: старые адреса вариантов отдаются, пока строки в БД не переключены.
func reprocessImages(ctx context.Context, cfg config, args []string) error {
	fs := flag.NewFlagSet("reprocess-images", flag.ContinueOnError)
	below := fs.Int("below", 0, "обработать изображения с версией профиля ниже N (текущая — "+strconv.Itoa(images.ProfileVersion)+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *below <= 0 || fs.NArg() > 0 {
		return errors.New("укажите -below N (N > 0), например site reprocess-images -below " + strconv.Itoa(images.ProfileVersion+1))
	}

	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	schemas, err := database.Status(ctx, db, migrationSources)
	if err != nil {
		return err
	}
	if n := database.Pending(schemas); n > 0 {
		return fmt.Errorf("%w (%d), выполните make migrate", errPendingMigrations, n)
	}
	media, closeMedia, err := openMedia(db, cfg)
	if err != nil {
		return err
	}
	defer closeMedia()

	st, err := media.Reprocess(ctx, *below)
	slog.Info("reprocess images", "below", *below, "profile", images.ProfileVersion, "processed", st.Processed, "failed", st.Failed)
	return err
}

// Команда site — эталонный сайт Keelkit.
//
// Подкоманды:
//
//	site serve    HTTP-сервер; не стартует при непримененных миграциях
//	site migrate  снимок БД и применение миграций (модули, затем сайт)
//	site seed     dev-данные, идемпотентно
//	site user create EMAIL  пользователь админки; пароль спрашивается интерактивно
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/keelkit/keelkit/reference/db/migrations"
	"github.com/keelkit/keelkit/reference/internal/database"
)

// version вшивается при сборке: -ldflags "-X main.version=<тег или хеш>".
var version = "dev"

// migrationSources — миграции в порядке применения: модули в порядке из site.yaml, затем сайт.
var migrationSources = []database.Source{
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
}

func loadConfig() (config, error) {
	cookieSecure, err := strconv.ParseBool(envOr("COOKIE_SECURE", "true"))
	if err != nil {
		return config{}, fmt.Errorf("COOKIE_SECURE: %w", err)
	}
	return config{
		Addr:         envOr("ADDR", ":8080"),
		DataDir:      envOr("DATA_DIR", "data"),
		BackupDir:    envOr("BACKUP_DIR", "backups"),
		CookieSecure: cookieSecure,
	}, nil
}

func (c config) dbPath() string { return filepath.Join(c.DataDir, "site.db") }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

const usage = "usage: site <serve|migrate|seed> | site user create EMAIL"

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

// errPendingMigrations — отказ старта: схема БД отстаёт от кода.
var errPendingMigrations = errors.New("есть непримененные миграции")

func serve(ctx context.Context, cfg config) error {
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

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newMux(db, cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("site started", "addr", cfg.Addr, "data_dir", cfg.DataDir, "version", version)
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

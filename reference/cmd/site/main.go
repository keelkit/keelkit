// Команда site — эталонный сайт Keelkit.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// version вшивается при сборке: -ldflags "-X main.version=<тег или хеш>".
var version = "dev"

// config — настройки сайта из переменных окружения.
type config struct {
	Addr    string // ADDR: адрес HTTP-сервера
	DataDir string // DATA_DIR: каталог с БД и загрузками
}

func loadConfig() config {
	return config{
		Addr:    envOr("ADDR", ":8080"),
		DataDir: envOr("DATA_DIR", "data"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(loadConfig()); err != nil {
		slog.Error("site stopped", "err", err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleHome)
	mux.HandleFunc("GET /healthz", handleHealthz)
	return mux
}

func handleHome(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<!doctype html>
<html lang="ru">
<head><meta charset="utf-8"><title>Keelkit reference</title></head>
<body><h1>Keelkit reference</h1></body>
</html>
`)
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: version})
}

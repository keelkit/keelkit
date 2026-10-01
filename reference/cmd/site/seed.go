package main

import (
	"context"
	"log/slog"

	"github.com/keelkit/keelkit/reference/internal/settings"
)

// seed заполняет БД данными для разработки. Идемпотентен: существующие записи не трогает.
func seed(ctx context.Context, cfg config) error {
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	err = settings.New(db).InsertSiteSettingsIfMissing(ctx, settings.InsertSiteSettingsIfMissingParams{
		Name:        "Keelkit — эталонный сайт",
		Description: "Образец устройства сайтов Keelkit",
	})
	if err != nil {
		return err
	}
	slog.Info("seed done")
	return nil
}

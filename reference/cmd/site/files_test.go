package main

import (
	"testing"

	"github.com/keelkit/keelkit/modules/files/testkit"

	"github.com/keelkit/keelkit/reference/internal/web"
)

// TestFilesContract — контрактные тесты модуля files на интеграции сайта: миграции модуля и сайта,
// хранилище в DATA_DIR/files, обработчик модуля под web.FilesPrefix в маршрутах newMux.
func TestFilesContract(t *testing.T) {
	testkit.Run(t, func(t *testing.T) testkit.Env {
		s := migratedSite(t, false)
		return testkit.Env{
			Service: s.files,
			Handler: s.handler(config{}),
			URL:     func(p string) string { return web.FilesPrefix + p },
		}
	})
}

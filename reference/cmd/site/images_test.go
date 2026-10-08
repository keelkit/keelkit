package main

import (
	"testing"

	"github.com/voffkin/keelkit/modules/images/testkit"

	"github.com/voffkin/keelkit/reference/internal/web"
)

// TestImagesContract — контрактные тесты модуля images на интеграции сайта: миграции модуля и сайта,
// хранилище в DATA_DIR/uploads, обработчик модуля под web.MediaPrefix в маршрутах newMux.
func TestImagesContract(t *testing.T) {
	testkit.Run(t, func(t *testing.T) testkit.Env {
		s := migratedSite(t, false)
		return testkit.Env{
			Service: s.media,
			Handler: s.handler(config{}),
			URL:     func(p string) string { return web.MediaPrefix + p },
		}
	})
}

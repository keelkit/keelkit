package main

import (
	"testing"
	"time"

	"github.com/keelkit/keelkit/modules/auth"
	"github.com/keelkit/keelkit/modules/auth/testkit"
)

// TestAuthContract — контрактные тесты модуля auth на интеграции сайта: настоящие страницы входа и админки
// из newMux (internal/admin), cookie с флагом Secure (как в проде), время сайта задаёт testkit.
func TestAuthContract(t *testing.T) {
	testkit.Run(t, func(t *testing.T, now func() time.Time) testkit.Site {
		s := migratedSite(t, false)
		if _, err := auth.NewService(s.db, auth.Config{}).CreateUser(t.Context(), seedAdminEmail, seedAdminPassword); err != nil {
			t.Fatal(err)
		}
		return testkit.Site{
			Handler:       s.handler(config{CookieSecure: true, now: now}),
			LoginPath:     "/admin/login",
			ProtectedPath: "/admin",
			LogoutPath:    "/admin/logout",
			Email:         seedAdminEmail,
			Password:      seedAdminPassword,
		}
	})
}

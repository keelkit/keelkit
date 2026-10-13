package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voffkin/keelkit/modules/richtext/testkit"

	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/content/pages"
)

// TestRichtextContract — контрактные тесты модуля richtext на выводе сайта: Markdown попадает в текстовый блок
// опубликованной страницы, testkit проверяет HTML, который отдаёт сервер.
func TestRichtextContract(t *testing.T) {
	s := migratedSite(t, false)
	srv := httptest.NewServer(s.handler(config{}))
	t.Cleanup(srv.Close)
	n := 0
	testkit.Run(t, func(t *testing.T, markdown string) string {
		n++
		slug := fmt.Sprintf("richtext-%d", n)
		if err := pages.SeedPublished(t.Context(), s.db, "Richtext", slug, []blocks.Block{textBlock("body", markdown)}); err != nil {
			t.Fatal(err)
		}
		resp, body := fetch(t, srv.Client(), srv.URL+"/"+slug)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /%s: status %d", slug, resp.StatusCode)
		}
		return body
	})
}

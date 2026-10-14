// Package testkit — контрактные тесты модуля images для сайтов.
//
// Сайт запускает их в своих тестах на своей интеграции: своя БД с миграциями (модуля и сайта),
// своё хранилище и свой HTTP-обработчик, под которым смонтирован Service.Handler. Тесты проверяют:
// загрузка → варианты доступны через обработчик сайта, оригинал не отдаётся, повторная обработка
// ничего не ломает, удаление убирает файлы, ошибки проверки распознаются через errors.Is.
//
//	func TestImagesContract(t *testing.T) {
//		testkit.Run(t, func(t *testing.T) testkit.Env {
//			db := newTestDB(t) // миграции модуля и сайта применены
//			st, _ := images.NewDiskStorage(t.TempDir())
//			svc := images.NewService(db, st, images.Config{})
//			return testkit.Env{Service: svc, Handler: newServer(db, svc), URL: func(p string) string { return "/media/" + p }}
//		})
//	}
package testkit

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	xwebp "golang.org/x/image/webp"

	"github.com/keelkit/keelkit/modules/images"
)

// Env — интеграция сайта, на которой идут контрактные тесты. Setup вызывается для каждого теста заново;
// БД и хранилище должны быть пустыми (без seed).
type Env struct {
	Service *images.Service
	// Handler — HTTP-обработчик сайта (весь сервер или его часть), под которым смонтирован Service.Handler.
	Handler http.Handler
	// URL — адрес для пути относительно корня Service.Handler, например func(p string) string { return "/media/" + p }.
	URL func(path string) string
}

// Run запускает контрактные тесты модуля как подтесты t.
func Run(t *testing.T, setup func(t *testing.T) Env) {
	t.Helper()
	t.Run("UploadServesVariants", func(t *testing.T) { testUpload(t, setup(t)) })
	t.Run("OriginalNotServed", func(t *testing.T) { testOriginalNotServed(t, setup(t)) })
	t.Run("Transparency", func(t *testing.T) { testTransparency(t, setup(t)) })
	t.Run("Rejected", func(t *testing.T) { testRejected(t, setup(t)) })
	t.Run("ReprocessKeepsVariants", func(t *testing.T) { testReprocess(t, setup(t)) })
	t.Run("Delete", func(t *testing.T) { testDelete(t, setup(t)) })
}

func testUpload(t *testing.T, env Env) {
	ctx := t.Context()
	img := upload(t, env, encodeJPEG(t, 1000, 600), "photo.jpg")
	if img.Format != images.JPEG || img.Width != 1000 || img.Height != 600 || img.Name != "photo.jpg" {
		t.Errorf("Upload = %s %d×%d %q, want jpeg 1000×600 \"photo.jpg\"", img.Format, img.Width, img.Height, img.Name)
	}
	if img.ProfileVersion != images.ProfileVersion {
		t.Errorf("ProfileVersion = %d, want %d", img.ProfileVersion, images.ProfileVersion)
	}
	// Профиль v1: 480, 960 и собственная ширина 1000 (меньше 2400), каждая в WebP и JPEG.
	var widths []int
	for _, v := range img.Variants {
		if v.Format == images.WebP {
			widths = append(widths, v.Width)
		}
	}
	if !slices.Equal(widths, []int{480, 960, 1000}) || len(img.Variants) != 6 {
		t.Errorf("variants: WebP widths %v of %d variants, want [480 960 1000] of 6", widths, len(img.Variants))
	}
	checkServed(t, env, img)

	got, err := env.Service.Get(ctx, img.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != img.ID || len(got.Variants) != len(img.Variants) || got.Variants[0] != img.Variants[0] {
		t.Errorf("Get = %+v, want %+v", got, img)
	}
	list, err := env.Service.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != img.ID {
		t.Errorf("List: %d images, want only %s", len(list), img.ID)
	}
}

func testOriginalNotServed(t *testing.T, env Env) {
	img := upload(t, env, encodeJPEG(t, 600, 400), "secret.jpg")
	for _, p := range []string{
		img.ID + "/original.jpg",
		img.ID + "/v1/../original.jpg",
		img.ID + "/v1/%2e%2e/original.jpg",
		img.ID + "/",
		img.ID,
	} {
		if code, _, _ := get(t, env, http.MethodGet, env.URL(p)); code == http.StatusOK {
			t.Errorf("GET %s = 200, want the original never served", env.URL(p))
		}
	}
}

func testTransparency(t *testing.T, env Env) {
	src := image.NewNRGBA(image.Rect(0, 0, 500, 300))
	for y := range 300 {
		for x := range 500 {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: uint8(x % 256)}) // #nosec G115 -- x % 256 < 256
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	img := upload(t, env, buf.Bytes(), "alpha.png")
	if img.Format != images.PNG {
		t.Errorf("Format = %s, want png", img.Format)
	}
	checkServed(t, env, img)
}

func testRejected(t *testing.T, env Env) {
	ctx := t.Context()
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), images.ErrUnsupportedFormat},
		{"text", []byte("hello"), images.ErrUnsupportedFormat},
		{"broken jpeg", []byte("\xFF\xD8\xFF\xE0garbage"), images.ErrInvalidImage},
		{"too large", bytes.Repeat([]byte{0}, images.MaxBytes+1), images.ErrTooLarge},
	}
	for _, c := range cases {
		img, err := env.Service.Upload(ctx, bytes.NewReader(c.data), c.name)
		if !errors.Is(err, c.want) {
			t.Errorf("Upload(%s) = %v, %v; want error %v", c.name, img, err, c.want)
		}
	}
	list, err := env.Service.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("List after rejected uploads: %d images, want 0", len(list))
	}
}

func testReprocess(t *testing.T, env Env) {
	ctx := t.Context()
	img := upload(t, env, encodeJPEG(t, 800, 800), "square.jpg")
	for range 2 {
		st, err := env.Service.Reprocess(ctx, images.ProfileVersion+1)
		if err != nil || st.Failed != 0 {
			t.Fatalf("Reprocess = %+v, %v; want no failures", st, err)
		}
		if st.Processed != 0 {
			t.Errorf("Reprocess processed %d images already at ProfileVersion, want 0", st.Processed)
		}
	}
	got, err := env.Service.Get(ctx, img.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(got.Variants, img.Variants) {
		t.Errorf("variants after Reprocess: %+v, want %+v", got.Variants, img.Variants)
	}
	checkServed(t, env, got)
}

func testDelete(t *testing.T, env Env) {
	ctx := t.Context()
	img := upload(t, env, encodeJPEG(t, 700, 500), "gone.jpg")
	if err := env.Service.Delete(ctx, img.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := env.Service.Get(ctx, img.ID); !errors.Is(err, images.ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}
	for _, v := range img.Variants {
		if code, _, _ := get(t, env, http.MethodGet, env.URL(v.Path)); code != http.StatusNotFound {
			t.Errorf("GET %s after Delete = %d, want 404", v.Path, code)
		}
	}
	if err := env.Service.Delete(ctx, img.ID); !errors.Is(err, images.ErrNotFound) {
		t.Errorf("second Delete: %v, want ErrNotFound", err)
	}
}

// checkServed проверяет, что каждый вариант отдаётся обработчиком сайта: тип, кэш, размеры.
func checkServed(t *testing.T, env Env, img *images.Image) {
	t.Helper()
	for _, v := range img.Variants {
		url := env.URL(v.Path)
		code, h, body := get(t, env, http.MethodGet, url)
		if code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", url, code)
			continue
		}
		if ct := h.Get("Content-Type"); ct != v.Format.ContentType() {
			t.Errorf("GET %s: Content-Type %q, want %q", url, ct, v.Format.ContentType())
		}
		if cc := h.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Errorf("GET %s: Cache-Control %q, want immutable", url, cc)
		}
		if int64(len(body)) != v.Bytes {
			t.Errorf("GET %s: %d bytes, want %d", url, len(body), v.Bytes)
		}
		cfg, err := decodeConfig(v.Format, body)
		if err != nil {
			t.Errorf("GET %s: decode: %v", url, err)
		} else if cfg.Width != v.Width || cfg.Height != v.Height {
			t.Errorf("GET %s: %d×%d, want %d×%d", url, cfg.Width, cfg.Height, v.Width, v.Height)
		}
		if code, _, body := get(t, env, http.MethodHead, url); code != http.StatusOK || len(body) != 0 {
			t.Errorf("HEAD %s = %d with %d bytes, want 200 without body", url, code, len(body))
		}
	}
}

func upload(t *testing.T, env Env, data []byte, name string) *images.Image {
	t.Helper()
	img, err := env.Service.Upload(t.Context(), bytes.NewReader(data), name)
	if err != nil {
		t.Fatalf("Upload(%s): %v", name, err)
	}
	return img
}

func get(t *testing.T, env Env, method, url string) (int, http.Header, []byte) {
	t.Helper()
	req := httptest.NewRequestWithContext(context.WithoutCancel(t.Context()), method, url, nil)
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)
	return rec.Code, rec.Header(), rec.Body.Bytes()
}

func decodeConfig(f images.Format, data []byte) (image.Config, error) {
	if f == images.WebP {
		return xwebp.DecodeConfig(bytes.NewReader(data))
	}
	return jpeg.DecodeConfig(bytes.NewReader(data))
}

// encodeJPEG — фото-подобный JPEG w×h с градиентом.
func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 128, A: 255}) // #nosec G115 -- < 256
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

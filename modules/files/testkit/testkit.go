// Package testkit — контрактные тесты модуля files для сайтов.
//
// Сайт запускает их в своих тестах на своей интеграции: своя БД с миграциями (модуля и сайта),
// своё хранилище и свой HTTP-обработчик, под которым смонтирован Service.Handler. Тесты проверяют:
// загрузка → файл отдаётся обработчиком сайта с верным типом, видео — частями (Range), SVG — в песочнице (CSP),
// замена не меняет адрес, другое имя в адресе не отдаётся, ошибки проверки распознаются через errors.Is,
// удаление убирает файл.
//
//	func TestFilesContract(t *testing.T) {
//		testkit.Run(t, func(t *testing.T) testkit.Env {
//			db := newTestDB(t) // миграции модуля и сайта применены
//			st, _ := files.NewDiskStorage(t.TempDir())
//			svc := files.NewService(db, st, files.Config{})
//			return testkit.Env{Service: svc, Handler: newServer(db, svc), URL: func(p string) string { return "/files/" + p }}
//		})
//	}
//
// Sample — файлы для тестов и dev-данных сайта (seed).
package testkit

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/keelkit/keelkit/modules/files"
)

// Env — интеграция сайта, на которой идут контрактные тесты. Setup вызывается для каждого теста заново;
// БД и хранилище должны быть пустыми (без seed).
type Env struct {
	Service *files.Service
	// Handler — HTTP-обработчик сайта (весь сервер или его часть), под которым смонтирован Service.Handler.
	Handler http.Handler
	// URL — адрес для пути относительно корня Service.Handler, например func(p string) string { return "/files/" + p }.
	URL func(path string) string
}

// Run запускает контрактные тесты модуля как подтесты t.
func Run(t *testing.T, setup func(t *testing.T) Env) {
	t.Helper()
	t.Run("UploadServes", func(t *testing.T) { testUpload(t, setup(t)) })
	t.Run("VideoRange", func(t *testing.T) { testRange(t, setup(t)) })
	t.Run("SVGSandboxed", func(t *testing.T) { testSVG(t, setup(t)) })
	t.Run("ReplaceKeepsAddress", func(t *testing.T) { testReplace(t, setup(t)) })
	t.Run("OnlyExactPathServed", func(t *testing.T) { testExactPath(t, setup(t)) })
	t.Run("Rejected", func(t *testing.T) { testRejected(t, setup(t)) })
	t.Run("Delete", func(t *testing.T) { testDelete(t, setup(t)) })
}

func testUpload(t *testing.T, env Env) {
	ctx := t.Context()
	for _, c := range []struct {
		format files.Format
		name   string
		url    string // ожидаемое имя в адресе
	}{
		{files.PDF, "Прайс 2026.pdf", "прайс-2026.pdf"},
		{files.MP4, "clip.MP4", "clip.mp4"},
		{files.WebM, "clip.webm", "clip.webm"},
		{files.SVG, "logo.svg", "logo.svg"},
	} {
		data := Sample(c.format, 4096)
		f := upload(t, env, data, c.name)
		if f.Format != c.format || f.Name != c.name || f.URLName != c.url || f.Bytes != int64(len(data)) {
			t.Errorf("Upload(%s) = %s %q %q %d bytes; want %s %q %q %d", c.name, f.Format, f.Name, f.URLName, f.Bytes,
				c.format, c.name, c.url, len(data))
		}
		if want := f.ID + "/"; !strings.HasPrefix(f.Path, want) {
			t.Errorf("Path = %q, want prefix %q", f.Path, want)
		}
		code, h, body := get(t, env, http.MethodGet, env.URL(f.Path), nil)
		if code != http.StatusOK || !bytes.Equal(body, data) {
			t.Errorf("GET %s = %d, %d bytes; want 200 and the uploaded %d bytes", f.Path, code, len(body), len(data))
		}
		if ct := h.Get("Content-Type"); ct != c.format.ContentType() {
			t.Errorf("GET %s: Content-Type %q, want %q", f.Path, ct, c.format.ContentType())
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Cache-Control"), "no-cache") || h.Get("ETag") == "" {
			t.Errorf("GET %s: nosniff %q, Cache-Control %q, ETag %q; want nosniff, no-cache, an ETag", f.Path,
				h.Get("X-Content-Type-Options"), h.Get("Cache-Control"), h.Get("ETag"))
		}
		if code, _, body := get(t, env, http.MethodHead, env.URL(f.Path), nil); code != http.StatusOK || len(body) != 0 {
			t.Errorf("HEAD %s = %d with %d bytes, want 200 without body", f.Path, code, len(body))
		}
		got, err := env.Service.Get(ctx, f.ID)
		if err != nil || *got != *f {
			t.Errorf("Get = %+v, %v; want %+v", got, err, f)
		}
	}
	list, err := env.Service.List(ctx, 10, 0)
	if err != nil || len(list) != 4 {
		t.Errorf("List = %d files, %v; want 4", len(list), err)
	}
}

func testRange(t *testing.T, env Env) {
	data := Sample(files.MP4, 1<<20)
	f := upload(t, env, data, "video.mp4")
	url := env.URL(f.Path)
	code, h, body := get(t, env, http.MethodGet, url, http.Header{"Range": {"bytes=1000-1999"}})
	if code != http.StatusPartialContent || !bytes.Equal(body, data[1000:2000]) {
		t.Errorf("GET %s Range 1000-1999 = %d, %d bytes; want 206 and the bytes 1000-1999", url, code, len(body))
	}
	if cr, want := h.Get("Content-Range"), fmt.Sprintf("bytes 1000-1999/%d", len(data)); cr != want {
		t.Errorf("Content-Range = %q, want %q", cr, want)
	}
	if h.Get("Accept-Ranges") != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", h.Get("Accept-Ranges"))
	}
	// If-Range: та же версия — часть, другая — файл целиком.
	etag := h.Get("ETag")
	if code, _, _ := get(t, env, http.MethodGet, url, http.Header{"Range": {"bytes=0-9"}, "If-Range": {etag}}); code != http.StatusPartialContent {
		t.Errorf("Range with current If-Range = %d, want 206", code)
	}
	if code, _, body := get(t, env, http.MethodGet, url, http.Header{"Range": {"bytes=0-9"}, "If-Range": {`"stale"`}}); code != http.StatusOK || len(body) != len(data) {
		t.Errorf("Range with stale If-Range = %d, %d bytes; want 200 with the whole file", code, len(body))
	}
}

func testSVG(t *testing.T, env Env) {
	f := upload(t, env, Sample(files.SVG, 0), "icon.svg")
	_, h, _ := get(t, env, http.MethodGet, env.URL(f.Path), nil)
	if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Errorf("SVG: Content-Security-Policy %q, want sandbox (scripts in the file must not run)", csp)
	}
	pdf := upload(t, env, Sample(files.PDF, 0), "doc.pdf")
	if _, h, _ := get(t, env, http.MethodGet, env.URL(pdf.Path), nil); h.Get("Content-Security-Policy") != "" {
		t.Errorf("PDF: Content-Security-Policy %q, want none (sandbox blocks the browser's PDF viewer)", h.Get("Content-Security-Policy"))
	}
}

func testReplace(t *testing.T, env Env) {
	ctx := t.Context()
	f := upload(t, env, Sample(files.PDF, 1000), "price.pdf")
	url := env.URL(f.Path)
	_, h, _ := get(t, env, http.MethodGet, url, nil)
	oldTag := h.Get("ETag")

	data := Sample(files.PDF, 3000)
	got, err := env.Service.Replace(ctx, f.ID, bytes.NewReader(data), "price-2027.pdf")
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if got.Path != f.Path || got.Name != "price-2027.pdf" || got.Bytes != int64(len(data)) {
		t.Errorf("Replace = %q %q %d bytes; want the same path %q, the new name and size", got.Path, got.Name, got.Bytes, f.Path)
	}
	code, h, body := get(t, env, http.MethodGet, url, http.Header{"If-None-Match": {oldTag}})
	if code != http.StatusOK || !bytes.Equal(body, data) || h.Get("ETag") == oldTag {
		t.Errorf("GET %s after Replace with the old ETag = %d, %d bytes, ETag %q; want 200, the new content, a new ETag",
			url, code, len(body), h.Get("ETag"))
	}
	if code, _, _ := get(t, env, http.MethodGet, url, http.Header{"If-None-Match": {h.Get("ETag")}}); code != http.StatusNotModified {
		t.Errorf("GET with the current ETag = %d, want 304", code)
	}

	// Другой формат — ошибка, прежний файл на месте.
	if _, err := env.Service.Replace(ctx, f.ID, bytes.NewReader(Sample(files.SVG, 0)), "x.svg"); !errors.Is(err, files.ErrFormatMismatch) {
		t.Errorf("Replace with SVG: %v, want ErrFormatMismatch", err)
	}
	if _, err := env.Service.Replace(ctx, f.ID, strings.NewReader("<html></html>"), "x.html"); !errors.Is(err, files.ErrUnsupportedFormat) {
		t.Errorf("Replace with HTML: %v, want ErrUnsupportedFormat", err)
	}
	if _, _, body := get(t, env, http.MethodGet, url, nil); !bytes.Equal(body, data) {
		t.Errorf("GET after a failed Replace: %d bytes, want the previous content", len(body))
	}
	if _, err := env.Service.Replace(ctx, "nosuchfile", bytes.NewReader(data), "x.pdf"); !errors.Is(err, files.ErrNotFound) {
		t.Errorf("Replace of a missing file: %v, want ErrNotFound", err)
	}
}

func testExactPath(t *testing.T, env Env) {
	f := upload(t, env, Sample(files.PDF, 0), "report.pdf")
	for _, p := range []string{
		f.ID + "/other.pdf",
		f.ID + "/",
		f.ID,
		f.ID + "/report.pdf/x",
		f.ID + "/../" + f.ID + "/report.pdf/..",
		strings.ToUpper(f.ID) + "/report.pdf",
	} {
		if code, _, _ := get(t, env, http.MethodGet, env.URL(p), nil); code == http.StatusOK {
			t.Errorf("GET %s = 200, want only %s served", env.URL(p), f.Path)
		}
	}
}

func testRejected(t *testing.T, env Env) {
	ctx := t.Context()
	cases := []struct {
		name string
		r    io.Reader
		want error
	}{
		{"empty", strings.NewReader(""), files.ErrInvalidFile},
		{"page.html", strings.NewReader("<!DOCTYPE html><html><script>alert(1)</script></html>"), files.ErrUnsupportedFormat},
		{"feed.xml", strings.NewReader(`<?xml version="1.0"?><rss version="2.0"></rss>`), files.ErrUnsupportedFormat},
		{"app.exe", strings.NewReader("MZ\x90\x00\x03"), files.ErrUnsupportedFormat},
		{"photo.gif", strings.NewReader("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), files.ErrUnsupportedFormat},
		{"text.txt", strings.NewReader("hello"), files.ErrUnsupportedFormat},
		{"movie.mov", bytes.NewReader(ftyp("qt  ", 100)), files.ErrUnsupportedFormat},
		{"photo.heic", bytes.NewReader(ftyp("heic", 100)), files.ErrUnsupportedFormat},
		{"big.pdf", io.MultiReader(strings.NewReader("%PDF-1.4\n"), io.LimitReader(zeros{}, env.Service.MaxBytes())), files.ErrTooLarge},
	}
	for _, c := range cases {
		f, err := env.Service.Upload(ctx, c.r, c.name)
		if !errors.Is(err, c.want) {
			t.Errorf("Upload(%s) = %v, %v; want error %v", c.name, f, err, c.want)
		}
	}
	// Ровно на лимите — можно.
	exact := io.MultiReader(strings.NewReader("%PDF-1.4\n"), io.LimitReader(zeros{}, env.Service.MaxBytes()-9))
	f, err := env.Service.Upload(ctx, exact, "limit.pdf")
	if err != nil || f.Bytes != env.Service.MaxBytes() {
		t.Fatalf("Upload of exactly MaxBytes = %v, %v; want it accepted", f, err)
	}
	list, err := env.Service.List(ctx, 10, 0)
	if err != nil || len(list) != 1 {
		t.Errorf("List after rejected uploads: %d files, %v; want only the one at the limit", len(list), err)
	}
}

func testDelete(t *testing.T, env Env) {
	ctx := t.Context()
	f := upload(t, env, Sample(files.WebM, 0), "gone.webm")
	if err := env.Service.Delete(ctx, f.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := env.Service.Get(ctx, f.ID); !errors.Is(err, files.ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}
	if code, _, _ := get(t, env, http.MethodGet, env.URL(f.Path), nil); code != http.StatusNotFound {
		t.Errorf("GET %s after Delete = %d, want 404", f.Path, code)
	}
	if err := env.Service.Delete(ctx, f.ID); !errors.Is(err, files.ErrNotFound) {
		t.Errorf("second Delete: %v, want ErrNotFound", err)
	}
}

// Sample — правдоподобный по заголовку файл формата f не меньше size байт: PDF с комментарием-заполнителем,
// MP4 (ftyp + mdat), WebM (заголовок EBML + Segment), SVG со скриптом внутри. Заполнитель — повторяющийся
// узор, поэтому части файла различимы (проверка Range). Для тестов и dev-данных, а не для показа людям.
func Sample(f files.Format, size int) []byte {
	var b []byte
	switch f {
	case files.PDF:
		b = []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\n%")
	case files.MP4:
		b = ftyp("isom", 0)
		mdat := max(size-len(b), 8)
		b = binary.BigEndian.AppendUint32(b, uint32(mdat)) // #nosec G115 -- размер тестового файла
		b = append(b, "mdat"...)
	case files.WebM:
		b = []byte{0x1A, 0x45, 0xDF, 0xA3, 0x93, 0x42, 0x82, 0x84, 'w', 'e', 'b', 'm', 0x42, 0x87, 0x81, 0x04, 0x42, 0x85, 0x81, 0x02,
			0x42, 0x86, 0x81, 0x01, 0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	case files.SVG:
		b = []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
			`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><script>alert(document.domain)</script>` +
			`<rect width="16" height="16" fill="#c33"/></svg>` + "\n<!--")
	default:
		panic("testkit: unknown format " + string(f))
	}
	for i := 0; len(b) < size; i++ {
		b = strconv.AppendInt(b, int64(i%1000), 10)
		b = append(b, ' ')
	}
	switch f {
	case files.PDF:
		b = append(b, "\n%%EOF\n"...)
	case files.SVG:
		b = append(b, "-->\n"...)
	case files.MP4:
		binary.BigEndian.PutUint32(b[24:], uint32(len(b)-24)) // #nosec G115 -- размер тестового файла
	}
	return b
}

// ftyp — бокс ftyp ISO BMFF с основным брендом brand и n байт заполнителя после него.
func ftyp(brand string, n int) []byte {
	b := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p'}
	b = append(b, brand...)
	b = append(b, 0, 0, 2, 0)
	b = append(b, "isomiso2"...)
	return append(b, make([]byte, n)...)
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func upload(t *testing.T, env Env, data []byte, name string) *files.File {
	t.Helper()
	f, err := env.Service.Upload(t.Context(), bytes.NewReader(data), name)
	if err != nil {
		t.Fatalf("Upload(%s): %v", name, err)
	}
	return f
}

func get(t *testing.T, env Env, method, url string, h http.Header) (int, http.Header, []byte) {
	t.Helper()
	req := httptest.NewRequestWithContext(context.WithoutCancel(t.Context()), method, url, nil)
	for k, v := range h {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	env.Handler.ServeHTTP(rec, req)
	return rec.Code, rec.Header(), rec.Body.Bytes()
}

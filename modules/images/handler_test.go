package images

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func serveReq(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandler(t *testing.T) {
	e := newEnv(t)
	s := e.service(ProfileVersion, nil)
	img := uploadFile(t, s, "small.jpg")
	h := http.StripPrefix("/media/", s.Handler())
	webp, jpg := img.Variants[0], img.Variants[1]

	rec := serveReq(h, httptest.NewRequest(http.MethodGet, "/media/"+webp.Path, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/webp" ||
		rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" || int64(rec.Body.Len()) != webp.Bytes {
		t.Errorf("GET webp: %d %v, %d bytes", rec.Code, rec.Header(), rec.Body.Len())
	}
	rec = serveReq(h, httptest.NewRequest(http.MethodGet, "/media/"+jpg.Path, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("GET jpg: %d %v", rec.Code, rec.Header())
	}

	req := httptest.NewRequest(http.MethodGet, "/media/"+webp.Path, nil)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	if rec := serveReq(h, req); rec.Code != http.StatusOK {
		t.Errorf("If-None-Match with other variant's ETag: %d, want 200", rec.Code)
	}
	req.Header.Set("If-None-Match", `"`+webp.Path+`"`)
	if rec := serveReq(h, req); rec.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: %d, want 304", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/media/"+webp.Path, nil)
	req.Header.Set("Range", "bytes=0-9")
	if rec := serveReq(h, req); rec.Code != http.StatusPartialContent || rec.Body.Len() != 10 {
		t.Errorf("Range: %d with %d bytes, want 206 with 10", rec.Code, rec.Body.Len())
	}

	if rec := serveReq(h, httptest.NewRequest(http.MethodPost, "/media/"+webp.Path, nil)); rec.Code != http.StatusMethodNotAllowed ||
		rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: %d, Allow %q", rec.Code, rec.Header().Get("Allow"))
	}

	for _, p := range []string{
		img.ID + "/original.jpg", // оригинал с EXIF не отдаётся никогда
		img.ID + "/v1/original.jpg",
		img.ID + "/v1/480.png",
		img.ID + "/v1/480.webp.tmp-0123",
		img.ID + "/v1/9999.webp",
		"nope/v1/480.webp",
		img.ID,
		"",
	} {
		if rec := serveReq(h, httptest.NewRequest(http.MethodGet, "/media/"+p, nil)); rec.Code != http.StatusNotFound {
			t.Errorf("GET /media/%s = %d, want 404", p, rec.Code)
		}
	}
	// Ключ оригинала проходит в хранилище, но handler его не спрашивает.
	if _, err := e.st.Get(t.Context(), img.ID+"/original.jpg"); err != nil {
		t.Fatalf("original must be stored: %v", err)
	}
}

package images

import (
	"errors"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// variantPath — путь варианта: «<id>/v<профиль>/<ширина>.<webp|jpg>». Оригинал («<id>/original.<ext>»)
// под него не подходит и поэтому не отдаётся никогда: в нём EXIF, в том числе GPS.
var variantPath = regexp.MustCompile(`^[a-z0-9]{1,64}/v[0-9]{1,9}/[0-9]{1,5}\.(webp|jpg)$`)

// Handler отдаёт варианты изображений по путям StoredVariant.Path. Сайт монтирует его под своим префиксом:
//
//	mux.Handle("/media/", http.StripPrefix("/media/", svc.Handler()))
//
// Отдаются только варианты, оригиналы — никогда. Адрес варианта неизменен (версия профиля — в пути),
// поэтому кэш бессрочный: Cache-Control: public, max-age=31536000, immutable. Поддерживаются GET и HEAD,
// Range и If-None-Match. Метаданные из БД не читаются: файл существует, пока существует вариант.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/")
	if !variantPath.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	f, err := s.st.Get(r.Context(), key)
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	h := w.Header()
	ct := WebP.ContentType()
	if path.Ext(key) == ".jpg" {
		ct = JPEG.ContentType()
	}
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("ETag", `"`+key+`"`)
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", time.Time{}, rs)
		return
	}
	if match := r.Header.Get("If-None-Match"); match != "" && match == h.Get("ETag") {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, f)
}

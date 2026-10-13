package files

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// idPattern — id файла (newID): base32 строчными буквами.
var idPattern = regexp.MustCompile(`^[a-z0-9]{1,64}$`)

// svgCSP — политика для SVG, открытого по прямому адресу: sandbox запрещает скрипты, формы и переходы,
// default-src 'none' — загрузку чего-либо со стороны (встроенные стили и картинки data: остаются).
const svgCSP = "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:"

// Handler отдаёт файлы по путям File.Path. Сайт монтирует его под своим префиксом:
//
//	mux.Handle("GET /files/", http.StripPrefix("/files", svc.Handler()))
//
// Путь — «<id>/<URLName>» в точности; другое имя при верном id — 404 (у файла один адрес). Поддерживаются
// GET и HEAD, Range (видео перематывается без загрузки целиком), If-None-Match и If-Range. Содержимое по
// адресу меняется при замене, поэтому кэш — с проверкой: Cache-Control: public, no-cache и ETag текущей
// версии. Всегда X-Content-Type-Options: nosniff; у SVG — ещё Content-Security-Policy с sandbox.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	id, name, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !ok || !idPattern.MatchString(id) || name == "" {
		http.NotFound(w, r)
		return
	}
	var key, urlName, format string
	err := s.db.QueryRowContext(r.Context(), `SELECT key, url_name, format FROM files_files WHERE id = ?`, id).
		Scan(&key, &urlName, &format)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && name != urlName) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
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
	h.Set("Content-Type", Format(format).ContentType())
	h.Set("Cache-Control", "public, no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	// Ключ меняется при каждой замене: он и есть версия содержимого.
	h.Set("ETag", `"`+strings.TrimSuffix(path.Base(key), path.Ext(key))+`"`)
	if Format(format) == SVG {
		h.Set("Content-Security-Policy", svgCSP)
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		// Без modtime: версию задаёт ETag, Last-Modified с точностью до секунды только мешал бы.
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

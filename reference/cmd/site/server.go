package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/keelkit/keelkit/reference/internal/database"
	"github.com/keelkit/keelkit/reference/internal/settings"
)

// defaultSiteName — название, пока в site_settings нет строки (до seed или настройки в админке).
const defaultSiteName = "Keelkit reference"

func newMux(db *sql.DB) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleHome(db))
	mux.HandleFunc("GET /healthz", handleHealthz(db))
	return mux
}

// homeTmpl — временная главная; с этапа 3 шаблоны пишутся на templ.
var homeTmpl = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="ru">
<head><meta charset="utf-8"><title>{{.Name}}</title></head>
<body><h1>{{.Name}}</h1>{{with .Description}}<p>{{.}}</p>{{end}}</body>
</html>
`))

func handleHome(db *sql.DB) http.HandlerFunc {
	q := settings.New(db)
	return func(w http.ResponseWriter, r *http.Request) {
		s, err := q.GetSiteSettings(r.Context())
		if errors.Is(err, sql.ErrNoRows) {
			s.Name = defaultSiteName
		} else if err != nil {
			slog.Error("get site settings", "err", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := homeTmpl.Execute(w, s); err != nil {
			slog.Error("render home", "err", err)
		}
	}
}

type healthResponse struct {
	Status  string                  `json:"status"`
	Version string                  `json:"version"`
	Schema  map[string]schemaHealth `json:"schema,omitempty"`
}

type schemaHealth struct {
	Version int64 `json:"version"`
	Pending int   `json:"pending"`
}

func handleHealthz(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := healthResponse{Status: "ok", Version: version}
		code := http.StatusOK
		schemas, err := database.Status(r.Context(), db, migrationSources)
		if err != nil {
			slog.Error("healthz: schema status", "err", err)
			resp.Status, code = "error", http.StatusServiceUnavailable
		} else {
			resp.Schema = make(map[string]schemaHealth, len(schemas))
			for _, s := range schemas {
				resp.Schema[s.Name] = schemaHealth{Version: s.Version, Pending: s.Pending}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

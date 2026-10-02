// Package static — CSS, JS и иконки сайта, встроенные в бинарник.
//
// Файлы отдаются по /static/<имя>. В шаблонах ссылка строится через Path: она содержит хеш
// содержимого (?v=…), поэтому браузер кэширует файл надолго и получает новый после пересборки.
// Вендоренные библиотеки хранятся с версией в имени файла: htmx-2.0.11.min.js.
package static

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed *.css *.js *.svg
var files embed.FS

// hashes — короткий хеш содержимого каждого файла, для ?v= в ссылках.
var hashes = func() map[string]string {
	m := map[string]string{}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		b, err := files.ReadFile(e.Name())
		if err != nil {
			panic(err)
		}
		sum := sha256.Sum256(b)
		m[e.Name()] = hex.EncodeToString(sum[:4])
	}
	return m
}()

// Path возвращает URL файла с хешем содержимого. Неизвестное имя — ошибка программиста: паника
// проявится в первом же e2e-тесте страницы.
func Path(name string) string {
	h, ok := hashes[name]
	if !ok {
		panic("static: unknown file " + name)
	}
	return "/static/" + name + "?v=" + h
}

// Handler отдаёт файлы по /static/. Ссылки с ?v= кэшируются на год, без него — проверяются каждый раз.
// Список файлов каталога не отдаётся.
func Handler() http.Handler {
	fileServer := http.StripPrefix("/static/", http.FileServerFS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

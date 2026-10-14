// Команда images-demo обрабатывает файлы по текущему профилю и строит HTML-отчёт: варианты с превью,
// размерами, форматом, весом и наличием EXIF; отклонённые файлы — с причиной.
//
// Запуск — из корня монорепо: make images-demo IN=<файл или папка>.
// Отчёт: modules/images/demo-out/index.html, файлы — modules/images/demo-out/files/ (через DiskStorage).
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gen2brain/webp"

	"github.com/keelkit/keelkit/modules/images"
	"github.com/keelkit/keelkit/modules/images/internal/exif"
)

type row struct {
	Name     string
	Size     string
	Rejected string
	Original string
	Orient   int
	Exif     bool
	Duration string
	Dir      string
	Variants []variant
}

type variant struct {
	File, Format, Size string
	Width, Height      int
	Exif               bool
}

func main() {
	in := flag.String("in", "testdata", "файл или папка с изображениями")
	out := flag.String("out", "demo-out", "каталог для отчёта")
	flag.Parse()
	if err := run(*in, *out); err != nil {
		slog.Error("images-demo", "err", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	files, err := inputs(in)
	if err != nil {
		return err
	}
	// Удаляются только результаты прошлого запуска, а не весь каталог out.
	if err := os.RemoveAll(filepath.Join(out, "files")); err != nil {
		return err
	}
	st, err := images.NewDiskStorage(filepath.Join(out, "files"))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	var rows []row
	for i, f := range files {
		data, err := os.ReadFile(f) // #nosec G304 -- путь задаёт разработчик, это локальная утилита
		if err != nil {
			return err
		}
		r := row{Name: filepath.Base(f), Size: kb(len(data))}
		start := time.Now()
		res, err := images.Process(ctx, bytes.NewReader(data))
		r.Duration = time.Since(start).Round(time.Millisecond).String()
		if err != nil {
			r.Rejected = reason(err)
			rows = append(rows, r)
			continue
		}
		r.Dir = fmt.Sprintf("%02d", i+1)
		if err := res.Save(ctx, st, r.Dir); err != nil {
			return err
		}
		o := res.Original
		r.Original = fmt.Sprintf("%s, %d×%d", strings.ToUpper(string(o.Format)), o.Width, o.Height)
		tiff := exifOf(o.Format, o.Data)
		r.Exif, r.Orient = tiff != nil, exif.Orientation(tiff)
		for _, v := range res.Variants {
			r.Variants = append(r.Variants, variant{
				File: "files/" + r.Dir + "/" + v.Name(), Format: strings.ToUpper(string(v.Format)), Size: kb(len(v.Data)),
				Width: v.Width, Height: v.Height, Exif: exifOf(v.Format, v.Data) != nil,
			})
		}
		rows = append(rows, r)
	}

	backend := "встроенная (libwebp, транслированная в Go)"
	if webp.Dynamic() == nil {
		backend = "системная libwebp (сборка без тега nodynamic)"
	}
	var buf bytes.Buffer
	if err := page.Execute(&buf, map[string]any{
		"Rows": rows, "Profile": images.ProfileVersion, "Backend": backend, "In": in,
	}); err != nil {
		return err
	}
	index := filepath.Join(out, "index.html")
	if err := os.WriteFile(index, buf.Bytes(), 0o600); err != nil {
		return err
	}
	abs, _ := filepath.Abs(index)
	fmt.Println("Отчёт:", abs)
	return nil
}

// inputs — файл или все файлы папки (без вложенных, без скрытых и README), по имени.
func inputs(in string) ([]string, error) {
	fi, err := os.Stat(in)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return []string{in}, nil
	}
	entries, err := os.ReadDir(in)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasPrefix(n, ".") || strings.HasPrefix(strings.ToUpper(n), "README") {
			continue
		}
		files = append(files, filepath.Join(in, n))
	}
	slices.Sort(files)
	return files, nil
}

func exifOf(f images.Format, data []byte) []byte {
	switch f {
	case images.JPEG:
		return exif.FromJPEG(data)
	case images.PNG:
		return exif.FromPNG(data)
	default:
		return exif.FromWebP(data)
	}
}

// reason — причина отказа по-русски, как её показал бы сайт, и технические подробности.
func reason(err error) string {
	var msg string
	switch {
	case errors.Is(err, images.ErrTooLarge):
		msg = "файл больше 20 МБ"
	case errors.Is(err, images.ErrTooManyPixels):
		msg = "больше 40 мегапикселей"
	case errors.Is(err, images.ErrUnsupportedFormat):
		msg = "неподдерживаемый формат"
	case errors.Is(err, images.ErrInvalidImage):
		msg = "файл повреждён"
	default:
		msg = "ошибка обработки"
	}
	return msg + " (" + err.Error() + ")"
}

func kb(n int) string { return fmt.Sprintf("%.1f КБ", float64(n)/1024) }

var page = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="ru">
<meta charset="utf-8">
<title>images — демо профиля v{{.Profile}}</title>
<style>
  body { font: 14px/1.4 system-ui, sans-serif; margin: 24px; color: #222; }
  h2 { margin: 32px 0 4px; font-size: 16px; }
  .meta { color: #555; margin: 0 0 8px; }
  .rejected { color: #b00020; font-weight: 600; }
  table { border-collapse: collapse; }
  td, th { border: 1px solid #ddd; padding: 6px 10px; text-align: left; vertical-align: middle; }
  th { background: #f5f5f5; }
  .preview { width: 180px; }
  /* Шахматка показывает прозрачность. */
  .preview img { max-width: 160px; max-height: 120px; display: block;
    background: repeating-conic-gradient(#ccc 0 25%, #fff 0 50%) 0 0 / 16px 16px; }
  .yes { color: #b00020; font-weight: 600; }
</style>
<h1>images — демо профиля обработки v{{.Profile}}</h1>
<p class="meta">Вход: {{.In}}. Кодировщик WebP: {{.Backend}}.</p>
{{range .Rows}}
<h2>{{.Name}}</h2>
{{if .Rejected}}
<p class="rejected">Отклонён: {{.Rejected}}</p>
{{else}}
<p class="meta">Оригинал: {{.Original}}, {{.Size}}, EXIF: {{if .Exif}}есть, ориентация {{.Orient}}{{else}}нет{{end}}
  (хранится как есть: files/{{.Dir}}/). Обработка: {{.Duration}}.</p>
<table>
  <tr><th>Превью</th><th>Ширина × высота</th><th>Формат</th><th>Размер</th><th>EXIF</th></tr>
  {{range .Variants}}
  <tr>
    <td class="preview"><a href="{{.File}}"><img src="{{.File}}" alt=""></a></td>
    <td>{{.Width}} × {{.Height}}</td><td>{{.Format}}</td><td>{{.Size}}</td>
    <td>{{if .Exif}}<span class="yes">есть</span>{{else}}нет{{end}}</td>
  </tr>
  {{end}}
</table>
{{end}}
{{end}}
`))

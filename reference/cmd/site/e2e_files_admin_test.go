package main

// e2e файлов: раздел «Файлы» в админке (загрузка, замена без смены адреса, удаление используемого), блок file
// на странице и отдача файлов сайтом (тип, Range, SVG в песочнице).

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/voffkin/keelkit/modules/files"
	"github.com/voffkin/keelkit/modules/files/testkit"

	"github.com/voffkin/keelkit/reference/internal/filelib"
)

// newFilesAdmin — сайт с seed без изображений, но с файлами из seedFiles (страница «Документы» — с блоками file),
// и вошедший «браузер». ids — id файлов seed по имени.
func newFilesAdmin(t *testing.T) (*pageAdmin, map[string]string) {
	t.Helper()
	site := migratedSite(t, false)
	ids := map[string]string{}
	ctx := context.Background()
	if err := seedFiles(ctx, site.files, ids); err != nil {
		t.Fatalf("seed files: %v", err)
	}
	if err := seedContent(ctx, site.db, ids); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return newPageAdminOn(t, site), ids
}

// sendFile — multipart-форма загрузки или замены: CSRF-токен полем (как без JavaScript) и файл name с data;
// name == "" — форма без файла.
func (a *pageAdmin) sendFile(path, name string, data []byte, header http.Header) (response, string) {
	a.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("csrf_token", a.token); err != nil {
		a.t.Fatal(err)
	}
	if name != "" {
		fw, err := mw.CreateFormFile("file", name)
		if err != nil {
			a.t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			a.t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		a.t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, a.srv.URL+path, &body)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatal(err)
	}
	return response{StatusCode: resp.StatusCode, Header: resp.Header}, string(b)
}

// fileURL — адрес файла на сайте по id.
func (a *pageAdmin) fileURL(id string) string {
	a.t.Helper()
	f, err := a.site.files.Get(context.Background(), id)
	if err != nil {
		a.t.Fatal(err)
	}
	return "/files/" + f.Path
}

// Страница «Документы»: PDF — ссылкой с форматом и размером, SVG — картинкой с alt, видео — плеером; сайт отдаёт
// файлы с их типом, видео — частями, SVG — в песочнице.
func TestE2EFileBlocks(t *testing.T) {
	a, ids := newFilesAdmin(t)
	pdf, svg, mp4 := a.fileURL(ids["price.pdf"]), a.fileURL(ids["logo.svg"]), a.fileURL(ids["clip.mp4"])
	doc := page(t, a.srv, "/documents", http.StatusOK)
	link := doc.Find("#block-docs-price a")
	if href, _ := link.Attr("href"); href != pdf || text(link) != "Прайс-лист" {
		t.Errorf("PDF link = %q %q, want %q «Прайс-лист»", href, text(link), pdf)
	}
	if got := text(doc.Find("#block-docs-price .file-meta")); got != "PDF, 1 КБ" {
		t.Errorf("PDF meta = %q, want «PDF, 1 КБ»", got)
	}
	if src, _ := doc.Find("#block-docs-logo img").Attr("src"); src != svg {
		t.Errorf("SVG img src = %q, want %q", src, svg)
	}
	if alt, _ := doc.Find("#block-docs-logo img").Attr("alt"); alt != "Логотип Keelkit" {
		t.Errorf("SVG alt = %q", alt)
	}
	if src, _ := doc.Find("#block-docs-clip video[controls]").Attr("src"); src != mp4 {
		t.Errorf("video src = %q, want %q", src, mp4)
	}

	resp, body := fetch(t, a.srv.Client(), a.srv.URL+pdf)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(body, "%PDF-") {
		t.Errorf("GET %s = %d %q", pdf, resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, a.srv.URL+mp4, nil)
	req.Header.Set("Range", "bytes=0-99")
	vr, err := a.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = vr.Body.Close()
	if vr.StatusCode != http.StatusPartialContent || vr.Header.Get("Content-Type") != "video/mp4" || vr.ContentLength != 100 {
		t.Errorf("GET %s Range 0-99 = %d %q %d bytes, want 206 video/mp4 100", mp4, vr.StatusCode, vr.Header.Get("Content-Type"), vr.ContentLength)
	}
	resp, _ = fetch(t, a.srv.Client(), a.srv.URL+svg)
	if csp := resp.Header.Get("Content-Security-Policy"); resp.Header.Get("Content-Type") != "image/svg+xml" || !strings.Contains(csp, "sandbox") {
		t.Errorf("GET %s: %q, CSP %q; want image/svg+xml with sandbox", svg, resp.Header.Get("Content-Type"), csp)
	}
	page(t, a.srv, "/files", http.StatusNotFound)
	assertStatus(t, a, "/files/"+ids["price.pdf"]+"/other.pdf", http.StatusNotFound)
}

// Раздел «Файлы»: загрузка (форма и htmx), ошибки, карточка, замена по тому же адресу, удаление используемого — 409.
func TestE2EFilesAdmin(t *testing.T) {
	a, ids := newFilesAdmin(t)
	doc := parse(t, mustGet(a, filelib.AdminPath))
	if n := doc.Find(".files-table tbody tr").Length(); n != 3 {
		t.Errorf("files list: %d rows, want 3 from seed", n)
	}

	// Загрузка: без JavaScript — 303 на список, с htmx — HX-Redirect.
	resp, _ := a.sendFile(filelib.AdminPath, "Отчёт 2026.pdf", testkit.Sample(files.PDF, 2000), nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != filelib.AdminPath+"?uploaded=1" {
		t.Errorf("upload: %d %q, want 303 to ?uploaded=1", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = a.sendFile(filelib.AdminPath, "clip.webm", testkit.Sample(files.WebM, 0), htmx)
	if resp.Header.Get("HX-Redirect") != filelib.AdminPath+"?uploaded=1" {
		t.Errorf("htmx upload: %d, HX-Redirect %q", resp.StatusCode, resp.Header.Get("HX-Redirect"))
	}
	list, err := a.site.files.List(context.Background(), 10, 0)
	if err != nil || len(list) != 5 {
		t.Fatalf("files after uploads: %d, %v; want 5", len(list), err)
	}

	// Ошибки — у поля, 422; с htmx — только форма.
	for _, c := range []struct{ name, data, want string }{
		{"page.html", "<!DOCTYPE html><html></html>", "Формат не поддерживается: загрузите PDF, видео MP4 или WebM, картинку SVG. Фотографии — в медиатеку."},
		{"empty.pdf", "", "Файл пустой или повреждён."},
		{"", "", "Выберите файл."},
	} {
		resp, body := a.sendFile(filelib.AdminPath, c.name, []byte(c.data), nil)
		if got := text(parse(t, body).Find("#upload-form-error")); resp.StatusCode != http.StatusUnprocessableEntity || got != c.want {
			t.Errorf("upload %q: %d %q, want 422 %q", c.name, resp.StatusCode, got, c.want)
		}
		resp, body = a.sendFile(filelib.AdminPath, c.name, []byte(c.data), htmx)
		if d := parse(t, body); resp.StatusCode != http.StatusUnprocessableEntity || d.Find("h1").Length() != 0 || text(d.Find("#upload-form-error")) != c.want {
			t.Errorf("htmx upload %q: %d, want 422 with the form only", c.name, resp.StatusCode)
		}
	}

	// Карточка используемого файла: адрес и где используется, удалить нельзя.
	id := ids["price.pdf"]
	url := a.fileURL(id)
	doc = parse(t, mustGet(a, filelib.ShowPath(id)))
	if got := text(doc.Find(".media-meta code")); got != url {
		t.Errorf("card URL = %q, want %q", got, url)
	}
	if got := text(doc.Find(".usage-list")); got != "Страница «Документы»" {
		t.Errorf("usage = %q", got)
	}
	if doc.Find(`a[href$="/delete"]`).Length() != 0 {
		t.Error("card of a used file links to delete")
	}

	// Замена: тот же адрес, новое содержимое; другой формат — ошибка у поля, файл прежний.
	data := testkit.Sample(files.PDF, 5000)
	resp, _ = a.sendFile(filelib.ShowPath(id)+"/replace", "price-2027.pdf", data, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != filelib.ShowPath(id)+"?replaced=1" {
		t.Errorf("replace: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if a.fileURL(id) != url {
		t.Errorf("URL after replace = %q, want %q", a.fileURL(id), url)
	}
	if _, body := fetch(t, a.srv.Client(), a.srv.URL+url); body != string(data) {
		t.Errorf("GET %s after replace: %d bytes, want the new %d", url, len(body), len(data))
	}
	resp, body := a.sendFile(filelib.ShowPath(id)+"/replace", "logo.svg", testkit.Sample(files.SVG, 0), htmx)
	if got := text(parse(t, body).Find("#replace-form-error")); resp.StatusCode != http.StatusUnprocessableEntity || !strings.HasPrefix(got, "Замена — только файлом того же формата") {
		t.Errorf("replace with SVG: %d %q", resp.StatusCode, got)
	}
	if _, body := fetch(t, a.srv.Client(), a.srv.URL+url); body != string(data) {
		t.Error("a failed replace changed the file")
	}
	if resp, _ := a.sendFile(filelib.AdminPath+"/nosuchfile/replace", "x.pdf", data, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("replace of a missing file: %d, want 404", resp.StatusCode)
	}

	// Удаление используемого — 409 со списком; файл на месте.
	if resp, _ := a.get(filelib.ShowPath(id) + "/delete"); resp.StatusCode != http.StatusConflict {
		t.Errorf("confirm delete of a used file: %d, want 409", resp.StatusCode)
	}
	resp, body = a.submit(filelib.ShowPath(id)+"/delete", nil, "", nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body, "Страница «Документы»") {
		t.Errorf("delete of a used file: %d, want 409 with the page", resp.StatusCode)
	}
	assertStatus(t, a, url, http.StatusOK)

	// Свободный файл (загруженный выше clip.webm) удаляется, его адрес — 404.
	i := slices.IndexFunc(list, func(f *files.File) bool { return f.Name == "clip.webm" })
	if i < 0 {
		t.Fatal("no uploaded clip.webm")
	}
	free := list[i]
	if resp, _ := a.get(filelib.ShowPath(free.ID) + "/delete"); resp.StatusCode != http.StatusOK {
		t.Errorf("confirm delete: %d", resp.StatusCode)
	}
	resp, _ = a.submit(filelib.ShowPath(free.ID)+"/delete", nil, "", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != filelib.AdminPath+"?deleted=1" {
		t.Errorf("delete: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	assertStatus(t, a, "/files/"+free.Path, http.StatusNotFound)
	if resp, _ := a.get(filelib.ShowPath(free.ID)); resp.StatusCode != http.StatusNotFound {
		t.Errorf("card of a deleted file: %d, want 404", resp.StatusCode)
	}
}

// Блок file в форме страницы: «Добавить блок» → выбор файла; файла нет — ошибка у поля; сохранение пишет ссылку
// (page_files), и раздел «Файлы» показывает страницу среди использующих.
func TestE2EFileBlockForm(t *testing.T) {
	a, ids := newFilesAdmin(t)
	form := newPageForm("С файлом", "s-faylom")
	resp, body := a.submit("/admin/pages", form, "add:file", nil)
	doc := parse(t, body)
	var bid string
	doc.Find(`input[name="block"]`).Each(func(_ int, s *goquery.Selection) {
		if v, _ := s.Attr("value"); doc.Find(`input[name="block-`+v+`-type"][value="file"]`).Length() > 0 {
			bid = v
		}
	})
	if resp.StatusCode != http.StatusOK || bid == "" {
		t.Fatalf("add:file: %d, file block %q", resp.StatusCode, bid)
	}
	if n := doc.Find(`select[name="block-` + bid + `-file"] option[value!=""]`).Length(); n != 3 {
		t.Errorf("file options: %d, want 3 from seed", n)
	}

	form = formValues(doc.Find("#page-form"))
	form.Set("block-"+bid+"-file", "nosuchfile")
	resp, body = a.submit("/admin/pages", form, "save", nil)
	if got := text(parse(t, body).Find("#block-" + bid + "-file-error")); resp.StatusCode != http.StatusUnprocessableEntity || got != "Файла нет в разделе «Файлы»: выберите другой." {
		t.Errorf("missing file: %d %q", resp.StatusCode, got)
	}
	form.Set("block-"+bid+"-file", ids["clip.mp4"])
	if resp, _ := a.submit("/admin/pages", form, "save", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %d", resp.StatusCode)
	}
	if body := mustGet(a, filelib.ShowPath(ids["clip.mp4"])); !strings.Contains(body, "Страница «С файлом»") {
		t.Error("files card does not list the page that uses the file")
	}
}

// assertStatus — статус ответа сайта на GET path (файлы — не HTML, поэтому не page).
func assertStatus(t *testing.T, a *pageAdmin, path string, want int) {
	t.Helper()
	if resp, _ := fetch(t, a.srv.Client(), a.srv.URL+path); resp.StatusCode != want {
		t.Errorf("GET %s = %d, want %d", path, resp.StatusCode, want)
	}
}

// mustGet — тело страницы админки со статусом 200.
func mustGet(a *pageAdmin, path string) string {
	a.t.Helper()
	resp, body := a.get(path)
	if resp.StatusCode != http.StatusOK {
		a.t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	return body
}

package main

// e2e медиатеки: изображения из seed, загрузка (htmx и обычная форма), ошибки проверки файла,
// карточка, удаление, запрет удалить используемое изображение, постраничный вывод.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/media"
)

// mediaAdmin — сайт с seed (и изображениями из seed, если withImages) и вошедший «браузер».
type mediaAdmin struct {
	t      *testing.T
	site   testSite
	srv    *httptest.Server
	client *http.Client
	token  string
}

func newMediaAdmin(t *testing.T, withImages bool) *mediaAdmin {
	t.Helper()
	site := migratedSite(t, true)
	if withImages {
		if _, err := seedImages(context.Background(), site.media); err != nil {
			t.Fatalf("seed images: %v", err)
		}
	}
	srv := httptest.NewServer(site.handler(config{}))
	t.Cleanup(srv.Close)
	client := adminClient(t)
	return &mediaAdmin{t: t, site: site, srv: srv, client: client, token: loginOK(t, client, srv)}
}

// uploadRequest — multipart-запрос формы загрузки: CSRF-токен полем (как без JavaScript) и файл name с data.
// name == "" — форма без файла.
func (a *mediaAdmin) uploadRequest(name string, data []byte, header http.Header) *http.Request {
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
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, a.srv.URL+media.AdminPath, &body)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range header {
		req.Header[k] = v
	}
	return req
}

// upload отправляет форму загрузки.
func (a *mediaAdmin) upload(name string, data []byte, header http.Header) (response, string) {
	a.t.Helper()
	resp, err := a.client.Do(a.uploadRequest(name, data, header))
	if err != nil {
		a.t.Fatalf("upload %s: %v", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatal(err)
	}
	return response{StatusCode: resp.StatusCode, Header: resp.Header}, string(body)
}

func (a *mediaAdmin) get(path string) (response, string) {
	a.t.Helper()
	return fetch(a.t, a.client, a.srv.URL+path)
}

// doc открывает страницу админки, проверяет статус и возвращает документ.
func (a *mediaAdmin) doc(path string, want int) *goquery.Document {
	a.t.Helper()
	resp, body := a.get(path)
	if resp.StatusCode != want {
		a.t.Fatalf("GET %s: status %d, want %d", path, resp.StatusCode, want)
	}
	return parse(a.t, body)
}

// encodeJPEG — JPEG w×h с градиентом.
func encodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// cardNames — подписи карточек сетки медиатеки по порядку.
func cardNames(doc *goquery.Document) []string {
	var names []string
	doc.Find(".media-card .media-name").Each(func(_ int, s *goquery.Selection) { names = append(names, text(s)) })
	return names
}

func TestE2EAdminMediaSeed(t *testing.T) {
	a := newMediaAdmin(t, true)

	// Раздел в меню админки.
	dash := a.doc("/admin", http.StatusOK)
	if href, _ := dash.Find(`.site-nav a:contains("Медиатека")`).Attr("href"); href != media.AdminPath {
		t.Errorf("menu: Медиатека → %q, want %s", href, media.AdminPath)
	}

	doc := a.doc(media.AdminPath, http.StatusOK)
	if n := doc.Find(".media-card").Length(); n != countSeedImages(t) {
		t.Fatalf("media cards = %d, want %d from seed", n, countSeedImages(t))
	}
	if cur, _ := doc.Find(`.site-nav a[aria-current="page"]`).Attr("href"); cur != media.AdminPath {
		t.Errorf("current menu item = %q, want %s", cur, media.AdminPath)
	}
	// Превью — <picture> с WebP и JPEG; каждый адрес из srcset отдаётся с нужным типом.
	doc.Find(".media-card picture").Each(func(_ int, p *goquery.Selection) {
		for sel, ct := range map[string]string{`source[type="image/webp"]`: "image/webp", "img": "image/jpeg"} {
			srcset, ok := p.Find(sel).Attr("srcset")
			if !ok || srcset == "" {
				t.Errorf("%s: no srcset", sel)
				continue
			}
			for item := range strings.SplitSeq(srcset, ", ") {
				u, _, _ := strings.Cut(item, " ")
				if resp, _ := fetch(t, a.client, a.srv.URL+u); resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != ct {
					t.Errorf("GET %s: status %d, Content-Type %q; want 200 %s", u, resp.StatusCode, resp.Header.Get("Content-Type"), ct)
				}
			}
		}
	})

	// Карточка фото 2600 px: все ширины профиля в WebP и JPEG, адреса — под /media/.
	imgs, err := a.site.media.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var photo *images.Image
	for _, img := range imgs {
		if img.Name == "photo.jpg" {
			photo = img
		}
	}
	if photo == nil {
		t.Fatal("photo.jpg not seeded")
	}
	show := a.doc(media.AdminPath+"/"+photo.ID, http.StatusOK)
	if got := text(show.Find("main h1")); got != "photo.jpg" {
		t.Errorf("h1 = %q", got)
	}
	if n := show.Find(".admin-table tbody tr").Length(); n != 8 {
		t.Errorf("variant rows = %d, want 8 (480/960/1600/2400 × WebP/JPEG)", n)
	}
	if show.Find(".media-preview picture").Length() != 1 {
		t.Error("no preview <picture>")
	}

	// Оригинал не отдаётся даже по прямому адресу; /media без пути — оформленная 404.
	if resp, _ := fetch(t, a.client, a.srv.URL+"/media/"+photo.ID+"/original.jpg"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("original: status %d, want 404", resp.StatusCode)
	}
	page(t, a.srv, "/media", http.StatusNotFound)
}

func TestE2EAdminMediaUpload(t *testing.T) {
	a := newMediaAdmin(t, false)
	if names := cardNames(a.doc(media.AdminPath, http.StatusOK)); len(names) != 0 {
		t.Fatalf("empty library shows %v", names)
	}

	// htmx: 204 и HX-Redirect на медиатеку с сообщением.
	resp, _ := a.upload("first.jpg", encodeJPEG(t, 1200, 800), htmx)
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Hx-Redirect") != media.AdminPath+"?uploaded=1" {
		t.Fatalf("htmx upload: status %d, HX-Redirect %q", resp.StatusCode, resp.Header.Get("Hx-Redirect"))
	}
	// Без JavaScript: 303 туда же.
	resp, _ = a.upload("second.jpg", encodeJPEG(t, 300, 200), nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != media.AdminPath+"?uploaded=1" {
		t.Fatalf("form upload: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	doc := a.doc(media.AdminPath+"?uploaded=1", http.StatusOK)
	if got := text(doc.Find(".flash")); got != "Изображение загружено." {
		t.Errorf("flash = %q", got)
	}
	// Порядок внутри одной секунды не задан: модуль хранит created_at с точностью до секунды.
	names := cardNames(doc)
	slices.Sort(names)
	if got := strings.Join(names, ", "); got != "first.jpg, second.jpg" {
		t.Errorf("cards = %q, want both uploads", got)
	}
	meta := doc.Find(`.media-card:has(.media-name:contains("second.jpg")) .muted`)
	if got := text(meta); !strings.HasPrefix(got, "300×200 · ") || !strings.HasSuffix(got, " КБ") {
		t.Errorf("second.jpg meta = %q", got)
	}
}

func TestE2EAdminMediaUploadErrors(t *testing.T) {
	a := newMediaAdmin(t, false)

	cases := []struct {
		name, file string
		data       []byte
		want       string
	}{
		{"no file", "", nil, "Выберите файл."},
		{"text", "notes.txt", []byte("hello"), "Формат не поддерживается"},
		{"gif", "anim.gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), "Формат не поддерживается"},
		{"broken jpeg", "broken.jpg", []byte("\xFF\xD8\xFF\xE0garbage"), "Файл повреждён"},
		{"25 MB", "huge.jpg", bytes.Repeat([]byte{0xFF}, 25<<20), "Файл больше 20 МБ"},
	}
	for _, c := range cases {
		// htmx: 422 и только форма загрузки с ошибкой у поля.
		resp, body := a.upload(c.file, c.data, htmx)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s (htmx): status %d, want 422", c.name, resp.StatusCode)
			continue
		}
		frag := parse(t, body)
		if frag.Find("html head title").Length() != 0 || frag.Find("form#upload-form").Length() != 1 {
			t.Errorf("%s (htmx): want only the upload form fragment", c.name)
		}
		if got := text(frag.Find("#file-error")); !strings.Contains(got, c.want) {
			t.Errorf("%s (htmx): error %q, want %q", c.name, got, c.want)
		}
		if v, _ := frag.Find(`input[name="file"]`).Attr("aria-invalid"); v != "true" {
			t.Errorf("%s (htmx): file input not marked invalid", c.name)
		}

		// Без JavaScript: 422 и страница медиатеки целиком с той же ошибкой.
		resp, body = a.upload(c.file, c.data, nil)
		doc := parse(t, body)
		if resp.StatusCode != http.StatusUnprocessableEntity || text(doc.Find("main h1")) != "Медиатека" {
			t.Errorf("%s (form): status %d, h1 %q; want 422 page", c.name, resp.StatusCode, text(doc.Find("main h1")))
		}
		if got := text(doc.Find("#file-error")); !strings.Contains(got, c.want) {
			t.Errorf("%s (form): error %q, want %q", c.name, got, c.want)
		}
	}
	if names := cardNames(a.doc(media.AdminPath, http.StatusOK)); len(names) != 0 {
		t.Errorf("rejected uploads are in the library: %v", names)
	}
}

// Тело больше media.MaxUploadRequest обрывается MaxBytesReader — пользователь получает ту же ошибку «больше 20 МБ».
// Через recorder: клиент по сети, отправляющий тело дальше лимита, получил бы обрыв соединения.
func TestE2EAdminMediaUploadOverRequestLimit(t *testing.T) {
	a := newMediaAdmin(t, false)
	req := a.uploadRequest("giant.jpg", bytes.Repeat([]byte{0xFF}, media.MaxUploadRequest), htmx)
	req.Header.Set("X-CSRF-Token", a.token)
	u, _ := url.Parse(a.srv.URL)
	for _, c := range a.client.Jar.Cookies(u) {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.site.handler(config{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	if got := text(parse(t, rec.Body.String()).Find("#file-error")); !strings.Contains(got, "Файл больше 20 МБ") {
		t.Errorf("error = %q", got)
	}
}

func TestE2EAdminMediaDelete(t *testing.T) {
	a := newMediaAdmin(t, false)
	ctx := context.Background()
	img, err := a.site.media.Upload(ctx, bytes.NewReader(encodeJPEG(t, 600, 400)), "gone.jpg")
	if err != nil {
		t.Fatal(err)
	}
	show := media.AdminPath + "/" + img.ID

	// Удаление — со страницы подтверждения, ссылка в .danger-zone карточки.
	if href, _ := a.doc(show, http.StatusOK).Find(".danger-zone a").Attr("href"); href != show+"/delete" {
		t.Errorf("delete link = %q", href)
	}
	confirm := a.doc(show+"/delete", http.StatusOK)
	if !strings.Contains(text(confirm.Find("main h1")), "gone.jpg") {
		t.Errorf("confirm h1 = %q", text(confirm.Find("main h1")))
	}

	// Изображение, на которое ссылается контент, не удаляется: 409 с объяснением, файлы на месте.
	if _, err := a.site.db.ExecContext(ctx, `CREATE TABLE test_refs (image_id TEXT NOT NULL REFERENCES images_files (id))`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.site.db.ExecContext(ctx, `INSERT INTO test_refs (image_id) VALUES (?)`, img.ID); err != nil {
		t.Fatal(err)
	}
	resp, body := post(t, a.client, a.srv.URL+show+"/delete", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(text(parse(t, body).Find("main h1")), "используется") {
		t.Fatalf("delete used image: status %d, want 409 with explanation", resp.StatusCode)
	}
	if r, _ := fetch(t, a.client, a.srv.URL+"/media/"+img.Variants[0].Path); r.StatusCode != http.StatusOK {
		t.Errorf("variant after blocked delete: status %d, want 200", r.StatusCode)
	}

	if _, err := a.site.db.ExecContext(ctx, `DELETE FROM test_refs`); err != nil {
		t.Fatal(err)
	}
	resp, _ = post(t, a.client, a.srv.URL+show+"/delete", url.Values{"csrf_token": {a.token}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != media.AdminPath+"?deleted=1" {
		t.Fatalf("delete: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if got := text(a.doc(media.AdminPath+"?deleted=1", http.StatusOK).Find(".flash")); got != "Изображение удалено." {
		t.Errorf("flash = %q", got)
	}
	a.doc(show, http.StatusNotFound)
	if resp, _ := post(t, a.client, a.srv.URL+show+"/delete", url.Values{"csrf_token": {a.token}}, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("second delete: status %d, want 404", resp.StatusCode)
	}
	if r, _ := fetch(t, a.client, a.srv.URL+"/media/"+img.Variants[0].Path); r.StatusCode != http.StatusNotFound {
		t.Errorf("variant after delete: status %d, want 404", r.StatusCode)
	}
	// Без CSRF-токена удаление не проходит.
	if resp, _ := post(t, a.client, a.srv.URL+show+"/delete", url.Values{}, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("delete without CSRF: status %d, want 403", resp.StatusCode)
	}
}

func TestE2EAdminMediaPagination(t *testing.T) {
	a := newMediaAdmin(t, false)
	data := encodeJPEG(t, 16, 16)
	const total = 50 // perPage = 48: две страницы
	for range total {
		if _, err := a.site.media.Upload(context.Background(), bytes.NewReader(data), "tiny.jpg"); err != nil {
			t.Fatal(err)
		}
	}
	first := a.doc(media.AdminPath, http.StatusOK)
	if n := first.Find(".media-card").Length(); n != 48 {
		t.Errorf("page 1: %d cards, want 48", n)
	}
	if href, _ := first.Find(`.pagination a[rel="next"]`).Attr("href"); href != media.AdminPath+"?page=2" {
		t.Errorf("next = %q", href)
	}
	second := a.doc(media.AdminPath+"?page=2", http.StatusOK)
	if n := second.Find(".media-card").Length(); n != total-48 {
		t.Errorf("page 2: %d cards, want %d", n, total-48)
	}
	if second.Find(`.pagination a[rel="next"]`).Length() != 0 || second.Find(`.pagination a[rel="prev"]`).Length() != 1 {
		t.Error("page 2: want only the prev link")
	}
	if n := a.doc(media.AdminPath+"?page=abc", http.StatusOK).Find(".media-card").Length(); n != 48 {
		t.Errorf("bad page number: %d cards, want page 1", n)
	}
}

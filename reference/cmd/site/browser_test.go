//go:build browser

package main

// Браузерные тесты критичных htmx-взаимодействий: настоящий Chrome через chromedp.
// Нужен установленный Chrome или Chromium; запуск — make e2e-browser (тег сборки browser),
// поэтому make test и make e2e их не трогают.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// browser — вкладка headless Chrome; закрывается в конце теста.
func browser(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	ctx, cancelAlloc := chromedp.NewExecAllocator(ctx, chromedp.DefaultExecAllocatorOptions[:]...)
	t.Cleanup(cancelAlloc)
	ctx, cancelTab := chromedp.NewContext(ctx, chromedp.WithErrorf(t.Logf))
	t.Cleanup(cancelTab)
	return ctx
}

func run(t *testing.T, ctx context.Context, step string, actions ...chromedp.Action) {
	t.Helper()
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("%s: %v", step, err)
	}
}

// browserLogin входит в админку dev-пользователем из seed.
func browserLogin(t *testing.T, ctx context.Context, baseURL string) {
	t.Helper()
	run(t, ctx, "login",
		chromedp.Navigate(baseURL+"/admin/login"),
		chromedp.SendKeys(`input[name="email"]`, seedAdminEmail, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="password"]`, seedAdminPassword, chromedp.ByQuery),
		chromedp.Click(`form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.counters`, chromedp.ByQuery),
	)
}

func TestBrowserPagePreviewAndSave(t *testing.T) {
	srv := e2eServer(t, true)
	ctx := browser(t)
	browserLogin(t, ctx, srv.URL)

	// Превью обновляется при наборе, без отправки формы.
	var preview string
	run(t, ctx, "preview",
		chromedp.Navigate(srv.URL+"/admin/pages/new"),
		chromedp.WaitVisible(`#page-form`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="title"]`, "Тестовая", chromedp.ByQuery),
		chromedp.SendKeys(`textarea[name="body"]`, "Текст **жирный**", chromedp.ByQuery),
		chromedp.WaitVisible(`#page-preview strong`, chromedp.ByQuery),
		chromedp.Text(`#page-preview strong`, &preview, chromedp.ByQuery),
	)
	if preview != "жирный" {
		t.Errorf("preview strong = %q", preview)
	}

	// Сохранение через htmx: HX-Redirect ведёт на форму с «Сохранено». Новая страница — черновик:
	// снятый флажок публикует её (htmx, как и браузер, не отправляет неотмеченный checkbox).
	var flash, slugValue string
	run(t, ctx, "save",
		chromedp.Click(`input[name="draft"]`, chromedp.ByQuery),
		chromedp.Click(`#page-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.flash`, chromedp.ByQuery),
		chromedp.Text(`.flash`, &flash, chromedp.ByQuery),
		chromedp.Value(`input[name="slug"]`, &slugValue, chromedp.ByQuery),
	)
	if flash != "Сохранено." || slugValue != "testovaya" {
		t.Errorf("after save: flash %q, slug %q", flash, slugValue)
	}

	// Ошибка валидации приходит фрагментом: форма заменяется без перезагрузки страницы
	// (метка в window переживает подмену), введённый текст на месте.
	var slugError, body string
	var marker bool
	run(t, ctx, "duplicate slug",
		chromedp.Navigate(srv.URL+"/admin/pages/new"),
		chromedp.WaitVisible(`#page-form`, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker = true`, nil),
		chromedp.SendKeys(`input[name="title"]`, "Тестовая", chromedp.ByQuery),
		chromedp.SendKeys(`textarea[name="body"]`, "Черновик", chromedp.ByQuery),
		chromedp.Click(`#page-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#slug-error`, chromedp.ByQuery),
		chromedp.Text(`#slug-error`, &slugError, chromedp.ByQuery),
		chromedp.Value(`textarea[name="body"]`, &body, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
	)
	if slugError != "Страница с адресом «testovaya» уже есть: выберите другой slug." {
		t.Errorf("slug error = %q", slugError)
	}
	if body != "Черновик" {
		t.Errorf("body after error = %q", body)
	}
	if !marker {
		t.Error("page was reloaded: want the form fragment swapped by htmx")
	}

	var h1 string
	run(t, ctx, "public page",
		chromedp.Navigate(srv.URL+"/testovaya"),
		chromedp.Text(`main h1`, &h1, chromedp.ByQuery),
	)
	if h1 != "Тестовая" {
		t.Errorf("public h1 = %q", h1)
	}
}

// Загрузка через htmx (hx-encoding="multipart/form-data"): файл доходит до сервера, после успеха HX-Redirect ведёт
// на медиатеку с новым изображением; ошибка проверки заменяет только форму, без перезагрузки страницы.
func TestBrowserMediaUpload(t *testing.T) {
	srv := e2eServer(t, true)
	ctx := browser(t)
	browserLogin(t, ctx, srv.URL)

	dir := t.TempDir()
	photo, notImage := filepath.Join(dir, "sunset.jpg"), filepath.Join(dir, "notes.jpg")
	if err := os.WriteFile(photo, encodeJPEG(t, 800, 600), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notImage, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	var fileError string
	var marker bool
	run(t, ctx, "rejected file",
		chromedp.Navigate(srv.URL+"/admin/media"),
		chromedp.WaitVisible(`#upload-form`, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker = true`, nil),
		chromedp.SetUploadFiles(`input[name="file"]`, []string{notImage}, chromedp.ByQuery),
		chromedp.Click(`#upload-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#file-error`, chromedp.ByQuery),
		chromedp.Text(`#file-error`, &fileError, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
	)
	if fileError != "Формат не поддерживается: загрузите JPEG, PNG или WebP (без анимации). HEIC с iPhone сначала сохраните как JPEG." {
		t.Errorf("file error = %q", fileError)
	}
	if !marker {
		t.Error("page was reloaded: want the upload form swapped by htmx")
	}

	var flash, name string
	var loaded bool
	run(t, ctx, "upload",
		chromedp.SetUploadFiles(`input[name="file"]`, []string{photo}, chromedp.ByQuery),
		chromedp.Click(`#upload-form button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.flash`, chromedp.ByQuery),
		chromedp.Text(`.flash`, &flash, chromedp.ByQuery),
		chromedp.Text(`.media-card .media-name`, &name, chromedp.ByQuery),
		// Превью действительно загрузилось: браузер выбрал вариант из srcset и декодировал его.
		chromedp.Poll(`(() => { const i = document.querySelector('.media-card img'); return i.complete && i.naturalWidth > 0 })()`, &loaded, chromedp.WithPollingTimeout(10*time.Second)),
	)
	if flash != "Изображение загружено." || name != "sunset.jpg" || !loaded {
		t.Errorf("after upload: flash %q, first card %q, preview loaded %v", flash, name, loaded)
	}
}

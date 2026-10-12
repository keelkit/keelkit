//go:build browser

package main

// Браузерные тесты критичных htmx-взаимодействий: настоящий Chrome через chromedp.
// Нужен установленный Chrome или Chromium; запуск — make e2e-browser (тег сборки browser),
// поэтому make test и make e2e их не трогают.

import (
	"context"
	"net/http/httptest"
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

// Превью Markdown блока при наборе, сохранение через HX-Redirect, подмена формы с ошибкой без перезагрузки.
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
		chromedp.SendKeys(`.block-form textarea`, "Текст **жирный**", chromedp.ByQuery),
		chromedp.WaitVisible(`.block-form .preview strong`, chromedp.ByQuery),
		chromedp.Text(`.block-form .preview strong`, &preview, chromedp.ByQuery),
	)
	if preview != "жирный" {
		t.Errorf("preview strong = %q", preview)
	}

	// «Опубликовать» через htmx: HX-Redirect ведёт на форму с «Опубликовано».
	var flash, slugValue string
	run(t, ctx, "publish",
		chromedp.Click(`.form-actions button[value="publish"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.flash`, chromedp.ByQuery),
		chromedp.Text(`.flash`, &flash, chromedp.ByQuery),
		chromedp.Value(`input[name="slug"]`, &slugValue, chromedp.ByQuery),
	)
	if flash != "Опубликовано." || slugValue != "testovaya" {
		t.Errorf("after publish: flash %q, slug %q", flash, slugValue)
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
		chromedp.SendKeys(`.block-form textarea`, "Черновик", chromedp.ByQuery),
		chromedp.Click(`.form-actions button[value="save"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#slug-error`, chromedp.ByQuery),
		chromedp.Text(`#slug-error`, &slugError, chromedp.ByQuery),
		chromedp.Value(`.block-form textarea`, &body, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
	)
	if slugError != "Страница с адресом «testovaya» уже есть (опубликованная или черновик): выберите другой slug." {
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

// htmxSettled ждёт, пока htmx закончит запрос и подмену: обработчики на новый фрагмент он навешивает после фазы
// settle, и клик раньше этого ушёл бы обычной отправкой формы (человек так быстро не кликнет, chromedp — да).
func htmxSettled() chromedp.Action {
	var idle bool
	return chromedp.Poll(`!document.querySelector('.htmx-request, .htmx-swapping, .htmx-settling, .htmx-added')`, &idle,
		chromedp.WithPollingTimeout(10*time.Second))
}

// blockTitles — заголовки блоков формы по порядку (JS-выражение).
const blockTitles = `Array.from(document.querySelectorAll('#blocks > .block-form h3')).map(h => h.textContent.trim()).join(',')`

// «Добавить блок» дописывает фрагмент нового блока, «↑» и «Скрыть» подменяют форму, выбор изображения
// перерисовывает блок с превью — всё через htmx, без перезагрузки страницы; результат публикуется.
func TestBrowserBlocks(t *testing.T) {
	site, _ := migratedSiteWithImages(t)
	srv := httptest.NewServer(site.handler(config{}))
	t.Cleanup(srv.Close)
	ctx := browser(t)
	browserLogin(t, ctx, srv.URL)

	var order string
	var marker bool
	run(t, ctx, "add blocks",
		chromedp.Navigate(srv.URL+"/admin/pages/new"),
		chromedp.WaitVisible(`#page-form`, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker = true`, nil),
		chromedp.SendKeys(`input[name="title"]`, "Блоки", chromedp.ByQuery),
		chromedp.SendKeys(`.block-form textarea`, "Первый", chromedp.ByQuery),
		chromedp.Click(`.block-add button[value="add:quote"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#blocks > .block-form:nth-child(2) textarea`, chromedp.ByQuery),
		htmxSettled(),
		chromedp.SendKeys(`#blocks > .block-form:nth-child(2) textarea`, "Цитата", chromedp.ByQuery),
		chromedp.Click(`.block-add button[value="add:image"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#blocks > .block-form:nth-child(3) select`, chromedp.ByQuery),
		htmxSettled(),
		chromedp.Evaluate(blockTitles, &order),
	)
	if order != "Текст,Цитата,Изображение" {
		t.Errorf("after add: %s", order)
	}

	// Выбор изображения: блок перерисован с превью, введённый alt не потерян.
	var thumbs int
	run(t, ctx, "pick image",
		chromedp.SendKeys(`#blocks > .block-form:nth-child(3) input[name$="-alt"]`, "Фото", chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const s = document.querySelector('#blocks > .block-form:nth-child(3) select');
			s.value = s.options[1].value; s.dispatchEvent(new Event('change', {bubbles: true})); })()`, nil),
		// Картинка с loading="lazy" до загрузки нулевого размера: ждём элемент, а не его видимость.
		chromedp.WaitReady(`#blocks > .block-form:nth-child(3) .block-thumb picture`, chromedp.ByQuery),
		htmxSettled(),
		chromedp.Evaluate(`document.querySelectorAll('.block-thumb picture').length`, &thumbs),
	)
	var alt string
	run(t, ctx, "alt kept", chromedp.Value(`#blocks > .block-form:nth-child(3) input[name$="-alt"]`, &alt, chromedp.ByQuery))
	if thumbs != 1 || alt != "Фото" {
		t.Errorf("after picking: thumbnails %d, alt %q", thumbs, alt)
	}

	// «↑» у цитаты и «Скрыть» у текста: форма подменяется, введённое на месте.
	var unsaved string
	run(t, ctx, "reorder and hide",
		chromedp.Click(`#blocks > .block-form:nth-child(2) button[value^="up:"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.flash-warning`, chromedp.ByQuery),
		htmxSettled(),
		chromedp.Evaluate(blockTitles, &order),
		chromedp.Text(`.flash-warning`, &unsaved, chromedp.ByQuery),
		chromedp.Click(`#blocks > .block-form:nth-child(2) button[value^="hide:"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#blocks > .block-form:nth-child(2).block-hidden`, chromedp.ByQuery),
		htmxSettled(),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
	)
	if order != "Цитата,Текст,Изображение" || unsaved == "" {
		t.Errorf("after up: %s, notice %q", order, unsaved)
	}
	if !marker {
		t.Error("page was reloaded: want htmx swaps")
	}

	var flash string
	run(t, ctx, "publish",
		chromedp.Click(`.form-actions button[value="publish"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.flash[role="status"]:not(.flash-warning)`, chromedp.ByQuery),
		chromedp.Text(`.flash[role="status"]:not(.flash-warning)`, &flash, chromedp.ByQuery),
	)
	if flash != "Опубликовано." {
		t.Fatalf("publish flash = %q", flash)
	}
	var public string
	run(t, ctx, "public page",
		chromedp.Navigate(srv.URL+"/bloki"),
		chromedp.WaitVisible(`main article`, chromedp.ByQuery),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('main article .block')).map(b => b.classList[1]).join(',')`, &public),
	)
	if public != "block-quote,block-image" {
		t.Errorf("public blocks = %s, want quote and image (text is hidden)", public)
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

// Выбор обложки поста: поле перерисовывается с превью через htmx, введённое в форме не теряется;
// после публикации обложка — в ленте блога.
func TestBrowserPostCover(t *testing.T) {
	site, _ := migratedSiteWithImages(t)
	srv := httptest.NewServer(site.handler(config{}))
	t.Cleanup(srv.Close)
	ctx := browser(t)
	browserLogin(t, ctx, srv.URL)

	var marker bool
	var title string
	run(t, ctx, "pick cover",
		chromedp.Navigate(srv.URL+"/admin/posts/new"),
		chromedp.WaitVisible(`#post-form`, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker = true`, nil),
		chromedp.SendKeys(`input[name="title"]`, "Пост с обложкой", chromedp.ByQuery),
		chromedp.SendKeys(`.block-form textarea`, "Текст", chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const s = document.querySelector('select[name="cover"]');
			s.value = s.options[1].value; s.dispatchEvent(new Event('change', {bubbles: true})); })()`, nil),
		chromedp.WaitReady(`#cover-field .block-thumb picture`, chromedp.ByQuery),
		htmxSettled(),
		chromedp.Value(`input[name="title"]`, &title, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
	)
	if title != "Пост с обложкой" || !marker {
		t.Errorf("after picking: title %q, no reload %v", title, marker)
	}

	var flash string
	run(t, ctx, "publish",
		chromedp.Click(`.form-actions button[value="publish"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.flash[role="status"]`, chromedp.ByQuery),
		chromedp.Text(`.flash[role="status"]`, &flash, chromedp.ByQuery),
	)
	if flash != "Опубликовано." {
		t.Fatalf("publish flash = %q", flash)
	}
	var covers int
	run(t, ctx, "blog",
		chromedp.Navigate(srv.URL+"/blog"),
		chromedp.WaitVisible(`.post-list`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll('.post-card:first-child .post-card-cover picture').length`, &covers),
	)
	if covers != 1 {
		t.Errorf("first card covers = %d, want 1", covers)
	}
}

// Превью сниппета SEO обновляется при вводе в любое поле формы (заголовок поста — подстановка) и в поля SEO,
// не сбивая ввод и не перезагружая страницу; выбранная картинка появляется в карточке соцсетей.
func TestBrowserSEOSnippet(t *testing.T) {
	site, _ := migratedSiteWithImages(t)
	srv := httptest.NewServer(site.handler(config{}))
	t.Cleanup(srv.Close)
	ctx := browser(t)
	browserLogin(t, ctx, srv.URL)

	const searchTitle = `document.querySelector('#seo-snippet .seo-snippet-search .seo-snippet-title').textContent.trim()`
	var marker bool
	var typed string
	run(t, ctx, "auto title",
		chromedp.Navigate(srv.URL+"/admin/posts/new"),
		chromedp.WaitVisible(`#post-form`, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker = true`, nil),
		chromedp.SendKeys(`input[name="title"]`, "Пост для сниппета", chromedp.ByQuery),
		chromedp.Poll(searchTitle+` === 'Пост для сниппета — Keelkit — эталонный сайт'`, nil),
		htmxSettled(),
	)
	run(t, ctx, "manual title and image",
		chromedp.Click(`details.seo-fields summary`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="seo_title"]`, "Свой заголовок", chromedp.ByQuery),
		chromedp.Poll(searchTitle+` === 'Свой заголовок'`, nil),
		htmxSettled(),
		chromedp.Evaluate(`(() => { const s = document.querySelector('select[name="seo_image"]');
			s.value = s.options[1].value; s.dispatchEvent(new Event('input', {bubbles: true})); })()`, nil),
		chromedp.WaitReady(`#seo-snippet .seo-snippet-card picture`, chromedp.ByQuery),
		htmxSettled(),
		chromedp.Value(`input[name="seo_title"]`, &typed, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
	)
	if typed != "Свой заголовок" || !marker {
		t.Errorf("seo_title %q, no reload %v", typed, marker)
	}
}

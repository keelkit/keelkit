//go:build browser

package main

// Браузерные тесты критичных htmx-взаимодействий: настоящий Chrome через chromedp.
// Нужен установленный Chrome или Chromium; запуск — make e2e-browser (тег сборки browser),
// поэтому make test и make e2e их не трогают.

import (
	"context"
	"fmt"
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

// Панель оформления Markdown (static/editor.js): кнопка оборачивает выделение, повторное нажатие снимает разметку,
// Ctrl+B — то же с клавиатуры, правка отменяется (история ввода), превью обновляется; ссылка и список.
func TestBrowserMarkdownToolbar(t *testing.T) {
	srv := e2eServer(t, true)
	ctx := browser(t)
	browserLogin(t, ctx, srv.URL)

	const ta = `.block-form .md-field textarea`
	// sel выделяет в textarea фрагмент [from, to) (в символах UTF-16, как у JavaScript).
	sel := func(from, to int) chromedp.Action {
		return chromedp.Evaluate(fmt.Sprintf(`(() => { const t = document.querySelector(%q); t.focus(); t.setSelectionRange(%d, %d) })()`, ta, from, to), nil)
	}
	click := func(md string) chromedp.Action {
		return chromedp.Click(`.block-form .md-toolbar [data-md="`+md+`"]`, chromedp.ByQuery)
	}
	var value, preview string
	check := func(step, want string) {
		t.Helper()
		run(t, ctx, step, chromedp.Value(ta, &value, chromedp.ByQuery))
		if value != want {
			t.Errorf("%s: value = %q, want %q", step, value, want)
		}
	}

	run(t, ctx, "bold",
		chromedp.Navigate(srv.URL+"/admin/pages/new"),
		chromedp.WaitVisible(`.block-form .md-toolbar`, chromedp.ByQuery),
		chromedp.SendKeys(ta, "Привет мир", chromedp.ByQuery),
		sel(7, 10),
		click("bold"),
		chromedp.WaitVisible(`.block-form .preview strong`, chromedp.ByQuery),
		chromedp.Text(`.block-form .preview strong`, &preview, chromedp.ByQuery),
	)
	check("bold", "Привет **мир**")
	if preview != "мир" {
		t.Errorf("preview strong = %q", preview)
	}

	run(t, ctx, "bold again", click("bold"))
	check("bold again", "Привет мир")

	// Ctrl+B — событием keydown: его ловит обработчик editor.js (по ev.code, раскладка не важна).
	ctrlB := fmt.Sprintf(`document.querySelector(%q).dispatchEvent(new KeyboardEvent('keydown', {code: 'KeyB', key: 'и', ctrlKey: true, bubbles: true}))`, ta)
	run(t, ctx, "ctrl+b", sel(0, 6), chromedp.Evaluate(ctrlB, nil))
	check("ctrl+b", "**Привет** мир")
	run(t, ctx, "undo", chromedp.Evaluate(`document.execCommand('undo')`, nil))
	check("undo", "Привет мир")

	run(t, ctx, "link", sel(7, 10), click("link"), chromedp.KeyEvent("https://go.dev"))
	check("link", "Привет [мир](https://go.dev)")

	run(t, ctx, "list", sel(0, 0), click("ul"))
	check("list", "- Привет [мир](https://go.dev)")
}

// Настройки: выбор картинки для соцсетей перерисовывает поле с превью через htmx, не сбивая ввод; «Добавить
// ссылку» на соцсеть подменяет форму, введённое остаётся; сохранение — через HX-Redirect. Баннер cookies: «Понятно» убирает его без перезагрузки, после обновления страницы его нет.
func TestBrowserSettingsAndCookieBanner(t *testing.T) {
	site, _ := migratedSiteWithImages(t)
	srv := httptest.NewServer(site.handler(config{}))
	t.Cleanup(srv.Close)
	ctx := browser(t)
	browserLogin(t, ctx, srv.URL)

	var marker bool
	var phone, flash string
	run(t, ctx, "pick share image",
		chromedp.Navigate(srv.URL+"/admin/settings"),
		chromedp.WaitVisible(`#settings-form`, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker = true`, nil),
		chromedp.Evaluate(`document.querySelector('input[name="phone"]').value = ''`, nil),
		chromedp.SendKeys(`input[name="phone"]`, "+7 999 000-11-22", chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const s = document.querySelector('select[name="share_image"]');
			s.value = s.options[1].value; s.dispatchEvent(new Event('change', {bubbles: true})); })()`, nil),
		chromedp.WaitReady(`#share_image-field .block-thumb picture`, chromedp.ByQuery),
		htmxSettled(),
		chromedp.Value(`input[name="phone"]`, &phone, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
	)
	if phone != "+7 999 000-11-22" || !marker {
		t.Errorf("after picking: phone %q, no reload %v", phone, marker)
	}
	// «Добавить ссылку» (поле-список): htmx подменяет форму, введённое остаётся; новую ссылку заполняем.
	var items int
	run(t, ctx, "add social link",
		chromedp.Click(`#social-field button[value="list-add:social"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`#social-field .block-item:nth-child(3)`, chromedp.ByQuery),
		htmxSettled(),
		chromedp.SendKeys(`#social-field .block-item:nth-child(3) input[name$="-name"]`, "YouTube", chromedp.ByQuery),
		chromedp.SendKeys(`#social-field .block-item:nth-child(3) input[name$="-url"]`, "https://youtube.com/@example", chromedp.ByQuery),
		chromedp.Value(`input[name="phone"]`, &phone, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll('#social-field .block-item').length`, &items),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
	)
	if items != 3 || phone != "+7 999 000-11-22" || !marker {
		t.Errorf("after add: items %d, phone %q, no reload %v", items, phone, marker)
	}
	run(t, ctx, "save",
		chromedp.Click(`#settings-form .form-actions button[type="submit"]`, chromedp.ByQuery),
		// После «Добавить ссылку» на форме плашка «не сохранены» (.flash-warning): ждём «Сохранено» после перехода.
		chromedp.WaitVisible(`.flash[role="status"]:not(.flash-warning)`, chromedp.ByQuery),
		chromedp.Text(`.flash[role="status"]`, &flash, chromedp.ByQuery),
	)
	if flash != "Сохранено. Изменения уже на сайте." {
		t.Fatalf("save flash = %q", flash)
	}

	var tel, social string
	var banners int
	run(t, ctx, "accept cookies",
		chromedp.Navigate(srv.URL+"/blog"),
		chromedp.WaitVisible(`.cookie-banner`, chromedp.ByQuery),
		chromedp.AttributeValue(`.site-footer a[href^="tel:"]`, "href", &tel, nil, chromedp.ByQuery),
		chromedp.Text(`.social-links li:last-child a`, &social, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker = true`, nil),
		chromedp.Click(`.cookie-banner button`, chromedp.ByQuery),
		chromedp.WaitNotPresent(`.cookie-banner`, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
		chromedp.Reload(),
		chromedp.WaitVisible(`.post-list`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelectorAll('.cookie-banner').length`, &banners),
	)
	if tel != "tel:+79990001122" {
		t.Errorf("footer tel = %q", tel)
	}
	if social != "YouTube" {
		t.Errorf("last social link = %q", social)
	}
	if !marker {
		t.Error("page reloaded on cookie consent")
	}
	if banners != 0 {
		t.Errorf("banners after reload = %d", banners)
	}
}

// Меню: «Добавить пункт» через htmx без перезагрузки; поле адреса видно только у «Свой адрес», флажок подпунктов
// из записей — только у раздела, где они есть (CSS :has по выбору); подпункт в шапке раскрывается при фокусе.
func TestBrowserMenu(t *testing.T) {
	srv := e2eServer(t, true)
	ctx := browser(t)
	browserLogin(t, ctx, srv.URL)

	const item = `#items-field .block-item:nth-child(4)`
	visible := func(sel string) string {
		return `getComputedStyle(document.querySelector('` + sel + `')).display !== 'none'`
	}
	var marker, urlBefore, urlAfter, children bool
	run(t, ctx, "add item",
		chromedp.Navigate(srv.URL+"/admin/menus/main"),
		chromedp.WaitVisible(`#menu-form`, chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker = true`, nil),
		chromedp.Click(`#items-field button[value="list-add:items"]`, chromedp.ByQuery),
		chromedp.WaitVisible(item, chromedp.ByQuery),
		htmxSettled(),
		chromedp.Evaluate(visible(item+` .menu-url`), &urlBefore),
		chromedp.SetValue(item+` select`, "url", chromedp.ByQuery),
		chromedp.Evaluate(visible(item+` .menu-url`), &urlAfter),
		chromedp.Evaluate(visible(item+` .menu-children`), &children),
		chromedp.SendKeys(item+` input[name$="-title"]`, "Прайс", chromedp.ByQuery),
		chromedp.SendKeys(item+` input[name$="-url"]`, "/price", chromedp.ByQuery),
		chromedp.Evaluate(`window.keelkitMarker === true`, &marker),
	)
	if urlBefore || !urlAfter || children || !marker {
		t.Errorf("url field before %v, after %v; children checkbox %v; no reload %v", urlBefore, urlAfter, children, marker)
	}
	var blogChildren bool
	run(t, ctx, "children checkbox", chromedp.Evaluate(visible(`#items-field .block-item:nth-child(3) .menu-children`), &blogChildren))
	if !blogChildren {
		t.Error("children checkbox hidden for the blog section")
	}

	var flash string
	run(t, ctx, "save",
		chromedp.Click(`#menu-form .form-actions button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`.flash[role="status"]:not(.flash-warning)`, chromedp.ByQuery),
		chromedp.Text(`.flash[role="status"]`, &flash, chromedp.ByQuery),
	)
	if flash != "Сохранено. Меню уже на сайте." {
		t.Fatalf("save flash = %q", flash)
	}

	// Подпункты блога (категории) скрыты, пока фокус не попал внутрь пункта.
	var subBefore, subAfter bool
	var last string
	run(t, ctx, "site menu",
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`.site-nav`, chromedp.ByQuery),
		chromedp.Text(`.site-nav > ul > li:last-child > a`, &last, chromedp.ByQuery),
		chromedp.Evaluate(visible(`.site-nav .sub`), &subBefore),
		chromedp.Focus(`.site-nav .has-children > a`, chromedp.ByQuery),
		chromedp.Evaluate(visible(`.site-nav .sub`), &subAfter),
	)
	if last != "Прайс" || subBefore || !subAfter {
		t.Errorf("last item %q; submenu before focus %v, after %v", last, subBefore, subAfter)
	}
}

# Тесты

Часть `AGENTS.md` сайта: читается, когда задача касается этой темы.

## Тесты

- Unit-тесты — рядом с пакетом (`internal/database`, `internal/slug`, `internal/blocks`, `internal/menu`,
  `internal/listfield`).
- e2e — `cmd/site/e2e_test.go`, функции `TestE2E*`: `httptest.NewServer(s.handler(config{}))` (все маршруты `newMux`) над БД после `migrate` и `seed`,
  HTML разбирается goquery. Хелперы `fetch` (тело уже прочитано и закрыто) и `page` (статус, `Content-Type`, документ).
- e2e админки — `cmd/site/e2e_admin_test.go`: `adminClient` (cookie jar, редиректы не выполняются), `post`, `login`, `loginOK`
  (вход dev-пользователем, возвращает CSRF-токен сессии), `csrfToken` (скрытое поле формы).
- `TestImagesContract` (`cmd/site/images_test.go`) — контрактные тесты модуля (`testkit.Run`) на интеграции сайта:
  `migratedSite` (миграции модуля и сайта, загрузки во временной папке) и маршруты `newMux`. При обновлении модуля
  они первыми покажут, что интеграция сломалась.
- `TestFilesContract` (`cmd/site/files_test.go`) — контрактные тесты модуля `files` на интеграции сайта (хранилище
  в `DATA_DIR/files`, маршруты `newMux`).
- `TestAuthContract` (`cmd/site/auth_test.go`) — контрактные тесты модуля `auth`: настоящие страницы входа и админки
  из `newMux` с cookie `Secure`, время сайта (`config.now`) задаёт testkit — проверяются вход, выход, CSRF, блокировка,
  истечение сессии. `TestMigrationAuthOverSiteTables` (`migrations_test.go`) — база до этапа 10.4 (таблицы создала
  миграция сайта) мигрирует, пользователь входит.
- `TestRichtextContract` (`cmd/site/richtext_test.go`) — контрактные тесты модуля `richtext`: Markdown публикуется текстовым
  блоком страницы (`pages.SeedPublished`), testkit проверяет HTML, который отдаёт сервер (XSS-примеры обезврежены).
- Хелпер `migratedSite(t, withSeed)` (`main_test.go`) — БД после `migrate`, сервисы изображений и файлов и `handler(cfg)`;
  `withSeed` заливает `seedContent` без изображений (их обработка ~1 с). Нужны картинки из seed — `migratedSiteWithImages(t)`
  (полный seed, возвращает id изображений по имени файла).
- e2e файлов — `cmd/site/e2e_files_admin_test.go`: страница «Документы» (ссылка, картинка, плеер; тип, Range, CSP у SVG),
  раздел «Файлы» (загрузка формой и htmx, ошибки, карточка, замена по тому же адресу, 409 для используемого, удаление),
  блок file в форме страницы (выбор, ошибка «файла нет», ссылка в «где используется»). Хелпер `newFilesAdmin` —
  seed без изображений, но с файлами.
- e2e медиатеки — `cmd/site/e2e_media_admin_test.go`: seed, загрузка (htmx и форма), каждая ошибка проверки файла,
  тело сверх лимита, карточка, удаление, 409 для используемого изображения, постраничный вывод.
- e2e раздела типа контента — `cmd/site/e2e_<type>_admin_test.go` (образец — `pages`): `newPageAdmin` (сайт + вошедший клиент),
  `form` (поля формы так, как их отправит браузер — `formValues`; другая форма — `formByID`), `submit(path, form, action, header)`, `addBlock`,
  заголовки `htmx`; каждая ошибка валидации проверяется и без `HX-Request` (страница), и с ним (фрагмент).
- e2e блога — `cmd/site/e2e_blog_test.go` (лента, страницы, категория, пост, скрытые черновик и будущий пост) и
  `cmd/site/e2e_posts_admin_test.go` (посты и категории в админке; хелперы `createPost`, `postForm`, `newPostForm`).
- e2e своей картинки для телефона — `TestE2EPageMobileImages` (блоки `image`, `image_text`, галерея) и
  `TestE2EAdminPostCoverMobile` (обложка): сохранение, ошибки у поля, публикация копирует ссылки, `<source media>`
  на сайте, `og:image` — основная, 409 медиатеки, убрать — прежняя разметка.
- `TestSpecBlocksMatchCode` (`cmd/site/spec_test.go`) — блоки из `allow` в `site.yaml` есть в коде и совпадают со списком типа;
  `TestSpecFeaturesMatchCode` — типы с feature `sitemap`, `rss`, `seo` и `order` совпадают с `sitemapSources`, `feedTypes`, `seoTypes`
  и `orderTypes` (у таблиц `orderTypes` есть `sort_order`); `TestBlockListsMatchKinds` — `blockLists` совпадают с блоками-списками;
  `TestSpecSettingsMatchCode` — поля `settings` совпадают с колонками `site_settings`; `TestSpecMenusMatchCode` — `menus`
  совпадают с `siteMenus`; `TestSpecModulesMatchCode` — `modules` (с `spec v0.4.0`) совпадают с модулями Keelkit
  в `go.mod` (мажорные версии тоже), а модули с миграциями стоят в порядке `migrationSources`.
- e2e настроек — `cmd/site/e2e_settings_test.go`: сохранение и вывод на сайте (подвал, мета-теги, баннер), каждая ошибка
  поля (форма и htmx), соцсети (кнопки списка формой и htmx, порядок после сохранения, ошибки у элемента, 400), картинка по умолчанию в `og:image` и в превью сниппета, 409 медиатеки, согласие с cookies
  (форма, htmx, `back` только на этот сайт, запрос с чужого сайта — 403).
- e2e меню — `cmd/site/e2e_menus_test.go`: список меню, кнопки пунктов (формой и htmx), сохранение и вывод в шапке и
  подвале (`menuLinks`), смена slug страницы, подпункты вручную и из записей, неопубликованная страница, каждая ошибка
  поля, 400; 409 удаления страницы из меню — в `TestE2EAdminPageUnpublishDelete`.
- e2e людей и блока people — `cmd/site/e2e_people_admin_test.go`: порядок кнопками (формой и htmx, 400/404), вывод на
  странице «Команда», создание и ошибки полей, удаление, скрытый блок и пустой список, фото (htmx-поле, 409 медиатеки).
- e2e живого предпросмотра — `cmd/site/e2e_live_test.go`: разметка панели в форме, страница из несохранённой
  формы (новая цитата, лид, дата, категория), ничего не сохранено, новая запись без id, 403 без CSRF.
- e2e SEO записи — `cmd/site/e2e_seo_admin_test.go`: превью сниппета (форма и htmx), `<title>`, описание и `og:image`
  поста и страницы (и главной), очистка — снова подстановка, ошибки полей, `seo_image` держит изображение.
- e2e SEO — `cmd/site/e2e_seo_test.go`: sitemap и RSS разбираются `encoding/xml` (без черновиков, будущих постов и
  неопубликованных страниц, адреса от `BASE_URL`), robots.txt, canonical и Open Graph, `noindex` предпросмотра,
  редиректы со старых slug. В тестах `BASE_URL` — `testBaseURL` (`https://example.test`; `handler(config{})` подставляет его сам).
- Браузерные тесты — `cmd/site/browser_test.go` (`//go:build browser`, `TestBrowser*`, chromedp, headless Chrome):
  поведение админки на htmx и своём JS — превью при наборе, сохранение через `HX-Redirect`, подмена формы с ошибкой
  без перезагрузки, «Добавить блок», «↑», «Скрыть» и выбор изображения у блока, выбор обложки поста, превью сниппета SEO, загрузка файла (`hx-encoding="multipart/form-data"`),
  выбор картинки и «Добавить ссылку» в настройках, «Понятно» в баннере cookies (`TestBrowserSettingsAndCookieBanner`),
  «Добавить пункт» меню, поле адреса по выбору и подпункты в шапке по фокусу (`TestBrowserMenu`),
  панель оформления Markdown (`TestBrowserMarkdownToolbar`: кнопка, повторное нажатие, Ctrl+B, отмена, ссылка, список),
  ↓ в списке людей (`TestBrowserPeopleOrder`), живой предпросмотр (`TestBrowserLivePreview`: ввод и «Добавить блок» видны
  в iframe, «Телефон», щелчок по блоку в предпросмотре, ссылка в нём не открывается, вкладки на узком экране),
  своя картинка для телефона (`TestBrowserMobileImage`: режим полей по кнопкам ширины, выбор своей, `currentSrc`
  в предпросмотре «Телефон» и «Компьютер», сохранение в режиме «Компьютер», переключатель над формой на узком экране).
  После каждого htmx-действия — `htmxSettled()`: обработчики на новый фрагмент htmx навешивает после фазы settle,
  и клик chromedp раньше этого ушёл бы обычной отправкой формы. Запуск — `make e2e-browser` (входит в `make check`); `make test` их не собирает, линтер — собирает (`build-tags` в `.golangci.yml`).

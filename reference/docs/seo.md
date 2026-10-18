# SEO

Часть `AGENTS.md` сайта: читается, когда задача касается этой темы.

## SEO (`internal/seo/`, `web.Meta`)

Пишется в коде эталона; вынос в модуль — после второго сайта. Пакет `seo` не знает о типах контента.
- `<head>` — `web.LayoutMeta(web.Meta{…})`: `Title`, `Description` (пусто — описание сайта; у типов — `blocks.Summary`),
  `Path` — адрес страницы для `canonical` и `og:url` (у ленты — с `?page=N`; лишние параметры запроса не попадают),
  `Article` и `Published` (пост: `og:type article`, `article:published_time`), `Image` (`og:image` — JPEG не шире
  1600 px, `web.ShareImage`; у поста — обложка, иначе первое изображение блоков, иначе картинка из настроек), `ImageAlt` (`og:image:alt`; у поста —
  alt обложки), `NoIndex`. Описание поста — анонс (ручной или начало текста). Без `Path` — нет canonical
  и Open Graph (404, 500). Предпросмотр черновика — без `Path` и с `NoIndex`. Абсолютные адреса — `web.Site.BaseURL` +
  путь; на каждой странице — `<link rel="alternate">` на RSS.
- SEO записи вручную — общий компонент `internal/seofields` у каждого типа со своим адресом (страницы, в том числе
  главная, и посты; у справочников без адреса его нет). В `site.yaml` — feature `seo` (с `spec v0.3.0`; поля
  `seo_title`, `seo_description`, `seo_image` она добавляет сама, в `fields` их не пишут); `TestSpecFeaturesMatchCode`
  сверяет типы с feature `seo` со списком `seoTypes`.
  Колонки — в копии записи (`<type>_contents`, миграция `00009`): правка SEO опубликованной записи видна после
  «Опубликовать» и отмечает «Есть неопубликованные изменения»; `seo_image` — в `<type>_images`, как обложка.
  Пустое поле — подстановка, её задаёт тип в `viewMeta` (у поста — заголовок, анонс, обложка; у страницы — заголовок,
  начало текста, первое изображение блоков), затем `Fields.Apply` накладывает заданное: SEO-заголовок идёт в `<title>`
  и `og:title` как есть, без названия сайта (`web.Meta.ExactTitle`); описание — в `meta description` и `og:description`
  (анонс в ленте и RSS не меняется); картинка — в `og:image`. Пробелы и переводы строк — один пробел.
  Форма: `seofields.Section(f.SEO, f.SEOEnv)` — свёрнутый `<details>` (раскрыт, если что-то задано или ошибка) с превью
  выдачи и карточки соцсетей (`seofields.Snippet` — то же, что выведет `web.LayoutMeta`, `<title>` — `web.TitleTag`);
  превью обновляется через htmx при вводе в любое поле формы (`hx-trigger="input delay:500ms from:closest form"`,
  `hx-include="closest form"`, `POST /admin/<type>/seo`: хендлер разбирает форму как при сохранении и собирает
  `viewData` из неё), без JavaScript — после отправки. Лимиты 200/500 символов, превью предупреждает о длине
  сверх ~60/160. Новый тип со своим адресом: колонки, feature `seo` в `site.yaml`, `SEO` в форме, `Apply` в `viewMeta`,
  маршрут `…/seo`, запись в `seoTypes` (`cmd/site/spec_test.go`).
- `/sitemap.xml` — `seo.Sitemap(baseURL, sources…)`: адреса от типов с feature `sitemap` в `site.yaml` —
  `sitemapSources` в `cmd/site/server.go` (метод `SitemapURLs` пакета типа: только видимое на сайте). Страницы — без
  `lastmod` (у копии нет времени публикации), лента блога и посты — `lastmod` по дате публикации. Один файл: лимит
  протокола — 50 000 адресов.
- `/rss.xml` — `seo.Feed(baseURL, feedSource)`: RSS 2.0, последние 20 видимых постов (`posts.FeedItems`), анонс —
  ручной или `blocks.Summary` (500 символов, без разметки), категория, обложка — `<enclosure>` JPEG; `guid` — `post-<id>`
  (`isPermaLink="false"`), не меняется со сменой slug. Типы с feature `rss` — `feedTypes`.
- `/robots.txt` — `seo.Robots(baseURL, "/admin/")` и ссылка на sitemap.
- Новый тип с feature `sitemap` или `rss`: метод `SitemapURLs` / `FeedItems` в пакете типа, запись в `sitemapSources`
  или `feedTypes` (несколько типов в ленте — слить записи по дате в `feedSource`); `TestSpecFeaturesMatchCode` сверяет
  списки с `site.yaml`. Публичные шаблоны типа передают `web.Meta` с `Path`.

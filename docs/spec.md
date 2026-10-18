# Спецификация сайта

Часть корневого `AGENTS.md`: читается, когда задача касается этой темы.

## Спецификация сайта

- Формат: YAML, валидируется по `spec/schema.json` командой `make spec-validate FILE=...`
  (модуль `spec`, CLI `spec/cmd/spec-validate`). Ошибки выводятся как `файл:строка:столбец: /json/pointer: сообщение`.
- Правила, не выражаемые в JSON Schema, проверяет валидатор: `ref` указывает на существующий тип контента,
  `to` — только у `ref`, `mobile` — только у `image`, `allow` — только у `blocks`, `fields` — только у `list`, в типе не больше одного поля `slug`,
  служебные имена полей запрещены (у типа — `id`, `created_at`, `updated_at`, `draft`, `publish_date`, `seo_title`,
  `seo_description`, `seo_image`, `sort_order`; в `settings` — `id`, `updated_at`; у элемента `list` — `id`), feature `seo` — только
  у типа с полем `slug`, в `settings` нет полей `slug` и `blocks`.
- Новое правило схемы — пример в `spec/examples/invalid/` с ожидаемым текстом ошибки (`# expect: …`).
- Хранится в репозитории сайта как `site.yaml`.
- Описывает: разделы и типы контента (поля, связи, правила), подключённые модули и их версии, настройки (языки, SEO, хранилище).
  В `modules` — все модули Keelkit из `go.mod` сайта, в том числе без таблиц (`richtext`), в порядке применения миграций;
  сайт сверяет этот список с `go.mod` и источниками миграций тестом (в эталоне — `TestSpecModulesMatchCode`).
- Структурные изменения сайта вносятся сначала в спецификацию, затем в код.
- Схему спецификации меняем осознанно: у неё есть версия, для старых версий нужен путь миграции.

**Версия схемы 1**
- Типы полей: `string`, `text`, `richtext` (Markdown), `int`, `bool`, `date`, `image`, `ref`, `slug`, `blocks`
  (с `spec v0.2.0`; `allow: [имена блоков]` — обязателен, непустой, без повторов; поля блоков описывает код сайта),
  `list` (с `spec v0.3.0`; `fields: {…}` — поля элемента: типы `string`…`ref`, без `slug`, `blocks` и вложенных `list`).
- Features: `draft`, `publish_date`, `rss`, `sitemap`, `seo` (с `spec v0.3.0`: SEO записи вручную — поля `seo_*`
  добавляет feature, в `fields` их не пишут; только у типа со своим адресом, то есть с полем `slug`), `order`
  (с `spec v0.5.0`: ручной порядок записей — кнопки ↑ ↓ в списке админки, тот же порядок на сайте; колонка `sort_order`).
- `settings: { fields: {…} }` (с `spec v0.3.0`) — одна запись настроек сайта, поля как у типа контента, кроме `slug` и `blocks`.
- `mobile: true` у поля `image` (с `spec v0.7.0`): своя картинка для узкого экрана; не задана — та же, что на
  компьютере. У полей блоков признака нет — их описывает код сайта.
- `modules`: `images`, `richtext`, `auth` (последние два — с `spec v0.4.0`), `files` (с `spec v0.6.0`), значение — мажорная версия (`v0`).
- `menus: [main, footer]` (с `spec v0.3.0`) — меню, которые редактируют в админке; устройство пунктов — код сайта.
- Ровно один язык: поле `languages` есть, но допускает один элемент. Мультиязычность — `spec_version: 2`.

Пример (ориентир, не окончательная схема):

```yaml
spec_version: 1
site:
  name: my-blog
  languages: [ru]
modules:
  images: v0
content:
  pages:
    fields:
      title: { type: string, required: true }
      body:  { type: richtext }
  posts:
    fields:
      title:    { type: string, required: true }
      body:     { type: richtext }
      cover:    { type: image }
      category: { type: ref, to: categories }
    features: [publish_date, draft, rss, sitemap]
  categories:
    fields:
      name: { type: string, required: true }
```

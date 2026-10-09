# Changelog — spec

Формат: [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — semver (теги `spec/vX.Y.Z`).

## [Unreleased]

### Added
- Тип поля `blocks` и его параметр `allow` — список имён блоков (`^[a-z][a-z0-9_]*$`, непустой, без повторов),
  например `body: { type: blocks, allow: [text, image, image_text, quote] }`. Поля блоков в спецификации не описываются:
  набор блоков — дизайн сайта, живёт в коде сайта. `allow` обязателен у `blocks` и запрещён у остальных типов.
  Изменение добавочное: прежние `site.yaml` остаются валидными.

### Fixed
- Ошибка в имени типа контента или поля (`propertyNames`) иногда указывала на чужой путь (например, `/site/BlogPosts`
  вместо `/content/BlogPosts`): `jsonschema` v6.0.3 не копирует путь ошибки, и проверка соседних свойств его затирала.
  Путь теперь сверяется с документом.

## [0.1.0] — 2026-10-02

Первый выпуск.

### Added
- `schema.json` — JSON Schema спецификации сайта, `spec_version: 1`: `site` (name, ровно один язык),
  `modules` (`images`), `content` с типами полей `string, text, richtext, int, bool, date, image, ref, slug`
  и features `draft, publish_date, rss, sitemap`.
- `spec.Validate` и команда `spec-validate`: проверка по схеме и смысловые правила
  (ref на существующий тип, `to` только у ref, не больше одного slug, служебные имена полей запрещены).
  Ошибки — с путём JSON Pointer и строкой/столбцом в YAML.

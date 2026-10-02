# Changelog — spec

Формат: [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — semver (теги `spec/vX.Y.Z`).

## [Unreleased]

## [0.1.0] — 2026-10-02

Первый выпуск.

### Added
- `schema.json` — JSON Schema спецификации сайта, `spec_version: 1`: `site` (name, ровно один язык),
  `modules` (`images`), `content` с типами полей `string, text, richtext, int, bool, date, image, ref, slug`
  и features `draft, publish_date, rss, sitemap`.
- `spec.Validate` и команда `spec-validate`: проверка по схеме и смысловые правила
  (ref на существующий тип, `to` только у ref, не больше одного slug, служебные имена полей запрещены).
  Ошибки — с путём JSON Pointer и строкой/столбцом в YAML.

# Changelog — modules/richtext

Формат: [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — semver (теги `modules/richtext/vX.Y.Z`).
Что делать сайту при обновлении — в [UPGRADE.md](UPGRADE.md).

## [Unreleased]

## [0.1.0] — 2026-10-13

Первый выпуск: код `reference/internal/richtext` вынесен в модуль, чтобы исправления санитайзера доезжали
до сайтов через `go get`. Подключение к сайту — UPGRADE.md, раздел v0.1.0.

### Added
- `Render` — Markdown (GFM) → очищенный HTML: goldmark без сырого HTML, затем bluemonday (политика UGC;
  `class="language-*"` у `code`; `nofollow` только у внешних ссылок), затем типографика (неразрывные пробелы
  после коротких предлогов и союзов и перед тире). Результат — тип `HTML`, реализует `templ.Component`
  (модуль от templ не зависит).
- `PlainText` — текст Markdown без разметки в одну строку: для описания страницы, RSS и поиска.
- `testkit.Run` — контрактные тесты для сайта: Markdown выводится разметкой, XSS-примеры обезврежены
  в HTML, который отдаёт сайт; `testkit.Check` — сама проверка HTML.

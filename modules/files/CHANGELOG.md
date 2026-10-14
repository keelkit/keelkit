# Changelog — modules/files

Формат: [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — semver (теги `modules/files/vX.Y.Z`).
Что делать сайту при обновлении — в [UPGRADE.md](UPGRADE.md).

## [Unreleased]

## [0.2.0] — 2026-10-14

Репозиторий переехал в `github.com/keelkit/keelkit` и стал публичным. Код и API — как в v0.1.0.

### Changed
- Путь модуля — `github.com/keelkit/keelkit/modules/files` (был `github.com/voffkin/keelkit/modules/files`); версии
  до v0.2.0 есть только под старым путём. Что сделать сайту — UPGRADE.md, раздел v0.2.0.

## [0.1.0] — 2026-10-13

Первый выпуск. Подключение к сайту — UPGRADE.md, раздел v0.1.0.

### Added
- Форматы PDF, MP4, WebM и SVG; формат определяется по содержимому (`ErrUnsupportedFormat` для остального,
  в том числе HTML, QuickTime, HEIC/AVIF, Matroska), пустой файл — `ErrInvalidFile`.
- Лимит размера `Config.MaxBytes`, по умолчанию `DefaultMaxBytes` (50 МБ); `ErrTooLarge`; `Service.MaxBytes`.
- `Storage` и `DiskStorage` (через `os.Root`, атомарная запись).
- Миграции `migrations.FS` (таблица версий `migrations.Table` = `goose_files_version`): `files_files`, `files_trash`.
- `Service`: `Upload`, `Replace` (того же формата, адрес не меняется; иначе `ErrFormatMismatch`), `Get`, `List`,
  `Delete` (используемый файл — внешний ключ сайта без `ON DELETE` — не удаляется).
- `Service.Handler`: адрес `<id>/<имя>.<ext>` только в точности; GET и HEAD, Range и If-Range, `ETag` версии
  и `Cache-Control: public, no-cache`; `X-Content-Type-Options: nosniff`; SVG — с `Content-Security-Policy: sandbox`.
- `testkit.Run` — контрактные тесты для сайта; `testkit.Sample` — файлы для тестов.

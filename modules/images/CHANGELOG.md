# Changelog — modules/images

Формат: [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — semver (теги `modules/images/vX.Y.Z`).
Что делать сайту при обновлении — в [UPGRADE.md](UPGRADE.md).

## [Unreleased]

## [0.3.0] — 2026-10-15

Профиль обработки v2: размытая заглушка на время загрузки. Что сделать сайту — UPGRADE.md, раздел v0.3.0.

### Added
- `Result.Placeholder` и `Image.Placeholder` — заглушка на время загрузки: WebP не больше `PlaceholderSize` (32 px)
  по длинной стороне, data URI «data:image/webp;base64,…», несколько сотен байт. У изображений с прозрачностью её нет.
- Миграция `00002_placeholder.sql`: колонка `images_files.placeholder` (у существующих строк — пустая).
- `testkit`: заглушка сохраняется и возвращается `Get`; у прозрачного PNG её нет.

### Changed
- `ProfileVersion` = 2. Новые загрузки получают варианты в `<id>/v2/…` и заглушку; старые изображения — только
  после `Reprocess(ctx, 2)` (адреса их вариантов сменятся с `v1` на `v2`). Ширины, форматы и качество — как в v1.

## [0.2.0] — 2026-10-14

Репозиторий переехал в `github.com/keelkit/keelkit` и стал публичным. Код и API — как в v0.1.0.

### Changed
- Путь модуля — `github.com/keelkit/keelkit/modules/images` (был `github.com/voffkin/keelkit/modules/images`); версии
  до v0.2.0 есть только под старым путём. Что сделать сайту — UPGRADE.md, раздел v0.2.0.

## [0.1.0] — 2026-10-02

Первый выпуск. Подключение к сайту — UPGRADE.md, раздел v0.1.0.

### Added
- `Process` — проверка и обработка по профилю v1: вход JPEG, PNG, WebP (без анимации); лимиты 20 МБ
  и 40 Мп (размеры — по заголовку, до декодирования); ориентация EXIF применяется, EXIF удаляется;
  ширины 480/960/1600/2400 без увеличения (плюс собственная ширина оригинала уже 2400); WebP q=80 и JPEG q=82.
  Ошибки `ErrTooLarge`, `ErrTooManyPixels`, `ErrUnsupportedFormat`, `ErrInvalidImage`.
- `Storage` и `DiskStorage` (через `os.Root`, атомарная запись).
- Миграции `migrations.FS` (таблица версий `migrations.Table` = `goose_images_version`):
  таблицы `images_files`, `images_variants`, `images_trash`; у каждого изображения хранится версия профиля.
- `Service`: `Upload`, `Get`, `List`, `Delete`; ограничение одновременных обработок (`Config.MaxConcurrent`, по умолчанию 2).
- `Service.Handler` — отдаёт только варианты (оригинал с EXIF/GPS — никогда), с `Cache-Control: immutable`:
  версия профиля входит в путь варианта (`<id>/v<профиль>/<ширина>.<ext>`).
- `Service.Reprocess` — повторная обработка изображений с версией профиля ниже N; идемпотентна,
  безопасна для прерывания и повторного запуска.
- `testkit.Run` — контрактные тесты для сайта.

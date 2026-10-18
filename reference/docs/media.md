# Изображения и файлы

Часть `AGENTS.md` сайта: читается, когда задача касается этой темы.

## Изображения (модуль `images`)

Как подключать модуль — `modules/images/UPGRADE.md` (v0.1.0); здесь — как это сделано в эталоне.

- Сервис создаёт `openMedia(db, cfg)` в `cmd/site/main.go`: `images.NewDiskStorage(DATA_DIR/uploads)` +
  `images.NewService(db, st, images.Config{})` над БД сайта. Его получают `newMux`, `seed` и `reprocess-images`.
- Таблицы `images_*` сайт не читает и не пишет запросами — только через `images.Service`. Ссылка из контента
  на изображение — `TEXT REFERENCES images_files (id)` без `ON DELETE` (`page_images`): тогда `Service.Delete`
  используемого изображения вернёт ошибку внешнего ключа (`database.IsForeignKeyViolation`), и медиатека отдаст 409.
- Вывод — `web.Picture(img, alt, sizes)` (`internal/web/picture.templ`): `<picture>` с `<source type="image/webp">`
  и `<img>` с JPEG-фолбэком (`src` — JPEG не шире 960 px), `srcset` по ширинам, `width`/`height` для пропорций,
  `loading="lazy"`. `sizes` — ширина картинки в вёрстке. Адрес варианта — `web.MediaURL(v)` (`/media/` + `v.Path`).
- Пока картинка грузится, под ней — размытая заглушка (`images.Image.Placeholder`, профиль v2): у `<img>` класс `ph`
  и `style="--ph:url(data:image/svg+xml,…)"` — SVG растягивает WebP 32 px и размывает (`feGaussianBlur`), правило
  `img.ph` в `style.css` ставит его фоном (`center / cover`). С мобильной картинкой — ещё `--ph-m` (её заглушка или
  `none`) для экрана уже `web.MobileMedia`. Заглушки нет у изображений с прозрачностью и профиля v1 (до
  `make reprocess-images BELOW=2`) — тогда ни класса, ни `style`. Правило `img.ph` стоит в `style.css` раньше
  правил отдельных картинок: их `background` (шахматка медиатеки) важнее; цвет подложки — `background-color`.
  Адреса вариантов меняются при повторной обработке, поэтому их не сохраняют в контенте — только id изображения.
- Своя картинка для телефона (другой кадр той же сцены) — `web.PictureMobile(img, mobile, alt, sizes)`: перед
  основными источниками два `<source media={ web.MobileMedia }>` (WebP и JPEG) со своими `srcset`, `width`, `height`;
  браузер скачивает одну из двух картинок. `web.MobileMedia` — единственная граница «телефон / компьютер» на сайте
  (экран уже 768 px). `mobile == nil` — разметка как у `web.Picture`. Только файл: alt один на обе, пропорции и
  фокус для телефона не задаются; `og:image` и RSS — всегда основная. Где есть: блоки `image`, `image_text`,
  элементы галереи (`image_mobile` в JSON блока, см. «Блоки» в `docs/content-types.md`), обложка поста (`cover_mobile`). Задана без основной —
  ошибка у поля «Сначала выберите основную картинку»; та же, что основная, — сбрасывается. Мобильные картинки — в
  `<type>_images`, как основные: используемую не удалить из медиатеки.
- Медиатека — `internal/media/` (не тип контента: данные в модуле). Загрузка одного файла формой
  `multipart/form-data`, через htmx (`hx-encoding`, `hx-disabled-elt` на время обработки) и без JavaScript.
  Ошибки модуля (`images.ErrTooLarge`, `ErrTooManyPixels`, `ErrUnsupportedFormat`, `ErrInvalidImage`) — 422 с
  сообщением у поля `file`. Тело запроса ограничивает `maxBody(media.MaxUploadRequest, …)` снаружи `signedIn`:
  форма без JavaScript передаёт CSRF-токен полем multipart, и `RequireUser` разбирает тело раньше обработчика.
  Лимит вдвое больше `images.MaxBytes`, чтобы файл сверх 20 МБ дочитался и пользователь увидел ошибку, а не обрыв.
- Seed — 4 изображения из `cmd/site/seeddata/` (копии из `modules/images/testdata`, встроены через embed);
  уже загруженное с тем же именем файла пропускается. `seedImages` возвращает id по имени файла, `seedContent`
  вставляет их в блоки «О проекте» (без изображений — тесты — эти блоки пропускаются).
- Картинки в контенте — только блоками (`image`, `image_text`) с id изображения и полями `image` (обложка поста);
  ссылки — в `page_images` (`<type>_images` для других типов, у постов — и обложка), перезаписываются в той же
  транзакции, что и копия. Медиатека не знает о типах
  контента: «где используется» ей передаёт `imageUsage` в `cmd/site/server.go` (`media.UsageFunc`) — новый тип
  со ссылками на изображения добавляет туда свой запрос (настройки, страницы, затем посты). Используемое изображение: на карточке — список,
  подтверждение удаления и `POST` — 409 со списком.

## Файлы (модуль `files`)

Как подключать модуль — `modules/files/UPGRADE.md` (v0.1.0); здесь — как это сделано в эталоне. Файлы — документы,
видео и SVG как есть; фотографии для страниц — по-прежнему медиатека (`images`).
- Сервис создаёт `openFiles(db, cfg)` в `cmd/site/main.go`: `files.NewDiskStorage(DATA_DIR/files)` +
  `files.NewService(db, st, filesConfig)`; лимит — `filesConfig` (по умолчанию 50 МБ; сайт с видео задаёт `MaxBytes`).
  Его получают `newMux` и `seed`. Таблицы `files_*` сайт не читает запросами — только через `files.Service`.
- Раздел «Файлы» — `internal/filelib/` (группа «Контент», иконка `files`): загрузка формой `multipart/form-data`
  (htmx и без JavaScript), ошибки модуля (`ErrTooLarge`, `ErrUnsupportedFormat`, `ErrInvalidFile`, `ErrFormatMismatch`) —
  422 у поля; тело — `maxBody(filesAdmin.MaxUploadRequest(), …)` снаружи `signedIn` (лимит модуля + 1 МБ, считается от
  `files.Service.MaxBytes`). Карточка — адрес на сайте, просмотр видео и SVG, форма замены (тот же формат: адрес
  с расширением не меняется), где используется, удаление.
- Ссылка из контента — блок `file` (см. «Блоки» в `docs/content-types.md`): id файла в JSON блока и строка в `page_files` / `post_files`
  (`file_id TEXT REFERENCES files_files (id)` без `ON DELETE`, миграция `00014`), перезаписываются в той же
  транзакции, что и копия, как `<type>_images`. «Где используется» — `fileUsage` в `cmd/site/server.go`
  (`filelib.UsageFunc`); используемый файл — 409 со списком.
- Адрес — `web.FileURL(f)` (`/files/` + `File.Path`, уже экранирован); формат и размер для людей — `web.FileFormat`,
  `web.FormatBytes`.
- Seed — `cmd/site/seedfiles/` (PDF на одну страницу, SVG-логотип, MP4 на 2 секунды, 4 КБ) через `seedFiles`; id
  дописываются в ту же карту по имени файла, что и изображения; страница «Документы» (`/documents`) — блоки file.

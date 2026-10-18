# UPGRADE — modules/images

Инструкции для агентов: что менять в коде сайта при переходе на версию. Версии без действий здесь не перечисляются.
Каждый раздел: что изменилось → изменения в коде сайта → проверка → нужна ли повторная обработка.

## v0.3.0 — профиль v2: заглушка на время загрузки

**Что изменилось.** `ProfileVersion` = 2: кроме вариантов, у изображения есть `Image.Placeholder` — WebP ~32 px
в data URI (`"data:image/webp;base64,…"`), который сайт выводит размытым фоном под `<img>`, пока картинка грузится.
У изображений с прозрачностью заглушки нет (пустая строка): она осталась бы видна под прозрачными местами.
Миграция модуля `00002` добавляет колонку `images_files.placeholder`; существующие данные не меняются, у старых
изображений заглушка пустая, пока их не обработают заново. Варианты новых загрузок — в `<id>/v2/…`.
API не ломается: `Upload`, `Get`, `List`, `Reprocess`, `Handler` — как раньше.

**Изменения в коде сайта** (образец — `reference/internal/web/picture.go`, `picture.templ`, `picture_test.go`,
`static/style.css`; раздел «Изображения» в `reference/docs/media.md`):
1. `go get github.com/keelkit/keelkit/modules/images@v0.3.0`, `go mod tidy`.
2. Вывод картинки (в эталоне — `web.PictureMobile`, через неё и `web.Picture`): у `<img>` с заглушкой — класс `ph`
   и `style` с CSS-переменной `--ph: url(data:image/svg+xml,…)`. SVG растягивает заглушку на всю картинку и
   размывает: `viewBox` — `Image.Width`×`Image.Height`, `<filter>` с `feGaussianBlur` (`stdDeviation` ≈ длинная
   сторона / 20) и `feFuncA type='discrete' tableValues='1 1'` (без полупрозрачного ободка), `<image href=заглушка>`.
   Перенести из эталона `placeholderStyle`, `placeholderURL`, `notBase64`; значение строится в Go как `templ.SafeCSS`,
   SVG кодируется `url.PathEscape` и пишется в `url()` без кавычек (templ экранирует `style` дважды — кавычки
   сломаются). В заглушке допускается только base64 после префикса — иначе заглушки нет.
   С мобильной картинкой (`mobile`, если она выводится) — ещё `--ph-m`: её заглушка или `none`.
   Без заглушки (прозрачность, профиль v1) — ни класса, ни `style`.
3. CSS сайта: правило `img.ph { background: var(--ph) center / cover no-repeat; }` и в `@media` границы телефона
   (`web.MobileMedia`) `img.ph { background-image: var(--ph-m, var(--ph)); }` — раньше правил отдельных картинок.
   Проверить свои правила `img` с `background`: шорткат перекрывает заглушку (так задумано для шахматки медиатеки
   с `object-fit: contain`); где заглушка нужна, а у картинки была подложка цветом (в эталоне `.person img`), —
   `background-color` вместо `background`.
4. Тесты сайта: что ждали версию схемы `images` 1 (`/healthz`, в эталоне `TestHealthz`) — теперь 2; снимок схемы
   (`make schema`, если в сайте есть `db/schema.sql`); тест вывода заглушки — как `TestPicturePlaceholder` эталона.
   Пути вариантов `…/v1/…` в тестах с настоящей загрузкой — `…/v2/…` (лучше через `images.ProfileVersion`).

**Проверка.** `make generate`, `make test` (в том числе `testkit.Run` — он проверяет заглушку), `make test-isolated`,
`make e2e`; `make migrate` на копии БД → `/healthz`: `images` с `version 2, pending 0`; новая загрузка JPEG → на
странице у `<img>` класс `ph` и `--ph:url(data:image/svg+xml,…)`; прозрачный PNG — без них.

**Повторная обработка.** Нужна, чтобы заглушки появились у старых изображений: `make reprocess-images BELOW=2`
после `make migrate` (на сервере — после выкладки; сервер может работать). Без неё всё работает, просто у старых
картинок нет заглушки. Адреса вариантов меняются (`/media/<id>/v1/…` → `/v2/…`), старые файлы удаляются:
страницы сайта выводят новые адреса сами, но внешние копии старых (`og:image` в кэше соцсетей, RSS-читатели)
получат 404, пока не обновятся.

## v0.2.0 — новый путь модуля

**Что изменилось.** Путь модуля `github.com/voffkin/keelkit/modules/images` → `github.com/keelkit/keelkit/modules/images`:
репозиторий переехал и стал публичным. Код, API, таблицы и миграции — как в v0.1.0.

**Изменения в коде сайта.** Модули Keelkit переехали вместе — делается один раз для всех:
1. Во всех файлах сайта (`go.mod`, Go-код, `.templ`, `Makefile`) заменить `github.com/voffkin/keelkit/` на
   `github.com/keelkit/keelkit/`, версии модулей в `go.mod` — v0.2.0; затем `make generate` и `go mod tidy`.
2. `Makefile` сайта: убрать `GOPRIVATE` и `GIT_CONFIG_*` — SSH-доступ к модулям больше не нужен, они идут через
   публичный прокси Go; из `AGENTS.md` сайта — раздел о доступе к приватным модулям.

**Проверка.** `make test`, `make test-isolated`, `make generate-check`; `grep -rn voffkin/keelkit .` ничего не находит.

## v0.1.0 — первое подключение

**Что изменилось.** Модуль впервые подключается к сайту: свои таблицы (`images_*`), свои миграции, сервис и HTTP-обработчик.

**Изменения в коде сайта** (образец — `reference/`):
1. `go get github.com/keelkit/keelkit/modules/images@v0.2.0` (под путём `github.com/keelkit/keelkit` первая версия — v0.2.0);
   в `site.yaml` — `modules: { images: v0 }`.
2. Миграции: источник модуля — **перед** миграциями сайта, в порядке из `site.yaml`:
   `database.Source{Name: "images", FS: migrations.FS, Table: migrations.Table}`
   (пакет `github.com/keelkit/keelkit/modules/images/migrations`). `/healthz` показывает схему `images`.
3. Сервис: `st, err := images.NewDiskStorage(filepath.Join(dataDir, "uploads"))`,
   `svc := images.NewService(db, st, images.Config{})`. `st.Close()` — при остановке.
   БД — та же SQLite сайта; таблицы модуля не трогать запросами сайта, только через `Service`.
4. Отдача: `mux.Handle("GET /media/", http.StripPrefix("/media/", svc.Handler()))`. Адрес варианта —
   `"/media/" + v.Path`. Оригиналы не отдаются (в них EXIF с GPS) и в бэкап загрузок входят обязательно.
5. Ссылки из контента — `TEXT REFERENCES images_files (id)` без `ON DELETE`: тогда `Service.Delete` изображения,
   которое где-то используется, вернёт ошибку, и ничего не удалится. Сайт показывает понятное сообщение.
6. Загрузка в админке: `svc.Upload(ctx, file, header.Filename)`; ошибки `images.ErrTooLarge`,
   `ErrTooManyPixels`, `ErrUnsupportedFormat`, `ErrInvalidImage` (через `errors.Is`) — пользователю, с текстом
   на языке сайта. Тело запроса ограничить (`http.MaxBytesReader`) чуть больше `images.MaxBytes`.
7. Повторная обработка: подкоманда сайта `site reprocess-images -below N` → `svc.Reprocess(ctx, N)` и цель
   `make reprocess-images BELOW=<N>`; вывести `Processed`/`Failed`, ненулевой код выхода при ошибке.
8. Тесты сайта: `testkit.Run(t, setup)` на настоящей интеграции сайта (своя БД с миграциями, свой роутер);
   e2e «загрузка изображения».

**Проверка.** `make migrate` → `/healthz`: `images` с `version 1, pending 0`; `make test` (в том числе testkit);
`make test-isolated` на опубликованной версии модуля; загрузка JPEG в админке → `<picture>` на странице отдаёт `.webp`.

**Повторная обработка.** Не нужна (первое подключение).

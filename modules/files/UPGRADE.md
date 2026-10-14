# UPGRADE — modules/files

Инструкции для агентов: что менять в коде сайта при переходе на версию. Версии без действий здесь не перечисляются.
Каждый раздел: что изменилось → изменения в коде сайта → проверка → нужны ли действия с данными.

## v0.2.0 — новый путь модуля

**Что изменилось.** Путь модуля `github.com/voffkin/keelkit/modules/files` → `github.com/keelkit/keelkit/modules/files`:
репозиторий переехал и стал публичным. Код, API, таблицы и миграции — как в v0.1.0.

**Изменения в коде сайта.** Модули Keelkit переехали вместе — делается один раз для всех:
1. Во всех файлах сайта (`go.mod`, Go-код, `.templ`, `Makefile`) заменить `github.com/voffkin/keelkit/` на
   `github.com/keelkit/keelkit/`, версии модулей в `go.mod` — v0.2.0; затем `make generate` и `go mod tidy`.
2. `Makefile` сайта: убрать `GOPRIVATE` и `GIT_CONFIG_*` — SSH-доступ к модулям больше не нужен, они идут через
   публичный прокси Go; из `AGENTS.md` сайта — раздел о доступе к приватным модулям.

**Проверка.** `make test`, `make test-isolated`, `make generate-check`; `grep -rn voffkin/keelkit .` ничего не находит.

## v0.1.0 — первое подключение

**Что изменилось.** Модуль впервые подключается к сайту: свои таблицы (`files_*`), свои миграции, сервис и
HTTP-обработчик. Файлы (PDF, видео MP4/WebM, SVG) хранятся как есть; картинки для вёрстки остаются в модуле `images`.

**Изменения в коде сайта** (образец — `reference/`):
1. `go get github.com/keelkit/keelkit/modules/files@v0.2.0` (под путём `github.com/keelkit/keelkit` первая версия — v0.2.0);
   в `site.yaml` — `modules: { files: v0 }` (нужен `spec` v0.6.0 или новее).
2. Миграции: источник модуля — **перед** миграциями сайта, в порядке из `site.yaml`:
   `database.Source{Name: "files", FS: migrations.FS, Table: migrations.Table}`
   (пакет `github.com/keelkit/keelkit/modules/files/migrations`). `/healthz` показывает схему `files`.
3. Сервис: `st, err := files.NewDiskStorage(filepath.Join(dataDir, "files"))`,
   `svc := files.NewService(db, st, files.Config{})` (лимит 50 МБ; сайт с видео задаёт `MaxBytes`). `st.Close()` —
   при остановке. Каталог файлов входит в бэкап вместе с БД. Таблицы модуля — только через `Service`.
4. Отдача: `mux.Handle("GET /files/", http.StripPrefix("/files", svc.Handler()))`; адрес файла —
   `"/files/" + f.Path` (`Path` уже экранирован). `files` — в зарезервированные slug сайта. Обработчик сам ставит
   `nosniff`, `ETag`, кэш с проверкой и CSP `sandbox` у SVG — не переопределять.
5. Ссылки из контента — `TEXT REFERENCES files_files (id)` без `ON DELETE` (таблица ссылок копии, как у изображений):
   тогда `Service.Delete` используемого файла вернёт ошибку внешнего ключа, и ничего не удалится.
6. Админка: загрузка `svc.Upload(ctx, file, header.Filename)`, замена `svc.Replace(ctx, id, file, name)`;
   ошибки `files.ErrTooLarge`, `ErrUnsupportedFormat`, `ErrInvalidFile`, `ErrFormatMismatch` (через `errors.Is`) —
   пользователю на языке сайта. Тело запроса ограничить (`http.MaxBytesReader`) на `svc.MaxBytes()` с запасом
   (~1 МБ на поля формы).
7. Вывод: видео — `<video controls preload="metadata" src>` (Range отдаёт обработчик), SVG — `<img>` с alt,
   остальное — ссылка с форматом и размером.
8. Тесты сайта: `testkit.Run(t, setup)` на настоящей интеграции сайта (своя БД с миграциями, свой роутер);
   e2e «загрузка файла, замена, удаление используемого».

**Проверка.** `make migrate` → `/healthz`: `files` с `version 1, pending 0`; `make test` (в том числе testkit);
`make test-isolated` на опубликованной версии модуля; загрузка PDF в админке → ссылка на странице открывает его;
замена → адрес тот же; `curl -sI -H 'Range: bytes=0-99' <адрес видео>` → `206`.

**Действия с данными.** Не нужны (первое подключение).

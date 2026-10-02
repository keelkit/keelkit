# UPGRADE — modules/images

Инструкции для агентов: что менять в коде сайта при переходе на версию. Версии без действий здесь не перечисляются.
Каждый раздел: что изменилось → изменения в коде сайта → проверка → нужна ли повторная обработка.

## v0.1.0 — первое подключение

**Что изменилось.** Модуль впервые подключается к сайту: свои таблицы (`images_*`), свои миграции, сервис и HTTP-обработчик.

**Изменения в коде сайта** (образец — `reference/`):
1. `go get github.com/voffkin/keelkit/modules/images@v0.1.0` (через `make`: нужны `GOPRIVATE` и SSH, см. `AGENTS.md`);
   в `site.yaml` — `modules: { images: v0 }`.
2. Миграции: источник модуля — **перед** миграциями сайта, в порядке из `site.yaml`:
   `database.Source{Name: "images", FS: migrations.FS, Table: migrations.Table}`
   (пакет `github.com/voffkin/keelkit/modules/images/migrations`). `/healthz` показывает схему `images`.
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

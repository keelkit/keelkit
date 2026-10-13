# UPGRADE — modules/richtext

Инструкции для агентов: что менять в коде сайта при переходе на версию. Версии без действий здесь не перечисляются.
Каждый раздел: что изменилось → изменения в коде сайта → проверка.

## v0.1.0 — первое подключение

**Что изменилось.** Пакет `internal/richtext` сайта переехал в модуль без изменений API: `Render`, `PlainText`, тип `HTML`.
Таблиц и миграций у модуля нет.

**Изменения в коде сайта** (образец — `reference/`):
1. `go get github.com/voffkin/keelkit/modules/richtext@v0.1.0` (через `make`: нужны `GOPRIVATE` и SSH, см. `AGENTS.md`).
2. Импорты `<модуль сайта>/internal/richtext` → `github.com/voffkin/keelkit/modules/richtext` (Go-код и `.templ`),
   затем `make generate`. Удалить каталог `internal/richtext`.
3. `go mod tidy`: goldmark, bluemonday и `golang.org/x/net` остаются в `go.mod`, если сайт использует их сам, иначе
   становятся косвенными.
4. Контрактный тест: `testkit.Run(t, render)`, где `render` выводит Markdown так же, как настоящая страница
   (например, пост с текстовым блоком через обработчик сайта), и возвращает полученный HTML.
   Если на сайте свой вывод richtext в обход `Render`, тест это покажет.

**Проверка.** `make test` (в том числе testkit), `make test-isolated` на опубликованной версии модуля,
`make generate-check`; `ls internal/richtext` — каталога нет; страница с Markdown выглядит как раньше
(таблицы, код, неразрывные пробелы).

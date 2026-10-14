# UPGRADE — modules/richtext

Инструкции для агентов: что менять в коде сайта при переходе на версию. Версии без действий здесь не перечисляются.
Каждый раздел: что изменилось → изменения в коде сайта → проверка.

## v0.2.0 — новый путь модуля

**Что изменилось.** Путь модуля `github.com/voffkin/keelkit/modules/richtext` → `github.com/keelkit/keelkit/modules/richtext`:
репозиторий переехал и стал публичным. Код, API — как в v0.1.1.

**Изменения в коде сайта.** Модули Keelkit переехали вместе — делается один раз для всех:
1. Во всех файлах сайта (`go.mod`, Go-код, `.templ`, `Makefile`) заменить `github.com/voffkin/keelkit/` на
   `github.com/keelkit/keelkit/`, версии модулей в `go.mod` — v0.2.0; затем `make generate` и `go mod tidy`.
2. `Makefile` сайта: убрать `GOPRIVATE` и `GIT_CONFIG_*` — SSH-доступ к модулям больше не нужен, они идут через
   публичный прокси Go; из `AGENTS.md` сайта — раздел о доступе к приватным модулям.

**Проверка.** `make test`, `make test-isolated`, `make generate-check`; `grep -rn voffkin/keelkit .` ничего не находит.

## v0.1.0 — первое подключение

**Что изменилось.** Пакет `internal/richtext` сайта переехал в модуль без изменений API: `Render`, `PlainText`, тип `HTML`.
Таблиц и миграций у модуля нет.

**Изменения в коде сайта** (образец — `reference/`):
1. `go get github.com/keelkit/keelkit/modules/richtext@v0.2.0` (под путём `github.com/keelkit/keelkit` первая версия — v0.2.0);
   в `site.yaml` — `modules: { …, richtext: v0 }` (валидатор `spec` с v0.4.0, `SPEC_VERSION` в `Makefile` сайта).
2. Импорты `<модуль сайта>/internal/richtext` → `github.com/keelkit/keelkit/modules/richtext` (Go-код и `.templ`),
   затем `make generate`. Удалить каталог `internal/richtext`.
3. `go mod tidy`: goldmark, bluemonday и `golang.org/x/net` остаются в `go.mod`, если сайт использует их сам, иначе
   становятся косвенными.
4. Контрактный тест: `testkit.Run(t, render)`, где `render` выводит Markdown так же, как настоящая страница
   (например, пост с текстовым блоком через обработчик сайта), и возвращает полученный HTML.
   Если на сайте свой вывод richtext в обход `Render`, тест это покажет.

**Проверка.** `make test` (в том числе testkit), `make test-isolated` на опубликованной версии модуля,
`make generate-check`; `ls internal/richtext` — каталога нет; страница с Markdown выглядит как раньше
(таблицы, код, неразрывные пробелы).

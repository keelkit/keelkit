# PLAN.md — план первого этапа Keelkit

Пошаговый план по разделу «Первый этап» из `AGENTS.md`. Отмечайте прогресс чекбоксами.

**Правило плана:** после каждого этапа сервис находится в рабочем состоянии —
его можно запустить одной командой и увидеть результат в браузере, в JSON или в сгенерированной HTML-странице.
Этап не считается завершённым, пока не пройден его раздел «Проверка».

**Общие соглашения для проверок** (появляются на этапе 0–2 и дальше не меняются):
- `make dev` — применить миграции, залить seed, запустить сайт на `http://localhost:8080`.
- `make reset` — удалить локальную dev-БД и загрузки (`reference/data/`), чтобы начать с нуля.
- `GET /healthz` — JSON о состоянии: версия сборки, версии схем БД (сайт и каждый модуль), число непримененных миграций.
- Seed идемпотентен: повторный `make seed` не создаёт дублей. Seed-данные только для dev, в проде не запускаются.

---

## Этап 0. Каркас монорепо и «hello»-сайт — ✅ готово

Цель: репозиторий собирается, `make` — единая точка входа, эталонный сайт отвечает по HTTP.

- [x] `git init`, `.gitignore` (`data/`, `backups/`, бинарники, `.env`)
- [x] Установить актуальный Go (локально сейчас 1.22.4); версию в `go.mod` выбрать по политике «актуальный релиз минус один минор», проверив по go.dev/dl
  — сделано через `GOTOOLCHAIN=auto`: `go 1.26.0`, `toolchain go1.27.1` в `go.work` и `go.mod`; Go сам скачивает тулчейн
- [x] `go.work` в корне (без `go.mod` в корне), подключены `modules/images` (пустой пакет-заглушка) и `reference/`
- [x] Корневой `Makefile`: `help` по умолчанию, `test`, `test-isolated`, `lint`, `run`, `dev`; совместимость с GNU Make 3.81
- [x] `.golangci.yml` — общий минимальный конфиг; версия golangci-lint закреплена в `Makefile`
- [x] `reference/cmd/site/main.go`: `net/http`, `log/slog`, конфиг из env (`ADDR`, `DATA_DIR`), страница `/` («Keelkit reference»), `GET /healthz`
- [x] `reference/Makefile`, `reference/AGENTS.md`, `reference/CLAUDE.md` (`@AGENTS.md`)
- [x] `.github/workflows/ci.yml`: `make lint`, `make test` для `reference/` и `modules/images`

### Проверка
```sh
make                      # выводит список целей с описаниями
make lint test            # зелёные
make run                  # сайт стартует
curl -s localhost:8080/healthz   # {"status":"ok","version":"dev"}
open http://localhost:8080/      # страница «Keelkit reference»
```
Проверить совместимость: `/usr/bin/make help` (системный make 3.81 на macOS) работает без ошибок.

---

## Этап 1. Спецификация v1 (черновик) — ✅ готово

Цель: есть схема `site.yaml`, и её можно проверить командой.

Сделано: модуль `spec/` (`github.com/keelkit/keelkit/spec`) — `schema.json`, `spec.Validate`, `cmd/spec-validate`;
зависимости `go.yaml.in/yaml/v3` v3.0.5, `santhosh-tekuri/jsonschema/v6` v6.0.3, `golang.org/x/text` v0.42.0.
Цели `spec-validate` в `reference/Makefile` пока нет: сайт будет вызывать `go run …/spec/cmd/spec-validate@vX`
после первого релиза `spec` (этап 7).

- [x] `spec/schema.json` — `spec_version: 1`: `site` (name, languages из одного элемента), `modules`, `content` с полями типов `string, text, richtext, int, bool, date, image, ref, slug` и features `draft, publish_date, rss, sitemap`
- [x] Правила в схеме: `ref` требует `to`, `languages` — ровно один элемент, неизвестные типы и features запрещены
- [x] `spec/examples/`: `blog.yaml` (страницы + блог), `landing.yaml` (визитка), `invalid/*.yaml` — заведомо ошибочные примеры
- [x] `reference/site.yaml` — пока только `site` и `pages`; дополняется на следующих этапах
- [x] Валидатор: цель `make spec-validate FILE=...` (внешний валидатор JSON Schema и YAML-парсер — согласовать, см. «Зависимости к согласованию»)
- [x] Проверка ссылок `ref` на существующий тип контента (в JSON Schema не выражается — отдельной проверкой в валидаторе)
- [x] Тест: все `examples/*.yaml` валидны, все `examples/invalid/*.yaml` — нет, с ожидаемым текстом ошибки

### Проверка
```sh
make spec-validate FILE=spec/examples/blog.yaml          # OK
make spec-validate FILE=spec/examples/invalid/two-languages.yaml   # понятная ошибка с путём к полю
make spec-validate FILE=reference/site.yaml              # OK
make test                                                # тесты валидатора зелёные
make run && curl -s localhost:8080/healthz               # сайт по-прежнему работает
```

---

## Этап 2. БД, миграции, seed — ✅ готово

Цель: у сайта есть SQLite, миграции применяются только явной командой со снимком, есть тестовые данные.

Сделано: `modernc.org/sqlite` v1.60.1, goose v3.28.0 — только как библиотека (подкоманда `site migrate`, CLI goose не нужен,
поэтому согласовывать версии CLI и библиотеки не требуется), sqlc v1.31.1 через `go run` с `CGO_ENABLED=0`.
Состояние схем читается без записи в БД (goose `Status` создаёт таблицу версий, поэтому сначала проверяется её наличие).
Найдено ограничение sqlc: кириллица в `queries.sql` обрезает текст запросов — правило «`queries.sql` только ASCII» в `reference/AGENTS.md`.
Миграций модулей пока нет; порядок «модули из `site.yaml`, затем сайт» заложен в `migrationSources`.

- [x] `modernc.org/sqlite`, goose (версия CLI и библиотеки совпадает), sqlc (версия закреплена в `Makefile`)
- [x] `reference/db/migrations/` — первая миграция: `site_settings` (название, описание сайта); таблица версий `goose_site_version`
- [x] Подкоманда `site migrate`: снимок `VACUUM INTO backups/<timestamp>.db` → без снимка отказ → применение миграций (модули в порядке `site.yaml`, затем сайт)
- [x] При старте `site serve` проверяет непримененные миграции и отказывается стартовать с понятным сообщением
- [x] Подкоманда `site seed` (dev): идемпотентно заполняет `site_settings`
- [x] Цели: `make migrate`, `make seed`, `make dev`, `make reset`, `make generate`, `make generate-check`
- [x] `/healthz` показывает версии схем и число непримененных миграций
- [x] Главная выводит название сайта из БД
- [x] Тесты: миграция на пустой БД, отказ старта при непримененной миграции, создание снимка
- [x] CI: `make generate-check`

### Проверка
```sh
make reset
make run          # отказ: «есть непримененные миграции, выполните make migrate»
make migrate      # в reference/backups/ появился снимок, миграции применены
make seed && make seed   # второй запуск не создаёт дублей
make dev
curl -s localhost:8080/healthz
# {"status":"ok","schema":{"site":{"version":1,"pending":0}}}
open http://localhost:8080/   # в шапке — название сайта из seed
make generate-check           # генерация ничего не меняет
```

---

## Этап 3. Шаблоны, статика и публичные страницы (`pages`) — ✅ готово

Цель: первый тип контента виден в браузере.

Сделано: templ v0.3.1020 (CLI через `go run` с `TEMPL_VERSION`; `make generate` сверяет его с библиотекой в `go.mod`),
goldmark v1.8.6, bluemonday v1.0.27, goquery v1.13.0, htmx 2.0.11 (сверен с sha512 из npm; 4.0.0 уже вышел, но в npm помечен `next`).
Данные шапки (название, меню из страниц) кладёт в контекст middleware `web.WithSite`; `internal/web` не знает о типах контента.
Markdown рендерит `internal/richtext` в тип `richtext.HTML` — в шаблон попадает только очищенный HTML;
`nofollow` — только у внешних ссылок. Статика отдаётся с `?v=<хеш>` и годовым кэшем.
e2e — `TestE2E*` в `cmd/site` (входят в `make test`; `make e2e` запускает только их), в CI — шаг `make e2e`.
Попутно: регулярка цели `help` не пропускала цифры в именах целей (`e2e`) — исправлено в обоих `Makefile` и в примере в `AGENTS.md`.

- [x] templ (CLI и `github.com/a-h/templ` одной версии), `internal/web/`: layout, шапка, подвал, страница 404
- [x] `static/`: чистый CSS, `htmx.min.js` с версией в имени файла, встраивание через `embed`
- [x] Миграция `pages` в `db/migrations/`; `internal/content/pages/`: sqlc-запросы, хендлеры, templ; маршруты `/` (страница со slug `home`) и `/{slug}`
- [x] `richtext`: goldmark + санитайзер `bluemonday` (политика UGC)
- [x] Seed: страницы «Главная», «О проекте», «Контакты»; в одной — Markdown с таблицей, кодом, ссылками и строкой `<script>alert(1)</script>` (проверка санитайзера)
- [x] `reference/site.yaml` — тип `pages` соответствует миграции
- [x] e2e (`httptest` + goquery): главная, страница по slug, 404, отсутствие `<script>` в выводе; цель `make e2e`

### Проверка
```sh
make reset && make dev
open http://localhost:8080/          # главная из seed, стили применены
open http://localhost:8080/about     # Markdown отрисован: таблица, код, ссылки
open http://localhost:8080/nope      # оформленная страница 404, статус 404
curl -s localhost:8080/contacts | grep -c '<script>alert'   # 0
curl -sI localhost:8080/static/htmx-2.0.11.min.js          # 200, из embed
make e2e
```

---

## Этап 4. Авторизация и оболочка админки

Цель: вход в админку одним пользователем («простой режим»).

- [ ] Миграция: `users` (одна таблица, поле роли зарезервировано на будущее), `sessions`
- [ ] Хеширование паролей (bcrypt/argon2 из `golang.org/x/crypto` — согласовать), cookie `HttpOnly`, `SameSite=Lax`, `Secure` вне dev
- [ ] CSRF-защита форм, ограничение попыток входа
- [ ] `internal/admin/`: вход, выход, оболочка (меню, дашборд со счётчиками контента)
- [ ] Подкоманда `site user create` / `make user-create EMAIL=...` (пароль спрашивается интерактивно)
- [ ] Seed: `admin@example.com` / `admin` — только dev, печатается в лог при `make seed`
- [ ] e2e: редирект неавторизованного, вход, неверный пароль, выход, запрет POST без CSRF-токена

### Проверка
```sh
make reset && make dev
open http://localhost:8080/admin      # редирект на /admin/login
# войти admin@example.com / admin → дашборд «Страниц: 3»
# неверный пароль → сообщение об ошибке, после N попыток — временная блокировка
# «Выйти» → снова /admin/login
curl -s -X POST localhost:8080/admin/pages -d 'title=x'    # 403 или редирект на вход
make e2e
```

---

## Этап 5. CRUD страниц в админке и превью Markdown (htmx)

Цель: страницы редактируются через админку, изменения сразу видны на сайте.

- [ ] Список, создание, редактирование, удаление страниц; валидация обязательных полей и уникальности slug
- [ ] Генерация slug из заголовка (транслитерация кириллицы)
- [ ] Редактор `richtext`: textarea + превью через htmx (`hx-post` с задержкой)
- [ ] Ошибки валидации отдаются фрагментом при `HX-Request`, полной страницей без него
- [ ] e2e: создание страницы и её появление на сайте, ошибка дублирующегося slug, превью с заголовком `HX-Request`
- [ ] chromedp: браузерный тест превью; цель `make e2e-browser`

### Проверка
```sh
make reset && make dev
# /admin/pages → «Создать»: заголовок «Тестовая», текст **жирный**
#   → превью обновляется при наборе
#   → «Сохранить» → открыть http://localhost:8080/testovaya — страница видна
# создать вторую страницу со slug testovaya → ошибка у поля, введённые данные не потеряны
# удалить страницу → /testovaya отдаёт 404
make e2e && make e2e-browser
```

---

## Этап 6. Модуль `images`: обработка и хранилище

Цель: ядро модуля работает и даёт наглядный результат без сайта.

- [ ] `modules/images/go.mod`, публичный API минимален, остальное в `internal/`
- [ ] Декодирование JPEG, PNG, WebP; отказ для HEIC и прочих форматов с понятной ошибкой
- [ ] Лимиты: 20 МБ и 40 мегапикселей — размеры проверяются по заголовку **до** полного декодирования
- [ ] Ориентация EXIF применяется, затем EXIF удаляется (парсер ориентации — свой минимальный или внешний, согласовать)
- [ ] Профиль v1: ширины 480/960/1600/2400 без увеличения, WebP q=80 (`gen2brain/webp`), JPEG q=82, ресайз `x/image/draw`
- [ ] Интерфейс `Storage` + реализация на локальном диске; оригинал сохраняется всегда
- [ ] `testdata/`: фото с каждой из 8 ориентаций EXIF, PNG с прозрачностью, маленькое изображение (без увеличения), «бомба» с огромными размерами в заголовке
- [ ] Демо: `make images-demo IN=<файл или папка>` → `modules/images/demo-out/index.html` — таблица вариантов: превью, ширина, формат, размер в КБ, наличие EXIF

### Проверка
```sh
make test MODULE=images
make images-demo IN=modules/images/testdata
open modules/images/demo-out/index.html
# все 8 ориентаций на превью стоят правильно; EXIF: нет
# у маленького изображения нет вариантов шире оригинала
# «бомба» и HEIC в отчёте помечены как отклонённые, с причиной
make dev   # сайт по-прежнему работает (модуль ещё не подключён)
```

---

## Этап 7. Модуль `images`: миграции, сервис, повторная обработка, testkit, релиз v0.1.0

Цель: модуль готов к подключению сайтом и опубликован с тегом.

- [ ] `migrations/` через `embed.FS`, таблица версий `goose_images_version`; у записи файла хранится версия профиля
- [ ] Сервис: загрузка, получение метаданных и вариантов, удаление; `http.Handler` для отдачи файлов
- [ ] Повторная обработка «всё с версией профиля ниже N»: идемпотентна, безопасна для прерывания (тест с отменой контекста посередине и повторным запуском)
- [ ] S3-совместимое хранилище — за тем же интерфейсом (внешняя зависимость — согласовать; допустимо отложить до появления потребности)
- [ ] `testkit/`: контрактные тесты для сайта (загрузка → варианты доступны через handler → повторная обработка ничего не ломает)
- [ ] `CHANGELOG.md`, `UPGRADE.md`
- [ ] Цель `make release MODULE=images VERSION=v0.1.0`: проверяет CHANGELOG, UPGRADE, чистое дерево, зелёный CI; ставит тег `modules/images/v0.1.0`
- [ ] `make deps-check` (`go list -m -u all` + govulncheck)
- [ ] Релиз `spec v0.1.0` (`make release MODULE=spec VERSION=v0.1.0`); в `reference/Makefile` — цель `spec-validate` через `go run github.com/keelkit/keelkit/spec/cmd/spec-validate@v0.1.0`
- [ ] Создать репозиторий `github.com/keelkit/keelkit` и запушить (**согласовать с человеком**: публичная публикация)

### Проверка
```sh
make test MODULE=images && make test-isolated MODULE=images
make deps-check                                # нет уязвимостей
make release MODULE=images VERSION=v0.1.0 DRY_RUN=1   # показывает, что сделает, и что все проверки пройдены
# после пуша тега:
GOWORK=off GOFLAGS=-mod=mod go list -m -versions github.com/keelkit/keelkit/modules/images   # v0.1.0
make images-demo IN=modules/images/testdata   # демо-страница работает как на этапе 6
```

---

## Этап 8. Подключение `images` к эталонному сайту

Цель: загрузка изображений через админку, отдача адаптивных картинок на сайте.

- [ ] `reference/go.mod` — `modules/images v0.1.0` без `replace`; `site.yaml`: `modules: { images: v0 }`
- [ ] `site migrate` применяет миграции модуля, затем сайта; `/healthz` показывает обе версии схемы
- [ ] Маршрут `/media/...` → handler модуля
- [ ] templ-компонент `<picture>` с `srcset` (WebP + JPEG-фолбэк) в `internal/web/`
- [ ] Админка `/admin/media`: загрузка (htmx), сетка превью, удаление; ошибки лимитов показываются пользователю
- [ ] Цель `make reprocess-images BELOW=<N>`
- [ ] Seed: 4 изображения из `testdata` модуля (или `reference/seed/`), одно — вставлено в страницу «О проекте»
- [ ] Запуск `testkit` модуля в тестах сайта; e2e сценарий «загрузка изображения»
- [ ] CI для `reference/` с `GOWORK=off`

### Проверка
```sh
make reset && make dev
curl -s localhost:8080/healthz
# {"schema":{"images":{"version":1,"pending":0},"site":{"version":4,"pending":0}}}
open http://localhost:8080/admin/media      # 4 изображения из seed
# загрузить свой JPEG → появился в сетке; загрузить файл 25 МБ → понятная ошибка
open http://localhost:8080/about            # картинка; в DevTools → Network грузится .webp нужной ширины
make reprocess-images BELOW=2 && make reprocess-images BELOW=2   # второй запуск: «обработано 0»
cd reference && GOWORK=off make test e2e    # на опубликованной версии модуля
```

---

## Этап 9. Блог: категории и посты

Цель: полноценный блог с черновиками и отложенной публикацией.

- [ ] `site.yaml`: `posts` (title, slug, body, cover: image, category: ref) с features `publish_date, draft, rss, sitemap`; `categories` (name, slug)
- [ ] `internal/content/categories/`, `internal/content/posts/`: миграции, sqlc, хендлеры, templ
- [ ] Публично: `/blog` (пагинация), `/blog/{slug}`, `/blog/category/{slug}`; видны только опубликованные с `publish_date <= now`
- [ ] Админка: CRUD категорий и постов, выбор обложки из медиатеки, флаг черновика, дата публикации, предпросмотр черновика
- [ ] Seed: 3 категории, 15 постов (обложки у части), из них 1 черновик и 1 с датой в будущем
- [ ] e2e: список, пагинация, фильтр по категории, черновик и будущий пост не видны публично, сценарий «публикация» (черновик → снять флаг → пост появился)

### Проверка
```sh
make reset && make dev
open http://localhost:8080/blog              # 13 постов на 2 страницах, обложки адаптивные
open http://localhost:8080/blog/category/go  # только посты категории
curl -s -o /dev/null -w '%{http_code}' localhost:8080/blog/draft-post    # 404
# /admin → посты: черновик помечен, «Предпросмотр» показывает его
# снять флаг «черновик» → пост появился в /blog
make spec-validate FILE=reference/site.yaml && make e2e
```

---

## Этап 10. SEO, sitemap, RSS

Цель: сайт готов к индексации.

- [ ] `<title>`, `description`, canonical, Open Graph (с обложкой поста) в layout
- [ ] `/sitemap.xml` — страницы и типы с feature `sitemap`; `/rss.xml` — типы с feature `rss`
- [ ] `/robots.txt` со ссылкой на sitemap; базовый URL сайта из env (`BASE_URL`)
- [ ] e2e: черновики и будущие посты отсутствуют в sitemap и RSS; XML корректен

### Проверка
```sh
make dev
curl -s localhost:8080/sitemap.xml | xmllint --noout -   # валидный XML
curl -s localhost:8080/rss.xml | xmllint --noout -
curl -s localhost:8080/sitemap.xml | grep -c draft-post  # 0
curl -s localhost:8080/robots.txt
curl -s localhost:8080/blog/<slug> | grep -E 'og:|canonical'
make e2e
```

---

## Этап 11. Эксплуатация и полный CI

Цель: сайт можно развернуть, резервировать и безопасно обновлять.

- [ ] `make build` — один статический бинарник (`CGO_ENABLED=0`), версия вшита через `-ldflags`
- [ ] `make backup` — снимок БД + архив загрузок; пример cron-строки в `reference/AGENTS.md`
- [ ] `deploy/site.service` (systemd), опциональный `Dockerfile`; описание litestream как опции
- [ ] CI: path-фильтры (`modules/<X>` и `spec/` → джобы модуля и `reference/`), `lint`, `test`, `test-isolated`, `generate-check`, `deps-check`, для `reference/` — ещё `e2e`, всё с `GOWORK=off`
- [ ] Тест миграции на копии реалистичных данных (seed + сгенерированные 1000 постов) — заготовка для будущих миграций данных

### Проверка
```sh
make build && file reference/bin/site        # статический бинарник
DATA_DIR=/tmp/kk ./reference/bin/site migrate && DATA_DIR=/tmp/kk ./reference/bin/site serve
curl -s localhost:8080/healthz               # version = git-хеш/тег, не "dev"
make backup && ls reference/backups/         # .db и .tar.gz
docker build -t keelkit-reference reference/ && docker run -p 8080:8080 keelkit-reference   # опционально
# PR с правкой только в reference/ → в Actions не запускаются джобы modules/images
```

---

## Этап 12. Скиллы `create-site` и `upgrade-site` с эвалами

Цель: агент создаёт новый сайт по спецификации и обновляет существующий.

- [ ] `.claude-plugin/marketplace.json` и манифест плагина; версия плагина
- [ ] `skills/create-site/`: вопросы → `site.yaml` → проверка `spec-validate` → копирование устройства `reference/` → генерация типов контента → `make generate test e2e`
- [ ] `skills/upgrade-site/`: поднять версию модуля → применить шаги из `UPGRADE.md` → `make migrate` на копии → тесты → PR
- [ ] Для эвала `upgrade-site` — выпустить `images v0.2.0` с изменением, требующим действий (например, новая версия профиля и запись в `UPGRADE.md`)
- [ ] `skills/evals/`: по `spec/examples/*.yaml` — сайт собирается, тесты проходят, структура совпадает с конвенциями (скрипт-проверка структуры папок и целей `Makefile`)
- [ ] CI: джоба эвалов через `workflow_dispatch`

### Проверка
```sh
# в Claude Code:
/plugin marketplace add ./
/plugin install keelkit
# запустить create-site со spec/examples/landing.yaml в пустой папке /tmp/kk-landing
cd /tmp/kk-landing && make dev    # сайт открывается на :8080, seed заполнен
make evals SKILL=create-site      # отчёт: собралось / тесты / конвенции — по каждому примеру
make evals SKILL=upgrade-site     # копия reference на v0.1.0 → обновлена до v0.2.0, тесты зелёные
```

---

## Этап 13. Второй сайт и итоги первого этапа

Цель: подтвердить переиспользование на реальном сайте в отдельном репозитории.

- [ ] Создать второй сайт через `create-site` в отдельном репозитории
- [ ] Запустить на нём `upgrade-site` при выходе новой версии модуля
- [ ] Ретроспектива (записать в `AGENTS.md` или отдельный документ): что переиспользовалось, где агент отклонился от конвенций, что вынести в модули (авторизация, SEO/sitemap/RSS), что изменить перед новостями и каталогом
- [ ] Решение с человеком о `v1.0.0` модулей

### Проверка
- Второй сайт: `make dev` открывается, `make test e2e` зелёные, в `go.mod` — опубликованная версия `images` без `replace`.
- Список расхождений со структурой `reference/` пуст или каждое расхождение объяснено.
- Итоги ретроспективы записаны и согласованы.

---

## Зависимости к согласованию

`AGENTS.md` уже закрепляет: `modernc.org/sqlite`, goose, sqlc, templ, htmx, goldmark, `golang.org/x/image`, `gen2brain/webp`, goquery, chromedp, golangci-lint, govulncheck.
Перед добавлением остальных нужно решение человека (и проверки из раздела «Зависимости»):

- [x] Санитайзер HTML для `richtext` — этап 3. Решено: `microcosm-cc/bluemonday` v1.0.27 (последний релиз 2024-07, репозиторий живой, BSD-3)
- [x] YAML-парсер и валидатор JSON Schema — этап 1. Решено: `go.yaml.in/yaml/v3` (преемник архивного `gopkg.in/yaml.v3`) и `santhosh-tekuri/jsonschema/v6`
- [ ] Хеширование паролей: `golang.org/x/crypto` (bcrypt или argon2id) — этап 4
- [ ] Чтение ориентации EXIF: свой минимальный парсер или внешняя библиотека — этап 6
- [ ] Клиент S3 (кандидат: `minio/minio-go`) или отложить S3 — этап 7
- [ ] Транслитерация для slug: своя таблица или библиотека — этап 5

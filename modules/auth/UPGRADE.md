# UPGRADE — modules/auth

Инструкции для агентов: что менять в коде сайта при переходе на версию. Версии без действий здесь не перечисляются.
Каждый раздел: что изменилось → изменения в коде сайта → проверка.

## v0.1.0 — первое подключение

**Что изменилось.** Ядро авторизации из `internal/admin` сайта (пароли argon2id, сессии и cookie, CSRF-токены,
ограничение попыток входа, `RequireUser`, создание пользователя) переехало в модуль. Страница входа, оболочка
админки, меню и страницы ошибок остаются в коде сайта. Таблицы `users` и `sessions` переходят к модулю
(таблица версий `goose_auth_version`); схема та же, поэтому данные не переносятся, пользователи и сессии сохраняются.

**Изменения в коде сайта** (образец — `reference/`, коммит перехода на `auth v0.1.0`):
1. `go get github.com/voffkin/keelkit/modules/auth@v0.1.0` (через `make`: нужны `GOPRIVATE` и SSH, см. `AGENTS.md`);
   в `site.yaml` — `modules: { …, auth: v0 }` (валидатор `spec` с v0.4.0).
2. Миграции: в источники миграций (`migrationSources` в `cmd/site/main.go`) — `{Name: "auth", FS: authmigrations.FS,
   Table: authmigrations.Table}` (пакет `github.com/voffkin/keelkit/modules/auth/migrations`) в порядке `site.yaml`,
   до миграций сайта. В миграции сайта, создающей `users` и `sessions` (в эталоне `00003_users_sessions.sql`), —
   `CREATE TABLE IF NOT EXISTS` и `CREATE INDEX IF NOT EXISTS`: на новой базе таблицы уже создал модуль.
   На существующей базе эта миграция давно применена и повторно не выполняется, а миграция модуля проходит поверх
   существующих таблиц, ничего не меняя. Схема в миграции сайта должна совпадать с
   `modules/auth/migrations/00001_auth.sql`; если сайт её менял — остановиться и согласовать с человеком.
3. Удалить из `internal/admin` код, который теперь в модуле: хеширование паролей, ограничитель попыток, сессии,
   cookie, CSRF, `safeNext`, sqlc-запросы к `users`/`sessions` (`queries.sql`, сгенерированный код, запись в `sqlc.yaml`)
   и их тесты. Сайт к таблицам `users`/`sessions` больше не обращается — только через `auth.Service`.
4. Оболочка админки создаёт `auth.NewService(db, auth.Config{…})`:
   `LoginPath` — путь страницы входа; `InsecureCookies: !COOKIE_SECURE` (по умолчанию cookie `Secure`);
   `Forbidden` — страница 403 админки; `ServerError` — страница 500 админки; `Now` — только для тестов.
   - Форма входа (GET): `CurrentUser(r)` — вошедшего на дашборд; токен — `LoginCSRF(w, r)` в скрытое поле
     `auth.CSRFField`; адрес возврата — `auth.SafeNext(next, "/admin")`. `LoginCSRF` ставит cookie: вызывать до
     записи статуса ответа.
   - Вход (POST): ограничить тело (`http.MaxBytesReader`), `ParseForm`, затем `Login(w, r, email, password)`.
     Ошибки в форме: `auth.ErrLoginCSRF` — 403 «Форма устарела», `*auth.LockedError` (`errors.As`; `RetryAfter`,
     заголовок `Retry-After` модуль ставит сам) — 429, `auth.ErrBadCredentials` — 422 «Неверный email или пароль»;
     прочие — 500. Перед повторным показом формы — новый `LoginCSRF(w, r)`.
   - Выход (POST внутри `RequireUser`): `Logout(w, r)`.
   - `RequireUser` модуля — вокруг всех маршрутов админки, кроме входа; данные оболочки (меню, текущий путь), которые
     сайт раньше клал в сессию, — своим middleware внутри него. `http.CrossOriginProtection` снаружи — как раньше.
   - В шаблонах: пользователь — `auth.UserFrom(ctx)`, токен — `auth.CSRFToken(ctx)` в поле `auth.CSRFField` и в
     `hx-headers` с заголовком `auth.CSRFHeader`.
5. `site user create`: `auth.CheckEmail` до запроса пароля, `auth.CheckPasswordPolicy`, затем
   `auth.NewService(db, auth.Config{}).CreateUser(ctx, email, password)`; `auth.ErrUserExists` — пользователь есть.
   Seed — тоже `CreateUser`, пропуская `ErrUserExists`. Пароль dev-пользователя должен проходить политику (от 8
   символов; в эталоне `admin@example.com` / `adminadmin`).
6. Контрактный тест: `testkit.Run(t, setup)` (`modules/auth/testkit`) — сайт после миграций, пользователь через
   `CreateUser`, обработчик всего сайта с `Config.Now = now` и cookie `Secure`; пути входа, страницы админки с полем
   `auth.CSRFField` (кнопка выхода) и выхода. Образец — `reference/cmd/site/auth_test.go`.

**Проверка.** `make test` (контрактный тест, e2e входа и админки), `make test-isolated`, `make generate-check`,
`make e2e-browser`; `/healthz` — в `schema` есть `auth` с `pending: 0`; на копии живой базы `make migrate` проходит,
прежние пользователи входят со своими паролями, действующие сессии не сбрасываются.

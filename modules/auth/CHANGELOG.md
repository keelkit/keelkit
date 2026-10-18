# Changelog — modules/auth

Формат: [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии — semver (теги `modules/auth/vX.Y.Z`).
Что делать сайту при обновлении — в [UPGRADE.md](UPGRADE.md).

## [Unreleased]

## [0.3.0] — 2026-10-18

### Added
- `Service.SetPassword(ctx, email, password)` — смена пароля: та же политика, что у `CreateUser`, все сессии
  пользователя удаляются (вошедшие с прежним паролем разлогиниваются). Ошибки: `ErrPasswordLength`,
  новая `ErrUserNotFound`.
- `testkit.Site.SetPassword` — контрактный тест смены пароля (старая сессия завершена, старый пароль не входит,
  новый входит); без поля тест пропускается.

## [0.2.0] — 2026-10-14

Репозиторий переехал в `github.com/keelkit/keelkit` и стал публичным. Код и API — как в v0.1.0.

### Changed
- Путь модуля — `github.com/keelkit/keelkit/modules/auth` (был `github.com/voffkin/keelkit/modules/auth`); версии
  до v0.2.0 есть только под старым путём. Что сделать сайту — UPGRADE.md, раздел v0.2.0.

## [0.1.0] — 2026-10-13

Первый выпуск: ядро авторизации `reference/internal/admin` вынесено в модуль, чтобы исправления входа и сессий
доезжали до сайтов через `go get`. Страница входа и оболочка админки остаются в коде сайта.
Подключение к сайту — [UPGRADE.md](UPGRADE.md), раздел v0.1.0.

### Added
- `NewService(db, Config)`: `Login` (токен формы входа, ограничение попыток, пароль, новая сессия),
  `Logout`, `CurrentUser`, `RequireUser` (сессия, CSRF-токен для небезопасных методов, `Cache-Control: no-store`),
  `LoginCSRF` — токен формы входа (double submit cookie), `CreateUser` (проверка email и политики пароля).
- `UserFrom`, `CSRFToken` — пользователь и токен сессии из контекста внутри `RequireUser`;
  `CSRFField`, `CSRFHeader` — где запрос передаёт токен.
- `SafeNext(next, base)` — адрес возврата после входа без открытого редиректа; `NormalizeEmail`, `CheckEmail`, `CheckPasswordPolicy`.
- Пароли — argon2id (m=64 МиБ, t=3, p=4) в формате PHC, пересчёт хеша со старыми параметрами при входе,
  не больше двух вычислений одновременно; сверка с фиктивным хешем для неизвестного email.
- Cookie `HttpOnly`, `SameSite=Lax`, `Secure` (снимается только `Config.InsecureCookies`); в БД — SHA-256 токена сессии.
- `testkit.Run` — контрактные тесты для сайта: по его настоящим страницам входа и админки (HTTPS, cookie)
  проверяются редирект на вход, вход и выход, флаги cookie сессии, отказ без CSRF-токена (форма входа и POST
  в админке), блокировка после неудачных попыток, истечение сессии; время сайта задаёт testkit (`Config.Now`).
- `SessionCookie` — имя cookie сессии.
- Миграция `00001_auth.sql` (таблица версий `goose_auth_version`): `users`, `sessions` с `IF NOT EXISTS` —
  на базе, где их создала миграция сайта, ничего не делает.

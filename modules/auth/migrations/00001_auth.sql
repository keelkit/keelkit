-- +goose Up
-- Таблицы users и sessions появились в миграции сайта 00003 (до выноса авторизации в модуль),
-- поэтому здесь IF NOT EXISTS: на базе, созданной до модуля, миграция ничего не делает.
-- Схема должна совпадать с той миграцией сайта.

-- Пользователи админки. «Простой режим»: любой пользователь — администратор.
-- role зарезервирована под роли; без CHECK, чтобы новые роли не требовали пересборки таблицы.
-- email хранится в нижнем регистре (нормализует код), пароль — хеш argon2id в формате PHC.
CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE CHECK (email <> '' AND email = lower(email)),
    password_hash TEXT NOT NULL CHECK (password_hash <> ''),
    role          TEXT NOT NULL DEFAULT 'admin',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

-- Сессии входа. В cookie — случайный токен, здесь — только его SHA-256: копия БД не даёт войти.
-- csrf_token — токен форм этой сессии; expires_at — UTC RFC 3339, сравнивается как строка.
CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE INDEX IF NOT EXISTS sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expires_at ON sessions (expires_at);

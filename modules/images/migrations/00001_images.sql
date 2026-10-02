-- +goose Up
-- Таблицы модуля images. Все имена начинаются с images_, чтобы не пересекаться с таблицами сайта.
-- Время — TEXT в UTC (RFC 3339), как у сайта.

-- Загруженное изображение. id — случайная строка: адреса картинок не перебираются по порядку.
-- Размеры — с учётом ориентации EXIF; bytes — размер оригинала.
-- profile_version — версия профиля, которым построены текущие варианты.
CREATE TABLE images_files (
    id              TEXT PRIMARY KEY CHECK (id <> ''),
    name            TEXT NOT NULL DEFAULT '',
    format          TEXT NOT NULL CHECK (format IN ('jpeg', 'png', 'webp')),
    width           INTEGER NOT NULL CHECK (width > 0),
    height          INTEGER NOT NULL CHECK (height > 0),
    bytes           INTEGER NOT NULL CHECK (bytes > 0),
    original_key    TEXT NOT NULL UNIQUE,
    profile_version INTEGER NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE INDEX images_files_profile_version ON images_files (profile_version);
CREATE INDEX images_files_created_at ON images_files (created_at);

-- Варианты текущего профиля. Модуль удаляет их сам, не полагаясь на PRAGMA foreign_keys.
CREATE TABLE images_variants (
    image_id TEXT NOT NULL REFERENCES images_files (id) ON DELETE CASCADE,
    format   TEXT NOT NULL CHECK (format IN ('jpeg', 'webp')),
    width    INTEGER NOT NULL CHECK (width > 0),
    height   INTEGER NOT NULL CHECK (height > 0),
    bytes    INTEGER NOT NULL CHECK (bytes > 0),
    key      TEXT NOT NULL UNIQUE,
    PRIMARY KEY (image_id, format, width)
) STRICT;

-- Файлы хранилища, на которые больше ничего не ссылается (удалённые изображения, варианты старого профиля).
-- Ключ попадает сюда в той же транзакции, что и изменение строк; файл удаляется после неё.
-- Если процесс прервался, удаление доделает следующий вызов Delete или Reprocess.
CREATE TABLE images_trash (
    key TEXT PRIMARY KEY
) STRICT;

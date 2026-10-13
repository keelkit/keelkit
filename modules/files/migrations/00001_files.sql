-- +goose Up
-- Таблицы модуля files. Все имена начинаются с files_, чтобы не пересекаться с таблицами сайта.
-- Время — TEXT в UTC (RFC 3339), как у сайта.

-- Загруженный файл. id — случайная строка: адреса файлов не перебираются по порядку.
-- url_name — имя в адресе (<id>/<url_name>), задаётся при загрузке и не меняется: замена файла адрес не меняет.
-- name — исходное имя последней загрузки, для админки. key — текущий файл в хранилище: замена пишет новый
-- ключ, а старый уходит в files_trash, поэтому читатель никогда не видит файл наполовину замененным.
CREATE TABLE files_files (
    id         TEXT PRIMARY KEY CHECK (id <> ''),
    name       TEXT NOT NULL DEFAULT '',
    url_name   TEXT NOT NULL CHECK (url_name <> ''),
    format     TEXT NOT NULL CHECK (format IN ('pdf', 'mp4', 'webm', 'svg')),
    bytes      INTEGER NOT NULL CHECK (bytes > 0),
    key        TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE INDEX files_files_created_at ON files_files (created_at);

-- Файлы хранилища, на которые больше ничего не ссылается (удалённые и замененные).
-- Ключ попадает сюда в той же транзакции, что и изменение строк; файл удаляется после неё.
-- Если процесс прервался, удаление доделает следующий вызов Replace или Delete.
CREATE TABLE files_trash (
    key TEXT PRIMARY KEY
) STRICT;

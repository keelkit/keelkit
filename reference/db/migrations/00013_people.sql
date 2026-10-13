-- +goose Up
-- Люди (этап 10.5): тип контента без своего адреса, на сайте его выводит блок people на любой странице.
-- Только новые таблицы, существующие данные не меняются.
--
-- Без черновика, как категории: правки сразу на сайте. sort_order — ручной порядок (feature order): кнопки ↑ ↓
-- в списке админки переписывают его у всех записей одной транзакцией; новая запись — в конец.
-- photo — без ON DELETE: используемое изображение медиатека не удалит.
CREATE TABLE people (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL CHECK (name <> ''),
    role       TEXT NOT NULL DEFAULT '',
    photo      TEXT REFERENCES images_files (id),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE INDEX people_sort_order ON people (sort_order, id);
CREATE INDEX people_photo ON people (photo);

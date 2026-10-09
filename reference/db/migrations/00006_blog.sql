-- +goose Up
-- Блог (этап 9): категории и посты. Только новые таблицы, существующие данные не меняются.
--
-- categories — без черновика: категория — справочник, её правки видны на сайте сразу.
CREATE TABLE categories (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL CHECK (name <> ''),
    slug       TEXT NOT NULL UNIQUE CHECK (slug <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

-- Посты — черновик-копия, как у страниц (00005): posts — идентичность, post_contents — черновик (есть всегда)
-- и опубликованная копия (если пост на сайте).
CREATE TABLE posts (
    id         INTEGER PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

-- cover и category_id необязательны (NULL — не выбрано); ссылки без ON DELETE: используемое изображение модуль
-- не удалит, используемую категорию — тоже (ошибка внешнего ключа, админка отвечает 409 со списком постов).
-- publish_date — UTC RFC 3339 ('2026-10-09T10:00:00Z'): строки этого вида сравниваются как время.
-- Пустая у черновика — «в момент публикации»; у опубликованной копии дата есть всегда.
CREATE TABLE post_contents (
    post_id      INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('draft', 'published')),
    title        TEXT NOT NULL CHECK (title <> ''),
    slug         TEXT NOT NULL CHECK (slug <> ''),
    cover        TEXT REFERENCES images_files (id),
    category_id  INTEGER REFERENCES categories (id),
    publish_date TEXT NOT NULL DEFAULT '',
    blocks       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(blocks) AND json_type(blocks) = 'array'),
    PRIMARY KEY (post_id, kind),
    CHECK (kind = 'draft' OR publish_date <> '')
) STRICT;

CREATE UNIQUE INDEX post_contents_published_slug ON post_contents (slug) WHERE kind = 'published';
CREATE UNIQUE INDEX post_contents_draft_slug ON post_contents (slug) WHERE kind = 'draft';
-- Лента блога: опубликованные по дате, новые сверху.
CREATE INDEX post_contents_published_date ON post_contents (publish_date) WHERE kind = 'published';
CREATE INDEX post_contents_category_id ON post_contents (category_id);

-- Изображения копии поста: обложка и картинки блоков. Перезаписывается в той же транзакции, что и копия.
CREATE TABLE post_images (
    post_id  INTEGER NOT NULL,
    kind     TEXT NOT NULL,
    image_id TEXT NOT NULL REFERENCES images_files (id),
    PRIMARY KEY (post_id, kind, image_id),
    FOREIGN KEY (post_id, kind) REFERENCES post_contents (post_id, kind) ON DELETE CASCADE
) STRICT;

CREATE INDEX post_images_image_id ON post_images (image_id);

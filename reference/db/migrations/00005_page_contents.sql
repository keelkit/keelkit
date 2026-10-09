-- +goose Up
-- Страницы из блоков и черновик-копия (этап 8.5).
--
-- pages — только идентичность страницы; содержимое — page_contents: копия-черновик (kind = 'draft', есть всегда)
-- и опубликованная копия (kind = 'published', если страница на сайте). «Сохранить» пишет черновик, «Опубликовать»
-- копирует его в опубликованную, «Снять с публикации» удаляет опубликованную. slug — часть версии.
-- blocks — JSON-массив блоков (internal/blocks), порядок элементов — порядок на странице.
--
-- Меняет живые данные. Двухэтапно (AGENTS.md, «Миграции»): прежняя таблица переименовывается в pages_legacy
-- и остаётся нетронутой до следующей версии, которая её удалит. Переименование, а не удаление колонок:
-- у title и slug ограничения NOT NULL, CHECK и UNIQUE, с ними новые страницы в старую таблицу не записать.
-- На pages до этой миграции никто не ссылается, поэтому переименование ничего не задевает.
ALTER TABLE pages RENAME TO pages_legacy;

CREATE TABLE pages (
    id         INTEGER PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE TABLE page_contents (
    page_id INTEGER NOT NULL REFERENCES pages (id) ON DELETE CASCADE,
    kind    TEXT NOT NULL CHECK (kind IN ('draft', 'published')),
    title   TEXT NOT NULL CHECK (title <> ''),
    slug    TEXT NOT NULL CHECK (slug <> ''),
    blocks  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(blocks) AND json_type(blocks) = 'array'),
    PRIMARY KEY (page_id, kind)
) STRICT;

-- slug уникален среди опубликованных и отдельно среди черновиков: у черновика опубликованной страницы
-- обычно тот же slug, что у её опубликованной копии.
CREATE UNIQUE INDEX page_contents_published_slug ON page_contents (slug) WHERE kind = 'published';
CREATE UNIQUE INDEX page_contents_draft_slug ON page_contents (slug) WHERE kind = 'draft';

-- Изображения, на которые ссылаются блоки копии. Перезаписывается в той же транзакции, что и копия.
-- Ссылка на images_files без ON DELETE: используемое изображение модуль не удалит (ошибка внешнего ключа).
CREATE TABLE page_images (
    page_id  INTEGER NOT NULL,
    kind     TEXT NOT NULL,
    image_id TEXT NOT NULL REFERENCES images_files (id),
    PRIMARY KEY (page_id, kind, image_id),
    FOREIGN KEY (page_id, kind) REFERENCES page_contents (page_id, kind) ON DELETE CASCADE
) STRICT;

CREATE INDEX page_images_image_id ON page_images (image_id);

-- Перенос: каждая страница — черновик с одним блоком text из прежнего body (пустой body — без блоков);
-- опубликованная (draft = 0) получает ещё и опубликованную копию, равную черновику (тот же id блока).
INSERT INTO pages (id, created_at, updated_at)
SELECT id, created_at, updated_at FROM pages_legacy;

INSERT INTO page_contents (page_id, kind, title, slug, blocks)
SELECT id, 'draft', title, slug,
       CASE WHEN body = '' THEN '[]'
            ELSE json_array(json_object('id', lower(hex(randomblob(6))), 'type', 'text', 'body', body))
       END
FROM pages_legacy;

INSERT INTO page_contents (page_id, kind, title, slug, blocks)
SELECT c.page_id, 'published', c.title, c.slug, c.blocks
FROM page_contents c JOIN pages_legacy l ON l.id = c.page_id
WHERE c.kind = 'draft' AND l.draft = 0;

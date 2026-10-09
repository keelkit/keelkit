-- +goose Up
-- Редиректы со старых адресов (этап 10). Только новые таблицы, существующие данные не меняются.
--
-- slug — часть опубликованной копии (00005, 00006): «Опубликовать» с новым slug меняет адрес на сайте.
-- Прежний опубликованный slug запоминается здесь (при публикации и при снятии с публикации), и сайт отвечает
-- 301 со старого адреса на текущий опубликованный. Запись со slug, который снова опубликован (той же или другой
-- записью), удаляется при публикации: опубликованная копия всегда важнее редиректа, циклов не бывает.
-- Удаление записи удаляет и её редиректы.
CREATE TABLE page_redirects (
    slug    TEXT PRIMARY KEY CHECK (slug <> ''),
    page_id INTEGER NOT NULL REFERENCES pages (id) ON DELETE CASCADE
) STRICT;

CREATE INDEX page_redirects_page_id ON page_redirects (page_id);

CREATE TABLE post_redirects (
    slug    TEXT PRIMARY KEY CHECK (slug <> ''),
    post_id INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE
) STRICT;

CREATE INDEX post_redirects_post_id ON post_redirects (post_id);

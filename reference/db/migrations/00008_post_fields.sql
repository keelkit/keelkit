-- +goose Up
-- Поля постов (этап 10.5): лид, ручной анонс, alt обложки. Только новые колонки с пустым значением по умолчанию:
-- существующие данные не меняются, у старых постов поля пусты (анонс — начало текста, обложка декоративная).
--
-- lead — подзаголовок статьи под заголовком; excerpt — анонс для ленты, RSS и описания (пусто — начало текста
-- блоков); cover_alt — альтернативный текст обложки (пусто — обложка декоративная).
ALTER TABLE post_contents ADD COLUMN lead TEXT NOT NULL DEFAULT '';
ALTER TABLE post_contents ADD COLUMN excerpt TEXT NOT NULL DEFAULT '';
ALTER TABLE post_contents ADD COLUMN cover_alt TEXT NOT NULL DEFAULT '';

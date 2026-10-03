-- +goose Up
-- Feature draft у pages (site.yaml): черновик не виден на сайте и в меню, удалить можно только черновик.
-- Существующие страницы остаются опубликованными (DEFAULT 0); новые админка создаёт черновиками.
-- STRICT не допускает BOOLEAN: флаг — INTEGER 0/1, в Go — bool (override в sqlc.yaml).
ALTER TABLE pages ADD COLUMN draft INTEGER NOT NULL DEFAULT 0 CHECK (draft IN (0, 1));

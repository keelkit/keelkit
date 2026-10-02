-- +goose Up
-- Тип контента pages (site.yaml): title, slug, body (Markdown).
-- Необязательные текстовые поля — NOT NULL DEFAULT '', время — TEXT в UTC (RFC 3339).
CREATE TABLE pages (
    id         INTEGER PRIMARY KEY,
    title      TEXT NOT NULL CHECK (title <> ''),
    slug       TEXT NOT NULL UNIQUE CHECK (slug <> ''),
    body       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

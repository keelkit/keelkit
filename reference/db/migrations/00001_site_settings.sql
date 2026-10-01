-- +goose Up
-- Настройки сайта: ровно одна строка с id = 1.
CREATE TABLE site_settings (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT ''
) STRICT;

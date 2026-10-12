-- +goose Up
-- Ссылки на соцсети в настройках (этап 10.5) — первое поле-список записи (site.yaml: type: list, internal/listfield):
-- JSON-массив элементов {id, name, url} по порядку. Новая колонка с пустым списком: существующие данные не меняются.
ALTER TABLE site_settings ADD COLUMN social TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(social) AND json_type(social) = 'array');

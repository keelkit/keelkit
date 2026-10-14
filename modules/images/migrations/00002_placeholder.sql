-- +goose Up
-- Профиль v2: заглушка на время загрузки — WebP ~32 px в data URI. Пустая строка — заглушки нет: изображение
-- с прозрачностью или построенное профилем v1 (появится после повторной обработки до v2).
ALTER TABLE images_files ADD COLUMN placeholder TEXT NOT NULL DEFAULT '';

-- +goose Up
-- Настройки сайта (этап 10.5): картинка для соцсетей по умолчанию, контакты, баннер cookies, коды подтверждения
-- Яндекс.Вебмастера и Google Search Console. Только новые колонки с пустым значением: существующие данные
-- не меняются, пустое поле на сайте не выводится.
--
-- share_image — как обложка поста: NULL — не выбрана, ссылка без ON DELETE (используемое изображение не удалить).
ALTER TABLE site_settings ADD COLUMN share_image TEXT REFERENCES images_files (id);
ALTER TABLE site_settings ADD COLUMN phone TEXT NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN email TEXT NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN address TEXT NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN cookie_text TEXT NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN cookie_link TEXT NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN yandex_verification TEXT NOT NULL DEFAULT '';
ALTER TABLE site_settings ADD COLUMN google_verification TEXT NOT NULL DEFAULT '';

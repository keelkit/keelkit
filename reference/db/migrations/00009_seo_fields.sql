-- +goose Up
-- SEO записи вручную (этап 10.5): заголовок и описание для поисковиков, картинка для соцсетей — у типов со своим
-- адресом (страницы и посты). Поля — часть копии: правка SEO опубликованной записи видна после «Опубликовать».
-- Только новые колонки с пустым значением: существующие данные не меняются, у старых записей всё подставляется
-- автоматически, как раньше.
--
-- seo_image — как обложка поста: NULL — не выбрана, ссылка без ON DELETE (используемое изображение не удалить),
-- изображение попадает и в таблицу ссылок копии (page_images, post_images).
ALTER TABLE page_contents ADD COLUMN seo_title TEXT NOT NULL DEFAULT '';
ALTER TABLE page_contents ADD COLUMN seo_description TEXT NOT NULL DEFAULT '';
ALTER TABLE page_contents ADD COLUMN seo_image TEXT REFERENCES images_files (id);

ALTER TABLE post_contents ADD COLUMN seo_title TEXT NOT NULL DEFAULT '';
ALTER TABLE post_contents ADD COLUMN seo_description TEXT NOT NULL DEFAULT '';
ALTER TABLE post_contents ADD COLUMN seo_image TEXT REFERENCES images_files (id);

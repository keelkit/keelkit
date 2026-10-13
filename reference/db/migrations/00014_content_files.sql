-- +goose Up
-- Ссылки страниц и постов на файлы модуля files (блок file, этап 10.5). Только новые таблицы, существующие данные
-- не меняются. Таблица files_files создана миграцией модуля files, которая идёт раньше миграций сайта.
--
-- Как page_images и post_images: строки копии (draft, published) перезаписываются в той же транзакции, что и
-- копия; внешний ключ на files_files без ON DELETE не даёт удалить используемый файл.
CREATE TABLE page_files (
    page_id INTEGER NOT NULL,
    kind    TEXT NOT NULL,
    file_id TEXT NOT NULL REFERENCES files_files (id),
    PRIMARY KEY (page_id, kind, file_id),
    FOREIGN KEY (page_id, kind) REFERENCES page_contents (page_id, kind) ON DELETE CASCADE
) STRICT;

CREATE INDEX page_files_file_id ON page_files (file_id);

CREATE TABLE post_files (
    post_id INTEGER NOT NULL,
    kind    TEXT NOT NULL,
    file_id TEXT NOT NULL REFERENCES files_files (id),
    PRIMARY KEY (post_id, kind, file_id),
    FOREIGN KEY (post_id, kind) REFERENCES post_contents (post_id, kind) ON DELETE CASCADE
) STRICT;

CREATE INDEX post_files_file_id ON post_files (file_id);

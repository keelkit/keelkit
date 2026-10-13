-- +goose Up
-- Меню сайта (этап 10.5; site.yaml: menus, пакет internal/menu): пункты всех меню в одной таблице, menu — имя
-- меню из site.yaml (main, footer). Новая таблица: существующие данные не меняются. Черновика нет — правки сразу
-- на сайте.
--
-- Пункт ведёт ровно на одно из трёх:
--   page_id — страницу по id: адрес строится из её текущего slug, поэтому смена slug меню не ломает. Ссылка без
--             ON DELETE: страницу из меню не удалить (админка показывает, в каком меню она стоит);
--   url     — свой адрес: путь на сайте или http(s);
--   section — раздел сайта по ключу из кода (blog — лента блога); children = 1 — подпункты раздела выводятся сами
--             из его записей (у блога — категории).
-- title — название пункта; пусто — название страницы или раздела (у своего адреса обязательно).
-- Вложенность — один уровень: parent_id — пункт верхнего уровня того же меню (глубже не даёт админка);
-- position — порядок среди пунктов с тем же родителем.
CREATE TABLE menu_items (
    id        INTEGER PRIMARY KEY,
    menu      TEXT NOT NULL CHECK (menu <> ''),
    parent_id INTEGER REFERENCES menu_items (id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    title     TEXT NOT NULL DEFAULT '',
    page_id   INTEGER REFERENCES pages (id),
    url       TEXT NOT NULL DEFAULT '',
    section   TEXT NOT NULL DEFAULT '',
    children  INTEGER NOT NULL DEFAULT 0 CHECK (children IN (0, 1)),
    CHECK ((page_id IS NOT NULL) + (url <> '') + (section <> '') = 1),
    CHECK (url = '' OR title <> '')
) STRICT;

CREATE INDEX menu_items_menu ON menu_items (menu, position);
CREATE INDEX menu_items_parent_id ON menu_items (parent_id);
CREATE INDEX menu_items_page_id ON menu_items (page_id);

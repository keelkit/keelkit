-- Итоговая схема БД после всех миграций (модули и сайт). Не редактировать: make schema.
-- Источник правды — миграции в db/migrations/ и у модулей; таблицы версий goose не показаны.

CREATE TABLE categories (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL CHECK (name <> ''),
    slug       TEXT NOT NULL UNIQUE CHECK (slug <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE TABLE files_files (
    id         TEXT PRIMARY KEY CHECK (id <> ''),
    name       TEXT NOT NULL DEFAULT '',
    url_name   TEXT NOT NULL CHECK (url_name <> ''),
    format     TEXT NOT NULL CHECK (format IN ('pdf', 'mp4', 'webm', 'svg')),
    bytes      INTEGER NOT NULL CHECK (bytes > 0),
    key        TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE INDEX files_files_created_at ON files_files (created_at);

CREATE TABLE files_trash (
    key TEXT PRIMARY KEY
) STRICT;

CREATE TABLE images_files (
    id              TEXT PRIMARY KEY CHECK (id <> ''),
    name            TEXT NOT NULL DEFAULT '',
    format          TEXT NOT NULL CHECK (format IN ('jpeg', 'png', 'webp')),
    width           INTEGER NOT NULL CHECK (width > 0),
    height          INTEGER NOT NULL CHECK (height > 0),
    bytes           INTEGER NOT NULL CHECK (bytes > 0),
    original_key    TEXT NOT NULL UNIQUE,
    profile_version INTEGER NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE INDEX images_files_created_at ON images_files (created_at);

CREATE INDEX images_files_profile_version ON images_files (profile_version);

CREATE TABLE images_trash (
    key TEXT PRIMARY KEY
) STRICT;

CREATE TABLE images_variants (
    image_id TEXT NOT NULL REFERENCES images_files (id) ON DELETE CASCADE,
    format   TEXT NOT NULL CHECK (format IN ('jpeg', 'webp')),
    width    INTEGER NOT NULL CHECK (width > 0),
    height   INTEGER NOT NULL CHECK (height > 0),
    bytes    INTEGER NOT NULL CHECK (bytes > 0),
    key      TEXT NOT NULL UNIQUE,
    PRIMARY KEY (image_id, format, width)
) STRICT;

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

CREATE INDEX menu_items_page_id ON menu_items (page_id);

CREATE INDEX menu_items_parent_id ON menu_items (parent_id);

CREATE TABLE page_contents (
    page_id INTEGER NOT NULL REFERENCES pages (id) ON DELETE CASCADE,
    kind    TEXT NOT NULL CHECK (kind IN ('draft', 'published')),
    title   TEXT NOT NULL CHECK (title <> ''),
    slug    TEXT NOT NULL CHECK (slug <> ''),
    blocks  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(blocks) AND json_type(blocks) = 'array'), seo_title TEXT NOT NULL DEFAULT '', seo_description TEXT NOT NULL DEFAULT '', seo_image TEXT REFERENCES images_files (id),
    PRIMARY KEY (page_id, kind)
) STRICT;

CREATE UNIQUE INDEX page_contents_draft_slug ON page_contents (slug) WHERE kind = 'draft';

CREATE UNIQUE INDEX page_contents_published_slug ON page_contents (slug) WHERE kind = 'published';

CREATE TABLE page_files (
    page_id INTEGER NOT NULL,
    kind    TEXT NOT NULL,
    file_id TEXT NOT NULL REFERENCES files_files (id),
    PRIMARY KEY (page_id, kind, file_id),
    FOREIGN KEY (page_id, kind) REFERENCES page_contents (page_id, kind) ON DELETE CASCADE
) STRICT;

CREATE INDEX page_files_file_id ON page_files (file_id);

CREATE TABLE page_images (
    page_id  INTEGER NOT NULL,
    kind     TEXT NOT NULL,
    image_id TEXT NOT NULL REFERENCES images_files (id),
    PRIMARY KEY (page_id, kind, image_id),
    FOREIGN KEY (page_id, kind) REFERENCES page_contents (page_id, kind) ON DELETE CASCADE
) STRICT;

CREATE INDEX page_images_image_id ON page_images (image_id);

CREATE TABLE page_redirects (
    slug    TEXT PRIMARY KEY CHECK (slug <> ''),
    page_id INTEGER NOT NULL REFERENCES pages (id) ON DELETE CASCADE
) STRICT;

CREATE INDEX page_redirects_page_id ON page_redirects (page_id);

CREATE TABLE pages (
    id         INTEGER PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE TABLE "pages_legacy" (
    id         INTEGER PRIMARY KEY,
    title      TEXT NOT NULL CHECK (title <> ''),
    slug       TEXT NOT NULL UNIQUE CHECK (slug <> ''),
    body       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
, draft INTEGER NOT NULL DEFAULT 0 CHECK (draft IN (0, 1))) STRICT;

CREATE TABLE people (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL CHECK (name <> ''),
    role       TEXT NOT NULL DEFAULT '',
    photo      TEXT REFERENCES images_files (id),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE INDEX people_photo ON people (photo);

CREATE INDEX people_sort_order ON people (sort_order, id);

CREATE TABLE post_contents (
    post_id      INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('draft', 'published')),
    title        TEXT NOT NULL CHECK (title <> ''),
    slug         TEXT NOT NULL CHECK (slug <> ''),
    cover        TEXT REFERENCES images_files (id),
    category_id  INTEGER REFERENCES categories (id),
    publish_date TEXT NOT NULL DEFAULT '',
    blocks       TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(blocks) AND json_type(blocks) = 'array'), lead TEXT NOT NULL DEFAULT '', excerpt TEXT NOT NULL DEFAULT '', cover_alt TEXT NOT NULL DEFAULT '', seo_title TEXT NOT NULL DEFAULT '', seo_description TEXT NOT NULL DEFAULT '', seo_image TEXT REFERENCES images_files (id), cover_mobile TEXT REFERENCES images_files (id),
    PRIMARY KEY (post_id, kind),
    CHECK (kind = 'draft' OR publish_date <> '')
) STRICT;

CREATE INDEX post_contents_category_id ON post_contents (category_id);

CREATE UNIQUE INDEX post_contents_draft_slug ON post_contents (slug) WHERE kind = 'draft';

CREATE INDEX post_contents_published_date ON post_contents (publish_date) WHERE kind = 'published';

CREATE UNIQUE INDEX post_contents_published_slug ON post_contents (slug) WHERE kind = 'published';

CREATE TABLE post_files (
    post_id INTEGER NOT NULL,
    kind    TEXT NOT NULL,
    file_id TEXT NOT NULL REFERENCES files_files (id),
    PRIMARY KEY (post_id, kind, file_id),
    FOREIGN KEY (post_id, kind) REFERENCES post_contents (post_id, kind) ON DELETE CASCADE
) STRICT;

CREATE INDEX post_files_file_id ON post_files (file_id);

CREATE TABLE post_images (
    post_id  INTEGER NOT NULL,
    kind     TEXT NOT NULL,
    image_id TEXT NOT NULL REFERENCES images_files (id),
    PRIMARY KEY (post_id, kind, image_id),
    FOREIGN KEY (post_id, kind) REFERENCES post_contents (post_id, kind) ON DELETE CASCADE
) STRICT;

CREATE INDEX post_images_image_id ON post_images (image_id);

CREATE TABLE post_redirects (
    slug    TEXT PRIMARY KEY CHECK (slug <> ''),
    post_id INTEGER NOT NULL REFERENCES posts (id) ON DELETE CASCADE
) STRICT;

CREATE INDEX post_redirects_post_id ON post_redirects (post_id);

CREATE TABLE posts (
    id         INTEGER PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

CREATE INDEX sessions_expires_at ON sessions (expires_at);

CREATE INDEX sessions_user_id ON sessions (user_id);

CREATE TABLE site_settings (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT ''
, share_image TEXT REFERENCES images_files (id), phone TEXT NOT NULL DEFAULT '', email TEXT NOT NULL DEFAULT '', address TEXT NOT NULL DEFAULT '', cookie_text TEXT NOT NULL DEFAULT '', cookie_link TEXT NOT NULL DEFAULT '', yandex_verification TEXT NOT NULL DEFAULT '', google_verification TEXT NOT NULL DEFAULT '', social TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(social) AND json_type(social) = 'array')) STRICT;

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE CHECK (email <> '' AND email = lower(email)),
    password_hash TEXT NOT NULL CHECK (password_hash <> ''),
    role          TEXT NOT NULL DEFAULT 'admin',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
) STRICT;

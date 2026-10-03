-- name: GetPublishedPageBySlug :one
-- Public site: drafts are never shown.
SELECT id, title, slug, body, created_at, updated_at, draft FROM pages WHERE slug = ? AND draft = 0;

-- name: ListPageLinks :many
-- Published pages for the header menu, in creation order; the home page is excluded by slug.
SELECT title, slug FROM pages WHERE slug <> sqlc.arg(exclude_slug) AND draft = 0 ORDER BY id;

-- name: InsertPageIfMissing :exec
-- Dev seed: inserts a page unless its slug already exists.
INSERT INTO pages (title, slug, body) VALUES (?, ?, ?)
ON CONFLICT (slug) DO NOTHING;

-- name: CountPages :one
SELECT count(*) FROM pages;

-- name: ListPages :many
-- Admin list, in creation order (the same order as the header menu).
SELECT id, title, slug, draft, updated_at FROM pages ORDER BY id;

-- name: GetPage :one
SELECT id, title, slug, body, created_at, updated_at, draft FROM pages WHERE id = ?;

-- name: CreatePage :one
INSERT INTO pages (title, slug, body, draft) VALUES (?, ?, ?, ?)
RETURNING id;

-- name: UpdatePage :execrows
UPDATE pages
SET title = ?, slug = ?, body = ?, draft = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?;

-- name: DeleteDraftPage :execrows
-- Only drafts can be deleted: a published page must be unpublished first.
DELETE FROM pages WHERE id = ? AND draft = 1;

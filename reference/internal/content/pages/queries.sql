-- name: GetPageBySlug :one
SELECT id, title, slug, body, created_at, updated_at FROM pages WHERE slug = ?;

-- name: ListPageLinks :many
-- Pages for the header menu, in creation order; the home page is excluded by slug.
SELECT title, slug FROM pages WHERE slug <> sqlc.arg(exclude_slug) ORDER BY id;

-- name: InsertPageIfMissing :exec
-- Dev seed: inserts a page unless its slug already exists.
INSERT INTO pages (title, slug, body) VALUES (?, ?, ?)
ON CONFLICT (slug) DO NOTHING;

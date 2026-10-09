-- name: ListPublishedPosts :many
-- Blog feed: published copies with publish_date <= now (UTC RFC 3339), newest first.
SELECT c.post_id, c.title, c.slug, c.cover, c.publish_date, c.blocks,
       CAST(coalesce(cat.name, '') AS TEXT) AS category_name,
       CAST(coalesce(cat.slug, '') AS TEXT) AS category_slug
FROM post_contents c
LEFT JOIN categories cat ON cat.id = c.category_id
WHERE c.kind = 'published' AND c.publish_date <= sqlc.arg(now)
ORDER BY c.publish_date DESC, c.post_id DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountPublishedPosts :one
SELECT count(*) FROM post_contents WHERE kind = 'published' AND publish_date <= sqlc.arg(now);

-- name: ListPublishedPostsInCategory :many
SELECT c.post_id, c.title, c.slug, c.cover, c.publish_date, c.blocks,
       CAST(coalesce(cat.name, '') AS TEXT) AS category_name,
       CAST(coalesce(cat.slug, '') AS TEXT) AS category_slug
FROM post_contents c
LEFT JOIN categories cat ON cat.id = c.category_id
WHERE c.kind = 'published' AND c.publish_date <= sqlc.arg(now) AND c.category_id = sqlc.arg(category_id)
ORDER BY c.publish_date DESC, c.post_id DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountPublishedPostsInCategory :one
SELECT count(*) FROM post_contents
WHERE kind = 'published' AND publish_date <= sqlc.arg(now) AND category_id = sqlc.arg(category_id);

-- name: GetPublishedPostBySlug :one
SELECT c.post_id, c.title, c.slug, c.cover, c.publish_date, c.blocks,
       CAST(coalesce(cat.name, '') AS TEXT) AS category_name,
       CAST(coalesce(cat.slug, '') AS TEXT) AS category_slug
FROM post_contents c
LEFT JOIN categories cat ON cat.id = c.category_id
WHERE c.kind = 'published' AND c.slug = sqlc.arg(slug) AND c.publish_date <= sqlc.arg(now);

-- name: GetCategoryBySlug :one
SELECT id, name, slug FROM categories WHERE slug = ?;

-- name: ListCategoryOptions :many
-- Category select in the post form.
SELECT id, name, slug FROM categories ORDER BY name, id;

-- name: CountPosts :one
SELECT count(*) FROM posts;

-- name: ListPosts :many
-- Admin list, newest first. The draft always exists; published_* are empty for a post that is not on the site;
-- changed: the draft differs from the published copy.
SELECT p.id, d.title, d.slug, d.publish_date, p.updated_at,
       CAST(coalesce(cat.name, '') AS TEXT) AS category_name,
       CAST(coalesce(pub.slug, '') AS TEXT) AS published_slug,
       CAST(coalesce(pub.publish_date, '') AS TEXT) AS published_date,
       CAST(pub.post_id IS NOT NULL AND (pub.title <> d.title OR pub.slug <> d.slug OR pub.cover IS NOT d.cover
            OR pub.category_id IS NOT d.category_id OR pub.publish_date <> d.publish_date OR pub.blocks <> d.blocks)
            AS INTEGER) AS changed
FROM posts p
JOIN post_contents d ON d.post_id = p.id AND d.kind = 'draft'
LEFT JOIN post_contents pub ON pub.post_id = p.id AND pub.kind = 'published'
LEFT JOIN categories cat ON cat.id = d.category_id
ORDER BY p.id DESC;

-- name: GetPostContent :one
SELECT post_id, kind, title, slug, cover, category_id, publish_date, blocks FROM post_contents WHERE post_id = ? AND kind = ?;

-- name: CreatePost :one
INSERT INTO posts DEFAULT VALUES RETURNING id;

-- name: TouchPost :execrows
UPDATE posts SET updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SaveDraft :exec
INSERT INTO post_contents (post_id, kind, title, slug, cover, category_id, publish_date, blocks)
VALUES (?, 'draft', ?, ?, ?, ?, ?, ?)
ON CONFLICT (post_id, kind) DO UPDATE SET title = excluded.title, slug = excluded.slug, cover = excluded.cover,
    category_id = excluded.category_id, publish_date = excluded.publish_date, blocks = excluded.blocks;

-- name: DeleteContentImages :exec
DELETE FROM post_images WHERE post_id = ? AND kind = ?;

-- name: InsertContentImage :exec
INSERT INTO post_images (post_id, kind, image_id) VALUES (?, ?, ?);

-- name: PublishDraft :exec
-- Copies the draft into the published copy (insert or replace its content).
INSERT INTO post_contents (post_id, kind, title, slug, cover, category_id, publish_date, blocks)
SELECT src.post_id, 'published', src.title, src.slug, src.cover, src.category_id, src.publish_date, src.blocks
FROM post_contents AS src WHERE src.post_id = ? AND src.kind = 'draft'
ON CONFLICT (post_id, kind) DO UPDATE SET title = excluded.title, slug = excluded.slug, cover = excluded.cover,
    category_id = excluded.category_id, publish_date = excluded.publish_date, blocks = excluded.blocks;

-- name: PublishDraftImages :exec
INSERT INTO post_images (post_id, kind, image_id)
SELECT src.post_id, 'published', src.image_id FROM post_images AS src WHERE src.post_id = ? AND src.kind = 'draft';

-- name: Unpublish :execrows
-- Removes the published copy; its post_images rows go with it (ON DELETE CASCADE).
DELETE FROM post_contents WHERE post_id = ? AND kind = 'published';

-- name: DeleteUnpublishedPost :execrows
-- Only a post that is not on the site can be deleted: a published post must be unpublished first.
DELETE FROM posts
WHERE id = ? AND NOT EXISTS (SELECT 1 FROM post_contents c WHERE c.post_id = posts.id AND c.kind = 'published');

-- name: DraftPostIDBySlug :one
-- Dev seed: an existing post with this draft slug.
SELECT post_id FROM post_contents WHERE kind = 'draft' AND slug = ?;

-- name: PostsUsingImage :many
-- Media library: posts whose draft or published copy uses the image (cover or blocks).
SELECT DISTINCT p.id, d.title
FROM post_images i
JOIN posts p ON p.id = i.post_id
JOIN post_contents d ON d.post_id = p.id AND d.kind = 'draft'
WHERE i.image_id = ?
ORDER BY p.id;

-- name: ListCategories :many
-- Admin list in name order; posts: how many posts (any copy) are in the category.
SELECT c.id, c.name, c.slug, c.updated_at,
       (SELECT count(DISTINCT pc.post_id) FROM post_contents pc WHERE pc.category_id = c.id) AS posts
FROM categories c
ORDER BY c.name, c.id;

-- name: CountCategories :one
SELECT count(*) FROM categories;

-- name: GetCategory :one
SELECT id, name, slug FROM categories WHERE id = ?;

-- name: GetCategoryIDBySlug :one
SELECT id FROM categories WHERE slug = ?;

-- name: CreateCategory :one
INSERT INTO categories (name, slug) VALUES (?, ?) RETURNING id;

-- name: UpdateCategory :execrows
UPDATE categories SET name = ?, slug = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: DeleteCategory :execrows
DELETE FROM categories WHERE id = ?;

-- name: PostsInCategory :many
-- Posts whose draft or published copy is in the category (the draft title names the post).
SELECT DISTINCT p.id, d.title
FROM post_contents c
JOIN posts p ON p.id = c.post_id
JOIN post_contents d ON d.post_id = p.id AND d.kind = 'draft'
WHERE c.category_id = ?
ORDER BY p.id;

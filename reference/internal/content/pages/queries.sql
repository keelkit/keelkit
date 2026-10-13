-- name: GetPublishedPageBySlug :one
-- Public site: only the published copy is ever shown.
SELECT page_id, title, slug, blocks, seo_title, seo_description, seo_image
FROM page_contents WHERE kind = 'published' AND slug = ?;

-- name: CountPages :one
SELECT count(*) FROM pages;

-- name: ListPages :many
-- Admin list, in creation order. The draft always exists;
-- published_slug is empty for a page that is not on the site; changed: the draft differs from the published copy.
SELECT p.id, d.title, d.slug, p.updated_at,
       CAST(coalesce(pub.slug, '') AS TEXT) AS published_slug,
       CAST(pub.page_id IS NOT NULL AND (pub.title <> d.title OR pub.slug <> d.slug OR pub.blocks <> d.blocks
            OR pub.seo_title <> d.seo_title OR pub.seo_description <> d.seo_description
            OR pub.seo_image IS NOT d.seo_image) AS INTEGER) AS changed
FROM pages p
JOIN page_contents d ON d.page_id = p.id AND d.kind = 'draft'
LEFT JOIN page_contents pub ON pub.page_id = p.id AND pub.kind = 'published'
ORDER BY p.id;

-- name: GetPageContent :one
SELECT page_id, kind, title, slug, blocks, seo_title, seo_description, seo_image
FROM page_contents WHERE page_id = ? AND kind = ?;

-- name: CreatePage :one
INSERT INTO pages DEFAULT VALUES RETURNING id;

-- name: TouchPage :execrows
UPDATE pages SET updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SaveDraft :exec
INSERT INTO page_contents (page_id, kind, title, slug, blocks, seo_title, seo_description, seo_image)
VALUES (?, 'draft', ?, ?, ?, ?, ?, ?)
ON CONFLICT (page_id, kind) DO UPDATE SET title = excluded.title, slug = excluded.slug, blocks = excluded.blocks,
    seo_title = excluded.seo_title, seo_description = excluded.seo_description, seo_image = excluded.seo_image;

-- name: DeleteContentImages :exec
DELETE FROM page_images WHERE page_id = ? AND kind = ?;

-- name: InsertContentImage :exec
INSERT INTO page_images (page_id, kind, image_id) VALUES (?, ?, ?);

-- name: PublishDraft :exec
-- Copies the draft into the published copy (insert or replace its content).
INSERT INTO page_contents (page_id, kind, title, slug, blocks, seo_title, seo_description, seo_image)
SELECT src.page_id, 'published', src.title, src.slug, src.blocks, src.seo_title, src.seo_description, src.seo_image
FROM page_contents AS src WHERE src.page_id = ? AND src.kind = 'draft'
ON CONFLICT (page_id, kind) DO UPDATE SET title = excluded.title, slug = excluded.slug, blocks = excluded.blocks,
    seo_title = excluded.seo_title, seo_description = excluded.seo_description, seo_image = excluded.seo_image;

-- name: PublishDraftImages :exec
INSERT INTO page_images (page_id, kind, image_id)
SELECT src.page_id, 'published', src.image_id FROM page_images AS src WHERE src.page_id = ? AND src.kind = 'draft';

-- name: Unpublish :execrows
-- Removes the published copy; its page_images rows go with it (ON DELETE CASCADE).
DELETE FROM page_contents WHERE page_id = ? AND kind = 'published';

-- name: DeleteUnpublishedPage :execrows
-- Only a page that is not on the site can be deleted: a published page must be unpublished first.
DELETE FROM pages
WHERE id = ? AND NOT EXISTS (SELECT 1 FROM page_contents c WHERE c.page_id = pages.id AND c.kind = 'published');

-- name: DraftPageIDBySlug :one
-- Dev seed: an existing page with this draft slug.
SELECT page_id FROM page_contents WHERE kind = 'draft' AND slug = ?;

-- name: PagesUsingImage :many
-- Media library: pages whose draft or published copy uses the image (the draft title names the page).
SELECT DISTINCT p.id, d.title
FROM page_images i
JOIN pages p ON p.id = i.page_id
JOIN page_contents d ON d.page_id = p.id AND d.kind = 'draft'
WHERE i.image_id = ?
ORDER BY p.id;

-- name: ListPublishedPageSlugs :many
-- Sitemap: every published page, in creation order.
SELECT slug FROM page_contents WHERE kind = 'published' ORDER BY page_id;

-- name: RememberPublishedSlug :exec
-- Before the published copy changes or goes away, its slug becomes a redirect to the page.
INSERT INTO page_redirects (slug, page_id)
SELECT src.slug, src.page_id FROM page_contents AS src WHERE src.page_id = ? AND src.kind = 'published'
ON CONFLICT (slug) DO UPDATE SET page_id = excluded.page_id;

-- name: DropRedirectToPublished :exec
-- A slug that is published again is no longer a redirect: the published copy always wins.
DELETE FROM page_redirects
WHERE slug = (SELECT c.slug FROM page_contents AS c WHERE c.page_id = ? AND c.kind = 'published');

-- name: RedirectTarget :one
-- Current published slug of the page an old slug belonged to.
SELECT c.slug FROM page_redirects r
JOIN page_contents c ON c.page_id = r.page_id AND c.kind = 'published'
WHERE r.slug = sqlc.arg(old_slug);

-- name: DeleteContentFiles :exec
DELETE FROM page_files WHERE page_id = ? AND kind = ?;

-- name: InsertContentFile :exec
INSERT INTO page_files (page_id, kind, file_id) VALUES (?, ?, ?);

-- name: PublishDraftFiles :exec
INSERT INTO page_files (page_id, kind, file_id)
SELECT src.page_id, 'published', src.file_id FROM page_files AS src WHERE src.page_id = ? AND src.kind = 'draft';

-- name: PagesUsingFile :many
-- Files section: pages whose draft or published copy uses the file (file block).
SELECT DISTINCT p.id, d.title
FROM page_files f
JOIN pages p ON p.id = f.page_id
JOIN page_contents d ON d.page_id = p.id AND d.kind = 'draft'
WHERE f.file_id = ?
ORDER BY p.id;

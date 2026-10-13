-- name: ListMenuItems :many
-- Admin form: items of one menu; top-level items first, then children, each by position.
SELECT m.id, m.parent_id, m.title, m.page_id, m.url, m.section, m.children
FROM menu_items m
WHERE m.menu = ?
ORDER BY m.parent_id IS NOT NULL, m.position, m.id;

-- name: ListSiteMenuItems :many
-- Public site: items of every menu (same order as ListMenuItems) with the published copy of the linked page (empty for an unpublished page).
SELECT m.id, m.menu, m.parent_id, m.title, m.page_id, m.url, m.section, m.children,
       CAST(coalesce(pub.title, '') AS TEXT) AS page_title,
       CAST(coalesce(pub.slug, '') AS TEXT) AS page_slug
FROM menu_items m
LEFT JOIN page_contents pub ON pub.page_id = m.page_id AND pub.kind = 'published'
ORDER BY m.menu, m.parent_id IS NOT NULL, m.position, m.id;

-- name: ListMenuPages :many
-- Page select in the admin form: every page by its draft title, in creation order; published: on the site.
SELECT p.id, d.title,
       CAST(EXISTS (SELECT 1 FROM page_contents pub WHERE pub.page_id = p.id AND pub.kind = 'published') AS INTEGER) AS published
FROM pages p
JOIN page_contents d ON d.page_id = p.id AND d.kind = 'draft'
ORDER BY p.id;

-- name: CountMenuItems :many
-- Admin list of menus: number of items in each menu.
SELECT menu, count(*) AS items FROM menu_items GROUP BY menu;

-- name: DeleteMenuItems :exec
-- Saving a menu replaces all its items.
DELETE FROM menu_items WHERE menu = ?;

-- name: InsertMenuItem :one
INSERT INTO menu_items (menu, parent_id, position, title, page_id, url, section, children)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: MenusUsingPage :many
-- Page deletion: menus that link to the page.
SELECT DISTINCT menu FROM menu_items WHERE page_id = ? ORDER BY menu;

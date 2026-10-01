-- name: GetSiteSettings :one
SELECT name, description FROM site_settings WHERE id = 1;

-- name: InsertSiteSettingsIfMissing :exec
INSERT INTO site_settings (id, name, description) VALUES (1, ?, ?)
ON CONFLICT (id) DO NOTHING;

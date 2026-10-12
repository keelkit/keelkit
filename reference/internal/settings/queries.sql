-- name: GetSiteSettings :one
SELECT name, description, share_image, phone, email, address, cookie_text, cookie_link,
       yandex_verification, google_verification
FROM site_settings WHERE id = 1;

-- name: InsertSiteSettingsIfMissing :exec
INSERT INTO site_settings (id, name, description, phone, email, address, cookie_text, cookie_link)
VALUES (1, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO NOTHING;

-- name: SaveSiteSettings :exec
INSERT INTO site_settings (id, name, description, share_image, phone, email, address, cookie_text, cookie_link,
                           yandex_verification, google_verification)
VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    name = excluded.name,
    description = excluded.description,
    share_image = excluded.share_image,
    phone = excluded.phone,
    email = excluded.email,
    address = excluded.address,
    cookie_text = excluded.cookie_text,
    cookie_link = excluded.cookie_link,
    yandex_verification = excluded.yandex_verification,
    google_verification = excluded.google_verification;

-- name: SettingsUseImage :one
SELECT EXISTS (SELECT 1 FROM site_settings WHERE id = 1 AND share_image = ?);

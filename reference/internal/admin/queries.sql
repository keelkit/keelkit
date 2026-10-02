-- name: GetUserByEmail :one
SELECT id, email, password_hash, role FROM users WHERE email = ?;

-- name: CreateUser :one
INSERT INTO users (email, password_hash) VALUES (?, ?) RETURNING id;

-- name: InsertUserIfMissing :exec
-- Dev seed: inserts a user unless the email already exists.
INSERT INTO users (email, password_hash) VALUES (?, ?)
ON CONFLICT (email) DO NOTHING;

-- name: UpdatePasswordHash :exec
UPDATE users SET password_hash = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = ?;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at) VALUES (?, ?, ?, ?);

-- name: GetSession :one
SELECT u.id AS user_id, u.email, u.role, s.csrf_token
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ? AND s.expires_at > sqlc.arg(now);

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= sqlc.arg(now);

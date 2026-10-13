-- name: ListPeople :many
-- Admin list and the site: manual order (feature order).
SELECT id, name, role, photo FROM people ORDER BY sort_order, id;

-- name: ListPeopleIDs :many
SELECT id FROM people ORDER BY sort_order, id;

-- name: CountPeople :one
SELECT count(*) FROM people;

-- name: GetPerson :one
SELECT id, name, role, photo FROM people WHERE id = ?;

-- name: GetPersonIDByName :one
SELECT id FROM people WHERE name = ? ORDER BY id LIMIT 1;

-- name: CreatePerson :one
-- A new person goes to the end of the list.
INSERT INTO people (name, role, photo, sort_order)
VALUES (?, ?, ?, (SELECT coalesce(max(sort_order), 0) + 1 FROM people))
RETURNING id;

-- name: UpdatePerson :execrows
UPDATE people SET name = ?, role = ?, photo = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?;

-- name: SetPersonOrder :exec
UPDATE people SET sort_order = ? WHERE id = ?;

-- name: DeletePerson :execrows
DELETE FROM people WHERE id = ?;

-- name: PeopleUsingImage :many
SELECT id, name FROM people WHERE photo = ? ORDER BY sort_order, id;

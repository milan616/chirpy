-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email)
VALUES (
  $1,
  $2,
  $3,
  $4
)
RETURNING *;

-- name: ResetUsers :exec
DELETE FROM users;

-- name: CreateChirp :one
INSERT INTO chirps (id, created_at, updated_at, body, user_id)
VALUES (
  $1,
  $2,
  $3,
  $4,
  $5
)
RETURNING *;

-- +goose Up
CREATE TABLE users (
  id UUID PRIMARY KEY,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  email TEXT UNIQUE NOT NULL,
  hashed_password TEXT NOT NULL
);

CREATE TABLE chirps (
  id UUID PRIMARY KEY,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  body TEXT NOT NULL,
  user_id UUID NOT NULL,
  CONSTRAINT fk_user_id
    FOREIGN KEY (user_id)
    REFERENCES users(id)
    ON DELETE CASCADE
);

CREATE TABLE refresh_tokens (
  token TEXT PRIMARY KEY,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  user_id UUID NOT NULL,
  expires_at TIMESTAMP,
  revoked_at TIMESTAMP,
  CONSTRAINT fk_user_id
    FOREIGN KEY (user_id)
    REFERENCES users(id)
    ON DELETE CASCADE
);

-- +goose Down
DROP TABLE refresh_tokens;
DROP TABLE chirps;
DROP TABLE users;

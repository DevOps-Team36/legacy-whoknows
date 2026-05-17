-- +goose Up
ALTER TABLE users
ADD COLUMN password_change_required BOOLEAN NOT NULL DEFAULT false;

UPDATE users
SET password_change_required = true;

-- +goose Down
ALTER TABLE users
DROP COLUMN password_change_required;

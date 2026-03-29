-- +goose Up
ALTER TABLE environments
    ADD COLUMN branch     TEXT NOT NULL DEFAULT '',
    ADD COLUMN status     TEXT NOT NULL DEFAULT '',
    ADD COLUMN is_deleted BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE environments
    DROP COLUMN branch,
    DROP COLUMN status,
    DROP COLUMN is_deleted,
    DROP COLUMN created_at;

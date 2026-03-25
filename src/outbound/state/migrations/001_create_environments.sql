-- +goose Up
CREATE TABLE environments (
    project    TEXT NOT NULL,
    name       TEXT NOT NULL,
    data       JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project, name)
);

-- +goose Down
DROP TABLE IF EXISTS environments;

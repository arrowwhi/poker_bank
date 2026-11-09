-- +goose Up

ALTER TABLE settlements ADD COLUMN IF NOT EXISTS is_received BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE settlements ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ;

-- +goose Down

ALTER TABLE settlements DROP COLUMN IF EXISTS received_at;
ALTER TABLE settlements DROP COLUMN IF EXISTS is_received;

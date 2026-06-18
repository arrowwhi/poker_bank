-- +goose Up

CREATE TABLE IF NOT EXISTS chat_topics (
    chat_id      BIGINT      PRIMARY KEY,
    topic_id     BIGINT      NOT NULL,
    set_by_tg_id BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down

DROP TABLE IF EXISTS chat_topics;

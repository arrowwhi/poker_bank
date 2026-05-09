-- +goose Up

CREATE TABLE IF NOT EXISTS pending_actions (
    id                BIGSERIAL   PRIMARY KEY,
    game_id           BIGINT      NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    action_type       TEXT        NOT NULL CHECK (action_type IN ('JOIN', 'REBUY')),
    requester_tg_id   BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    target_tg_id      BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    payload           JSONB       NOT NULL DEFAULT '{}',
    status            TEXT        NOT NULL DEFAULT 'pending'
                                  CHECK (status IN ('pending', 'confirmed', 'rejected', 'expired', 'cancelled')),
    chat_id           BIGINT      NOT NULL,
    message_id        BIGINT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at       TIMESTAMPTZ,
    resolved_by_tg_id BIGINT      REFERENCES players (telegram_user_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS pending_one_per_target
    ON pending_actions (game_id, target_tg_id, action_type)
    WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS fsm_states (
    chat_id    BIGINT      NOT NULL,
    user_tg_id BIGINT      NOT NULL,
    state      TEXT,
    data       JSONB       NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, user_tg_id)
);

-- +goose Down

DROP TABLE IF EXISTS fsm_states;
DROP INDEX IF EXISTS pending_one_per_target;
DROP TABLE IF EXISTS pending_actions;

-- +goose Up

CREATE TABLE players (
    telegram_user_id BIGINT      PRIMARY KEY,
    username         TEXT,
    display_name     TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE games (
    id             BIGSERIAL   PRIMARY KEY,
    chat_id        BIGINT      NOT NULL,
    dealer_tg_id   BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    buy_in_rub     INTEGER     NOT NULL CHECK (buy_in_rub > 0),
    buy_in_chips   INTEGER     NOT NULL CHECK (buy_in_chips > 0),
    rebuy_rub      INTEGER     NOT NULL CHECK (rebuy_rub > 0),
    rebuy_chips    INTEGER     NOT NULL CHECK (rebuy_chips > 0),
    status         TEXT        NOT NULL CHECK (status IN ('active', 'finished', 'cancelled')),
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at       TIMESTAMPTZ,
    bank_delta_rub INTEGER     NOT NULL DEFAULT 0,
    CONSTRAINT proportional_rate
        CHECK (buy_in_rub * rebuy_chips = rebuy_rub * buy_in_chips)
);

CREATE UNIQUE INDEX games_one_active_per_chat
    ON games (chat_id) WHERE status = 'active';

CREATE INDEX games_chat_history
    ON games (chat_id, ended_at DESC) WHERE status IN ('finished', 'cancelled');

CREATE TABLE ledger (
    id               BIGSERIAL   PRIMARY KEY,
    game_id          BIGINT      NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    player_tg_id     BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    type             TEXT        NOT NULL CHECK (type IN ('BUY_IN', 'REBUY', 'CASH_OUT')),
    amount_rub       INTEGER     NOT NULL,
    amount_chips     INTEGER     NOT NULL,
    created_by_tg_id BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_void          BOOLEAN     NOT NULL DEFAULT false,
    void_reason      TEXT
);

CREATE INDEX ledger_game ON ledger (game_id, created_at);

CREATE TABLE participants (
    game_id      BIGINT      NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    player_tg_id BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    is_active    BOOLEAN     NOT NULL DEFAULT true,
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (game_id, player_tg_id)
);

CREATE TABLE game_results (
    game_id         BIGINT  NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    player_tg_id    BIGINT  NOT NULL REFERENCES players (telegram_user_id),
    buy_in_count    INTEGER NOT NULL DEFAULT 0,
    rebuy_count     INTEGER NOT NULL DEFAULT 0,
    total_in_rub    INTEGER NOT NULL,
    total_out_rub   INTEGER NOT NULL,
    total_out_chips INTEGER NOT NULL,
    net_rub         INTEGER NOT NULL,
    PRIMARY KEY (game_id, player_tg_id)
);

CREATE INDEX game_results_player ON game_results (player_tg_id);

CREATE TABLE settlements (
    id         BIGSERIAL   PRIMARY KEY,
    game_id    BIGINT      NOT NULL REFERENCES games (id) ON DELETE CASCADE,
    from_tg_id BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    to_tg_id   BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    amount_rub INTEGER     NOT NULL CHECK (amount_rub > 0),
    is_paid    BOOLEAN     NOT NULL DEFAULT false,
    paid_at    TIMESTAMPTZ
);

CREATE INDEX settlements_from ON settlements (from_tg_id) WHERE is_paid = false;
CREATE INDEX settlements_game ON settlements (game_id);

CREATE TABLE pending_actions (
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

CREATE UNIQUE INDEX pending_one_per_target
    ON pending_actions (game_id, target_tg_id, action_type)
    WHERE status = 'pending';

CREATE TABLE fsm_states (
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
DROP INDEX IF EXISTS settlements_game;
DROP INDEX IF EXISTS settlements_from;
DROP TABLE IF EXISTS settlements;
DROP INDEX IF EXISTS game_results_player;
DROP TABLE IF EXISTS game_results;
DROP TABLE IF EXISTS participants;
DROP INDEX IF EXISTS ledger_game;
DROP TABLE IF EXISTS ledger;
DROP INDEX IF EXISTS games_chat_history;
DROP INDEX IF EXISTS games_one_active_per_chat;
DROP TABLE IF EXISTS games;
DROP TABLE IF EXISTS players;
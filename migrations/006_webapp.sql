-- +goose Up

-- Группы, где бот видел активность: источник списка столов Mini App.
CREATE TABLE IF NOT EXISTS chats (
    id         BIGINT      PRIMARY KEY,
    title      TEXT        NOT NULL DEFAULT '',
    type       TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Участники групп, известные боту (писали команды, жали кнопки, играли).
-- Источник picker'а игроков и проверки доступа к играм чата.
CREATE TABLE IF NOT EXISTS chat_members (
    chat_id      BIGINT      NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    player_tg_id BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, player_tg_id)
);

CREATE INDEX IF NOT EXISTS chat_members_player ON chat_members (player_tg_id);

-- Сообщение с планом переводов: его правят и кнопка в чате, и Mini App.
ALTER TABLE games ADD COLUMN IF NOT EXISTS settle_message_id BIGINT;

CREATE INDEX IF NOT EXISTS settlements_to_open ON settlements (to_tg_id) WHERE is_received = false;

-- Backfill по уже сыгранным играм.
INSERT INTO chats (id)
SELECT DISTINCT chat_id FROM games
ON CONFLICT (id) DO NOTHING;

INSERT INTO chat_members (chat_id, player_tg_id, last_seen_at)
SELECT g.chat_id, p.player_tg_id, max(p.joined_at)
FROM participants p
JOIN games g ON g.id = p.game_id
GROUP BY g.chat_id, p.player_tg_id
ON CONFLICT (chat_id, player_tg_id) DO NOTHING;

INSERT INTO chat_members (chat_id, player_tg_id, last_seen_at)
SELECT chat_id, dealer_tg_id, max(started_at)
FROM games
GROUP BY chat_id, dealer_tg_id
ON CONFLICT (chat_id, player_tg_id) DO NOTHING;

-- +goose Down

DROP INDEX IF EXISTS settlements_to_open;
ALTER TABLE games DROP COLUMN IF EXISTS settle_message_id;
DROP INDEX IF EXISTS chat_members_player;
DROP TABLE IF EXISTS chat_members;
DROP TABLE IF EXISTS chats;

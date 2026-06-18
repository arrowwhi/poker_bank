package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
)

// ChatTopicRepo implements interfaces.ChatTopicRepository using a PostgreSQL connection pool.
type ChatTopicRepo struct {
	db *pgxpool.Pool
}

// NewChatTopicRepo creates a new ChatTopicRepo backed by the given connection pool.
func NewChatTopicRepo(db *pgxpool.Pool) *ChatTopicRepo {
	return &ChatTopicRepo{db: db}
}

// Get returns the topic binding for a chat, or (nil, nil) if the chat has no binding.
func (r *ChatTopicRepo) Get(ctx context.Context, chatID int64) (*domain.ChatTopic, error) {
	ct := &domain.ChatTopic{}
	err := r.db.QueryRow(ctx, `
		SELECT chat_id, topic_id, set_by_tg_id, created_at, updated_at
		FROM chat_topics WHERE chat_id = $1
	`, chatID).Scan(&ct.ChatID, &ct.TopicID, &ct.SetByTgID, &ct.CreatedAt, &ct.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get chat topic: %w", err)
	}
	return ct, nil
}

// Set inserts or updates the topic binding for a chat.
func (r *ChatTopicRepo) Set(ctx context.Context, chatID int64, topicID int64, setByTgID int64) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO chat_topics (chat_id, topic_id, set_by_tg_id, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (chat_id) DO UPDATE
		  SET topic_id     = EXCLUDED.topic_id,
		      set_by_tg_id = EXCLUDED.set_by_tg_id,
		      updated_at   = now()
	`, chatID, topicID, setByTgID)
	if err != nil {
		return fmt.Errorf("set chat topic: %w", err)
	}
	return nil
}

// Delete removes the topic binding for a chat. It is a no-op if no binding exists.
func (r *ChatTopicRepo) Delete(ctx context.Context, chatID int64) error {
	_, err := r.db.Exec(ctx, `DELETE FROM chat_topics WHERE chat_id = $1`, chatID)
	if err != nil {
		return fmt.Errorf("delete chat topic: %w", err)
	}
	return nil
}

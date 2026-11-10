package postgres

import (
	"context"
	"errors"
	"fmt"

	infrapg "github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/jackc/pgx/v5"

	"poker_bank/internal/domain"
)

// ChatRepo — группы и их участники, известные боту.
type ChatRepo struct {
	db *infrapg.Pool
}

// NewChatRepo creates a ChatRepo backed by the given pool.
func NewChatRepo(db *infrapg.Pool) *ChatRepo {
	return &ChatRepo{db: db}
}

// Upsert сохраняет группу; пустое название не затирает известное.
func (r *ChatRepo) Upsert(ctx context.Context, c *domain.Chat) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO chats (id, title, type, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (id) DO UPDATE
		  SET title      = COALESCE(NULLIF(EXCLUDED.title, ''), chats.title),
		      type       = COALESCE(NULLIF(EXCLUDED.type, ''), chats.type),
		      updated_at = now()
	`, c.ID, c.Title, c.Type)
	if err != nil {
		return fmt.Errorf("upsert chat: %w", err)
	}
	return nil
}

// TouchMember отмечает, что пользователь состоит в группе (группа создаётся при необходимости).
func (r *ChatRepo) TouchMember(ctx context.Context, chatID, tgID int64) error {
	if _, err := r.db.Exec(ctx, `INSERT INTO chats (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, chatID); err != nil {
		return fmt.Errorf("ensure chat: %w", err)
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO chat_members (chat_id, player_tg_id, last_seen_at)
		VALUES ($1, $2, now())
		ON CONFLICT (chat_id, player_tg_id) DO UPDATE SET last_seen_at = now()
	`, chatID, tgID)
	if err != nil {
		return fmt.Errorf("touch chat member: %w", err)
	}
	return nil
}

// RemoveMember забывает участника (вышел из группы).
func (r *ChatRepo) RemoveMember(ctx context.Context, chatID, tgID int64) error {
	_, err := r.db.Exec(ctx, `DELETE FROM chat_members WHERE chat_id = $1 AND player_tg_id = $2`, chatID, tgID)
	if err != nil {
		return fmt.Errorf("remove chat member: %w", err)
	}
	return nil
}

// Get возвращает группу; (nil, nil), если бот её не знает.
func (r *ChatRepo) Get(ctx context.Context, id int64) (*domain.Chat, error) {
	c := &domain.Chat{}
	err := r.db.QueryRow(ctx, `
		SELECT id, title, type, created_at, updated_at FROM chats WHERE id = $1
	`, id).Scan(&c.ID, &c.Title, &c.Type, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get chat: %w", err)
	}
	return c, nil
}

// ListByMember возвращает группы, где пользователь известен как участник.
func (r *ChatRepo) ListByMember(ctx context.Context, tgID int64) ([]domain.Chat, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.title, c.type, c.created_at, c.updated_at
		FROM chats c
		JOIN chat_members m ON m.chat_id = c.id
		WHERE m.player_tg_id = $1
		ORDER BY m.last_seen_at DESC
	`, tgID)
	if err != nil {
		return nil, fmt.Errorf("list chats by member: %w", err)
	}
	defer rows.Close()

	var chats []domain.Chat
	for rows.Next() {
		var c domain.Chat
		if err := rows.Scan(&c.ID, &c.Title, &c.Type, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan chat: %w", err)
		}
		chats = append(chats, c)
	}
	return chats, rows.Err()
}

// IsMember сообщает, известен ли пользователь как участник группы.
func (r *ChatRepo) IsMember(ctx context.Context, chatID, tgID int64) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM chat_members WHERE chat_id = $1 AND player_tg_id = $2)
	`, chatID, tgID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("is chat member: %w", err)
	}
	return ok, nil
}

// ListMembers возвращает известных участников группы, недавно активные — первыми.
func (r *ChatRepo) ListMembers(ctx context.Context, chatID int64) ([]domain.Player, error) {
	rows, err := r.db.Query(ctx, `
		SELECT p.telegram_user_id, COALESCE(p.username, ''), COALESCE(p.display_name, ''), p.created_at, p.updated_at
		FROM chat_members m
		JOIN players p ON p.telegram_user_id = m.player_tg_id
		WHERE m.chat_id = $1
		ORDER BY m.last_seen_at DESC
	`, chatID)
	if err != nil {
		return nil, fmt.Errorf("list chat members: %w", err)
	}
	defer rows.Close()

	var ps []domain.Player
	for rows.Next() {
		var p domain.Player
		if err := rows.Scan(&p.TelegramUserID, &p.Username, &p.DisplayName, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan chat member: %w", err)
		}
		ps = append(ps, p)
	}
	return ps, rows.Err()
}

// CountMembers — число известных участников группы.
func (r *ChatRepo) CountMembers(ctx context.Context, chatID int64) (int, error) {
	var n int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM chat_members WHERE chat_id = $1`, chatID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count chat members: %w", err)
	}
	return n, nil
}

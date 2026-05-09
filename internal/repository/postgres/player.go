package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
)

type PlayerRepo struct {
	db *pgxpool.Pool
}

func NewPlayerRepo(db *pgxpool.Pool) *PlayerRepo {
	return &PlayerRepo{db: db}
}

func (r *PlayerRepo) Upsert(ctx context.Context, p *domain.Player) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO players (telegram_user_id, username, display_name, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (telegram_user_id) DO UPDATE
		  SET username     = EXCLUDED.username,
		      display_name = EXCLUDED.display_name,
		      updated_at   = now()
	`, p.TelegramUserID, p.Username, p.DisplayName)
	if err != nil {
		return fmt.Errorf("upsert player: %w", err)
	}
	return nil
}

func (r *PlayerRepo) GetByID(ctx context.Context, tgID int64) (*domain.Player, error) {
	p := &domain.Player{}
	err := r.db.QueryRow(ctx, `
		SELECT telegram_user_id, username, display_name, created_at, updated_at
		FROM players WHERE telegram_user_id = $1
	`, tgID).Scan(&p.TelegramUserID, &p.Username, &p.DisplayName, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get player by id: %w", err)
	}
	return p, nil
}

func (r *PlayerRepo) GetByUsername(ctx context.Context, username string) (*domain.Player, error) {
	p := &domain.Player{}
	err := r.db.QueryRow(ctx, `
		SELECT telegram_user_id, username, display_name, created_at, updated_at
		FROM players WHERE username = $1
	`, username).Scan(&p.TelegramUserID, &p.Username, &p.DisplayName, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get player by username: %w", err)
	}
	return p, nil
}

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
)

type FSMStateRepo struct {
	db *pgxpool.Pool
}

func NewFSMStateRepo(db *pgxpool.Pool) *FSMStateRepo {
	return &FSMStateRepo{db: db}
}

func (r *FSMStateRepo) Get(ctx context.Context, chatID int64, userTgID int64) (*domain.FSMState, error) {
	s := &domain.FSMState{}
	err := r.db.QueryRow(ctx, `
		SELECT chat_id, user_tg_id, state, data, updated_at
		FROM fsm_states WHERE chat_id = $1 AND user_tg_id = $2
	`, chatID, userTgID).Scan(&s.ChatID, &s.UserTgID, &s.State, &s.Data, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get fsm state: %w", err)
	}
	return s, nil
}

func (r *FSMStateRepo) Set(ctx context.Context, s *domain.FSMState) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO fsm_states (chat_id, user_tg_id, state, data, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (chat_id, user_tg_id) DO UPDATE
		  SET state = EXCLUDED.state,
		      data  = EXCLUDED.data,
		      updated_at = now()
	`, s.ChatID, s.UserTgID, s.State, s.Data)
	if err != nil {
		return fmt.Errorf("set fsm state: %w", err)
	}
	return nil
}

func (r *FSMStateRepo) Delete(ctx context.Context, chatID int64, userTgID int64) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM fsm_states WHERE chat_id = $1 AND user_tg_id = $2`,
		chatID, userTgID,
	)
	if err != nil {
		return fmt.Errorf("delete fsm state: %w", err)
	}
	return nil
}

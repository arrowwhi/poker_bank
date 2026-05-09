package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
)

// FSMStateRepo implements interfaces.FSMStateRepository using a PostgreSQL connection pool.
type FSMStateRepo struct {
	db *pgxpool.Pool
}

// NewFSMStateRepo creates a new FSMStateRepo backed by the given connection pool.
func NewFSMStateRepo(db *pgxpool.Pool) *FSMStateRepo {
	return &FSMStateRepo{db: db}
}

// Get retrieves the FSM state for the given chat and user, returning an error if not found.
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

// Set upserts the FSM state for the given chat and user.
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

// Delete removes the FSM state for the given chat and user.
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

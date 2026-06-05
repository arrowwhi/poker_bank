package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
)

// PendingActionRepo implements interfaces.PendingActionRepository using a PostgreSQL connection pool.
type PendingActionRepo struct {
	db *pgxpool.Pool
}

// NewPendingActionRepo creates a new PendingActionRepo backed by the given connection pool.
func NewPendingActionRepo(db *pgxpool.Pool) *PendingActionRepo {
	return &PendingActionRepo{db: db}
}

// Create inserts a new pending action and returns its generated ID.
func (r *PendingActionRepo) Create(ctx context.Context, a *domain.PendingAction) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx, `
		INSERT INTO pending_actions
		  (game_id, action_type, requester_tg_id, target_tg_id, payload, chat_id, message_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, a.GameID, a.ActionType, a.RequesterTgID, a.TargetTgID,
		a.Payload, a.ChatID, a.MessageID,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create pending action: %w", err)
	}
	return id, nil
}

// GetByID retrieves a pending action by its primary key.
func (r *PendingActionRepo) GetByID(ctx context.Context, id int64) (*domain.PendingAction, error) {
	a := &domain.PendingAction{}
	err := r.db.QueryRow(ctx, `
		SELECT id, game_id, action_type, requester_tg_id, target_tg_id, payload,
		       status, chat_id, message_id, created_at, resolved_at, resolved_by_tg_id
		FROM pending_actions WHERE id = $1
	`, id).Scan(
		&a.ID, &a.GameID, &a.ActionType, &a.RequesterTgID, &a.TargetTgID, &a.Payload,
		&a.Status, &a.ChatID, &a.MessageID, &a.CreatedAt, &a.ResolvedAt, &a.ResolvedByTgID,
	)
	if err != nil {
		return nil, fmt.Errorf("get pending action: %w", err)
	}
	return a, nil
}

// GetPending returns the pending action for the given game, target player, and action type.
func (r *PendingActionRepo) GetPending(ctx context.Context, gameID int64, targetTgID int64, actionType domain.ActionType) (*domain.PendingAction, error) {
	a := &domain.PendingAction{}
	err := r.db.QueryRow(ctx, `
		SELECT id, game_id, action_type, requester_tg_id, target_tg_id, payload,
		       status, chat_id, message_id, created_at, resolved_at, resolved_by_tg_id
		FROM pending_actions
		WHERE game_id = $1 AND target_tg_id = $2 AND action_type = $3 AND status = 'pending'
	`, gameID, targetTgID, actionType).Scan(
		&a.ID, &a.GameID, &a.ActionType, &a.RequesterTgID, &a.TargetTgID, &a.Payload,
		&a.Status, &a.ChatID, &a.MessageID, &a.CreatedAt, &a.ResolvedAt, &a.ResolvedByTgID,
	)
	if err != nil {
		return nil, fmt.Errorf("get pending action by target: %w", err)
	}
	return a, nil
}

// ListPendingByGame returns all unresolved pending actions for a game ordered by creation time.
func (r *PendingActionRepo) ListPendingByGame(ctx context.Context, gameID int64) ([]domain.PendingAction, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, game_id, action_type, requester_tg_id, target_tg_id, payload,
		       status, chat_id, message_id, created_at, resolved_at, resolved_by_tg_id
		FROM pending_actions
		WHERE game_id = $1 AND status = 'pending'
		ORDER BY created_at
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list pending actions: %w", err)
	}
	defer rows.Close()

	var result []domain.PendingAction
	for rows.Next() {
		var a domain.PendingAction
		if err := rows.Scan(
			&a.ID, &a.GameID, &a.ActionType, &a.RequesterTgID, &a.TargetTgID, &a.Payload,
			&a.Status, &a.ChatID, &a.MessageID, &a.CreatedAt, &a.ResolvedAt, &a.ResolvedByTgID,
		); err != nil {
			return nil, fmt.Errorf("scan pending action: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// Resolve idempotently updates the action status. Returns nil if already resolved.
func (r *PendingActionRepo) Resolve(ctx context.Context, id int64, status domain.PendingStatus, resolvedByTgID int64) (*domain.PendingAction, error) {
	a := &domain.PendingAction{}
	err := r.db.QueryRow(ctx, `
		UPDATE pending_actions
		SET status = $1, resolved_at = now(), resolved_by_tg_id = $2
		WHERE id = $3 AND status = 'pending'
		RETURNING id, game_id, action_type, requester_tg_id, target_tg_id, payload,
		          status, chat_id, message_id, created_at, resolved_at, resolved_by_tg_id
	`, status, resolvedByTgID, id).Scan(
		&a.ID, &a.GameID, &a.ActionType, &a.RequesterTgID, &a.TargetTgID, &a.Payload,
		&a.Status, &a.ChatID, &a.MessageID, &a.CreatedAt, &a.ResolvedAt, &a.ResolvedByTgID,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve pending action: %w", err)
	}
	return a, nil
}

// CancelByPlayer cancels all pending actions for a player in a game and returns them.
func (r *PendingActionRepo) CancelByPlayer(ctx context.Context, gameID int64, playerTgID int64) ([]domain.PendingAction, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE pending_actions
		SET status = 'cancelled', resolved_at = now()
		WHERE game_id = $1 AND target_tg_id = $2 AND status = 'pending'
		RETURNING id, game_id, action_type, requester_tg_id, target_tg_id, payload,
		          status, chat_id, message_id, created_at, resolved_at, resolved_by_tg_id
	`, gameID, playerTgID)
	if err != nil {
		return nil, fmt.Errorf("cancel pending actions by player: %w", err)
	}
	defer rows.Close()

	var result []domain.PendingAction
	for rows.Next() {
		var a domain.PendingAction
		if err := rows.Scan(
			&a.ID, &a.GameID, &a.ActionType, &a.RequesterTgID, &a.TargetTgID, &a.Payload,
			&a.Status, &a.ChatID, &a.MessageID, &a.CreatedAt, &a.ResolvedAt, &a.ResolvedByTgID,
		); err != nil {
			return nil, fmt.Errorf("scan cancelled pending action: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// ExpireOlderThan marks all still-pending actions older than olderThan as expired and returns the count.
func (r *PendingActionRepo) ExpireOlderThan(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE pending_actions
		SET status = 'expired', resolved_at = now()
		WHERE status = 'pending' AND created_at < now() - $1::interval
	`, olderThan.String())
	if err != nil {
		return 0, fmt.Errorf("expire pending actions: %w", err)
	}
	return tag.RowsAffected(), nil
}

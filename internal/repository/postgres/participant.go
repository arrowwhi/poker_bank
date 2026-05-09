package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
)

// ParticipantRepo implements interfaces.ParticipantRepository using a PostgreSQL connection pool.
type ParticipantRepo struct {
	db *pgxpool.Pool
}

// NewParticipantRepo creates a new ParticipantRepo backed by the given connection pool.
func NewParticipantRepo(db *pgxpool.Pool) *ParticipantRepo {
	return &ParticipantRepo{db: db}
}

// Create inserts a new participant or updates the is_active flag if already present.
func (r *ParticipantRepo) Create(ctx context.Context, p *domain.Participant) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO participants (game_id, player_tg_id, is_active)
		VALUES ($1, $2, $3)
		ON CONFLICT (game_id, player_tg_id) DO UPDATE SET is_active = EXCLUDED.is_active
	`, p.GameID, p.PlayerTgID, p.IsActive)
	if err != nil {
		return fmt.Errorf("create participant: %w", err)
	}
	return nil
}

// GetByPlayer returns the participant record for a given game and player.
func (r *ParticipantRepo) GetByPlayer(ctx context.Context, gameID int64, playerTgID int64) (*domain.Participant, error) {
	p := &domain.Participant{}
	err := r.db.QueryRow(ctx, `
		SELECT game_id, player_tg_id, is_active, joined_at
		FROM participants WHERE game_id = $1 AND player_tg_id = $2
	`, gameID, playerTgID).Scan(&p.GameID, &p.PlayerTgID, &p.IsActive, &p.JoinedAt)
	if err != nil {
		return nil, fmt.Errorf("get participant: %w", err)
	}
	return p, nil
}

// ListByGame returns all participants (active and inactive) for a game.
func (r *ParticipantRepo) ListByGame(ctx context.Context, gameID int64) ([]domain.Participant, error) {
	return r.list(ctx, gameID, false)
}

// ListActive returns only active participants for a game.
func (r *ParticipantRepo) ListActive(ctx context.Context, gameID int64) ([]domain.Participant, error) {
	return r.list(ctx, gameID, true)
}

func (r *ParticipantRepo) list(ctx context.Context, gameID int64, onlyActive bool) ([]domain.Participant, error) {
	query := `SELECT game_id, player_tg_id, is_active, joined_at FROM participants WHERE game_id = $1`
	if onlyActive {
		query += ` AND is_active = true`
	}

	rows, err := r.db.Query(ctx, query, gameID)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()

	var ps []domain.Participant
	for rows.Next() {
		var p domain.Participant
		if err = rows.Scan(&p.GameID, &p.PlayerTgID, &p.IsActive, &p.JoinedAt); err != nil {
			return nil, fmt.Errorf("scan participant: %w", err)
		}
		ps = append(ps, p)
	}
	return ps, rows.Err()
}

// SetActive updates the is_active flag for a participant.
func (r *ParticipantRepo) SetActive(ctx context.Context, gameID int64, playerTgID int64, active bool) error {
	_, err := r.db.Exec(ctx,
		`UPDATE participants SET is_active = $1 WHERE game_id = $2 AND player_tg_id = $3`,
		active, gameID, playerTgID,
	)
	if err != nil {
		return fmt.Errorf("set participant active: %w", err)
	}
	return nil
}

package postgres

import (
	"context"
	"fmt"

	infrapg "github.com/arrowwhi/infrastructure/connections/postgres"

	"poker_bank/internal/domain"
)

// SettlementRepo is a PostgreSQL implementation of interfaces.SettlementRepository.
type SettlementRepo struct {
	db *infrapg.Pool
}

// NewSettlementRepo creates a new SettlementRepo backed by the given connection pool.
func NewSettlementRepo(db *infrapg.Pool) *SettlementRepo {
	return &SettlementRepo{db: db}
}

// InsertBulk inserts a slice of settlements into the database.
func (r *SettlementRepo) InsertBulk(ctx context.Context, settlements []domain.Settlement) error {
	for _, s := range settlements {
		_, err := r.db.Exec(ctx, `
			INSERT INTO settlements (game_id, from_tg_id, to_tg_id, amount_rub)
			VALUES ($1,$2,$3,$4)
		`, s.GameID, s.FromTgID, s.ToTgID, s.AmountRub)
		if err != nil {
			return fmt.Errorf("insert settlement: %w", err)
		}
	}
	return nil
}

// ListByGame returns all settlements for the given game ordered by ID.
func (r *SettlementRepo) ListByGame(ctx context.Context, gameID int64) ([]domain.Settlement, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, game_id, from_tg_id, to_tg_id, amount_rub, is_paid, paid_at, is_received, received_at
		FROM settlements WHERE game_id = $1 ORDER BY id
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list settlements: %w", err)
	}
	defer rows.Close()

	var ss []domain.Settlement
	for rows.Next() {
		var s domain.Settlement
		if err = rows.Scan(&s.ID, &s.GameID, &s.FromTgID, &s.ToTgID, &s.AmountRub, &s.IsPaid, &s.PaidAt, &s.IsReceived, &s.ReceivedAt); err != nil {
			return nil, fmt.Errorf("scan settlement: %w", err)
		}
		ss = append(ss, s)
	}
	return ss, rows.Err()
}

// MarkPaid marks a settlement as paid and records the payment timestamp.
func (r *SettlementRepo) MarkPaid(ctx context.Context, id int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE settlements SET is_paid = true, paid_at = now() WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("mark settlement paid: %w", err)
	}
	return nil
}

// MarkByPlayer marks the player's outgoing settlements as paid and incoming ones
// as received within a single transaction. Already-marked rows are left untouched,
// so repeated calls are idempotent. Returns the number of changed rows.
func (r *SettlementRepo) MarkByPlayer(ctx context.Context, gameID, tgID int64) (int64, error) {
	var changed int64
	err := r.db.InTx(ctx, func(ctx context.Context) error {
		tag, err := r.db.Exec(ctx, `
			UPDATE settlements SET is_paid = true, paid_at = now()
			WHERE game_id = $1 AND from_tg_id = $2 AND NOT is_paid
		`, gameID, tgID)
		if err != nil {
			return fmt.Errorf("mark settlements paid: %w", err)
		}
		changed += tag.RowsAffected()

		tag, err = r.db.Exec(ctx, `
			UPDATE settlements SET is_received = true, received_at = now()
			WHERE game_id = $1 AND to_tg_id = $2 AND NOT is_received
		`, gameID, tgID)
		if err != nil {
			return fmt.Errorf("mark settlements received: %w", err)
		}
		changed += tag.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}

// ListOpenByPlayer — незакрытые переводы игрока в завершённых играх: исходящие без отметки
// «оплатил» и входящие без отметки «получил».
func (r *SettlementRepo) ListOpenByPlayer(ctx context.Context, tgID int64) ([]domain.Settlement, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.id, s.game_id, s.from_tg_id, s.to_tg_id, s.amount_rub, s.is_paid, s.paid_at, s.is_received, s.received_at
		FROM settlements s
		JOIN games g ON g.id = s.game_id
		WHERE g.status = 'finished'
		  AND ((s.from_tg_id = $1 AND NOT s.is_paid) OR (s.to_tg_id = $1 AND NOT s.is_received))
		ORDER BY g.ended_at DESC, s.id
	`, tgID)
	if err != nil {
		return nil, fmt.Errorf("list open settlements: %w", err)
	}
	defer rows.Close()

	var ss []domain.Settlement
	for rows.Next() {
		var s domain.Settlement
		if err = rows.Scan(&s.ID, &s.GameID, &s.FromTgID, &s.ToTgID, &s.AmountRub, &s.IsPaid, &s.PaidAt, &s.IsReceived, &s.ReceivedAt); err != nil {
			return nil, fmt.Errorf("scan settlement: %w", err)
		}
		ss = append(ss, s)
	}
	return ss, rows.Err()
}

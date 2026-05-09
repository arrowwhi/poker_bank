package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
)

type SettlementRepo struct {
	db *pgxpool.Pool
}

func NewSettlementRepo(db *pgxpool.Pool) *SettlementRepo {
	return &SettlementRepo{db: db}
}

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

func (r *SettlementRepo) ListByGame(ctx context.Context, gameID int64) ([]domain.Settlement, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, game_id, from_tg_id, to_tg_id, amount_rub, is_paid, paid_at
		FROM settlements WHERE game_id = $1 ORDER BY id
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list settlements: %w", err)
	}
	defer rows.Close()

	var ss []domain.Settlement
	for rows.Next() {
		var s domain.Settlement
		if err = rows.Scan(&s.ID, &s.GameID, &s.FromTgID, &s.ToTgID, &s.AmountRub, &s.IsPaid, &s.PaidAt); err != nil {
			return nil, fmt.Errorf("scan settlement: %w", err)
		}
		ss = append(ss, s)
	}
	return ss, rows.Err()
}

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

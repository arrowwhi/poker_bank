package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
)

type LedgerRepo struct {
	db *pgxpool.Pool
}

func NewLedgerRepo(db *pgxpool.Pool) *LedgerRepo {
	return &LedgerRepo{db: db}
}

func (r *LedgerRepo) Create(ctx context.Context, e *domain.LedgerEntry) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx, `
		INSERT INTO ledger (game_id, player_tg_id, type, amount_rub, amount_chips, created_by_tg_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, e.GameID, e.PlayerTgID, e.Type, e.AmountRub, e.AmountChips, e.CreatedByTgID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create ledger entry: %w", err)
	}
	return id, nil
}

func (r *LedgerRepo) ListByGame(ctx context.Context, gameID int64) ([]domain.LedgerEntry, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, game_id, player_tg_id, type, amount_rub, amount_chips,
		       created_by_tg_id, created_at, is_void, void_reason
		FROM ledger WHERE game_id = $1 ORDER BY created_at
	`, gameID)
	if err != nil {
		return nil, fmt.Errorf("list ledger: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err = rows.Scan(
			&e.ID, &e.GameID, &e.PlayerTgID, &e.Type,
			&e.AmountRub, &e.AmountChips, &e.CreatedByTgID,
			&e.CreatedAt, &e.IsVoid, &e.VoidReason,
		); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// VoidLastN marks the last N non-void entries for gameID as void.
func (r *LedgerRepo) VoidLastN(ctx context.Context, gameID int64, n int) ([]domain.LedgerEntry, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE ledger SET is_void = true
		WHERE id IN (
			SELECT id FROM ledger
			WHERE game_id = $1 AND is_void = false
			ORDER BY created_at DESC
			LIMIT $2
		)
		RETURNING id, game_id, player_tg_id, type, amount_rub, amount_chips,
		          created_by_tg_id, created_at, is_void, void_reason
	`, gameID, n)
	if err != nil {
		return nil, fmt.Errorf("void last n: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err = rows.Scan(
			&e.ID, &e.GameID, &e.PlayerTgID, &e.Type,
			&e.AmountRub, &e.AmountChips, &e.CreatedByTgID,
			&e.CreatedAt, &e.IsVoid, &e.VoidReason,
		); err != nil {
			return nil, fmt.Errorf("scan voided entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *LedgerRepo) GetBank(ctx context.Context, gameID int64) (int, error) {
	var bank int
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(
		  SUM(CASE WHEN type IN ('BUY_IN','REBUY') THEN amount_rub
		           WHEN type = 'CASH_OUT'          THEN -amount_rub
		           ELSE 0 END), 0)
		FROM ledger WHERE game_id = $1 AND is_void = false
	`, gameID).Scan(&bank)
	if err != nil {
		return 0, fmt.Errorf("get bank: %w", err)
	}
	return bank, nil
}

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

type GameRepo struct {
	db *pgxpool.Pool
}

func NewGameRepo(db *pgxpool.Pool) *GameRepo {
	return &GameRepo{db: db}
}

func (r *GameRepo) Create(ctx context.Context, g *domain.Game) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx, `
		INSERT INTO games (chat_id, dealer_tg_id, buy_in_rub, buy_in_chips, rebuy_rub, rebuy_chips, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'active')
		RETURNING id
	`, g.ChatID, g.DealerTgID, g.BuyInRub, g.BuyInChips, g.RebuyRub, g.RebuyChips).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create game: %w", err)
	}
	return id, nil
}

func (r *GameRepo) GetByID(ctx context.Context, id int64) (*domain.Game, error) {
	g := &domain.Game{}
	err := r.db.QueryRow(ctx, `
		SELECT id, chat_id, dealer_tg_id, buy_in_rub, buy_in_chips, rebuy_rub, rebuy_chips,
		       status, started_at, ended_at, bank_delta_rub
		FROM games WHERE id = $1
	`, id).Scan(
		&g.ID, &g.ChatID, &g.DealerTgID,
		&g.BuyInRub, &g.BuyInChips, &g.RebuyRub, &g.RebuyChips,
		&g.Status, &g.StartedAt, &g.EndedAt, &g.BankDeltaRub,
	)
	if err != nil {
		return nil, fmt.Errorf("get game by id: %w", err)
	}
	return g, nil
}

func (r *GameRepo) GetActiveByChat(ctx context.Context, chatID int64) (*domain.Game, error) {
	g := &domain.Game{}
	err := r.db.QueryRow(ctx, `
		SELECT id, chat_id, dealer_tg_id, buy_in_rub, buy_in_chips, rebuy_rub, rebuy_chips,
		       status, started_at, ended_at, bank_delta_rub
		FROM games WHERE chat_id = $1 AND status = 'active'
	`, chatID).Scan(
		&g.ID, &g.ChatID, &g.DealerTgID,
		&g.BuyInRub, &g.BuyInChips, &g.RebuyRub, &g.RebuyChips,
		&g.Status, &g.StartedAt, &g.EndedAt, &g.BankDeltaRub,
	)
	if err != nil {
		return nil, fmt.Errorf("get active game: %w", err)
	}
	return g, nil
}

func (r *GameRepo) ListFinishedByChat(ctx context.Context, chatID int64, limit int) ([]domain.Game, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, chat_id, dealer_tg_id, buy_in_rub, buy_in_chips, rebuy_rub, rebuy_chips,
		       status, started_at, ended_at, bank_delta_rub
		FROM games
		WHERE chat_id = $1 AND status = 'finished'
		ORDER BY ended_at DESC
		LIMIT $2
	`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("list finished games: %w", err)
	}
	defer rows.Close()

	var games []domain.Game
	for rows.Next() {
		var g domain.Game
		if err := rows.Scan(
			&g.ID, &g.ChatID, &g.DealerTgID,
			&g.BuyInRub, &g.BuyInChips, &g.RebuyRub, &g.RebuyChips,
			&g.Status, &g.StartedAt, &g.EndedAt, &g.BankDeltaRub,
		); err != nil {
			return nil, fmt.Errorf("scan game: %w", err)
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

func (r *GameRepo) UpdateDealer(ctx context.Context, id int64, dealerTgID int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE games SET dealer_tg_id = $1 WHERE id = $2`,
		dealerTgID, id,
	)
	if err != nil {
		return fmt.Errorf("update dealer: %w", err)
	}
	return nil
}

// Finish executes the end-game transaction: writes game_results, settlements, updates status.
func (r *GameRepo) Finish(ctx context.Context, params interfaces.FinishGameParams) error {
	return pgx.BeginTxFunc(ctx, r.db, pgx.TxOptions{}, func(tx pgx.Tx) error {
		for _, res := range params.Results {
			_, err := tx.Exec(ctx, `
				INSERT INTO game_results
				  (game_id, player_tg_id, buy_in_count, rebuy_count,
				   total_in_rub, total_out_rub, total_out_chips, net_rub)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			`, res.GameID, res.PlayerTgID, res.BuyInCount, res.RebuyCount,
				res.TotalInRub, res.TotalOutRub, res.TotalOutChips, res.NetRub,
			)
			if err != nil {
				return fmt.Errorf("insert game_result: %w", err)
			}
		}

		for _, s := range params.Settlements {
			_, err := tx.Exec(ctx, `
				INSERT INTO settlements (game_id, from_tg_id, to_tg_id, amount_rub)
				VALUES ($1,$2,$3,$4)
			`, s.GameID, s.FromTgID, s.ToTgID, s.AmountRub)
			if err != nil {
				return fmt.Errorf("insert settlement: %w", err)
			}
		}

		_, err := tx.Exec(ctx, `
			UPDATE games SET status = 'finished', ended_at = now(), bank_delta_rub = $1 WHERE id = $2
		`, params.BankDeltaRub, params.GameID)
		if err != nil {
			return fmt.Errorf("finish game: %w", err)
		}
		return nil
	})
}

func (r *GameRepo) Cancel(ctx context.Context, id int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE games SET status = 'cancelled', ended_at = now() WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("cancel game: %w", err)
	}
	return nil
}

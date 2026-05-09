package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"poker_bank/internal/domain"
)

// GameResultRepo is a PostgreSQL implementation of GameResultRepository.
type GameResultRepo struct {
	db *pgxpool.Pool
}

// NewGameResultRepo creates a new GameResultRepo.
func NewGameResultRepo(db *pgxpool.Pool) *GameResultRepo {
	return &GameResultRepo{db: db}
}

// InsertBulk inserts multiple game results in a loop.
func (r *GameResultRepo) InsertBulk(ctx context.Context, results []domain.GameResult) error {
	for _, res := range results {
		_, err := r.db.Exec(ctx, `
			INSERT INTO game_results
			  (game_id, player_tg_id, buy_in_count, rebuy_count,
			   total_in_rub, total_out_rub, total_out_chips, net_rub)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		`, res.GameID, res.PlayerTgID, res.BuyInCount, res.RebuyCount,
			res.TotalInRub, res.TotalOutRub, res.TotalOutChips, res.NetRub,
		)
		if err != nil {
			return fmt.Errorf("insert game result: %w", err)
		}
	}
	return nil
}

// ListByGame returns all results for the given game.
func (r *GameResultRepo) ListByGame(ctx context.Context, gameID int64) ([]domain.GameResult, error) {
	return r.list(ctx, `WHERE game_id = $1`, gameID)
}

// GetLeaderboard returns aggregated player stats for the given chat.
func (r *GameResultRepo) GetLeaderboard(ctx context.Context, chatID int64) ([]domain.PlayerChatStats, error) {
	rows, err := r.db.Query(ctx, `
		SELECT gr.player_tg_id,
		       COUNT(*)                                    AS game_count,
		       SUM(gr.net_rub)                             AS total_net_rub,
		       COUNT(*) FILTER (WHERE gr.net_rub > 0)     AS wins_count
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id = $1 AND g.status = 'finished'
		GROUP BY gr.player_tg_id
		ORDER BY total_net_rub DESC
	`, chatID)
	if err != nil {
		return nil, fmt.Errorf("get leaderboard: %w", err)
	}
	defer rows.Close()
	var stats []domain.PlayerChatStats
	for rows.Next() {
		var s domain.PlayerChatStats
		if err := rows.Scan(&s.PlayerTgID, &s.GameCount, &s.TotalNetRub, &s.WinsCount); err != nil {
			return nil, fmt.Errorf("scan leaderboard row: %w", err)
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// ListByPlayer returns results for the given player in the given chat.
func (r *GameResultRepo) ListByPlayer(ctx context.Context, playerTgID int64, chatID int64) ([]domain.GameResult, error) {
	rows, err := r.db.Query(ctx, `
		SELECT gr.game_id, gr.player_tg_id, gr.buy_in_count, gr.rebuy_count,
		       gr.total_in_rub, gr.total_out_rub, gr.total_out_chips, gr.net_rub
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE gr.player_tg_id = $1 AND g.chat_id = $2 AND g.status = 'finished'
		ORDER BY g.ended_at DESC
	`, playerTgID, chatID)
	if err != nil {
		return nil, fmt.Errorf("list game results by player: %w", err)
	}
	defer rows.Close()
	return scanGameResults(rows)
}

func (r *GameResultRepo) list(ctx context.Context, where string, args ...any) ([]domain.GameResult, error) {
	rows, err := r.db.Query(ctx, `
		SELECT game_id, player_tg_id, buy_in_count, rebuy_count,
		       total_in_rub, total_out_rub, total_out_chips, net_rub
		FROM game_results `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list game results: %w", err)
	}
	defer rows.Close()
	return scanGameResults(rows)
}

func scanGameResults(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]domain.GameResult, error) {
	var results []domain.GameResult
	for rows.Next() {
		var res domain.GameResult
		if err := rows.Scan(
			&res.GameID, &res.PlayerTgID, &res.BuyInCount, &res.RebuyCount,
			&res.TotalInRub, &res.TotalOutRub, &res.TotalOutChips, &res.NetRub,
		); err != nil {
			return nil, fmt.Errorf("scan game result: %w", err)
		}
		results = append(results, res)
	}
	return results, rows.Err()
}

package postgres

import (
	"context"
	"fmt"

	infrapg "github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/jackc/pgx/v5"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

// GameRepo is a PostgreSQL implementation of interfaces.GameRepository.
type GameRepo struct {
	db *infrapg.Pool
}

// NewGameRepo creates a new GameRepo backed by the given connection pool.
func NewGameRepo(db *infrapg.Pool) *GameRepo {
	return &GameRepo{db: db}
}

// Create inserts a new game record and returns its generated ID.
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

// GetByID retrieves a game by its primary key.
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

// LockByID читает игру и блокирует её строку до конца транзакции (SELECT … FOR UPDATE).
// Вызывается первым в каждой изменяющей транзакции стола: операции над одной игрой
// выполняются по очереди, и проверки делаются по актуальному состоянию.
func (r *GameRepo) LockByID(ctx context.Context, id int64) (*domain.Game, error) {
	g := &domain.Game{}
	err := r.db.QueryRow(ctx, `
		SELECT id, chat_id, dealer_tg_id, buy_in_rub, buy_in_chips, rebuy_rub, rebuy_chips,
		       status, started_at, ended_at, bank_delta_rub
		FROM games WHERE id = $1
		FOR UPDATE
	`, id).Scan(
		&g.ID, &g.ChatID, &g.DealerTgID,
		&g.BuyInRub, &g.BuyInChips, &g.RebuyRub, &g.RebuyChips,
		&g.Status, &g.StartedAt, &g.EndedAt, &g.BankDeltaRub,
	)
	if err != nil {
		return nil, fmt.Errorf("lock game: %w", err)
	}
	return g, nil
}

// GetActiveByChat returns the currently active game for the given chat, or an error if none exists.
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

// ListFinishedByChat returns the most recent finished games for a chat, up to limit rows.
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

// ListActiveByPlayer returns active games where the player is the dealer or a participant.
func (r *GameRepo) ListActiveByPlayer(ctx context.Context, tgID int64) ([]domain.Game, error) {
	rows, err := r.db.Query(ctx, `
		SELECT g.id, g.chat_id, g.dealer_tg_id, g.buy_in_rub, g.buy_in_chips, g.rebuy_rub, g.rebuy_chips,
		       g.status, g.started_at, g.ended_at, g.bank_delta_rub
		FROM games g
		WHERE g.status = 'active'
		  AND (g.dealer_tg_id = $1
		       OR EXISTS (SELECT 1 FROM participants p WHERE p.game_id = g.id AND p.player_tg_id = $1))
		ORDER BY g.started_at DESC
	`, tgID)
	if err != nil {
		return nil, fmt.Errorf("list active games by player: %w", err)
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

// UpdateDealer sets a new dealer for the given active game. Если игра уже не активна,
// возвращает ошибку, оборачивающую pgx.ErrNoRows.
func (r *GameRepo) UpdateDealer(ctx context.Context, id int64, dealerTgID int64) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE games SET dealer_tg_id = $1 WHERE id = $2 AND status = 'active'`,
		dealerTgID, id,
	)
	if err != nil {
		return fmt.Errorf("update dealer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update dealer: game %d is not active: %w", id, pgx.ErrNoRows)
	}
	return nil
}

// Finish executes the end-game transaction: updates status, writes game_results and settlements.
// Статус меняется только у активной игры; иначе — ошибка, оборачивающая pgx.ErrNoRows,
// и ничего не записывается.
func (r *GameRepo) Finish(ctx context.Context, params interfaces.FinishGameParams) error {
	return r.db.InTx(ctx, func(ctx context.Context) error {
		tag, err := r.db.Exec(ctx, `
			UPDATE games SET status = 'finished', ended_at = now(), bank_delta_rub = $1
			WHERE id = $2 AND status = 'active'
		`, params.BankDeltaRub, params.GameID)
		if err != nil {
			return fmt.Errorf("finish game: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("finish game: game %d is not active: %w", params.GameID, pgx.ErrNoRows)
		}

		for _, res := range params.Results {
			_, err := r.db.Exec(ctx, `
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
			_, err := r.db.Exec(ctx, `
				INSERT INTO settlements (game_id, from_tg_id, to_tg_id, amount_rub)
				VALUES ($1,$2,$3,$4)
			`, s.GameID, s.FromTgID, s.ToTgID, s.AmountRub)
			if err != nil {
				return fmt.Errorf("insert settlement: %w", err)
			}
		}
		return nil
	})
}

// Cancel marks an active game as cancelled and records the end time. Если игра уже
// не активна, возвращает ошибку, оборачивающую pgx.ErrNoRows.
func (r *GameRepo) Cancel(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE games SET status = 'cancelled', ended_at = now() WHERE id = $1 AND status = 'active'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("cancel game: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("cancel game: game %d is not active: %w", id, pgx.ErrNoRows)
	}
	return nil
}

const gameCols = `g.id, g.chat_id, g.dealer_tg_id, g.buy_in_rub, g.buy_in_chips, g.rebuy_rub, g.rebuy_chips,
		       g.status, g.started_at, g.ended_at, g.bank_delta_rub`

func (r *GameRepo) queryGames(ctx context.Context, query string, args ...any) ([]domain.Game, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query games: %w", err)
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

// ListVisible возвращает игры, которые видит пользователь: в его группах или с его участием.
// chatID = 0 — все группы; status = "" — любой статус.
//
// Приведение $2::bigint обязательно: по `$2 = 0` Postgres выводит тип параметра как integer,
// и id супергруппы (-100xxxxxxxxxx) не кодируется в int4.
func (r *GameRepo) ListVisible(ctx context.Context, tgID, chatID int64, status domain.GameStatus, limit int) ([]domain.Game, error) {
	return r.queryGames(ctx, `
		SELECT `+gameCols+`
		FROM games g
		WHERE ($2::bigint = 0 OR g.chat_id = $2)
		  AND ($3::text = '' OR g.status = $3)
		  AND (g.dealer_tg_id = $1
		       OR EXISTS (SELECT 1 FROM chat_members m WHERE m.chat_id = g.chat_id AND m.player_tg_id = $1)
		       OR EXISTS (SELECT 1 FROM participants p WHERE p.game_id = g.id AND p.player_tg_id = $1))
		ORDER BY g.started_at DESC
		LIMIT $4`, tgID, chatID, string(status), limit)
}

// GetLastByChatStatus — последняя по started_at игра группы в заданном статусе; nil, если таких нет.
func (r *GameRepo) GetLastByChatStatus(ctx context.Context, chatID int64, status domain.GameStatus) (*domain.Game, error) {
	games, err := r.queryGames(ctx, `
		SELECT `+gameCols+` FROM games g
		WHERE g.chat_id = $1 AND g.status = $2
		ORDER BY g.started_at DESC LIMIT 1`, chatID, string(status))
	if err != nil || len(games) == 0 {
		return nil, err
	}
	return &games[0], nil
}

// Number — порядковый номер игры в её группе («Игра #13»).
func (r *GameRepo) Number(ctx context.Context, g *domain.Game) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM games WHERE chat_id = $1 AND id <= $2`, g.ChatID, g.ID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("game number: %w", err)
	}
	return n, nil
}

// SetSettleMessageID запоминает сообщение с планом переводов.
func (r *GameRepo) SetSettleMessageID(ctx context.Context, id, messageID int64) error {
	if _, err := r.db.Exec(ctx, `UPDATE games SET settle_message_id = $1 WHERE id = $2`, messageID, id); err != nil {
		return fmt.Errorf("set settle message id: %w", err)
	}
	return nil
}

// GetSettleMessageID — сообщение с планом переводов или nil.
func (r *GameRepo) GetSettleMessageID(ctx context.Context, id int64) (*int64, error) {
	var msgID *int64
	if err := r.db.QueryRow(ctx, `SELECT settle_message_id FROM games WHERE id = $1`, id).Scan(&msgID); err != nil {
		return nil, fmt.Errorf("get settle message id: %w", err)
	}
	return msgID, nil
}

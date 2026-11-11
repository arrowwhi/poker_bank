package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

// GameService coordinates game lifecycle, ledger operations, and result calculations.
type GameService struct {
	games        interfaces.GameRepository
	ledger       interfaces.LedgerRepository
	participants interfaces.ParticipantRepository
	results      interfaces.GameResultRepository
	settlements  interfaces.SettlementRepository
	pending      interfaces.PendingActionRepository
	chats        interfaces.ChatRepository
	tx           interfaces.TxManager
	exporter     interfaces.SheetsExporter
	log          *zap.Logger
}

// NewGameService creates a GameService wired to the provided repository implementations.
func NewGameService(
	games interfaces.GameRepository,
	ledger interfaces.LedgerRepository,
	participants interfaces.ParticipantRepository,
	results interfaces.GameResultRepository,
	settlements interfaces.SettlementRepository,
	pending interfaces.PendingActionRepository,
	chats interfaces.ChatRepository,
	tx interfaces.TxManager,
	exporter interfaces.SheetsExporter,
	log *zap.Logger,
) *GameService {
	return &GameService{
		games:        games,
		ledger:       ledger,
		participants: participants,
		results:      results,
		settlements:  settlements,
		pending:      pending,
		chats:        chats,
		tx:           tx,
		exporter:     exporter,
		log:          log,
	}
}

// NewGame creates a new game record and returns its generated ID.
func (s *GameService) NewGame(ctx context.Context, g *domain.Game) (int64, error) {
	id, err := s.games.Create(ctx, g)
	if err != nil {
		return 0, err
	}
	s.createSheetAsync(id)
	return id, nil
}

// createSheetAsync заводит лист Google Sheets для игры в фоне. Вызывать после коммита
// транзакции, создавшей игру: экспортёр читает её отдельным соединением.
func (s *GameService) createSheetAsync(gameID int64) {
	go func() {
		if err := s.exporter.CreateGameSheet(context.Background(), gameID); err != nil {
			s.log.Error("создание листа Google Sheets", zap.Int64("game_id", gameID), zap.Error(err))
		}
	}()
}

// syncSheetAsync пересчитывает и перезаписывает лист Google Sheets в фоне; ошибка не блокирует вызывающего.
func (s *GameService) syncSheetAsync(gameID int64) {
	go func() {
		if err := s.exporter.SyncGame(context.Background(), gameID); err != nil {
			s.log.Error("синхронизация листа Google Sheets", zap.Int64("game_id", gameID), zap.Error(err))
		}
	}()
}

// GetActiveGame returns the active game for a chat, or ErrNoActiveGame if none exists.
func (s *GameService) GetActiveGame(ctx context.Context, chatID int64) (*domain.Game, error) {
	g, err := s.games.GetActiveByChat(ctx, chatID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoActiveGame
		}
		return nil, err
	}
	return g, nil
}

// finish завершает игру внутри транзакции вызывающего (игра заблокирована): считает
// агрегаты и переводы по ledger и записывает их вместе со сменой статуса (ТЗ §6).
// bankDelta — расхождение банка (0 для обычного /endgame, != 0 для /endgame_force).
// Возвращает рассчитанный план переводов для отображения в чате.
func (s *GameService) finish(ctx context.Context, gameID int64, bankDelta int) ([]domain.Settlement, error) {
	entries, err := s.ledger.ListByGame(ctx, gameID)
	if err != nil {
		return nil, err
	}
	results := ComputeGameResults(gameID, entries)

	// Для расчёта переводов применяем дельту; в game_results пишем оригинальные данные
	settleResults := results
	if bankDelta != 0 {
		settleResults = ApplyBankDelta(results, bankDelta)
	}
	settlements := ComputeSettlements(gameID, settleResults)

	if err := s.games.Finish(ctx, interfaces.FinishGameParams{
		GameID:       gameID,
		Results:      results,
		Settlements:  settlements,
		BankDeltaRub: bankDelta,
	}); err != nil {
		return nil, notActiveErr(err)
	}
	return settlements, nil
}

// ListSettlements returns the transfer plan of a finished game.
func (s *GameService) ListSettlements(ctx context.Context, gameID int64) ([]domain.Settlement, error) {
	return s.settlements.ListByGame(ctx, gameID)
}

// MarkSettlementsByPlayer marks the player's transfers as paid (outgoing) and
// received (incoming). Returns the number of changed transfers.
func (s *GameService) MarkSettlementsByPlayer(ctx context.Context, gameID, tgID int64) (int64, error) {
	return s.settlements.MarkByPlayer(ctx, gameID, tgID)
}

// GetGame retrieves a game by its primary key.
func (s *GameService) GetGame(ctx context.Context, id int64) (*domain.Game, error) {
	return s.games.GetByID(ctx, id)
}

// recordBuyIn пишет BUY_IN и участника внутри транзакции вызывающего.
func (s *GameService) recordBuyIn(ctx context.Context, game *domain.Game, playerTgID int64, createdByTgID int64) error {
	entry := &domain.LedgerEntry{
		GameID:        game.ID,
		PlayerTgID:    playerTgID,
		Type:          domain.LedgerBuyIn,
		AmountRub:     game.BuyInRub,
		AmountChips:   game.BuyInChips,
		CreatedByTgID: createdByTgID,
	}
	if _, err := s.ledger.Create(ctx, entry); err != nil {
		return err
	}
	return s.participants.Create(ctx, &domain.Participant{
		GameID:     game.ID,
		PlayerTgID: playerTgID,
		IsActive:   true,
	})
}

// recordRebuy пишет REBUY внутри транзакции вызывающего.
func (s *GameService) recordRebuy(ctx context.Context, game *domain.Game, playerTgID int64, createdByTgID int64) error {
	entry := &domain.LedgerEntry{
		GameID:        game.ID,
		PlayerTgID:    playerTgID,
		Type:          domain.LedgerRebuy,
		AmountRub:     game.RebuyRub,
		AmountChips:   game.RebuyChips,
		CreatedByTgID: createdByTgID,
	}
	_, err := s.ledger.Create(ctx, entry)
	return err
}

// recordCashOut пишет CASH_OUT, снимает игрока со стола и отменяет его висящие запросы
// (например, REBUY) внутри транзакции вызывающего. Возвращает отменённые запросы, чтобы
// вызывающий поправил их сообщения в Telegram.
func (s *GameService) recordCashOut(ctx context.Context, game *domain.Game, playerTgID int64, chips int, dealerTgID int64) ([]domain.PendingAction, error) {
	amountRub, ok := game.ChipsToRub(chips)
	if !ok {
		return nil, ErrInvalidChipsAmount
	}

	entry := &domain.LedgerEntry{
		GameID:        game.ID,
		PlayerTgID:    playerTgID,
		Type:          domain.LedgerCashOut,
		AmountRub:     amountRub,
		AmountChips:   chips,
		CreatedByTgID: dealerTgID,
	}
	if _, err := s.ledger.Create(ctx, entry); err != nil {
		return nil, err
	}
	if err := s.participants.SetActive(ctx, game.ID, playerTgID, false); err != nil {
		return nil, err
	}
	return s.pending.CancelByPlayer(ctx, game.ID, playerTgID)
}

// UndoLast аннулирует последние N записей леджера и восстанавливает места игроков.
// Возвращает аннулированные записи для отображения в чате. Вызывается внутри
// транзакции DealerUndo, где игра заблокирована и проверены права дилера.
//
// Место каждого затронутого игрока пересчитывается по оставшимся записям, а не по
// типу аннулированной: порядок строк из VoidLastN не гарантирован, и при откате сразу
// нескольких записей одного игрока (buy-in + cash-out) итог не должен от него зависеть.
// Игрок без единой записи удаляется из participants (ТЗ §8.3) — иначе пустая игра
// выглядит сыгранной и «Завершить» записывает её как finished вместо отмены.
func (s *GameService) UndoLast(ctx context.Context, gameID int64, n int) ([]domain.LedgerEntry, error) {
	var voided []domain.LedgerEntry
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if voided, err = s.ledger.VoidLastN(ctx, gameID, n); err != nil || len(voided) == 0 {
			return err
		}
		entries, err := s.ledger.ListByGame(ctx, gameID)
		if err != nil {
			return err
		}
		done := make(map[int64]bool, len(voided))
		for _, e := range voided {
			if done[e.PlayerTgID] {
				continue
			}
			done[e.PlayerTgID] = true

			inGame, active := seatFromLedger(entries, e.PlayerTgID)
			if !inGame {
				err = s.participants.Delete(ctx, gameID, e.PlayerTgID)
			} else {
				err = s.participants.SetActive(ctx, gameID, e.PlayerTgID, active)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return voided, nil
}

// seatFromLedger восстанавливает место игрока по действующим записям игры (в порядке
// создания): inGame — у него есть хотя бы одна запись, active — он за столом
// (после последнего buy-in не было cash-out).
func seatFromLedger(entries []domain.LedgerEntry, playerTgID int64) (inGame, active bool) {
	for _, e := range entries {
		if e.IsVoid || e.PlayerTgID != playerTgID {
			continue
		}
		inGame = true
		switch e.Type {
		case domain.LedgerBuyIn:
			active = true
		case domain.LedgerCashOut:
			active = false
		}
	}
	return inGame, active
}

// GetBank returns the current bank balance for a game.
func (s *GameService) GetBank(ctx context.Context, gameID int64) (int, error) {
	return s.ledger.GetBank(ctx, gameID)
}

// GetAllParticipants returns all participants (active and cashed-out) for a game.
func (s *GameService) GetAllParticipants(ctx context.Context, gameID int64) ([]domain.Participant, error) {
	return s.participants.ListByGame(ctx, gameID)
}

// GetParticipant returns the participation record for a specific player in a game.
func (s *GameService) GetParticipant(ctx context.Context, gameID int64, playerTgID int64) (*domain.Participant, error) {
	return s.participants.GetByPlayer(ctx, gameID, playerTgID)
}

// GetActiveParticipants returns only the currently active (not yet cashed-out) participants.
func (s *GameService) GetActiveParticipants(ctx context.Context, gameID int64) ([]domain.Participant, error) {
	return s.participants.ListActive(ctx, gameID)
}

// GetLedger returns all ledger entries for a game.
func (s *GameService) GetLedger(ctx context.Context, gameID int64) ([]domain.LedgerEntry, error) {
	return s.ledger.ListByGame(ctx, gameID)
}

// ListActiveGamesOfPlayer returns active games where the player deals or plays.
func (s *GameService) ListActiveGamesOfPlayer(ctx context.Context, tgID int64) ([]domain.Game, error) {
	return s.games.ListActiveByPlayer(ctx, tgID)
}

// GetHistory returns the last n finished games for a chat.
func (s *GameService) GetHistory(ctx context.Context, chatID int64, n int) ([]domain.Game, error) {
	return s.games.ListFinishedByChat(ctx, chatID, n)
}

// GetResultsByGame returns per-player result records for a finished game.
func (s *GameService) GetResultsByGame(ctx context.Context, gameID int64) ([]domain.GameResult, error) {
	return s.results.ListByGame(ctx, gameID)
}

// GetLeaderboard returns aggregated stats for all players in a chat, ordered by net profit.
func (s *GameService) GetLeaderboard(ctx context.Context, chatID int64) ([]domain.PlayerChatStats, error) {
	return s.results.GetLeaderboard(ctx, chatID)
}

// GetPlayerStats returns all game results for a specific player within a chat.
func (s *GameService) GetPlayerStats(ctx context.Context, playerTgID int64, chatID int64) ([]domain.GameResult, error) {
	return s.results.ListByPlayer(ctx, playerTgID, chatID)
}

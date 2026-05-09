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
	log *zap.Logger,
) *GameService {
	return &GameService{
		games:        games,
		ledger:       ledger,
		participants: participants,
		results:      results,
		settlements:  settlements,
		pending:      pending,
		log:          log,
	}
}

// NewGame creates a new game record and returns its generated ID.
func (s *GameService) NewGame(ctx context.Context, g *domain.Game) (int64, error) {
	return s.games.Create(ctx, g)
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

// Finish завершает игру: считает агрегаты и переводы, записывает их в одной транзакции.
// bankDelta — расхождение банка (0 для обычного /endgame, != 0 для /endgame_force).
// Возвращает рассчитанный план переводов для отображения в чате.
func (s *GameService) Finish(ctx context.Context, gameID int64, bankDelta int) ([]domain.Settlement, error) {
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
		return nil, err
	}
	return settlements, nil
}

// GetGame retrieves a game by its primary key.
func (s *GameService) GetGame(ctx context.Context, id int64) (*domain.Game, error) {
	return s.games.GetByID(ctx, id)
}

// TransferDealer reassigns the dealer role to another player in the game.
func (s *GameService) TransferDealer(ctx context.Context, gameID int64, newDealerTgID int64) error {
	return s.games.UpdateDealer(ctx, gameID, newDealerTgID)
}

// Cancel marks a game as cancelled without computing results.
func (s *GameService) Cancel(ctx context.Context, gameID int64) error {
	return s.games.Cancel(ctx, gameID)
}

// BuyIn records a BUY_IN ledger entry and adds the player as a participant.
func (s *GameService) BuyIn(ctx context.Context, game *domain.Game, playerTgID int64, createdByTgID int64) error {
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

// Rebuy records a REBUY ledger entry.
func (s *GameService) Rebuy(ctx context.Context, game *domain.Game, playerTgID int64, createdByTgID int64) error {
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

// CashOut records a CASH_OUT ledger entry and deactivates the participant.
func (s *GameService) CashOut(ctx context.Context, game *domain.Game, playerTgID int64, chips int, dealerTgID int64) error {
	amountRub, ok := game.ChipsToRub(chips)
	if !ok {
		return ErrInvalidChipsAmount
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
		return err
	}
	return s.participants.SetActive(ctx, game.ID, playerTgID, false)
}

// UndoLast аннулирует последние N записей леджера и восстанавливает статусы участников.
// Возвращает аннулированные записи для отображения в чате.
func (s *GameService) UndoLast(ctx context.Context, gameID int64, n int) ([]domain.LedgerEntry, error) {
	voided, err := s.ledger.VoidLastN(ctx, gameID, n)
	if err != nil {
		return nil, err
	}
	for _, e := range voided {
		switch e.Type {
		case domain.LedgerBuyIn:
			if err = s.participants.SetActive(ctx, gameID, e.PlayerTgID, false); err != nil {
				return nil, err
			}
		case domain.LedgerCashOut:
			if err = s.participants.SetActive(ctx, gameID, e.PlayerTgID, true); err != nil {
				return nil, err
			}
		}
	}
	return voided, nil
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

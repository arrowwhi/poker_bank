//nolint:revive // interfaces is an internal package name
package interfaces

import (
	"context"
	"time"

	"poker_bank/internal/domain"
)

// PlayerRepository defines persistence operations for players.
type PlayerRepository interface {
	Upsert(ctx context.Context, player *domain.Player) error
	GetByID(ctx context.Context, tgID int64) (*domain.Player, error)
	GetByUsername(ctx context.Context, username string) (*domain.Player, error)
}

// GameRepository defines persistence operations for games.
type GameRepository interface {
	Create(ctx context.Context, game *domain.Game) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.Game, error)
	GetActiveByChat(ctx context.Context, chatID int64) (*domain.Game, error)
	ListFinishedByChat(ctx context.Context, chatID int64, limit int) ([]domain.Game, error)
	UpdateDealer(ctx context.Context, id int64, dealerTgID int64) error
	Finish(ctx context.Context, params FinishGameParams) error
	Cancel(ctx context.Context, id int64) error
}

// FinishGameParams carries all data needed to atomically finish a game.
type FinishGameParams struct {
	GameID       int64
	Results      []domain.GameResult
	Settlements  []domain.Settlement
	BankDeltaRub int
}

// LedgerRepository defines persistence operations for ledger entries.
type LedgerRepository interface {
	Create(ctx context.Context, entry *domain.LedgerEntry) (int64, error)
	ListByGame(ctx context.Context, gameID int64) ([]domain.LedgerEntry, error)
	VoidLastN(ctx context.Context, gameID int64, n int) ([]domain.LedgerEntry, error)
	GetBank(ctx context.Context, gameID int64) (int, error)
}

// ParticipantRepository defines persistence operations for game participants.
type ParticipantRepository interface {
	Create(ctx context.Context, p *domain.Participant) error
	GetByPlayer(ctx context.Context, gameID int64, playerTgID int64) (*domain.Participant, error)
	ListByGame(ctx context.Context, gameID int64) ([]domain.Participant, error)
	ListActive(ctx context.Context, gameID int64) ([]domain.Participant, error)
	SetActive(ctx context.Context, gameID int64, playerTgID int64, active bool) error
}

// PendingActionRepository defines persistence operations for pending actions.
type PendingActionRepository interface {
	Create(ctx context.Context, action *domain.PendingAction) (int64, error)
	GetByID(ctx context.Context, id int64) (*domain.PendingAction, error)
	GetPending(ctx context.Context, gameID int64, targetTgID int64, actionType domain.ActionType) (*domain.PendingAction, error)
	ListPendingByGame(ctx context.Context, gameID int64) ([]domain.PendingAction, error)
	Resolve(ctx context.Context, id int64, status domain.PendingStatus, resolvedByTgID int64) (*domain.PendingAction, error)
	CancelByPlayer(ctx context.Context, gameID int64, playerTgID int64) ([]domain.PendingAction, error)
	ExpireOlderThan(ctx context.Context, olderThan time.Duration) (int64, error)
}

// GameResultRepository defines persistence operations for game results.
type GameResultRepository interface {
	InsertBulk(ctx context.Context, results []domain.GameResult) error
	ListByGame(ctx context.Context, gameID int64) ([]domain.GameResult, error)
	ListByPlayer(ctx context.Context, playerTgID int64, chatID int64) ([]domain.GameResult, error)
	GetLeaderboard(ctx context.Context, chatID int64) ([]domain.PlayerChatStats, error)
}

// SettlementRepository defines persistence operations for settlements.
type SettlementRepository interface {
	InsertBulk(ctx context.Context, settlements []domain.Settlement) error
	ListByGame(ctx context.Context, gameID int64) ([]domain.Settlement, error)
	MarkPaid(ctx context.Context, id int64) error
}

// ChatTopicRepository defines persistence operations for chat-to-topic bindings.
type ChatTopicRepository interface {
	Get(ctx context.Context, chatID int64) (*domain.ChatTopic, error)
	Set(ctx context.Context, chatID int64, topicID int64, setByTgID int64) error
	Delete(ctx context.Context, chatID int64) error
}

// FSMStateRepository defines persistence operations for FSM states.
type FSMStateRepository interface {
	Get(ctx context.Context, chatID int64, userTgID int64) (*domain.FSMState, error)
	Set(ctx context.Context, state *domain.FSMState) error
	Delete(ctx context.Context, chatID int64, userTgID int64) error
}

// SheetsExporter duplicates a game's progress into an external spreadsheet.
type SheetsExporter interface {
	// CreateGameSheet creates a new sheet for the game and writes its initial state.
	CreateGameSheet(ctx context.Context, gameID int64) error
	// SyncGame recomputes the game's current state from the ledger and rewrites
	// the existing sheet created by CreateGameSheet.
	SyncGame(ctx context.Context, gameID int64) error
}

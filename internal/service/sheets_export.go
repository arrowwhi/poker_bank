package service

import (
	"context"
	"fmt"
	"sort"

	"go.uber.org/zap"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

// sheetsClient is the subset of the Google Sheets client used by GoogleSheetsExporter.
type sheetsClient interface {
	CreateSheet(ctx context.Context, title string) error
	WriteRows(ctx context.Context, sheetTitle string, values [][]any) error
}

// GoogleSheetsExporter mirrors a game's progress into a Google Sheets spreadsheet,
// dedicating one sheet (tab) per game that is created at game start and rewritten
// in full after every ledger-changing action.
type GoogleSheetsExporter struct {
	client      sheetsClient
	games       interfaces.GameRepository
	ledger      interfaces.LedgerRepository
	settlements interfaces.SettlementRepository
	players     interfaces.PlayerRepository
	log         *zap.Logger
}

// NewGoogleSheetsExporter creates a GoogleSheetsExporter wired to the given client and repositories.
func NewGoogleSheetsExporter(
	client sheetsClient,
	games interfaces.GameRepository,
	ledger interfaces.LedgerRepository,
	settlements interfaces.SettlementRepository,
	players interfaces.PlayerRepository,
	log *zap.Logger,
) *GoogleSheetsExporter {
	return &GoogleSheetsExporter{
		client:      client,
		games:       games,
		ledger:      ledger,
		settlements: settlements,
		players:     players,
		log:         log,
	}
}

// CreateGameSheet creates a new sheet for the game (titled with its start time and
// ID, which together are guaranteed unique) and writes its initial state to it.
func (e *GoogleSheetsExporter) CreateGameSheet(ctx context.Context, gameID int64) error {
	game, err := e.games.GetByID(ctx, gameID)
	if err != nil {
		return fmt.Errorf("get game: %w", err)
	}
	if err := e.client.CreateSheet(ctx, sheetTitle(game)); err != nil {
		return fmt.Errorf("create sheet: %w", err)
	}
	return e.SyncGame(ctx, gameID)
}

// SyncGame recomputes the game's current totals from the ledger (and, once the
// game is finished, its settlement plan) and rewrites the sheet created by
// CreateGameSheet. The sheet is addressed by its deterministic title — no extra
// state is kept outside the database.
func (e *GoogleSheetsExporter) SyncGame(ctx context.Context, gameID int64) error {
	game, err := e.games.GetByID(ctx, gameID)
	if err != nil {
		return fmt.Errorf("get game: %w", err)
	}
	entries, err := e.ledger.ListByGame(ctx, gameID)
	if err != nil {
		return fmt.Errorf("list ledger: %w", err)
	}
	results := ComputeGameResults(gameID, entries)

	var settlements []domain.Settlement
	if game.Status == domain.GameStatusFinished {
		settlements, err = e.settlements.ListByGame(ctx, gameID)
		if err != nil {
			return fmt.Errorf("list settlements: %w", err)
		}
	}

	dealer := e.playerLabel(ctx, game.DealerTgID)
	rows := e.buildRows(ctx, game, dealer, results, settlements)
	if err := e.client.WriteRows(ctx, sheetTitle(game), rows); err != nil {
		return fmt.Errorf("write rows: %w", err)
	}
	return nil
}

func sheetTitle(game *domain.Game) string {
	return fmt.Sprintf("%s #%d", game.StartedAt.Format("2006-01-02 15:04"), game.ID)
}

func (e *GoogleSheetsExporter) buildRows(
	ctx context.Context,
	game *domain.Game,
	dealer string,
	results []domain.GameResult,
	settlements []domain.Settlement,
) [][]any {
	rows := [][]any{
		{fmt.Sprintf("Игра #%d", game.ID)},
		{"Дата", game.StartedAt.Format("2006-01-02 15:04")},
		{"Курс", fmt.Sprintf("%d₽ / %d фишек", game.BuyInRub, game.BuyInChips)},
		{"Дилер", dealer},
		{},
		{"Игрок", "Байины", "Ребаи", "Внесено (₽)", "Кэшаут (фишки)", "Кэшаут (₽)", "Net (₽)"},
	}
	sort.Slice(results, func(i, j int) bool { return results[i].PlayerTgID < results[j].PlayerTgID })
	for _, r := range results {
		rows = append(rows, []any{
			e.playerLabel(ctx, r.PlayerTgID),
			r.BuyInCount,
			r.RebuyCount,
			r.TotalInRub,
			r.TotalOutChips,
			r.TotalOutRub,
			r.NetRub,
		})
	}

	rows = append(rows, []any{}, []any{"Переводы"})
	if game.Status != domain.GameStatusFinished {
		rows = append(rows, []any{"— игра ещё не завершена —"})
		return rows
	}
	if len(settlements) == 0 {
		rows = append(rows, []any{"Все квиты — переводов нет"})
		return rows
	}
	rows = append(rows, []any{"Из", "Кому", "Сумма (₽)"})
	for _, s := range settlements {
		rows = append(rows, []any{
			e.playerLabel(ctx, s.FromTgID),
			e.playerLabel(ctx, s.ToTgID),
			s.AmountRub,
		})
	}
	return rows
}

func (e *GoogleSheetsExporter) playerLabel(ctx context.Context, tgID int64) string {
	p, err := e.players.GetByID(ctx, tgID)
	if err != nil || p == nil {
		return fmt.Sprintf("id%d", tgID)
	}
	if p.Username != "" {
		return "@" + p.Username
	}
	return p.DisplayName
}

// NoopSheetsExporter is a no-op SheetsExporter used when Google Sheets export is disabled.
type NoopSheetsExporter struct{}

// NewNoopSheetsExporter creates a SheetsExporter that does nothing.
func NewNoopSheetsExporter() *NoopSheetsExporter {
	return &NoopSheetsExporter{}
}

// CreateGameSheet is a no-op.
func (*NoopSheetsExporter) CreateGameSheet(context.Context, int64) error { return nil }

// SyncGame is a no-op.
func (*NoopSheetsExporter) SyncGame(context.Context, int64) error { return nil }

var _ interfaces.SheetsExporter = (*GoogleSheetsExporter)(nil)
var _ interfaces.SheetsExporter = (*NoopSheetsExporter)(nil)

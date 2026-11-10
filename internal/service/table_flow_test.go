package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

// memDB — хранилище в памяти для сценарных тестов стола. Повторяет то, что важно
// сценариям: одна активная игра на чат, upsert участника, откат транзакции.
type memDB struct {
	games        []domain.Game
	ledger       []domain.LedgerEntry
	participants []domain.Participant
	nextID       int64

	// failParticipantCreate — ошибка записи участника (проверка атомарности).
	failParticipantCreate error
	// voidOldestFirst — VoidLastN возвращает записи от старых к новым: порядок
	// RETURNING в Postgres не гарантирован, сервис не должен от него зависеть.
	voidOldestFirst bool
}

func (db *memDB) id() int64 { db.nextID++; return db.nextID }

// InTx откатывает все изменения, если fn вернула ошибку.
func (db *memDB) InTx(ctx context.Context, fn func(context.Context) error) error {
	games := append([]domain.Game(nil), db.games...)
	ledger := append([]domain.LedgerEntry(nil), db.ledger...)
	participants := append([]domain.Participant(nil), db.participants...)
	if err := fn(ctx); err != nil {
		db.games, db.ledger, db.participants = games, ledger, participants
		return err
	}
	return nil
}

type memGames struct {
	interfaces.GameRepository
	db *memDB
}

func (r memGames) Create(_ context.Context, g *domain.Game) (int64, error) {
	for _, x := range r.db.games {
		if x.ChatID == g.ChatID && x.Status == domain.GameStatusActive {
			return 0, errors.New("duplicate key value violates unique constraint games_one_active_per_chat")
		}
	}
	c := *g
	c.ID, c.Status, c.StartedAt = r.db.id(), domain.GameStatusActive, time.Now()
	r.db.games = append(r.db.games, c)
	return c.ID, nil
}

func (r memGames) GetByID(_ context.Context, id int64) (*domain.Game, error) {
	for _, g := range r.db.games {
		if g.ID == id {
			return &g, nil
		}
	}
	return nil, pgx.ErrNoRows
}

func (r memGames) GetActiveByChat(_ context.Context, chatID int64) (*domain.Game, error) {
	for _, g := range r.db.games {
		if g.ChatID == chatID && g.Status == domain.GameStatusActive {
			return &g, nil
		}
	}
	return nil, pgx.ErrNoRows
}

func (r memGames) ListActiveByPlayer(_ context.Context, tgID int64) ([]domain.Game, error) {
	var out []domain.Game
	for _, g := range r.db.games {
		if g.Status != domain.GameStatusActive {
			continue
		}
		in := g.DealerTgID == tgID
		for _, p := range r.db.participants {
			in = in || (p.GameID == g.ID && p.PlayerTgID == tgID)
		}
		if in {
			out = append(out, g)
		}
	}
	return out, nil
}

func (r memGames) setStatus(id int64, st domain.GameStatus) error {
	for i := range r.db.games {
		if r.db.games[i].ID == id {
			r.db.games[i].Status = st
			return nil
		}
	}
	return pgx.ErrNoRows
}

func (r memGames) Cancel(_ context.Context, id int64) error {
	return r.setStatus(id, domain.GameStatusCancelled)
}

func (r memGames) Finish(_ context.Context, p interfaces.FinishGameParams) error {
	return r.setStatus(p.GameID, domain.GameStatusFinished)
}

type memLedger struct{ db *memDB }

func (r memLedger) Create(_ context.Context, e *domain.LedgerEntry) (int64, error) {
	c := *e
	c.ID, c.CreatedAt = r.db.id(), time.Now()
	r.db.ledger = append(r.db.ledger, c)
	return c.ID, nil
}

func (r memLedger) ListByGame(_ context.Context, gameID int64) ([]domain.LedgerEntry, error) {
	var out []domain.LedgerEntry
	for _, e := range r.db.ledger {
		if e.GameID == gameID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r memLedger) VoidLastN(_ context.Context, gameID int64, n int) ([]domain.LedgerEntry, error) {
	var out []domain.LedgerEntry
	for i := len(r.db.ledger) - 1; i >= 0 && len(out) < n; i-- {
		e := &r.db.ledger[i]
		if e.GameID == gameID && !e.IsVoid {
			e.IsVoid = true
			out = append(out, *e)
		}
	}
	if r.db.voidOldestFirst {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, nil
}

func (r memLedger) GetBank(_ context.Context, gameID int64) (int, error) {
	bank := 0
	for _, e := range r.db.ledger {
		switch {
		case e.GameID != gameID || e.IsVoid:
		case e.Type == domain.LedgerCashOut:
			bank -= e.AmountRub
		default:
			bank += e.AmountRub
		}
	}
	return bank, nil
}

type memParticipants struct{ db *memDB }

func (r memParticipants) find(gameID, tgID int64) int {
	for i, p := range r.db.participants {
		if p.GameID == gameID && p.PlayerTgID == tgID {
			return i
		}
	}
	return -1
}

func (r memParticipants) Create(_ context.Context, p *domain.Participant) error {
	if r.db.failParticipantCreate != nil {
		return r.db.failParticipantCreate
	}
	if i := r.find(p.GameID, p.PlayerTgID); i >= 0 {
		r.db.participants[i].IsActive = p.IsActive
		return nil
	}
	r.db.participants = append(r.db.participants, *p)
	return nil
}

func (r memParticipants) GetByPlayer(_ context.Context, gameID, tgID int64) (*domain.Participant, error) {
	if i := r.find(gameID, tgID); i >= 0 {
		p := r.db.participants[i]
		return &p, nil
	}
	return nil, pgx.ErrNoRows
}

func (r memParticipants) list(gameID int64, onlyActive bool) []domain.Participant {
	var out []domain.Participant
	for _, p := range r.db.participants {
		if p.GameID == gameID && (p.IsActive || !onlyActive) {
			out = append(out, p)
		}
	}
	return out
}

func (r memParticipants) ListByGame(_ context.Context, gameID int64) ([]domain.Participant, error) {
	return r.list(gameID, false), nil
}

func (r memParticipants) ListActive(_ context.Context, gameID int64) ([]domain.Participant, error) {
	return r.list(gameID, true), nil
}

func (r memParticipants) SetActive(_ context.Context, gameID, tgID int64, active bool) error {
	if i := r.find(gameID, tgID); i >= 0 {
		r.db.participants[i].IsActive = active
	}
	return nil
}

func (r memParticipants) Delete(_ context.Context, gameID, tgID int64) error {
	if i := r.find(gameID, tgID); i >= 0 {
		r.db.participants = append(r.db.participants[:i:i], r.db.participants[i+1:]...)
	}
	return nil
}

// memPending — запросов в сценариях нет: дилер действует сам.
type memPending struct {
	interfaces.PendingActionRepository
}

func (memPending) GetPending(context.Context, int64, int64, domain.ActionType) (*domain.PendingAction, error) {
	return nil, pgx.ErrNoRows
}
func (memPending) CancelByPlayer(context.Context, int64, int64) ([]domain.PendingAction, error) {
	return nil, nil
}
func (memPending) CancelByGame(context.Context, int64) ([]domain.PendingAction, error) {
	return nil, nil
}

type memSettlements struct {
	interfaces.SettlementRepository
}

func (memSettlements) ListByGame(context.Context, int64) ([]domain.Settlement, error) {
	return nil, nil
}

const (
	testChat   int64 = -1001
	testDealer int64 = 42
)

func newFlowService() (*GameService, *memDB) {
	db := &memDB{}
	return NewGameService(
		memGames{db: db}, memLedger{db}, memParticipants{db},
		nil, memSettlements{}, memPending{}, nil, db,
		NewNoopSheetsExporter(), zap.NewNop(),
	), db
}

// newTable начинает игру 1000₽ → 1000 фишек с авто-buy-in дилера.
func newTable(t *testing.T, s *GameService) *domain.Game {
	t.Helper()
	g, err := s.CreateGameIn(context.Background(), testChat, testDealer, 1000, 1000, 1000, 1000, true)
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	return g
}

func mustActive(t *testing.T, s *GameService, gameID int64, want bool) {
	t.Helper()
	got, err := s.IsActivePlayer(context.Background(), gameID, testDealer)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("dealer active in game %d = %v, want %v", gameID, got, want)
	}
}

func mustRows(t *testing.T, db *memDB, gameID int64, want int) {
	t.Helper()
	if got := len(memParticipants{db}.list(gameID, false)); got != want {
		t.Fatalf("participants rows of game %d = %d, want %d", gameID, got, want)
	}
}

func mustStatus(t *testing.T, s *GameService, gameID int64, want domain.GameStatus) {
	t.Helper()
	g, err := s.GameByID(context.Background(), gameID)
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != want {
		t.Fatalf("game %d status = %s, want %s", gameID, g.Status, want)
	}
}

// Сценарий из баг-репорта: игра с авто-buy-in → «Отменить последнее» → отмена игры →
// новая игра. После отката BUY_IN дилер не должен оставаться участником (ТЗ §8.3),
// а после отмены у него не должно быть активных игр.
func TestAutoBuyInUndoCancelThenNewGame(t *testing.T) {
	ctx := context.Background()
	s, db := newFlowService()

	g1 := newTable(t, s)
	mustActive(t, s, g1.ID, true)

	voided, err := s.DealerUndo(ctx, g1, testDealer, 1)
	if err != nil || len(voided) != 1 || voided[0].Type != domain.LedgerBuyIn {
		t.Fatalf("undo = %+v, %v; want one BUY_IN", voided, err)
	}
	mustActive(t, s, g1.ID, false)
	mustRows(t, db, g1.ID, 0)

	if _, err := s.CancelGame(ctx, g1, testDealer); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	mustStatus(t, s, g1.ID, domain.GameStatusCancelled)
	if active, _ := s.ListActiveGamesOfPlayer(ctx, testDealer); len(active) != 0 {
		t.Fatalf("active games after cancel = %d, want 0", len(active))
	}

	g2 := newTable(t, s)
	if g2.ID == g1.ID {
		t.Fatal("new game reused the cancelled one")
	}
	mustActive(t, s, g2.ID, true)
	mustActive(t, s, g1.ID, false)
	if bank, _ := s.GetBank(ctx, g2.ID); bank != 1000 {
		t.Fatalf("bank of new game = %d, want 1000", bank)
	}
}

// После отката единственного buy-in игра пуста: «Завершить» должно её отменить
// (ТЗ §11), а не записать завершённую игру без результатов.
func TestEndGameAfterUndoneBuyInCancels(t *testing.T) {
	ctx := context.Background()
	s, _ := newFlowService()

	g := newTable(t, s)
	if _, err := s.DealerUndo(ctx, g, testDealer, 1); err != nil {
		t.Fatal(err)
	}
	res, err := s.EndGame(ctx, g, testDealer, false)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if !res.Cancelled {
		t.Fatal("empty game was finished instead of cancelled")
	}
	mustStatus(t, s, g.ID, domain.GameStatusCancelled)

	newTable(t, s)
}

// Игра и buy-in дилера создаются одной транзакцией: если buy-in не записался,
// в чате не должно остаться активной игры без дилера за столом.
func TestCreateGameWithBuyInIsAtomic(t *testing.T) {
	ctx := context.Background()
	s, db := newFlowService()

	boom := errors.New("participants unavailable")
	db.failParticipantCreate = boom
	if _, err := s.CreateGameIn(ctx, testChat, testDealer, 1000, 1000, 1000, 1000, true); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if _, err := s.GetActiveGame(ctx, testChat); !errors.Is(err, ErrNoActiveGame) {
		t.Fatalf("active game after failed create: err = %v, want ErrNoActiveGame", err)
	}
	if len(db.games) != 0 || len(db.ledger) != 0 || len(db.participants) != 0 {
		t.Fatalf("leftovers: games=%d ledger=%d participants=%d", len(db.games), len(db.ledger), len(db.participants))
	}

	db.failParticipantCreate = nil
	g := newTable(t, s)
	mustActive(t, s, g.ID, true)
}

// Прямой buy-in тоже атомарен: без участника не остаётся записи в ledger.
func TestBuyInIsAtomic(t *testing.T) {
	ctx := context.Background()
	s, db := newFlowService()
	g, err := s.CreateGameIn(ctx, testChat, testDealer, 1000, 1000, 1000, 1000, false)
	if err != nil {
		t.Fatal(err)
	}

	db.failParticipantCreate = errors.New("participants unavailable")
	if err := s.BuyIn(ctx, g, testDealer, testDealer); err == nil {
		t.Fatal("buy-in succeeded without participant")
	}
	if bank, _ := s.GetBank(ctx, g.ID); bank != 0 || len(db.ledger) != 0 {
		t.Fatalf("bank = %d, ledger rows = %d after failed buy-in; want 0, 0", bank, len(db.ledger))
	}
}

// Соседние сценарии: после отмены или завершения игры можно начать новую,
// а после отката buy-in — снова сесть за тот же стол.
func TestNewGameAfterGameIsClosed(t *testing.T) {
	ctx := context.Background()

	t.Run("cancel without undo", func(t *testing.T) {
		s, db := newFlowService()
		g1 := newTable(t, s)
		if _, err := s.CancelGame(ctx, g1, testDealer); err != nil {
			t.Fatal(err)
		}
		// Состав отменённой игры сохраняется (ТЗ §8.1) и не мешает новой.
		mustRows(t, db, g1.ID, 1)
		if active, _ := s.ListActiveGamesOfPlayer(ctx, testDealer); len(active) != 0 {
			t.Fatalf("active games after cancel = %d, want 0", len(active))
		}
		g2 := newTable(t, s)
		mustActive(t, s, g2.ID, true)
	})

	t.Run("endgame", func(t *testing.T) {
		s, _ := newFlowService()
		g1 := newTable(t, s)
		if _, err := s.DealerCashOut(ctx, g1, testDealer, testDealer, 1000); err != nil {
			t.Fatal(err)
		}
		res, err := s.EndGame(ctx, g1, testDealer, false)
		if err != nil || res.Cancelled {
			t.Fatalf("end = %+v, %v; want finished", res, err)
		}
		mustStatus(t, s, g1.ID, domain.GameStatusFinished)
		if active, _ := s.ListActiveGamesOfPlayer(ctx, testDealer); len(active) != 0 {
			t.Fatalf("active games after endgame = %d, want 0", len(active))
		}
		g2 := newTable(t, s)
		mustActive(t, s, g2.ID, true)
	})

	t.Run("undo buy-in then buy-in again", func(t *testing.T) {
		s, db := newFlowService()
		g := newTable(t, s)
		if _, err := s.DealerUndo(ctx, g, testDealer, 1); err != nil {
			t.Fatal(err)
		}
		// Запрос дилера самому себе записывается сразу.
		res, err := s.CreateRequest(ctx, g, testDealer, domain.ActionJoin, 0)
		if err != nil || !res.Applied {
			t.Fatalf("join after undo = %+v, %v; want applied", res, err)
		}
		mustActive(t, s, g.ID, true)
		mustRows(t, db, g.ID, 1)
		if bank, _ := s.GetBank(ctx, g.ID); bank != 1000 {
			t.Fatalf("bank = %d, want 1000", bank)
		}
		if _, err := s.CreateRequest(ctx, g, testDealer, domain.ActionJoin, 0); !errors.Is(err, ErrAlreadyInGame) {
			t.Fatalf("second join err = %v, want ErrAlreadyInGame", err)
		}
	})
}

// Откат пересчитывает место игрока по оставшимся записям: вышедший игрок остаётся
// в составе, а игрок без единой записи — удаляется, в каком бы порядке БД ни вернула
// аннулированные строки.
func TestUndoRestoresSeatFromLedger(t *testing.T) {
	ctx := context.Background()

	t.Run("undo re-entry keeps cashed-out player", func(t *testing.T) {
		s, db := newFlowService()
		g := newTable(t, s)
		if _, err := s.DealerCashOut(ctx, g, testDealer, testDealer, 500); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DealerBuyIn(ctx, g, testDealer, testDealer); err != nil {
			t.Fatal(err)
		}
		mustActive(t, s, g.ID, true)

		// Откат повторного входа: игрок снова «вышел», строка остаётся.
		if _, err := s.DealerUndo(ctx, g, testDealer, 1); err != nil {
			t.Fatal(err)
		}
		mustActive(t, s, g.ID, false)
		mustRows(t, db, g.ID, 1)

		// Откат cash-out: игрок снова за столом.
		if _, err := s.DealerUndo(ctx, g, testDealer, 1); err != nil {
			t.Fatal(err)
		}
		mustActive(t, s, g.ID, true)
	})

	for _, oldestFirst := range []bool{false, true} {
		name := "undo buy-in and cash-out at once, newest first"
		if oldestFirst {
			name = "undo buy-in and cash-out at once, oldest first"
		}
		t.Run(name, func(t *testing.T) {
			s, db := newFlowService()
			db.voidOldestFirst = oldestFirst
			g := newTable(t, s)
			if _, err := s.DealerCashOut(ctx, g, testDealer, testDealer, 1000); err != nil {
				t.Fatal(err)
			}
			if voided, err := s.UndoLast(ctx, g.ID, 2); err != nil || len(voided) != 2 {
				t.Fatalf("undo 2 = %d entries, %v", len(voided), err)
			}
			mustActive(t, s, g.ID, false)
			mustRows(t, db, g.ID, 0)
		})
	}
}

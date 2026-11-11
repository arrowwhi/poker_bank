package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"poker_bank/internal/domain"
	"poker_bank/internal/repository/postgres"
	"poker_bank/internal/repository/postgres/pgtest"
)

// Гонки, которые в памяти не воспроизвести: два одновременных действия над одной игрой
// (двойной тап, бот и Mini App сразу). Блокировка строки игры (lockGame) выстраивает их
// в очередь, и второе видит результат первого. Нужен TEST_DATABASE_URL.

func newPgService(t *testing.T) (*GameService, *domain.Game) {
	t.Helper()
	pool := pgtest.Pool(t)
	ctx := context.Background()

	players := postgres.NewPlayerRepo(pool)
	for _, id := range []int64{testDealer, 7} {
		if err := players.Upsert(ctx, &domain.Player{TelegramUserID: id, Username: "p", DisplayName: "P"}); err != nil {
			t.Fatal(err)
		}
	}
	s := NewGameService(
		postgres.NewGameRepo(pool), postgres.NewLedgerRepo(pool), postgres.NewParticipantRepo(pool),
		postgres.NewGameResultRepo(pool), postgres.NewSettlementRepo(pool), postgres.NewPendingActionRepo(pool),
		postgres.NewChatRepo(pool), postgres.NewTxManager(pool), NewNoopSheetsExporter(),
	)
	g, err := s.CreateGameIn(ctx, pgtest.SupergroupChatID, testDealer, 1000, 1000, 1000, 1000, true)
	if err != nil {
		t.Fatal(err)
	}
	return s, g
}

// race запускает n копий fn одновременно и возвращает их ошибки.
func race(n int, fn func() error) []error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
		gate = make(chan struct{})
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			err := fn()
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}()
	}
	close(gate)
	wg.Wait()
	return errs
}

func countOK(errs []error) (ok int, others []error) {
	for _, err := range errs {
		if err == nil {
			ok++
		} else {
			others = append(others, err)
		}
	}
	return ok, others
}

func TestConcurrentCashOutRecordsOnce(t *testing.T) {
	s, g := newPgService(t)
	ctx := context.Background()

	errs := race(5, func() error {
		_, err := s.DealerCashOut(ctx, stale(g), testDealer, testDealer, 1000)
		return err
	})
	ok, others := countOK(errs)
	if ok != 1 {
		t.Fatalf("successful cash-outs = %d, want 1 (errors: %v)", ok, others)
	}
	for _, err := range others {
		if !errors.Is(err, ErrNotInGame) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if bank, err := s.GetBank(ctx, g.ID); err != nil || bank != 0 {
		t.Fatalf("bank = %d, %v; want 0", bank, err)
	}
}

func TestConcurrentBuyInRecordsOnce(t *testing.T) {
	s, g := newPgService(t)
	ctx := context.Background()

	errs := race(5, func() error {
		_, err := s.DealerBuyIn(ctx, stale(g), testDealer, 7)
		return err
	})
	if ok, others := countOK(errs); ok != 1 {
		t.Fatalf("successful buy-ins = %d, want 1 (errors: %v)", ok, others)
	}
	if bank, _ := s.GetBank(ctx, g.ID); bank != 2000 {
		t.Fatalf("bank = %d, want 2000 (dealer + one buy-in)", bank)
	}
}

// Завершение и отмена одновременно: побеждает одно, второе получает ErrGameNotActive,
// и статус игры согласован с тем, записаны ли итоги.
func TestConcurrentEndAndCancel(t *testing.T) {
	s, g := newPgService(t)
	ctx := context.Background()
	if _, err := s.DealerCashOut(ctx, g, testDealer, testDealer, 1000); err != nil {
		t.Fatal(err)
	}

	var i int
	var mu sync.Mutex
	errs := race(2, func() error {
		mu.Lock()
		i++
		first := i == 1
		mu.Unlock()
		if first {
			_, err := s.EndGame(ctx, stale(g), testDealer, false)
			return err
		}
		_, err := s.CancelGame(ctx, stale(g), testDealer)
		return err
	})
	ok, others := countOK(errs)
	if ok != 1 || len(others) != 1 || !errors.Is(others[0], ErrGameNotActive) {
		t.Fatalf("results = %v, want one success and one ErrGameNotActive", errs)
	}

	cur, err := s.GameByID(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	results, err := s.GetResultsByGame(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	switch cur.Status {
	case domain.GameStatusFinished:
		if len(results) == 0 {
			t.Fatal("finished game without results")
		}
	case domain.GameStatusCancelled:
		if len(results) != 0 {
			t.Fatal("cancelled game has results")
		}
	default:
		t.Fatalf("status = %s", cur.Status)
	}
}

// Двойной «Завершить»: второй получает ErrGameNotActive, а не сырую ошибку PK game_results.
func TestDoubleEndGame(t *testing.T) {
	s, g := newPgService(t)
	ctx := context.Background()
	if _, err := s.DealerCashOut(ctx, g, testDealer, testDealer, 1000); err != nil {
		t.Fatal(err)
	}
	errs := race(2, func() error {
		_, err := s.EndGame(ctx, stale(g), testDealer, false)
		return err
	})
	ok, others := countOK(errs)
	if ok != 1 || len(others) != 1 || !errors.Is(others[0], ErrGameNotActive) {
		t.Fatalf("results = %v, want one success and one ErrGameNotActive", errs)
	}
}

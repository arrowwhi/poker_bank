package postgres_test

import (
	"context"
	"testing"
	"time"

	"poker_bank/internal/domain"
	"poker_bank/internal/repository/postgres"
	"poker_bank/internal/repository/postgres/pgtest"
)

// Рейтинг отдаёт сразу три меры: рубли, фишки и сумму входа (для «% от входа»).
// Фишки на входе — неотменённые BUY_IN и REBUY из ledger; отменённые записи и CASH_OUT не считаются.
func TestLeaderboardSince_ChipsAndBuyIn(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	const (
		chatID  int64 = -4000010
		alice   int64 = 7000000010
		bob     int64 = 7000000011
		outside int64 = 7000000012
	)

	// Алиса, игра 1: +500₽, вышла с 1500 фишками, вошла на 1000 (+ отменённый rebuy).
	g1 := pgtest.SeedFinishedGame(t, pool, chatID, alice, 500)
	pgtest.SeedLedger(t, pool, g1, alice, domain.LedgerBuyIn, 1000, 1000)
	pgtest.SeedLedger(t, pool, g1, alice, domain.LedgerRebuy, 1000, 1000)
	if _, err := postgres.NewLedgerRepo(pool).VoidLastN(ctx, g1, 1); err != nil {
		t.Fatalf("void rebuy: %v", err)
	}
	pgtest.SeedLedger(t, pool, g1, alice, domain.LedgerCashOut, 1500, 1500)

	// Алиса, игра 2: −200₽, вышла с 800 фишками; курс другой — вход 1000₽ это 5000 фишек.
	g2 := pgtest.SeedFinishedGame(t, pool, chatID, alice, -200)
	pgtest.SeedLedger(t, pool, g2, alice, domain.LedgerBuyIn, 500, 2500)
	pgtest.SeedLedger(t, pool, g2, alice, domain.LedgerRebuy, 500, 2500)
	pgtest.SeedLedger(t, pool, g2, alice, domain.LedgerCashOut, 800, 800)

	// Боб: +100₽, вход 1000 фишек.
	g3 := pgtest.SeedFinishedGame(t, pool, chatID, bob, 100)
	pgtest.SeedLedger(t, pool, g3, bob, domain.LedgerBuyIn, 1000, 1000)

	// Игра в другой группе в рейтинг не попадает.
	g4 := pgtest.SeedFinishedGame(t, pool, -4000011, outside, 900)
	pgtest.SeedLedger(t, pool, g4, outside, domain.LedgerBuyIn, 1000, 1000)

	repo := postgres.NewGameResultRepo(pool)

	got, err := repo.LeaderboardSince(ctx, chatID, nil)
	if err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	want := []domain.PlayerChatStats{
		{PlayerTgID: alice, GameCount: 2, TotalNetRub: 300, WinsCount: 1, TotalInRub: 2000, TotalNetChips: (1500 - 1000) + (800 - 5000)},
		{PlayerTgID: bob, GameCount: 1, TotalNetRub: 100, WinsCount: 1, TotalInRub: 1000, TotalNetChips: 1100 - 1000},
	}
	if len(got) != len(want) {
		t.Fatalf("leaderboard: got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}

	// Фильтр по периоду действует и на новые суммы.
	future := time.Now().Add(time.Hour)
	none, err := repo.LeaderboardSince(ctx, chatID, &future)
	if err != nil {
		t.Fatalf("leaderboard since future: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("leaderboard since future: got %+v, want empty", none)
	}
}

package service

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"poker_bank/internal/domain"
)

// Свойства расчёта на случайных играх (ТЗ §9, ревью settle-1/3/6). Seed фиксирован —
// падение воспроизводится; в сообщении есть вход.
func TestSettlementProperties(t *testing.T) {
	rnd := rand.New(rand.NewPCG(2026, 11))

	for iter := range 20000 {
		n := 1 + rnd.IntN(10)
		results := make([]domain.GameResult, n)
		for i := range results {
			// Суммы мелкие и крупные: мелкие чаще дают проблемы с округлением.
			scale := []int{3, 50, 5000}[rnd.IntN(3)]
			results[i] = domain.GameResult{PlayerTgID: int64(100 + i), NetRub: rnd.IntN(2*scale+1) - scale}
		}
		delta := 0
		for _, r := range results {
			delta -= r.NetRub // bank = X - Y = -Σnet
		}
		in := fmt.Sprintf("iter %d: net=%v delta=%d", iter, nets(results), delta)

		adj := ApplyBankDelta(results, delta)
		checkAdjusted(t, in, results, adj, delta)

		transfers := ComputeSettlements(1, adj)
		checkTransfers(t, in, adj, transfers)

		// Тот же вход в другом порядке — тот же план.
		shuffled := slices.Clone(results)
		rnd.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		again := ComputeSettlements(1, ApplyBankDelta(sortedByID(shuffled), delta))
		if !slices.Equal(transfers, again) {
			t.Fatalf("%s: plan depends on input order:\n%v\n%v", in, transfers, again)
		}
		if t.Failed() {
			return
		}
	}
}

func checkAdjusted(t *testing.T, in string, before, after []domain.GameResult, delta int) {
	t.Helper()
	sum := 0
	for i := range after {
		b, a := before[i].NetRub, after[i].NetRub
		sum += a
		switch {
		case b < 0 && (a > 0 || a < b):
			t.Fatalf("%s: debtor %d: %d -> %d (flipped or grew)", in, before[i].PlayerTgID, b, a)
		case b > 0 && (a < 0 || a > b):
			t.Fatalf("%s: creditor %d: %d -> %d (flipped or grew)", in, before[i].PlayerTgID, b, a)
		case b == 0 && a != 0:
			t.Fatalf("%s: zero player %d got %d", in, before[i].PlayerTgID, a)
		case delta > 0 && b > 0 && a != b, delta < 0 && b < 0 && a != b:
			t.Fatalf("%s: smaller side changed for %d: %d -> %d", in, before[i].PlayerTgID, b, a)
		}
	}
	if sum != 0 {
		t.Fatalf("%s: Σ adjusted = %d, want 0", in, sum)
	}
}

func checkTransfers(t *testing.T, in string, adj []domain.GameResult, transfers []domain.Settlement) {
	t.Helper()
	balance := map[int64]int{}
	nonZero := 0
	for _, r := range adj {
		if r.NetRub != 0 {
			nonZero++
		}
	}
	for _, s := range transfers {
		if s.AmountRub <= 0 || s.FromTgID == s.ToTgID {
			t.Fatalf("%s: bad transfer %+v", in, s)
		}
		balance[s.FromTgID] -= s.AmountRub
		balance[s.ToTgID] += s.AmountRub
	}
	for _, r := range adj {
		if balance[r.PlayerTgID] != r.NetRub {
			t.Fatalf("%s: player %d gets %d, net %d", in, r.PlayerTgID, balance[r.PlayerTgID], r.NetRub)
		}
	}
	if nonZero > 0 && len(transfers) > nonZero-1 {
		t.Fatalf("%s: %d transfers for %d non-zero players", in, len(transfers), nonZero)
	}
}

func nets(rs []domain.GameResult) []int {
	out := make([]int, len(rs))
	for i, r := range rs {
		out[i] = r.NetRub
	}
	return out
}

func sortedByID(rs []domain.GameResult) []domain.GameResult {
	slices.SortFunc(rs, func(a, b domain.GameResult) int { return int(a.PlayerTgID - b.PlayerTgID) })
	return rs
}

package service

import (
	"testing"

	"poker_bank/internal/domain"
)

func sumNet(results []domain.GameResult) int {
	sum := 0
	for _, r := range results {
		sum += r.NetRub
	}
	return sum
}

func findByTgID(results []domain.GameResult, tgID int64) domain.GameResult {
	for _, r := range results {
		if r.PlayerTgID == tgID {
			return r
		}
	}
	panic("player not found")
}

func TestApplyBankDelta_ZeroDelta_NoOp(t *testing.T) {
	in := []domain.GameResult{
		{PlayerTgID: 1, NetRub: -100},
		{PlayerTgID: 2, NetRub: 100},
	}
	out := ApplyBankDelta(in, 0)
	if len(out) != len(in) {
		t.Fatalf("expected %d results, got %d", len(in), len(out))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("expected %+v unchanged, got %+v", in[i], out[i])
		}
	}
}

func TestApplyBankDelta_PositiveDelta_ReducesDebtors(t *testing.T) {
	in := []domain.GameResult{
		{PlayerTgID: 1, NetRub: -100},
		{PlayerTgID: 2, NetRub: -50},
		{PlayerTgID: 3, NetRub: 140},
	}
	out := ApplyBankDelta(in, 10)

	if sumNet(out) != 0 {
		t.Fatalf("expected sum(NetRub) == 0, got %d", sumNet(out))
	}
	if got := findByTgID(out, 3).NetRub; got != 140 {
		t.Errorf("creditor should be unchanged, got NetRub=%d", got)
	}
	if got := findByTgID(out, 1).NetRub; got != -93 {
		t.Errorf("largest debtor: expected -93 (cut 6 + remainder 1), got %d", got)
	}
	if got := findByTgID(out, 2).NetRub; got != -47 {
		t.Errorf("smaller debtor: expected -47 (cut 3), got %d", got)
	}
}

func TestApplyBankDelta_NegativeDelta_ReducesCreditors(t *testing.T) {
	in := []domain.GameResult{
		{PlayerTgID: 1, NetRub: -140},
		{PlayerTgID: 2, NetRub: 100},
		{PlayerTgID: 3, NetRub: 50},
	}
	out := ApplyBankDelta(in, -10)

	if sumNet(out) != 0 {
		t.Fatalf("expected sum(NetRub) == 0, got %d", sumNet(out))
	}
	if got := findByTgID(out, 1).NetRub; got != -140 {
		t.Errorf("debtor should be unchanged, got NetRub=%d", got)
	}
	if got := findByTgID(out, 2).NetRub; got != 93 {
		t.Errorf("largest creditor: expected 93 (cut 6 + remainder 1), got %d", got)
	}
	if got := findByTgID(out, 3).NetRub; got != 47 {
		t.Errorf("smaller creditor: expected 47 (cut 3), got %d", got)
	}
}

func TestApplyBankDelta_RemainderByLargestFraction(t *testing.T) {
	// Debtors -10,-30,-70 (total X=110), creditor 100 (Y=100), delta=X-Y=10.
	// Exact shares: 0.91, 2.73, 6.36 -> floors 0,2,6 (distributed 8), remainder 2
	// goes to the largest fractional parts: player 1 (.91) and player 2 (.73).
	in := []domain.GameResult{
		{PlayerTgID: 1, NetRub: -10},
		{PlayerTgID: 2, NetRub: -30},
		{PlayerTgID: 3, NetRub: -70},
		{PlayerTgID: 4, NetRub: 100},
	}
	out := ApplyBankDelta(in, 10)

	if sumNet(out) != 0 {
		t.Fatalf("expected sum(NetRub) == 0, got %d", sumNet(out))
	}
	want := map[int64]int{1: -9, 2: -27, 3: -64, 4: 100}
	for id, w := range want {
		if got := findByTgID(out, id).NetRub; got != w {
			t.Errorf("player %d: NetRub = %d, want %d", id, got, w)
		}
	}
}

// Ревью settle-1: при старом правиле «весь остаток крупнейшему» проигравший мог
// превратиться в получателя денег. [-1,-1,-1,+1], delta = 2 давало [+1,-1,-1,+1].
func TestApplyBankDelta_NobodyFlipsSide(t *testing.T) {
	in := []domain.GameResult{
		{PlayerTgID: 1, NetRub: -1},
		{PlayerTgID: 2, NetRub: -1},
		{PlayerTgID: 3, NetRub: -1},
		{PlayerTgID: 4, NetRub: 1},
	}
	out := ApplyBankDelta(in, 2)
	if sumNet(out) != 0 {
		t.Fatalf("sum = %d, want 0", sumNet(out))
	}
	for _, r := range out[:3] {
		if r.NetRub > 0 {
			t.Fatalf("debtor %d became a creditor: %+v", r.PlayerTgID, out)
		}
	}
}

func TestApplyBankDelta_SingleSidedGuard(t *testing.T) {
	// delta < 0 but no creditors present — the creditor-side total is 0, guard must
	// return the input unchanged without dividing by zero.
	in := []domain.GameResult{
		{PlayerTgID: 1, NetRub: -100},
		{PlayerTgID: 2, NetRub: -50},
	}
	out := ApplyBankDelta(in, -10)
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("expected %+v unchanged under zero-total guard, got %+v", in[i], out[i])
		}
	}
}

func TestApplyBankDelta_PreservesOtherFields(t *testing.T) {
	in := []domain.GameResult{
		{GameID: 42, PlayerTgID: 1, BuyInCount: 1, RebuyCount: 2, TotalInRub: 300, TotalOutRub: 200, TotalOutChips: 200, NetRub: -100},
		{GameID: 42, PlayerTgID: 2, BuyInCount: 1, RebuyCount: 0, TotalInRub: 100, TotalOutRub: 200, TotalOutChips: 200, NetRub: 100},
	}

	for _, delta := range []int{10, -10} {
		out := ApplyBankDelta(in, delta)
		for i := range in {
			want, got := in[i], out[i]
			if want.GameID != got.GameID || want.PlayerTgID != got.PlayerTgID ||
				want.BuyInCount != got.BuyInCount || want.RebuyCount != got.RebuyCount ||
				want.TotalInRub != got.TotalInRub || want.TotalOutRub != got.TotalOutRub ||
				want.TotalOutChips != got.TotalOutChips {
				t.Errorf("delta=%d: non-NetRub fields changed: want %+v, got %+v", delta, want, got)
			}
		}
	}
}

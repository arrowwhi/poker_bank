package service

import (
	"testing"

	"poker_bank/internal/domain"
)

// bank воспроизводит формулу bank(game_id) из ledger.go:GetBank —
// сумма buy-in/rebuy минус сумма cash-out по неаннулированным записям.
func bank(entries []domain.LedgerEntry) int {
	b := 0
	for _, e := range entries {
		if e.IsVoid {
			continue
		}
		switch e.Type {
		case domain.LedgerBuyIn, domain.LedgerRebuy:
			b += e.AmountRub
		case domain.LedgerCashOut:
			b -= e.AmountRub
		}
	}
	return b
}

func sumSettlementsBySender(settlements []domain.Settlement, tgID int64) int {
	sum := 0
	for _, s := range settlements {
		if s.FromTgID == tgID {
			sum += s.AmountRub
		}
	}
	return sum
}

func sumSettlementsByReceiver(settlements []domain.Settlement, tgID int64) int {
	sum := 0
	for _, s := range settlements {
		if s.ToTgID == tgID {
			sum += s.AmountRub
		}
	}
	return sum
}

// TestComputeGameResults_AggregatesLedgerAndSkipsVoid проверяет подсчёт
// buy-in/rebuy/cash-out по каждому игроку из сырого леджера, включая
// игнорирование аннулированных (is_void=true) записей.
func TestComputeGameResults_AggregatesLedgerAndSkipsVoid(t *testing.T) {
	entries := []domain.LedgerEntry{
		{PlayerTgID: 1, Type: domain.LedgerBuyIn, AmountRub: 500, AmountChips: 500},
		{PlayerTgID: 1, Type: domain.LedgerRebuy, AmountRub: 500, AmountChips: 500},
		{PlayerTgID: 1, Type: domain.LedgerCashOut, AmountRub: 300, AmountChips: 300},
		{PlayerTgID: 2, Type: domain.LedgerBuyIn, AmountRub: 500, AmountChips: 500},
		{PlayerTgID: 2, Type: domain.LedgerCashOut, AmountRub: 700, AmountChips: 700},
		{PlayerTgID: 3, Type: domain.LedgerBuyIn, AmountRub: 1000, AmountChips: 1000, IsVoid: true}, // отменённый buy-in — не должен учитываться
		{PlayerTgID: 3, Type: domain.LedgerBuyIn, AmountRub: 500, AmountChips: 500},
		{PlayerTgID: 3, Type: domain.LedgerCashOut, AmountRub: 500, AmountChips: 500},
	}

	results := ComputeGameResults(7, entries)
	if len(results) != 3 {
		t.Fatalf("expected 3 players, got %d", len(results))
	}

	p1 := findByTgID(results, 1)
	if p1.BuyInCount != 1 || p1.RebuyCount != 1 || p1.TotalInRub != 1000 || p1.TotalOutRub != 300 || p1.TotalOutChips != 300 || p1.NetRub != -700 {
		t.Errorf("player 1: unexpected result %+v", p1)
	}

	p2 := findByTgID(results, 2)
	if p2.BuyInCount != 1 || p2.RebuyCount != 0 || p2.TotalInRub != 500 || p2.TotalOutRub != 700 || p2.NetRub != 200 {
		t.Errorf("player 2: unexpected result %+v", p2)
	}

	p3 := findByTgID(results, 3)
	if p3.BuyInCount != 1 || p3.TotalInRub != 500 || p3.TotalOutRub != 500 || p3.NetRub != 0 {
		t.Errorf("player 3: void buy-in must be excluded, got %+v", p3)
	}
}

// TestSettlementPipeline_BalancedBank проверяет полный путь ledger -> results ->
// settlements для игры, где банк сошёлся ровно (сумма buy-in/rebuy == сумма
// cash-out, "полное совпадение"). Дельта не применяется, и каждый перевод
// должен в точности покрывать net каждого игрока: должники не платят больше,
// а кредиторы получают не меньше выигранного.
func TestSettlementPipeline_BalancedBank(t *testing.T) {
	entries := []domain.LedgerEntry{
		{PlayerTgID: 1, Type: domain.LedgerBuyIn, AmountRub: 1000},
		{PlayerTgID: 1, Type: domain.LedgerCashOut, AmountRub: 900}, // net -100
		{PlayerTgID: 2, Type: domain.LedgerBuyIn, AmountRub: 500},
		{PlayerTgID: 2, Type: domain.LedgerCashOut, AmountRub: 450}, // net -50
		{PlayerTgID: 3, Type: domain.LedgerBuyIn, AmountRub: 300},
		{PlayerTgID: 3, Type: domain.LedgerCashOut, AmountRub: 390}, // net +90
		{PlayerTgID: 4, Type: domain.LedgerBuyIn, AmountRub: 200},
		{PlayerTgID: 4, Type: domain.LedgerCashOut, AmountRub: 260}, // net +60
	}

	if b := bank(entries); b != 0 {
		t.Fatalf("test setup: expected balanced bank == 0, got %d", b)
	}

	results := ComputeGameResults(1, entries)
	if sumNet(results) != 0 {
		t.Fatalf("expected sum(NetRub) == 0 for a balanced game, got %d", sumNet(results))
	}

	settlements := ComputeSettlements(1, results)

	for _, r := range results {
		if r.NetRub < 0 {
			paid := sumSettlementsBySender(settlements, r.PlayerTgID)
			if paid != -r.NetRub {
				t.Errorf("player %d: expected to pay exactly %d, paid %d", r.PlayerTgID, -r.NetRub, paid)
			}
		} else if r.NetRub > 0 {
			received := sumSettlementsByReceiver(settlements, r.PlayerTgID)
			if received != r.NetRub {
				t.Errorf("player %d: expected to receive exactly %d, received %d", r.PlayerTgID, r.NetRub, received)
			}
		}
	}
}

// TestSettlementPipeline_UnbalancedBank_Surplus_LosersPayLess проверяет
// /endgame_force для случая, когда фишек выдано больше, чем возвращено
// (bank(game_id) > 0). Правило: выигравшие получают ровно то, что выиграли;
// проигравшие платят пропорционально МЕНЬШЕ, чем их номинальный проигрыш.
func TestSettlementPipeline_UnbalancedBank_Surplus_LosersPayLess(t *testing.T) {
	entries := []domain.LedgerEntry{
		{PlayerTgID: 1, Type: domain.LedgerBuyIn, AmountRub: 1000},
		{PlayerTgID: 1, Type: domain.LedgerCashOut, AmountRub: 600}, // net -400 (нехватка фишек на руках)
		{PlayerTgID: 2, Type: domain.LedgerBuyIn, AmountRub: 500},
		{PlayerTgID: 2, Type: domain.LedgerCashOut, AmountRub: 200}, // net -300
		{PlayerTgID: 3, Type: domain.LedgerBuyIn, AmountRub: 300},
		{PlayerTgID: 3, Type: domain.LedgerCashOut, AmountRub: 900}, // net +600
	}

	b := bank(entries)
	if b != 100 {
		t.Fatalf("test setup: expected bank == 100 (surplus), got %d", b)
	}

	results := ComputeGameResults(2, entries)
	nominal := map[int64]int{1: -400, 2: -300, 3: 600}
	for tgID, want := range nominal {
		if got := findByTgID(results, tgID).NetRub; got != want {
			t.Fatalf("test setup: player %d nominal net expected %d, got %d", tgID, want, got)
		}
	}

	adjusted := ApplyBankDelta(results, b)
	if sumNet(adjusted) != 0 {
		t.Fatalf("expected sum(adjusted NetRub) == 0, got %d", sumNet(adjusted))
	}

	settlements := ComputeSettlements(2, adjusted)

	// Кредитор получает выигрыш полностью, без урезания.
	if got := sumSettlementsByReceiver(settlements, 3); got != 600 {
		t.Errorf("winner should receive exactly nominal 600, got %d", got)
	}

	// Должники платят строго меньше номинального проигрыша.
	if paid := sumSettlementsBySender(settlements, 1); paid >= 400 {
		t.Errorf("player 1 should pay less than nominal 400, paid %d", paid)
	}
	if paid := sumSettlementsBySender(settlements, 2); paid >= 300 {
		t.Errorf("player 2 should pay less than nominal 300, paid %d", paid)
	}

	// Сумма выплат должников должна ровно покрыть выплату кредитору.
	totalPaid := sumSettlementsBySender(settlements, 1) + sumSettlementsBySender(settlements, 2)
	if totalPaid != 600 {
		t.Errorf("total paid by debtors should equal total received by creditors (600), got %d", totalPaid)
	}
}

// TestSettlementPipeline_UnbalancedBank_Deficit_WinnersReceiveLess проверяет
// /endgame_force для обратного случая: фишек возвращено больше, чем выдано
// (bank(game_id) < 0). Правило: проигравшие платят ровно столько, сколько
// внесли (не больше); выигравшие получают пропорционально МЕНЬШЕ выигрыша.
func TestSettlementPipeline_UnbalancedBank_Deficit_WinnersReceiveLess(t *testing.T) {
	entries := []domain.LedgerEntry{
		{PlayerTgID: 1, Type: domain.LedgerBuyIn, AmountRub: 1000},
		{PlayerTgID: 1, Type: domain.LedgerCashOut, AmountRub: 1500}, // net +500
		{PlayerTgID: 2, Type: domain.LedgerBuyIn, AmountRub: 500},
		{PlayerTgID: 2, Type: domain.LedgerCashOut, AmountRub: 700}, // net +200
		{PlayerTgID: 3, Type: domain.LedgerBuyIn, AmountRub: 900},
		{PlayerTgID: 3, Type: domain.LedgerCashOut, AmountRub: 300}, // net -600
	}

	b := bank(entries)
	if b != -100 {
		t.Fatalf("test setup: expected bank == -100 (deficit), got %d", b)
	}

	results := ComputeGameResults(3, entries)
	nominal := map[int64]int{1: 500, 2: 200, 3: -600}
	for tgID, want := range nominal {
		if got := findByTgID(results, tgID).NetRub; got != want {
			t.Fatalf("test setup: player %d nominal net expected %d, got %d", tgID, want, got)
		}
	}

	adjusted := ApplyBankDelta(results, b)
	if sumNet(adjusted) != 0 {
		t.Fatalf("expected sum(adjusted NetRub) == 0, got %d", sumNet(adjusted))
	}

	settlements := ComputeSettlements(3, adjusted)

	// Должник платит ровно номинальный проигрыш, не больше.
	if paid := sumSettlementsBySender(settlements, 3); paid != 600 {
		t.Errorf("loser should pay exactly nominal 600, paid %d", paid)
	}

	// Кредиторы получают строго меньше номинального выигрыша.
	if got := sumSettlementsByReceiver(settlements, 1); got >= 500 {
		t.Errorf("player 1 should receive less than nominal 500, got %d", got)
	}
	if got := sumSettlementsByReceiver(settlements, 2); got >= 200 {
		t.Errorf("player 2 should receive less than nominal 200, got %d", got)
	}

	// Сумма полученного кредиторами должна ровно равняться уплаченному должником.
	totalReceived := sumSettlementsByReceiver(settlements, 1) + sumSettlementsByReceiver(settlements, 2)
	if totalReceived != 600 {
		t.Errorf("total received by creditors should equal total paid by debtor (600), got %d", totalReceived)
	}
}

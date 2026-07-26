package service

import (
	"sort"

	"poker_bank/internal/domain"
)

// ComputeGameResults агрегирует непустые записи леджера в результаты по каждому игроку.
func ComputeGameResults(gameID int64, entries []domain.LedgerEntry) []domain.GameResult {
	type agg struct {
		buyInCount    int
		rebuyCount    int
		totalInRub    int
		totalOutRub   int
		totalOutChips int
	}
	m := make(map[int64]*agg)
	for _, e := range entries {
		if e.IsVoid {
			continue
		}
		a, ok := m[e.PlayerTgID]
		if !ok {
			a = &agg{}
			m[e.PlayerTgID] = a
		}
		switch e.Type {
		case domain.LedgerBuyIn:
			a.buyInCount++
			a.totalInRub += e.AmountRub
		case domain.LedgerRebuy:
			a.rebuyCount++
			a.totalInRub += e.AmountRub
		case domain.LedgerCashOut:
			a.totalOutRub += e.AmountRub
			a.totalOutChips += e.AmountChips
		}
	}
	results := make([]domain.GameResult, 0, len(m))
	for tgID, a := range m {
		results = append(results, domain.GameResult{
			GameID:        gameID,
			PlayerTgID:    tgID,
			BuyInCount:    a.buyInCount,
			RebuyCount:    a.rebuyCount,
			TotalInRub:    a.totalInRub,
			TotalOutRub:   a.totalOutRub,
			TotalOutChips: a.totalOutChips,
			NetRub:        a.totalOutRub - a.totalInRub,
		})
	}
	return results
}

// ApplyBankDelta корректирует net_rub игроков на величину расхождения банка так,
// чтобы сумма скорректированных NetRub стала равна нулю, никого не заставляя
// платить или получать больше расчётного значения — корректировка всегда
// уменьшает величину (модуль) и применяется только к «большей» стороне.
//
// delta = bank(game_id) = SUM(buy_in_rub + rebuy_rub) - SUM(cash_out_rub) по
// неаннулированным записям леджера. Всегда верно: X - Y == delta, где
// X = сумма |NetRub| у должников, Y = сумма NetRub у кредиторов.
//
// delta > 0 (выдано фишек больше, чем возвращено): долг (X) больше выигрыша (Y).
//
//	Кредиторы получают ровно то, что выиграли (NetRub не меняется).
//	Долг должников пропорционально УМЕНЬШАЕТСЯ, пока X не сравняется с Y.
//
// delta < 0 (возвращено фишек больше, чем выдано): выигрыш (Y) больше долга (X).
//
//	Должники платят ровно то, что проиграли (NetRub не меняется).
//	Выигрыш кредиторов пропорционально УМЕНЬШАЕТСЯ, пока Y не сравняется с X.
//
// Остаток от целочисленного деления достаётся записи с наибольшим по модулю
// NetRub на скорректированной стороне.
func ApplyBankDelta(results []domain.GameResult, delta int) []domain.GameResult {
	if delta == 0 {
		return results
	}
	adj := make([]domain.GameResult, len(results))
	copy(adj, results)

	if delta > 0 {
		// Долг больше выигрыша — уменьшаем долг должников.
		total := 0
		for _, r := range adj {
			if r.NetRub < 0 {
				total += -r.NetRub
			}
		}
		if total == 0 {
			return adj
		}
		distributed, maxIdx := 0, -1
		for i := range adj {
			if adj[i].NetRub < 0 {
				cut := delta * (-adj[i].NetRub) / total
				adj[i].NetRub += cut // долг уменьшается (к нулю)
				distributed += cut
				if maxIdx == -1 || (-adj[i].NetRub) > (-adj[maxIdx].NetRub) {
					maxIdx = i
				}
			}
		}
		if rem := delta - distributed; rem != 0 && maxIdx >= 0 {
			adj[maxIdx].NetRub += rem
		}
	} else {
		// Выигрыш больше долга — уменьшаем выигрыш кредиторов.
		absDelta := -delta
		total := 0
		for _, r := range adj {
			if r.NetRub > 0 {
				total += r.NetRub
			}
		}
		if total == 0 {
			return adj
		}
		distributed, maxIdx := 0, -1
		for i := range adj {
			if adj[i].NetRub > 0 {
				cut := absDelta * adj[i].NetRub / total
				adj[i].NetRub -= cut // выигрыш уменьшается (к нулю)
				distributed += cut
				if maxIdx == -1 || adj[i].NetRub > adj[maxIdx].NetRub {
					maxIdx = i
				}
			}
		}
		if rem := absDelta - distributed; rem != 0 && maxIdx >= 0 {
			adj[maxIdx].NetRub -= rem
		}
	}
	return adj
}

// ComputeSettlements вычисляет минимальный набор переводов по результатам игры.
// Шаг 1: точные совпадения (должник X ↔ кредитор Y с одинаковой суммой).
// Шаг 2: жадный алгоритм — крупнейший должник → крупнейший кредитор.
func ComputeSettlements(gameID int64, results []domain.GameResult) []domain.Settlement {
	type entry struct {
		tgID int64
		net  int
	}

	all := make([]entry, 0, len(results))
	for _, r := range results {
		if r.NetRub != 0 {
			all = append(all, entry{tgID: r.PlayerTgID, net: r.NetRub})
		}
	}

	var transfers []domain.Settlement
	matched := make([]bool, len(all))

	// Шаг 1: точные совпадения
	for i := range all {
		if matched[i] || all[i].net >= 0 {
			continue
		}
		for j := range all {
			if matched[j] || all[j].net <= 0 {
				continue
			}
			if -all[i].net == all[j].net {
				transfers = append(transfers, domain.Settlement{
					GameID:    gameID,
					FromTgID:  all[i].tgID,
					ToTgID:    all[j].tgID,
					AmountRub: all[j].net,
				})
				matched[i] = true
				matched[j] = true
				break
			}
		}
	}

	// Шаг 2: жадный матчинг
	var debtors, creditors []entry
	for i, e := range all {
		if matched[i] {
			continue
		}
		if e.net < 0 {
			debtors = append(debtors, e)
		} else if e.net > 0 {
			creditors = append(creditors, e)
		}
	}

	absVal := func(x int) int {
		if x < 0 {
			return -x
		}
		return x
	}
	sortByAbs := func(s []entry) {
		sort.Slice(s, func(i, j int) bool { return absVal(s[i].net) > absVal(s[j].net) })
	}
	sortByAbs(debtors)
	sortByAbs(creditors)

	for len(debtors) > 0 && len(creditors) > 0 {
		d := &debtors[0]
		c := &creditors[0]
		amount := absVal(d.net)
		if c.net < amount {
			amount = c.net
		}
		transfers = append(transfers, domain.Settlement{
			GameID:    gameID,
			FromTgID:  d.tgID,
			ToTgID:    c.tgID,
			AmountRub: amount,
		})
		d.net += amount
		c.net -= amount
		if d.net == 0 {
			debtors = debtors[1:]
		}
		if c.net == 0 {
			creditors = creditors[1:]
		}
		sortByAbs(debtors)
		sortByAbs(creditors)
	}

	return transfers
}

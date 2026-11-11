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
	// Порядок итерации по map случаен — сортируем, чтобы план переводов для одной и той же
	// игры всегда получался одинаковым.
	sort.Slice(results, func(i, j int) bool { return results[i].PlayerTgID < results[j].PlayerTgID })
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
// Округление — методом наибольших остатков: каждый получает целую часть своей доли,
// оставшиеся рубли раздаются по одному тем, у кого дробная часть доли больше
// (при равенстве — меньший tg_id). Так ни у кого срез не превышает его долю, округлённую
// вверх, и никто не переходит на другую сторону: проигравший не начинает получать деньги.
func ApplyBankDelta(results []domain.GameResult, delta int) []domain.GameResult {
	if delta == 0 {
		return results
	}
	adj := make([]domain.GameResult, len(results))
	copy(adj, results)

	if delta > 0 {
		// Долг больше выигрыша — уменьшаем долг должников (их NetRub < 0 растёт к нулю).
		shrinkSide(adj, delta, func(net int) int { return -net }, +1)
	} else {
		// Выигрыш больше долга — уменьшаем выигрыш кредиторов (их NetRub > 0 падает к нулю).
		shrinkSide(adj, -delta, func(net int) int { return net }, -1)
	}
	return adj
}

// shrinkSide уменьшает модули одной стороны (size(net) > 0) суммарно на cut пропорционально
// размеру, методом наибольших остатков. dir — в какую сторону двигается NetRub (+1 / -1).
// cut не может превышать сумму размеров стороны: delta = X - Y по тем же записям.
func shrinkSide(adj []domain.GameResult, cut int, size func(net int) int, dir int) {
	type share struct {
		idx int
		rem int
	}
	total := 0
	for _, r := range adj {
		if v := size(r.NetRub); v > 0 {
			total += v
		}
	}
	if total == 0 {
		return
	}
	if cut > total {
		cut = total
	}

	shares := make([]share, 0, len(adj))
	distributed := 0
	for i, r := range adj {
		v := size(r.NetRub)
		if v <= 0 {
			continue
		}
		exact := cut * v
		base := exact / total
		adj[i].NetRub += dir * base
		distributed += base
		shares = append(shares, share{idx: i, rem: exact % total})
	}

	sort.SliceStable(shares, func(i, j int) bool {
		if shares[i].rem != shares[j].rem {
			return shares[i].rem > shares[j].rem
		}
		return adj[shares[i].idx].PlayerTgID < adj[shares[j].idx].PlayerTgID
	})
	for k := 0; k < cut-distributed; k++ {
		adj[shares[k].idx].NetRub += dir
	}
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
		sort.SliceStable(s, func(i, j int) bool {
			if absVal(s[i].net) != absVal(s[j].net) {
				return absVal(s[i].net) > absVal(s[j].net)
			}
			return s[i].tgID < s[j].tgID
		})
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

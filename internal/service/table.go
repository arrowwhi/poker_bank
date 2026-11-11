package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"poker_bank/internal/domain"
)

// Операции за столом, общие для бота и Mini App: проверки прав и состояния,
// запись в ledger и разрешение запросов в одной транзакции. Сообщения в чат
// здесь не отправляются — это делает вызывающий по результату операции.

// RequestResult — итог запроса игрока (JOIN/REBUY/CASHOUT).
type RequestResult struct {
	// Action — созданный запрос (pending) или, если действие применено сразу, запись о нём
	// со статусом confirmed.
	Action *domain.PendingAction
	// Applied — действие записано сразу: запрос сделал сам дилер.
	Applied bool
	// Cancelled — запросы, отменённые попутно (например, при cash-out).
	Cancelled []domain.PendingAction
}

// ResolveResult — итог решения дилера по запросу.
type ResolveResult struct {
	Action    *domain.PendingAction
	Game      *domain.Game
	Cancelled []domain.PendingAction
}

// EndResult — итог завершения игры.
type EndResult struct {
	// Cancelled — в игре не было ни одного buy-in, и она отменена вместо завершения.
	Cancelled        bool
	BankRub          int
	Settlements      []domain.Settlement
	CancelledPending []domain.PendingAction
}

// GameByID возвращает игру или ErrNotFound.
func (s *GameService) GameByID(ctx context.Context, id int64) (*domain.Game, error) {
	g, err := s.games.GetByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func requireActive(g *domain.Game) error {
	if g.Status != domain.GameStatusActive {
		return ErrGameNotActive
	}
	return nil
}

func requireActiveDealer(g *domain.Game, userID int64) error {
	if err := requireActive(g); err != nil {
		return err
	}
	if g.DealerTgID != userID {
		return ErrNotDealer
	}
	return nil
}

// lockGame блокирует строку игры до конца текущей транзакции (SELECT … FOR UPDATE)
// и возвращает её актуальное состояние. Каждая изменяющая операция стола начинается
// с неё: операции над одной игрой из бота и Mini App выполняются по очереди, и
// проверки «игра активна», «игрок за столом» не устаревают до записи.
// *g обновляется актуальными данными, чтобы вызывающий видел то же, что и сервис.
func (s *GameService) lockGame(ctx context.Context, g *domain.Game) (*domain.Game, error) {
	cur, err := s.games.LockByID(ctx, g.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	*g = *cur
	return cur, nil
}

// lockDealer — lockGame с проверкой, что игра активна и userID — её дилер.
func (s *GameService) lockDealer(ctx context.Context, g *domain.Game, userID int64) (*domain.Game, error) {
	cur, err := s.lockGame(ctx, g)
	if err != nil {
		return nil, err
	}
	if err := requireActiveDealer(cur, userID); err != nil {
		return nil, err
	}
	return cur, nil
}

// notActiveErr переводит отказ репозитория изменить неактивную игру (pgx.ErrNoRows
// от Finish/Cancel/UpdateDealer) в ErrGameNotActive.
func notActiveErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGameNotActive
	}
	return err
}

// IsActivePlayer сообщает, сидит ли игрок за столом (buy-in без cash-out).
func (s *GameService) IsActivePlayer(ctx context.Context, gameID, tgID int64) (bool, error) {
	p, err := s.participants.GetByPlayer(ctx, gameID, tgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return p.IsActive, nil
}

// CreateRequest создаёт запрос игрока на подтверждение дилером. Если запрашивает сам
// дилер, действие записывается сразу, без подтверждения.
func (s *GameService) CreateRequest(ctx context.Context, g *domain.Game, userID int64, t domain.ActionType, chips int) (*RequestResult, error) {
	var (
		res          *RequestResult
		createFailed bool
	)
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockGame(ctx, g)
		if err != nil {
			return err
		}
		if err := requireActive(cur); err != nil {
			return err
		}

		active, err := s.IsActivePlayer(ctx, cur.ID, userID)
		if err != nil {
			return err
		}

		var rub, ch int
		switch t {
		case domain.ActionJoin:
			if active {
				return ErrAlreadyInGame
			}
			rub, ch = cur.BuyInRub, cur.BuyInChips
		case domain.ActionRebuy:
			if !active {
				return ErrNotInGame
			}
			rub, ch = cur.RebuyRub, cur.RebuyChips
		case domain.ActionCashOut:
			if !active {
				return ErrNotInGame
			}
			r, ok := cur.ChipsToRub(chips)
			if chips < 0 || !ok {
				return ErrInvalidChipsAmount
			}
			rub, ch = r, chips
		default:
			return ErrNotFound
		}

		now := time.Now()
		if cur.DealerTgID == userID {
			res = &RequestResult{Applied: true, Action: &domain.PendingAction{
				GameID: cur.ID, ActionType: t, RequesterTgID: userID, TargetTgID: userID,
				Payload: map[string]any{"chips": ch, "rub": rub}, Status: domain.PendingStatusConfirmed,
				ChatID: cur.ChatID, CreatedAt: now, ResolvedAt: &now, ResolvedByTgID: &userID,
			}}
			res.Cancelled, err = s.apply(ctx, cur, t, userID, ch, userID)
			return err
		}

		if _, err := s.pending.GetPending(ctx, cur.ID, userID, t); err == nil {
			return ErrPendingExists
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		pa := &domain.PendingAction{
			GameID: cur.ID, ActionType: t, RequesterTgID: userID, TargetTgID: userID,
			Payload: map[string]any{}, Status: domain.PendingStatusPending, ChatID: cur.ChatID, CreatedAt: now,
		}
		if t == domain.ActionCashOut {
			pa.Payload["chips"] = chips
		}
		id, err := s.pending.Create(ctx, pa)
		if err != nil {
			createFailed = true
			return err
		}
		pa.ID = id
		res = &RequestResult{Action: pa}
		return nil
	})
	if err != nil {
		// Уникальный индекс pending_one_per_target ловит гонку двух одинаковых запросов.
		if createFailed {
			if _, gErr := s.pending.GetPending(ctx, g.ID, userID, t); gErr == nil {
				return nil, ErrPendingExists
			}
		}
		return nil, err
	}
	if res.Applied {
		s.syncSheetAsync(g.ID)
	}
	return res, nil
}

// RequestAmount — сумма и фишки запроса (для текстов сообщений).
func RequestAmount(g *domain.Game, pa *domain.PendingAction) (rub, chips int) {
	switch pa.ActionType {
	case domain.ActionJoin:
		return g.BuyInRub, g.BuyInChips
	case domain.ActionRebuy:
		return g.RebuyRub, g.RebuyChips
	default:
		chips, _ = PayloadInt(pa.Payload, "chips")
		rub, _ = g.ChipsToRub(chips)
		return rub, chips
	}
}

// PayloadInt читает целое из JSONB-payload (числа приходят как float64).
func PayloadInt(p map[string]any, key string) (int, bool) {
	switch v := p[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

// apply записывает действие в ledger (внутри транзакции вызывающего, игра заблокирована).
func (s *GameService) apply(ctx context.Context, g *domain.Game, t domain.ActionType, target int64, chips int, by int64) ([]domain.PendingAction, error) {
	switch t {
	case domain.ActionJoin:
		return nil, s.recordBuyIn(ctx, g, target, by)
	case domain.ActionRebuy:
		return nil, s.recordRebuy(ctx, g, target, by)
	case domain.ActionCashOut:
		return s.recordCashOut(ctx, g, target, chips, by)
	}
	return nil, ErrNotFound
}

// ResolveRequest подтверждает или отклоняет запрос. Повторное решение возвращает
// ErrAlreadyResolved вместе с текущим состоянием запроса в результате.
func (s *GameService) ResolveRequest(ctx context.Context, paID, dealerID int64, approve bool) (*ResolveResult, error) {
	res := &ResolveResult{}
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		pa, err := s.pending.GetByID(ctx, paID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		res.Action = pa

		g, err := s.lockGame(ctx, &domain.Game{ID: pa.GameID})
		if err != nil {
			return err
		}
		res.Game = g

		if pa.Status != domain.PendingStatusPending {
			return ErrAlreadyResolved
		}
		if err := requireActiveDealer(g, dealerID); err != nil {
			return err
		}

		status := domain.PendingStatusRejected
		if approve {
			status = domain.PendingStatusConfirmed
		}
		resolved, err := s.pending.Resolve(ctx, paID, status, dealerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAlreadyResolved
		}
		if err != nil {
			return err
		}
		res.Action = resolved

		if !approve {
			return nil
		}

		// Состояние могло измениться, пока запрос ждал решения.
		active, err := s.IsActivePlayer(ctx, g.ID, resolved.TargetTgID)
		if err != nil {
			return err
		}
		if resolved.ActionType == domain.ActionJoin && active {
			return ErrAlreadyInGame
		}
		if resolved.ActionType != domain.ActionJoin && !active {
			return ErrNotInGame
		}

		chips := 0
		if resolved.ActionType == domain.ActionCashOut {
			var ok bool
			if chips, ok = PayloadInt(resolved.Payload, "chips"); !ok {
				return ErrInvalidChipsAmount
			}
		}
		res.Cancelled, err = s.apply(ctx, g, resolved.ActionType, resolved.TargetTgID, chips, dealerID)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrAlreadyResolved) {
			return res, err
		}
		return nil, err
	}
	if approve {
		s.syncSheetAsync(res.Game.ID)
	}
	return res, nil
}

// CancelRequest — игрок отзывает свой запрос.
func (s *GameService) CancelRequest(ctx context.Context, gameID, paID, userID int64) (*domain.PendingAction, error) {
	pa, err := s.pending.GetByID(ctx, paID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && pa.GameID != gameID) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if pa.RequesterTgID != userID {
		return nil, ErrNotRequester
	}
	cancelled, err := s.pending.CancelOne(ctx, paID, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pa, ErrAlreadyResolved
	}
	return cancelled, err
}

// cancelPendingOf отменяет висящий запрос игрока данного типа (дилер сделал действие сам).
func (s *GameService) cancelPendingOf(ctx context.Context, gameID, target int64, t domain.ActionType, by int64) ([]domain.PendingAction, error) {
	pa, err := s.pending.GetPending(ctx, gameID, target, t)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := s.pending.CancelOne(ctx, pa.ID, by)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []domain.PendingAction{*c}, nil
}

// DealerBuyIn — дилер сажает игрока за стол без запроса.
func (s *GameService) DealerBuyIn(ctx context.Context, g *domain.Game, dealerID, target int64) ([]domain.PendingAction, error) {
	var cancelled []domain.PendingAction
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockDealer(ctx, g, dealerID)
		if err != nil {
			return err
		}
		active, err := s.IsActivePlayer(ctx, cur.ID, target)
		if err != nil {
			return err
		}
		if active {
			return ErrAlreadyInGame
		}
		if err := s.recordBuyIn(ctx, cur, target, dealerID); err != nil {
			return err
		}
		cancelled, err = s.cancelPendingOf(ctx, cur.ID, target, domain.ActionJoin, dealerID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.syncSheetAsync(g.ID)
	return cancelled, nil
}

// DealerRebuy — дилер записывает rebuy игроку без запроса.
func (s *GameService) DealerRebuy(ctx context.Context, g *domain.Game, dealerID, target int64) ([]domain.PendingAction, error) {
	var cancelled []domain.PendingAction
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockDealer(ctx, g, dealerID)
		if err != nil {
			return err
		}
		active, err := s.IsActivePlayer(ctx, cur.ID, target)
		if err != nil {
			return err
		}
		if !active {
			return ErrNotInGame
		}
		if err := s.recordRebuy(ctx, cur, target, dealerID); err != nil {
			return err
		}
		cancelled, err = s.cancelPendingOf(ctx, cur.ID, target, domain.ActionRebuy, dealerID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.syncSheetAsync(g.ID)
	return cancelled, nil
}

// DealerCashOut — дилер выводит игрока из-за стола.
func (s *GameService) DealerCashOut(ctx context.Context, g *domain.Game, dealerID, target int64, chips int) ([]domain.PendingAction, error) {
	var cancelled []domain.PendingAction
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockDealer(ctx, g, dealerID)
		if err != nil {
			return err
		}
		if _, ok := cur.ChipsToRub(chips); chips < 0 || !ok {
			return ErrInvalidChipsAmount
		}
		active, err := s.IsActivePlayer(ctx, cur.ID, target)
		if err != nil {
			return err
		}
		if !active {
			return ErrNotInGame
		}
		cancelled, err = s.recordCashOut(ctx, cur, target, chips, dealerID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.syncSheetAsync(g.ID)
	return cancelled, nil
}

// DealerUndo — дилер отменяет последние n записей.
func (s *GameService) DealerUndo(ctx context.Context, g *domain.Game, dealerID int64, n int) ([]domain.LedgerEntry, error) {
	if n < 1 {
		n = 1
	}
	var voided []domain.LedgerEntry
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockDealer(ctx, g, dealerID)
		if err != nil {
			return err
		}
		voided, err = s.UndoLast(ctx, cur.ID, n)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(voided) > 0 {
		s.syncSheetAsync(g.ID)
	}
	return voided, nil
}

// DealerTransfer передаёт роль дилера активному игроку.
func (s *GameService) DealerTransfer(ctx context.Context, g *domain.Game, dealerID, target int64) error {
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockDealer(ctx, g, dealerID)
		if err != nil {
			return err
		}
		active, err := s.IsActivePlayer(ctx, cur.ID, target)
		if err != nil {
			return err
		}
		if !active {
			return ErrNotInGame
		}
		if err := s.games.UpdateDealer(ctx, cur.ID, target); err != nil {
			return notActiveErr(err)
		}
		g.DealerTgID = target
		return nil
	})
}

// EndGame завершает игру. Пустая игра (без buy-in) отменяется. Без force расхождение
// банка возвращает *BankMismatchError, активные игроки — *ActiveParticipantsError.
//
// Проверки, расчёт и запись итогов идут в одной транзакции под блокировкой игры (ТЗ §6):
// между проверкой «все вышли, банк сошёлся» и записью итогов в ledger ничего не вклинится.
func (s *GameService) EndGame(ctx context.Context, g *domain.Game, dealerID int64, force bool) (*EndResult, error) {
	res := &EndResult{}
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockDealer(ctx, g, dealerID)
		if err != nil {
			return err
		}

		all, err := s.participants.ListByGame(ctx, cur.ID)
		if err != nil {
			return err
		}
		if len(all) == 0 {
			res.Cancelled = true
			res.CancelledPending, err = s.cancelLocked(ctx, cur.ID)
			return err
		}

		var activeIDs []int64
		for _, p := range all {
			if p.IsActive {
				activeIDs = append(activeIDs, p.PlayerTgID)
			}
		}
		if len(activeIDs) > 0 {
			return &ActiveParticipantsError{PlayerIDs: activeIDs}
		}

		bank, err := s.ledger.GetBank(ctx, cur.ID)
		if err != nil {
			return err
		}
		if bank != 0 && !force {
			return &BankMismatchError{BankRub: bank}
		}
		res.BankRub = bank

		if res.Settlements, err = s.finish(ctx, cur.ID, bank); err != nil {
			return err
		}
		res.CancelledPending, err = s.pending.CancelByGame(ctx, cur.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if res.Cancelled {
		return res, nil
	}
	s.syncSheetAsync(g.ID)
	// finish возвращает переводы без ID и флагов — берём записанные.
	if stored, lErr := s.settlements.ListByGame(ctx, g.ID); lErr == nil {
		res.Settlements = stored
	}
	return res, nil
}

// CancelGame отменяет игру дилером; висящие запросы закрываются.
func (s *GameService) CancelGame(ctx context.Context, g *domain.Game, dealerID int64) ([]domain.PendingAction, error) {
	var cancelled []domain.PendingAction
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockDealer(ctx, g, dealerID)
		if err != nil {
			return err
		}
		cancelled, err = s.cancelLocked(ctx, cur.ID)
		return err
	})
	return cancelled, err
}

// AdminIdleTimeout — сколько дилер должен бездействовать, чтобы админ чата мог отменить игру (ТЗ §8.1).
const AdminIdleTimeout = 6 * time.Hour

// DealerLastActivity — время последнего действия текущего дилера: начало игры или
// последняя запись в ledger, сделанная им (прямые действия и подтверждения запросов).
func (s *GameService) DealerLastActivity(ctx context.Context, g *domain.Game) (time.Time, error) {
	last := g.StartedAt
	entries, err := s.ledger.ListByGame(ctx, g.ID)
	if err != nil {
		return last, err
	}
	for _, e := range entries {
		if e.CreatedByTgID == g.DealerTgID && e.CreatedAt.After(last) {
			last = e.CreatedAt
		}
	}
	return last, nil
}

// AdminCancelGame отменяет игру по команде админа чата, если дилер бездействует
// дольше AdminIdleTimeout. Права админа проверяет вызывающий (Telegram).
// При слишком ранней попытке возвращает ErrDealerNotIdle и время последнего действия.
func (s *GameService) AdminCancelGame(ctx context.Context, g *domain.Game, now time.Time) ([]domain.PendingAction, time.Time, error) {
	if err := requireActive(g); err != nil {
		return nil, time.Time{}, err
	}
	last, err := s.DealerLastActivity(ctx, g)
	if err != nil {
		return nil, last, err
	}
	if now.Sub(last) < AdminIdleTimeout {
		return nil, last, ErrDealerNotIdle
	}

	// Повторная проверка под блокировкой: пока админ жал кнопку, дилер мог успеть
	// что-то сделать, передать роль или завершить игру.
	var cancelled []domain.PendingAction
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockGame(ctx, g)
		if err != nil {
			return err
		}
		if err := requireActive(cur); err != nil {
			return err
		}
		if last, err = s.DealerLastActivity(ctx, cur); err != nil {
			return err
		}
		if now.Sub(last) < AdminIdleTimeout {
			return ErrDealerNotIdle
		}
		cancelled, err = s.cancelLocked(ctx, cur.ID)
		return err
	})
	return cancelled, last, err
}

// cancelLocked отменяет игру и закрывает её висящие запросы (внутри транзакции
// вызывающего, игра заблокирована).
func (s *GameService) cancelLocked(ctx context.Context, gameID int64) ([]domain.PendingAction, error) {
	if err := s.games.Cancel(ctx, gameID); err != nil {
		return nil, notActiveErr(err)
	}
	return s.pending.CancelByGame(ctx, gameID)
}

// CreateGameIn начинает игру в группе; при dealerBuyIn сразу записывает buy-in дилеру.
func (s *GameService) CreateGameIn(ctx context.Context, chatID, dealerID int64, buyInRub, buyInChips, rebuyRub, rebuyChips int, dealerBuyIn bool) (*domain.Game, error) {
	if buyInRub <= 0 || buyInChips <= 0 || rebuyRub <= 0 || rebuyChips <= 0 ||
		buyInRub*rebuyChips != rebuyRub*buyInChips {
		return nil, ErrBadRate
	}
	if _, err := s.GetActiveGame(ctx, chatID); err == nil {
		return nil, ErrGameAlreadyActive
	} else if !errors.Is(err, ErrNoActiveGame) {
		return nil, err
	}

	g := &domain.Game{
		ChatID: chatID, DealerTgID: dealerID,
		BuyInRub: buyInRub, BuyInChips: buyInChips, RebuyRub: rebuyRub, RebuyChips: rebuyChips,
		Status: domain.GameStatusActive, StartedAt: time.Now(),
	}
	// Игра и buy-in дилера — одной транзакцией: если buy-in не записался, в чате не
	// остаётся активной игры, которую дилер не просил в таком виде.
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		id, err := s.games.Create(ctx, g)
		if err != nil {
			return err
		}
		g.ID = id
		if dealerBuyIn {
			return s.recordBuyIn(ctx, g, dealerID, dealerID)
		}
		return nil
	})
	if err != nil {
		// Уникальный индекс games_one_active_per_chat ловит гонку двух /newgame.
		if _, aErr := s.GetActiveGame(ctx, chatID); aErr == nil {
			return nil, ErrGameAlreadyActive
		}
		return nil, err
	}
	// Лист создаётся после коммита и сразу с актуальным состоянием (включая buy-in).
	s.createSheetAsync(g.ID)
	return s.GameByID(ctx, g.ID)
}

// ── чтение для Mini App ──

// ListPending — висящие запросы игры.
func (s *GameService) ListPending(ctx context.Context, gameID int64) ([]domain.PendingAction, error) {
	return s.pending.ListPendingByGame(ctx, gameID)
}

// ListMyRequests — последние запросы игрока в игре.
func (s *GameService) ListMyRequests(ctx context.Context, gameID, tgID int64, limit int) ([]domain.PendingAction, error) {
	return s.pending.ListByTarget(ctx, gameID, tgID, limit)
}

// ListVisibleGames — игры в группах пользователя или с его участием.
func (s *GameService) ListVisibleGames(ctx context.Context, tgID, chatID int64, status domain.GameStatus, limit int) ([]domain.Game, error) {
	return s.games.ListVisible(ctx, tgID, chatID, status, limit)
}

// GameNumber — номер игры в её группе.
func (s *GameService) GameNumber(ctx context.Context, g *domain.Game) (int, error) {
	return s.games.Number(ctx, g)
}

// PreviousGameInChat — игра, чьи параметры предлагаются по умолчанию при создании новой
// («как в прошлый раз»), или nil, если в группе ещё не играли.
//
// Берётся последняя по started_at завершённая игра: её параметрами реально играли.
// Отменённая игра часто означает ошибку в параметрах (создали — отменили — пересоздали),
// поэтому она подходит, только если завершённых в группе нет. Активная игра не учитывается:
// пока она идёт, новую в этой группе создать нельзя.
func (s *GameService) PreviousGameInChat(ctx context.Context, chatID int64) (*domain.Game, error) {
	for _, status := range []domain.GameStatus{domain.GameStatusFinished, domain.GameStatusCancelled} {
		g, err := s.games.GetLastByChatStatus(ctx, chatID, status)
		if err != nil {
			return nil, err
		}
		if g != nil {
			return g, nil
		}
	}
	return nil, nil
}

// PlayerHistory — результаты игрока по завершённым играм (chatID = 0 — все группы).
func (s *GameService) PlayerHistory(ctx context.Context, tgID, chatID int64) ([]domain.PlayerGameResult, error) {
	return s.results.ListHistory(ctx, tgID, chatID)
}

// LeaderboardSince — рейтинг группы с даты since (nil — за всё время).
func (s *GameService) LeaderboardSince(ctx context.Context, chatID int64, since *time.Time) ([]domain.PlayerChatStats, error) {
	return s.results.LeaderboardSince(ctx, chatID, since)
}

// OpenSettlements — незакрытые переводы игрока.
func (s *GameService) OpenSettlements(ctx context.Context, tgID int64) ([]domain.Settlement, error) {
	return s.settlements.ListOpenByPlayer(ctx, tgID)
}

// SettleMessageID / SetSettleMessageID — сообщение с планом переводов в чате.
func (s *GameService) SettleMessageID(ctx context.Context, gameID int64) (*int64, error) {
	return s.games.GetSettleMessageID(ctx, gameID)
}

// SetSettleMessageID запоминает сообщение с планом переводов.
func (s *GameService) SetSettleMessageID(ctx context.Context, gameID, messageID int64) error {
	return s.games.SetSettleMessageID(ctx, gameID, messageID)
}

// SetPendingMessageID запоминает сообщение с кнопками запроса.
func (s *GameService) SetPendingMessageID(ctx context.Context, paID, messageID int64) error {
	return s.pending.SetMessageID(ctx, paID, messageID)
}

// ── группы ──

// RememberChat сохраняет группу и отмечает участника (вызывается на каждое обновление из группы).
func (s *GameService) RememberChat(ctx context.Context, c *domain.Chat, memberID int64) error {
	if err := s.chats.Upsert(ctx, c); err != nil {
		return err
	}
	if memberID == 0 {
		return nil
	}
	return s.chats.TouchMember(ctx, c.ID, memberID)
}

// RememberMember отмечает, что пользователь состоит в группе.
func (s *GameService) RememberMember(ctx context.Context, chatID, tgID int64) error {
	return s.chats.TouchMember(ctx, chatID, tgID)
}

// ForgetMember забывает участника группы.
func (s *GameService) ForgetMember(ctx context.Context, chatID, tgID int64) error {
	return s.chats.RemoveMember(ctx, chatID, tgID)
}

// Chat возвращает группу или nil, если бот её не знает.
func (s *GameService) Chat(ctx context.Context, id int64) (*domain.Chat, error) {
	return s.chats.Get(ctx, id)
}

// ChatsOf — группы, где пользователь известен боту, от самых популярных вниз:
// по числу завершённых игр, при равенстве — по дате последней игры (свежая выше).
// Остальные равные (в том числе чаты без игр) сохраняют порядок репозитория.
func (s *GameService) ChatsOf(ctx context.Context, tgID int64) ([]domain.ChatActivity, error) {
	chats, err := s.chats.ListByMember(ctx, tgID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(chats, func(i, j int) bool { return morePopular(&chats[i], &chats[j]) })
	return chats, nil
}

// morePopular сообщает, должен ли чат a стоять в списке выше чата b.
func morePopular(a, b *domain.ChatActivity) bool {
	if a.GamesCount != b.GamesCount {
		return a.GamesCount > b.GamesCount
	}
	if a.LastGameAt == nil || b.LastGameAt == nil {
		return a.LastGameAt != nil && b.LastGameAt == nil
	}
	return a.LastGameAt.After(*b.LastGameAt)
}

// IsKnownMember — пользователь известен как участник группы.
func (s *GameService) IsKnownMember(ctx context.Context, chatID, tgID int64) (bool, error) {
	return s.chats.IsMember(ctx, chatID, tgID)
}

// ChatMembers — известные участники группы.
func (s *GameService) ChatMembers(ctx context.Context, chatID int64) ([]domain.Player, error) {
	return s.chats.ListMembers(ctx, chatID)
}

// CountChatMembers — число известных участников группы.
func (s *GameService) CountChatMembers(ctx context.Context, chatID int64) (int, error) {
	return s.chats.CountMembers(ctx, chatID)
}

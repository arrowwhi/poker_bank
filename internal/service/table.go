package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

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
	if err := requireActive(g); err != nil {
		return nil, err
	}

	active, err := s.IsActivePlayer(ctx, g.ID, userID)
	if err != nil {
		return nil, err
	}

	var rub, ch int
	switch t {
	case domain.ActionJoin:
		if active {
			return nil, ErrAlreadyInGame
		}
		rub, ch = g.BuyInRub, g.BuyInChips
	case domain.ActionRebuy:
		if !active {
			return nil, ErrNotInGame
		}
		rub, ch = g.RebuyRub, g.RebuyChips
	case domain.ActionCashOut:
		if !active {
			return nil, ErrNotInGame
		}
		r, ok := g.ChipsToRub(chips)
		if chips < 0 || !ok {
			return nil, ErrInvalidChipsAmount
		}
		rub, ch = r, chips
	default:
		return nil, ErrNotFound
	}

	now := time.Now()
	if g.DealerTgID == userID {
		res := &RequestResult{Applied: true, Action: &domain.PendingAction{
			GameID: g.ID, ActionType: t, RequesterTgID: userID, TargetTgID: userID,
			Payload: map[string]any{"chips": ch, "rub": rub}, Status: domain.PendingStatusConfirmed,
			ChatID: g.ChatID, CreatedAt: now, ResolvedAt: &now, ResolvedByTgID: &userID,
		}}
		err := s.tx.InTx(ctx, func(ctx context.Context) error {
			var err error
			res.Cancelled, err = s.apply(ctx, g, t, userID, ch, userID)
			return err
		})
		if err != nil {
			return nil, err
		}
		return res, nil
	}

	if _, err := s.pending.GetPending(ctx, g.ID, userID, t); err == nil {
		return nil, ErrPendingExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	pa := &domain.PendingAction{
		GameID: g.ID, ActionType: t, RequesterTgID: userID, TargetTgID: userID,
		Payload: map[string]any{}, Status: domain.PendingStatusPending, ChatID: g.ChatID, CreatedAt: now,
	}
	if t == domain.ActionCashOut {
		pa.Payload["chips"] = chips
	}
	id, err := s.pending.Create(ctx, pa)
	if err != nil {
		// Уникальный индекс pending_one_per_target ловит гонку двух одинаковых запросов.
		if _, gErr := s.pending.GetPending(ctx, g.ID, userID, t); gErr == nil {
			return nil, ErrPendingExists
		}
		return nil, err
	}
	pa.ID = id
	return &RequestResult{Action: pa}, nil
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

// apply записывает действие в ledger (внутри транзакции вызывающего).
func (s *GameService) apply(ctx context.Context, g *domain.Game, t domain.ActionType, target int64, chips int, by int64) ([]domain.PendingAction, error) {
	switch t {
	case domain.ActionJoin:
		return nil, s.BuyIn(ctx, g, target, by)
	case domain.ActionRebuy:
		return nil, s.Rebuy(ctx, g, target, by)
	case domain.ActionCashOut:
		return s.CashOut(ctx, g, target, chips, by)
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

		g, err := s.GameByID(ctx, pa.GameID)
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
	if err := requireActiveDealer(g, dealerID); err != nil {
		return nil, err
	}
	var cancelled []domain.PendingAction
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		active, err := s.IsActivePlayer(ctx, g.ID, target)
		if err != nil {
			return err
		}
		if active {
			return ErrAlreadyInGame
		}
		if err := s.BuyIn(ctx, g, target, dealerID); err != nil {
			return err
		}
		cancelled, err = s.cancelPendingOf(ctx, g.ID, target, domain.ActionJoin, dealerID)
		return err
	})
	return cancelled, err
}

// DealerRebuy — дилер записывает rebuy игроку без запроса.
func (s *GameService) DealerRebuy(ctx context.Context, g *domain.Game, dealerID, target int64) ([]domain.PendingAction, error) {
	if err := requireActiveDealer(g, dealerID); err != nil {
		return nil, err
	}
	var cancelled []domain.PendingAction
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		active, err := s.IsActivePlayer(ctx, g.ID, target)
		if err != nil {
			return err
		}
		if !active {
			return ErrNotInGame
		}
		if err := s.Rebuy(ctx, g, target, dealerID); err != nil {
			return err
		}
		cancelled, err = s.cancelPendingOf(ctx, g.ID, target, domain.ActionRebuy, dealerID)
		return err
	})
	return cancelled, err
}

// DealerCashOut — дилер выводит игрока из-за стола.
func (s *GameService) DealerCashOut(ctx context.Context, g *domain.Game, dealerID, target int64, chips int) ([]domain.PendingAction, error) {
	if err := requireActiveDealer(g, dealerID); err != nil {
		return nil, err
	}
	if _, ok := g.ChipsToRub(chips); chips < 0 || !ok {
		return nil, ErrInvalidChipsAmount
	}
	var cancelled []domain.PendingAction
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		active, err := s.IsActivePlayer(ctx, g.ID, target)
		if err != nil {
			return err
		}
		if !active {
			return ErrNotInGame
		}
		cancelled, err = s.CashOut(ctx, g, target, chips, dealerID)
		return err
	})
	return cancelled, err
}

// DealerUndo — дилер отменяет последние n записей.
func (s *GameService) DealerUndo(ctx context.Context, g *domain.Game, dealerID int64, n int) ([]domain.LedgerEntry, error) {
	if err := requireActiveDealer(g, dealerID); err != nil {
		return nil, err
	}
	var voided []domain.LedgerEntry
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		voided, err = s.UndoLast(ctx, g.ID, n)
		return err
	})
	return voided, err
}

// DealerTransfer передаёт роль дилера активному игроку.
func (s *GameService) DealerTransfer(ctx context.Context, g *domain.Game, dealerID, target int64) error {
	if err := requireActiveDealer(g, dealerID); err != nil {
		return err
	}
	active, err := s.IsActivePlayer(ctx, g.ID, target)
	if err != nil {
		return err
	}
	if !active {
		return ErrNotInGame
	}
	return s.games.UpdateDealer(ctx, g.ID, target)
}

// EndGame завершает игру. Пустая игра (без buy-in) отменяется. Без force расхождение
// банка возвращает *BankMismatchError, активные игроки — *ActiveParticipantsError.
func (s *GameService) EndGame(ctx context.Context, g *domain.Game, dealerID int64, force bool) (*EndResult, error) {
	if err := requireActiveDealer(g, dealerID); err != nil {
		return nil, err
	}

	all, err := s.participants.ListByGame(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		cancelled, err := s.CancelGame(ctx, g, dealerID)
		if err != nil {
			return nil, err
		}
		return &EndResult{Cancelled: true, CancelledPending: cancelled}, nil
	}

	var activeIDs []int64
	for _, p := range all {
		if p.IsActive {
			activeIDs = append(activeIDs, p.PlayerTgID)
		}
	}
	if len(activeIDs) > 0 {
		return nil, &ActiveParticipantsError{PlayerIDs: activeIDs}
	}

	bank, err := s.ledger.GetBank(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	if bank != 0 && !force {
		return nil, &BankMismatchError{BankRub: bank}
	}

	settlements, err := s.Finish(ctx, g.ID, bank)
	if err != nil {
		return nil, err
	}
	cancelled, err := s.pending.CancelByGame(ctx, g.ID)
	if err != nil {
		s.log.Warn("cancel pending after finish", zap.Error(err), zap.Int64("game_id", g.ID))
	}
	// Finish возвращает переводы без ID и флагов — берём записанные.
	if stored, lErr := s.settlements.ListByGame(ctx, g.ID); lErr == nil {
		settlements = stored
	}
	return &EndResult{BankRub: bank, Settlements: settlements, CancelledPending: cancelled}, nil
}

// CancelGame отменяет игру дилером; висящие запросы закрываются.
func (s *GameService) CancelGame(ctx context.Context, g *domain.Game, dealerID int64) ([]domain.PendingAction, error) {
	if err := requireActiveDealer(g, dealerID); err != nil {
		return nil, err
	}
	return s.cancelGame(ctx, g)
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
	cancelled, err := s.cancelGame(ctx, g)
	return cancelled, last, err
}

func (s *GameService) cancelGame(ctx context.Context, g *domain.Game) ([]domain.PendingAction, error) {
	var cancelled []domain.PendingAction
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.games.Cancel(ctx, g.ID); err != nil {
			return err
		}
		var err error
		cancelled, err = s.pending.CancelByGame(ctx, g.ID)
		return err
	})
	return cancelled, err
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
	id, err := s.NewGame(ctx, g)
	if err != nil {
		if _, aErr := s.GetActiveGame(ctx, chatID); aErr == nil {
			return nil, ErrGameAlreadyActive
		}
		return nil, err
	}
	g.ID = id

	if dealerBuyIn {
		if err := s.BuyIn(ctx, g, dealerID, dealerID); err != nil {
			return g, err
		}
	}
	return s.GameByID(ctx, id)
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

// LastGameInChat — последняя игра группы или nil.
func (s *GameService) LastGameInChat(ctx context.Context, chatID int64) (*domain.Game, error) {
	return s.games.GetLastByChat(ctx, chatID)
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

// ChatsOf — группы, где пользователь известен боту.
func (s *GameService) ChatsOf(ctx context.Context, tgID int64) ([]domain.Chat, error) {
	return s.chats.ListByMember(ctx, tgID)
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

// ClosePending отменяет висящие запросы игры и возвращает их.
func (s *GameService) ClosePending(ctx context.Context, gameID int64) ([]domain.PendingAction, error) {
	return s.pending.CancelByGame(ctx, gameID)
}

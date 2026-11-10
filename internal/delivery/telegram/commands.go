package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tg "github.com/arrowwhi/infrastructure/telegram"
	"go.uber.org/zap"

	"poker_bank/internal/domain"
	"poker_bank/internal/service"
)

// Состояния FSM для пошагового ввода /newgame
const (
	fsmNewGameBuyInRub   = "newgame_buy_in_rub"
	fsmNewGameBuyInChips = "newgame_buy_in_chips"
	fsmNewGameRebuyRub   = "newgame_rebuy_rub"
	fsmNewGameRebuyChips = "newgame_rebuy_chips"
)

// handleNewGame создаёт новую игру.
// Синтаксис: /newgame [buy_in_rub buy_in_chips rebuy_rub rebuy_chips]
// Если параметры не указаны — запускает пошаговый ввод через FSM.
func (h *Handler) handleNewGame(c *Context) error {
	ctx := context.Background()
	chatID := c.Chat().ID

	_, err := h.game.GetActiveGame(ctx, chatID)
	if err == nil {
		return c.Reply("❌ В этом чате уже есть активная игра. Используй /status для деталей.")
	}
	if !errors.Is(err, service.ErrNoActiveGame) {
		h.log.Error("проверка активной игры", zap.Error(err))
		return c.Reply("Внутренняя ошибка. Попробуй позже.")
	}

	args := c.Args()
	switch len(args) {
	case 4:
		return h.newGameFromArgs(c, ctx, chatID, c.Sender().ID, args)
	case 0:
		return h.newGameStartFSM(c, ctx, chatID, c.Sender().ID)
	default:
		return c.Reply("Использование: /newgame [buy_in_rub buy_in_chips rebuy_rub rebuy_chips]")
	}
}

// newGameStartFSM запускает пошаговый ввод параметров игры.
func (h *Handler) newGameStartFSM(c *Context, ctx context.Context, chatID, senderID int64) error {
	if err := h.fsm.Set(ctx, &domain.FSMState{
		ChatID:   chatID,
		UserTgID: senderID,
		State:    fsmNewGameBuyInRub,
		Data:     map[string]any{},
	}); err != nil {
		return err
	}
	return c.Reply("Введи сумму buy-in в рублях (например: 1000):")
}

// newGameFromArgs создаёт игру из четырёх аргументов командной строки.
func (h *Handler) newGameFromArgs(c *Context, ctx context.Context, chatID, senderID int64, args []string) error {
	buyInRub, e1 := strconv.Atoi(args[0])
	buyInChips, e2 := strconv.Atoi(args[1])
	rebuyRub, e3 := strconv.Atoi(args[2])
	rebuyChips, e4 := strconv.Atoi(args[3])
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return c.Reply("❌ Все параметры должны быть целыми числами.")
	}
	if buyInRub <= 0 || buyInChips <= 0 || rebuyRub <= 0 || rebuyChips <= 0 {
		return c.Reply("❌ Все параметры должны быть положительными.")
	}
	if buyInRub*rebuyChips != rebuyRub*buyInChips {
		return c.Reply("❌ Курс не пропорционален: buy_in_rub × rebuy_chips ≠ rebuy_rub × buy_in_chips.")
	}
	return h.createGame(c, ctx, chatID, senderID, buyInRub, buyInChips, rebuyRub, rebuyChips)
}

// createGame записывает игру в БД и отправляет сводку в чат.
func (h *Handler) createGame(c *Context, ctx context.Context, chatID, dealerID int64, buyInRub, buyInChips, rebuyRub, rebuyChips int) error {
	id, err := h.game.NewGame(ctx, &domain.Game{
		ChatID:     chatID,
		DealerTgID: dealerID,
		BuyInRub:   buyInRub,
		BuyInChips: buyInChips,
		RebuyRub:   rebuyRub,
		RebuyChips: rebuyChips,
	})
	if err != nil {
		h.log.Error("создание игры", zap.Error(err))
		return c.Reply("Ошибка при создании игры.")
	}

	return c.Reply(fmt.Sprintf(
		"🎰 Игра #%d начата!\nДилер: %s\nBuy-in: %d₽ → %d фишек\nRebuy: %d₽ → %d фишек\nКурс: %s₽/фишка",
		id, playerName(c.Sender(), dealerID),
		buyInRub, buyInChips,
		rebuyRub, rebuyChips,
		formatRate(buyInRub, buyInChips),
	))
}

// handleText обрабатывает текстовые сообщения для FSM-состояний (пошаговый /newgame).
func (h *Handler) handleText(c *Context) error {
	ctx := context.Background()

	state, err := h.fsm.Get(ctx, c.Chat().ID, c.Sender().ID)
	if err != nil {
		// Нет активного FSM-состояния — текстовое сообщение игнорируется
		return nil
	}

	switch state.State {
	case fsmNewGameBuyInRub:
		return h.fsmStepBuyInRub(c, ctx, state)
	case fsmNewGameBuyInChips:
		return h.fsmStepBuyInChips(c, ctx, state)
	case fsmNewGameRebuyRub:
		return h.fsmStepRebuyRub(c, ctx, state)
	case fsmNewGameRebuyChips:
		return h.fsmStepRebuyChips(c, ctx, state)
	}
	return nil
}

func (h *Handler) fsmStepBuyInRub(c *Context, ctx context.Context, state *domain.FSMState) error {
	v, err := parsePositiveInt(c.Text())
	if err != nil {
		return c.Reply("❌ Введи положительное целое число (рублей):")
	}
	state.State = fsmNewGameBuyInChips
	state.Data["buy_in_rub"] = v
	if err := h.fsm.Set(ctx, state); err != nil {
		return err
	}
	return c.Reply("Введи количество фишек для buy-in:")
}

func (h *Handler) fsmStepBuyInChips(c *Context, ctx context.Context, state *domain.FSMState) error {
	v, err := parsePositiveInt(c.Text())
	if err != nil {
		return c.Reply("❌ Введи положительное целое число (фишек):")
	}
	state.State = fsmNewGameRebuyRub
	state.Data["buy_in_chips"] = v
	if err := h.fsm.Set(ctx, state); err != nil {
		return err
	}
	return c.Reply("Введи сумму rebuy в рублях:")
}

func (h *Handler) fsmStepRebuyRub(c *Context, ctx context.Context, state *domain.FSMState) error {
	v, err := parsePositiveInt(c.Text())
	if err != nil {
		return c.Reply("❌ Введи положительное целое число (рублей):")
	}
	state.State = fsmNewGameRebuyChips
	state.Data["rebuy_rub"] = v
	if err := h.fsm.Set(ctx, state); err != nil {
		return err
	}
	return c.Reply("Введи количество фишек для rebuy:")
}

func (h *Handler) fsmStepRebuyChips(c *Context, ctx context.Context, state *domain.FSMState) error {
	rebuyChips, err := parsePositiveInt(c.Text())
	if err != nil {
		return c.Reply("❌ Введи положительное целое число (фишек):")
	}

	// JSON-числа при чтении из JSONB возвращаются как float64
	buyInRub := int(state.Data["buy_in_rub"].(float64))
	buyInChips := int(state.Data["buy_in_chips"].(float64))
	rebuyRub := int(state.Data["rebuy_rub"].(float64))

	_ = h.fsm.Delete(ctx, state.ChatID, state.UserTgID)

	if buyInRub*rebuyChips != rebuyRub*buyInChips {
		return c.Reply(fmt.Sprintf(
			"❌ Курс не пропорционален: %d×%d ≠ %d×%d.\nНачни заново с /newgame.",
			buyInRub, rebuyChips, rebuyRub, buyInChips,
		))
	}

	// Повторная проверка на случай гонки
	if _, err = h.game.GetActiveGame(ctx, state.ChatID); err == nil {
		return c.Reply("❌ В этом чате уже есть активная игра.")
	}

	return h.createGame(c, ctx, state.ChatID, state.UserTgID, buyInRub, buyInChips, rebuyRub, rebuyChips)
}

func (h *Handler) handleEndGame(c *Context) error      { return h.doEndGame(c, false) }
func (h *Handler) handleEndGameForce(c *Context) error { return h.doEndGame(c, true) }

// doEndGame — общая логика завершения игры.
// force=false: требует нулевой банк.
// force=true:  принимает расхождение, распределяет дельту пропорционально.
func (h *Handler) doEndGame(c *Context, force bool) error {
	ctx := context.Background()

	game, err := h.getActiveGameForDealer(c, ctx)
	if err != nil {
		return err
	}
	if game == nil {
		return nil
	}

	res, err := h.game.EndGame(ctx, game, c.Sender().ID, force)
	var active *service.ActiveParticipantsError
	var mismatch *service.BankMismatchError
	switch {
	case errors.As(err, &active):
		return c.Reply(h.buildActiveParticipantsList(ctx, active.PlayerIDs))
	case errors.As(err, &mismatch):
		return c.Reply(fmt.Sprintf(
			"⚠️ Расхождение: банк = %+d₽. Чтобы продолжить — /endgame_force.",
			mismatch.BankRub,
		))
	case err != nil:
		h.log.Error("завершение игры", zap.Error(err))
		return c.Reply("Ошибка при завершении игры.")
	}

	h.RequestsClosed(ctx, res.CancelledPending)
	if res.Cancelled {
		return c.Reply(emptyGameCancelledText(game.ID))
	}
	m, err := c.ReplyMessage(h.buildEndGameMessage(ctx, game.ID, res.BankRub, res.Settlements), settleMarkup(game.ID, res.Settlements))
	if err != nil {
		return err
	}
	h.rememberSettleMessage(ctx, game.ID, m.MessageID)
	return nil
}

// emptyGameCancelledText — сообщение об отмене игры, в которой никто не сделал buy-in:
// «Завершить» для неё равносильно /cancel (ТЗ §11).
func emptyGameCancelledText(gameID int64) string {
	return fmt.Sprintf("🚫 Игра #%d отменена: никто не сделал buy-in.", gameID)
}

// handleCancel запрашивает подтверждение отмены игры у дилера.
func (h *Handler) handleCancel(c *Context) error {
	ctx := context.Background()

	game, err := h.getActiveGameForDealer(c, ctx)
	if err != nil {
		return err
	}
	if game == nil {
		return nil
	}

	markup := &Markup{InlineKeyboard: [][]Button{{
		{Text: "✅ Да, отменить", CallbackData: fmt.Sprintf("cancel:%d:yes", game.ID)},
		{Text: "❌ Нет", CallbackData: fmt.Sprintf("cancel:%d:no", game.ID)},
	}},
	}
	return c.Reply("⚠️ Отменить игру? Данные сохранятся, переводы не рассчитываются.", markup)
}

// handleAdminCancel — админ чата отменяет игру, если дилер бездействует больше 6 часов (ТЗ §8.1).
func (h *Handler) handleAdminCancel(c *Context) error {
	ctx := context.Background()

	game, err := h.game.GetActiveGame(ctx, c.Chat().ID)
	if errors.Is(err, service.ErrNoActiveGame) {
		return c.Reply("❌ Нет активной игры.")
	}
	if err != nil {
		return err
	}
	if !h.isChatAdmin(c) {
		return c.Reply("❌ Команда доступна только администраторам чата.")
	}

	cancelled, last, err := h.game.AdminCancelGame(ctx, game, time.Now())
	if errors.Is(err, service.ErrDealerNotIdle) {
		wait := time.Until(last.Add(service.AdminIdleTimeout)).Round(time.Minute)
		return c.Reply(fmt.Sprintf("⏳ Дилер был активен %s. Отменить игру можно через 6 ч после его последнего действия (осталось ~%d ч %02d мин).",
			last.Format("02.01 15:04"), int(wait.Hours()), int(wait.Minutes())%60))
	}
	if err != nil {
		return err
	}
	h.RequestsClosed(ctx, cancelled)

	return c.Reply(fmt.Sprintf("🚫 Игра #%d отменена администратором: дилер не отвечал больше 6 часов.", game.ID))
}

// handleTransferDealer передаёт роль дилера активному участнику. Только текущий дилер.
// Синтаксис: /transfer_dealer @username
func (h *Handler) handleTransferDealer(c *Context) error {
	ctx := context.Background()

	game, err := h.getActiveGameForDealer(c, ctx)
	if err != nil {
		return err
	}
	if game == nil {
		return nil
	}

	args := c.Args()
	if len(args) != 1 {
		return c.Reply("Использование: /transfer_dealer @username")
	}

	username := strings.TrimPrefix(args[0], "@")
	target, err := h.player.GetByUsername(ctx, username)
	if err != nil {
		return c.Reply(fmt.Sprintf("❌ Игрок @%s не найден в базе.", username))
	}

	p, err := h.game.GetParticipant(ctx, game.ID, target.TelegramUserID)
	if err != nil || !p.IsActive {
		return c.Reply(fmt.Sprintf("❌ %s не является активным участником игры.", formatPlayerByID(target, target.TelegramUserID)))
	}

	if err := h.game.TransferDealer(ctx, game.ID, target.TelegramUserID); err != nil {
		return err
	}

	return c.Reply(fmt.Sprintf(
		"🎩 Роль дилера передана %s.",
		formatPlayerByID(target, target.TelegramUserID),
	))
}

// handleJoin создаёт запрос на вход в игру, ожидающий подтверждения дилера.
func (h *Handler) handleJoin(c *Context) error {
	return h.requestFromChat(c, domain.ActionJoin, 0)
}

// handleRebuy создаёт запрос на rebuy, ожидающий подтверждения дилера.
func (h *Handler) handleRebuy(c *Context) error {
	return h.requestFromChat(c, domain.ActionRebuy, 0)
}

// requestFromChat — общий путь /join, /rebuy, /cashout: запрос дилеру (или сразу запись,
// если просит сам дилер) и сообщение с кнопками, id которого сохраняется.
func (h *Handler) requestFromChat(c *Context, t domain.ActionType, chips int) error {
	ctx := context.Background()

	game, err := h.game.GetActiveGame(ctx, c.Chat().ID)
	if errors.Is(err, service.ErrNoActiveGame) {
		return c.Reply("❌ Нет активной игры.")
	}
	if err != nil {
		return err
	}

	res, err := h.game.CreateRequest(ctx, game, c.Sender().ID, t, chips)
	switch {
	case errors.Is(err, service.ErrAlreadyInGame):
		return c.Reply("❌ Вы уже в игре.")
	case errors.Is(err, service.ErrNotInGame):
		return c.Reply("❌ Вы не в игре. Сначала используйте /join.")
	case errors.Is(err, service.ErrPendingExists):
		return c.Reply("❌ У вас уже есть такой запрос, ожидающий подтверждения.")
	case errors.Is(err, service.ErrInvalidChipsAmount):
		return c.Reply(fmt.Sprintf("❌ %d фишек даёт нецелое число рублей. Курс: %s₽/фишку.",
			chips, formatRate(game.BuyInRub, game.BuyInChips)))
	case err != nil:
		h.log.Error("создание запроса", zap.Error(err), zap.String("type", string(t)))
		return c.Reply("Ошибка при создании запроса.")
	}

	if res.Applied {
		h.RequestsClosed(ctx, res.Cancelled)
		return c.Reply(h.appliedText(ctx, game, res.Action))
	}
	h.RequestCreated(ctx, game, res.Action)
	return nil
}

// appliedText — сообщение о действии, записанном без подтверждения (дилер сам себе).
func (h *Handler) appliedText(ctx context.Context, g *domain.Game, pa *domain.PendingAction) string {
	who := h.name(ctx, pa.TargetTgID)
	rub, chips := service.RequestAmount(g, pa)
	switch pa.ActionType {
	case domain.ActionJoin:
		return fmt.Sprintf("✅ %s в игре (buy-in: %d₽ → %d фишек).", who, rub, chips)
	case domain.ActionRebuy:
		return fmt.Sprintf("✅ %s rebuy (%d₽ → %d фишек).", who, rub, chips)
	default:
		return fmt.Sprintf("💸 %s вышел: %d фишек = %d₽.", who, chips, rub)
	}
}

// handleDealerJoin запрашивает у дилера подтверждение добавления игрока.
// Синтаксис: /dealer_join @username
func (h *Handler) handleDealerJoin(c *Context) error {
	ctx := context.Background()

	game, err := h.getActiveGameForDealer(c, ctx)
	if err != nil {
		return err
	}
	if game == nil {
		return nil
	}

	args := c.Args()
	if len(args) != 1 {
		return c.Reply("Использование: /dealer_join @username")
	}

	username := strings.TrimPrefix(args[0], "@")
	target, err := h.player.GetByUsername(ctx, username)
	if err != nil {
		return c.Reply(fmt.Sprintf(
			"❌ Игрок @%s не найден в базе. Попроси его написать /join самостоятельно.",
			username,
		))
	}

	p, _ := h.game.GetParticipant(ctx, game.ID, target.TelegramUserID)
	if p != nil && p.IsActive {
		return c.Reply(fmt.Sprintf("❌ %s уже в игре.", formatPlayerByID(target, target.TelegramUserID)))
	}

	markup := &Markup{InlineKeyboard: [][]Button{{
		{Text: "✅ Да", CallbackData: fmt.Sprintf("djoin:%d:%d:yes", game.ID, target.TelegramUserID)},
		{Text: "❌ Нет", CallbackData: fmt.Sprintf("djoin:%d:%d:no", game.ID, target.TelegramUserID)},
	}},
	}
	return c.Reply(fmt.Sprintf(
		"Добавить %s в игру? (buy-in: %d₽ → %d фишек)",
		formatPlayerByID(target, target.TelegramUserID), game.BuyInRub, game.BuyInChips,
	), markup)
}

// handleDealerRebuy запрашивает у дилера подтверждение rebuy для игрока.
// Синтаксис: /dealer_rebuy @username
func (h *Handler) handleDealerRebuy(c *Context) error {
	ctx := context.Background()

	game, err := h.getActiveGameForDealer(c, ctx)
	if err != nil {
		return err
	}
	if game == nil {
		return nil
	}

	args := c.Args()
	if len(args) != 1 {
		return c.Reply("Использование: /dealer_rebuy @username")
	}

	username := strings.TrimPrefix(args[0], "@")
	target, err := h.player.GetByUsername(ctx, username)
	if err != nil {
		return c.Reply(fmt.Sprintf("❌ Игрок @%s не найден в базе.", username))
	}

	p, err := h.game.GetParticipant(ctx, game.ID, target.TelegramUserID)
	if err != nil || !p.IsActive {
		return c.Reply(fmt.Sprintf("❌ %s не является активным участником игры.", formatPlayerByID(target, target.TelegramUserID)))
	}

	markup := &Markup{InlineKeyboard: [][]Button{{
		{Text: "✅ Да", CallbackData: fmt.Sprintf("drebuy:%d:%d:yes", game.ID, target.TelegramUserID)},
		{Text: "❌ Нет", CallbackData: fmt.Sprintf("drebuy:%d:%d:no", game.ID, target.TelegramUserID)},
	}},
	}
	return c.Reply(fmt.Sprintf(
		"Rebuy для %s? (%d₽ → %d фишек)",
		formatPlayerByID(target, target.TelegramUserID), game.RebuyRub, game.RebuyChips,
	), markup)
}

// handleCashOut создаёт запрос на выход из игры, ожидающий подтверждения дилера.
// Синтаксис: /cashout <chips>
func (h *Handler) handleCashOut(c *Context) error {
	args := c.Args()
	if len(args) != 1 {
		return c.Reply("Использование: /cashout <chips>")
	}
	chips, err := parseNonNegativeInt(args[0])
	if err != nil {
		return c.Reply("❌ Количество фишек должно быть целым числом (0 или больше).")
	}
	return h.requestFromChat(c, domain.ActionCashOut, chips)
}

// handleDealerCashOut делает cashout за игрока с подтверждением дилера.
// Синтаксис: /dealer_cashout @username <chips>
func (h *Handler) handleDealerCashOut(c *Context) error {
	ctx := context.Background()

	game, err := h.getActiveGameForDealer(c, ctx)
	if err != nil {
		return err
	}
	if game == nil {
		return nil
	}

	args := c.Args()
	if len(args) != 2 {
		return c.Reply("Использование: /dealer_cashout @username <chips>")
	}

	username := strings.TrimPrefix(args[0], "@")
	chips, err := parseNonNegativeInt(args[1])
	if err != nil {
		return c.Reply("❌ Количество фишек должно быть целым числом (0 или больше).")
	}

	target, err := h.player.GetByUsername(ctx, username)
	if err != nil {
		return c.Reply(fmt.Sprintf("❌ Игрок @%s не найден в базе.", username))
	}

	p, err := h.game.GetParticipant(ctx, game.ID, target.TelegramUserID)
	if err != nil || !p.IsActive {
		return c.Reply(fmt.Sprintf("❌ %s не является активным участником игры.", formatPlayerByID(target, target.TelegramUserID)))
	}

	amountRub, ok := game.ChipsToRub(chips)
	if !ok {
		return c.Reply(fmt.Sprintf(
			"❌ %d фишек даёт нецелое число рублей. Курс: %s₽/фишку.",
			chips, formatRate(game.BuyInRub, game.BuyInChips),
		))
	}

	markup := &Markup{InlineKeyboard: [][]Button{{
		{Text: "✅ Да", CallbackData: fmt.Sprintf("dcashout:%d:%d:%d:yes", game.ID, target.TelegramUserID, chips)},
		{Text: "❌ Нет", CallbackData: fmt.Sprintf("dcashout:%d:%d:%d:no", game.ID, target.TelegramUserID, chips)},
	}},
	}
	return c.Reply(fmt.Sprintf(
		"💸 Cashout для %s: %d фишек = %d₽?",
		formatPlayerByID(target, target.TelegramUserID), chips, amountRub,
	), markup)
}

// handleUndo аннулирует последние N записей леджера. Только дилер.
// Синтаксис: /undo [N] (по умолчанию N=1)
func (h *Handler) handleUndo(c *Context) error {
	ctx := context.Background()

	game, err := h.getActiveGameForDealer(c, ctx)
	if err != nil {
		return err
	}
	if game == nil {
		return nil
	}

	n := 1
	if args := c.Args(); len(args) == 1 {
		if n, err = parsePositiveInt(args[0]); err != nil {
			return c.Reply("❌ N должно быть положительным целым числом.")
		}
	}

	voided, err := h.game.UndoLast(ctx, game.ID, n)
	if err != nil {
		return err
	}
	if len(voided) == 0 {
		return c.Reply("Нечего отменять.")
	}

	sb := strings.Builder{}
	fmt.Fprintf(&sb, "↩️ Отменено %d запис(ей):\n", len(voided))
	for _, e := range voided {
		player, _ := h.player.GetByID(ctx, e.PlayerTgID)
		fmt.Fprintf(&sb, "• %s — %s %d₽\n",
			formatPlayerByID(player, e.PlayerTgID), e.Type, e.AmountRub)
	}
	return c.Reply(sb.String())
}

// handleStatus показывает состояние текущей игры: курс, банк, участники, pending.
func (h *Handler) handleStatus(c *Context) error {
	ctx := context.Background()

	game, err := h.game.GetActiveGame(ctx, c.Chat().ID)
	if errors.Is(err, service.ErrNoActiveGame) {
		return c.Reply("Нет активной игры.")
	}
	if err != nil {
		return err
	}

	// Загружаем всё параллельно через отдельные вызовы
	participants, err := h.game.GetAllParticipants(ctx, game.ID)
	if err != nil {
		return err
	}
	entries, err := h.game.GetLedger(ctx, game.ID)
	if err != nil {
		return err
	}
	bank, err := h.game.GetBank(ctx, game.ID)
	if err != nil {
		return err
	}
	pendingList, err := h.pending.ListByGame(ctx, game.ID)
	if err != nil {
		return err
	}

	// Считаем агрегаты по каждому игроку из леджера
	type playerAgg struct {
		net    int
		buyIns int
		rebuys int
	}
	aggByPlayer := make(map[int64]*playerAgg)
	for _, e := range entries {
		if e.IsVoid {
			continue
		}
		a, ok := aggByPlayer[e.PlayerTgID]
		if !ok {
			a = &playerAgg{}
			aggByPlayer[e.PlayerTgID] = a
		}
		switch e.Type {
		case domain.LedgerBuyIn:
			a.net -= e.AmountRub
			a.buyIns++
		case domain.LedgerRebuy:
			a.net -= e.AmountRub
			a.rebuys++
		case domain.LedgerCashOut:
			a.net += e.AmountRub
		}
	}

	dealer, _ := h.player.GetByID(ctx, game.DealerTgID)

	sb := strings.Builder{}
	fmt.Fprintf(&sb, "🎮 Игра #%d\n", game.ID)
	fmt.Fprintf(&sb, "Дилер: %s\n", formatPlayerByID(dealer, game.DealerTgID))
	fmt.Fprintf(&sb, "Курс: %s₽/фишку | Buy-in: %d₽ | Rebuy: %d₽\n",
		formatRate(game.BuyInRub, game.BuyInChips), game.BuyInRub, game.RebuyRub)
	fmt.Fprintf(&sb, "💰 Банк: %d₽\n", bank)

	var active, out []domain.Participant
	for _, p := range participants {
		if p.IsActive {
			active = append(active, p)
		} else {
			out = append(out, p)
		}
	}

	formatParticipantLine := func(tgID int64) string {
		player, _ := h.player.GetByID(ctx, tgID)
		a := aggByPlayer[tgID]
		if a == nil {
			return fmt.Sprintf("• %s: нет данных\n", formatPlayerByID(player, tgID))
		}
		detail := "1 buy-in"
		if a.buyIns != 1 {
			detail = fmt.Sprintf("%d buy-in", a.buyIns)
		}
		if a.rebuys > 0 {
			detail += fmt.Sprintf(" + %d rebuy", a.rebuys)
		}
		return fmt.Sprintf("• %s: %s → %+d₽\n",
			formatPlayerByID(player, tgID), detail, a.net)
	}

	if len(active) > 0 {
		fmt.Fprintf(&sb, "\n🟢 В игре (%d):\n", len(active))
		for _, p := range active {
			sb.WriteString(formatParticipantLine(p.PlayerTgID))
		}
	}

	if len(out) > 0 {
		fmt.Fprintf(&sb, "\n⬛ Вышли (%d):\n", len(out))
		for _, p := range out {
			sb.WriteString(formatParticipantLine(p.PlayerTgID))
		}
	}

	if len(pendingList) > 0 {
		fmt.Fprintf(&sb, "\n⏳ Ожидают подтверждения (%d):\n", len(pendingList))
		for _, pa := range pendingList {
			player, _ := h.player.GetByID(ctx, pa.TargetTgID)
			fmt.Fprintf(&sb, "• %s — %s\n",
				formatPlayerByID(player, pa.TargetTgID), pa.ActionType)
		}
	}

	// Если все вышли и вызывает дилер — предлагаем завершить игру
	if len(active) == 0 && len(out) > 0 && game.DealerTgID == c.Sender().ID {
		markup := &Markup{InlineKeyboard: [][]Button{{
			{Text: "🏁 Завершить игру", CallbackData: fmt.Sprintf("endgame:%d", game.ID)},
		}},
		}
		return c.Reply(sb.String(), markup)
	}

	return c.Reply(sb.String())
}

// handleMe показывает личные показатели пользователя в текущей игре.
func (h *Handler) handleMe(c *Context) error {
	ctx := context.Background()
	senderID := c.Sender().ID

	game, err := h.game.GetActiveGame(ctx, c.Chat().ID)
	if errors.Is(err, service.ErrNoActiveGame) {
		return c.Reply("Нет активной игры.")
	}
	if err != nil {
		return err
	}

	p, err := h.game.GetParticipant(ctx, game.ID, senderID)
	if err != nil {
		return c.Reply("Вы не участвовали в этой игре.")
	}

	entries, err := h.game.GetLedger(ctx, game.ID)
	if err != nil {
		return err
	}

	var buyInCount, rebuyCount, totalInRub, totalOutRub, totalOutChips int
	for _, e := range entries {
		if e.IsVoid || e.PlayerTgID != senderID {
			continue
		}
		switch e.Type {
		case domain.LedgerBuyIn:
			buyInCount++
			totalInRub += e.AmountRub
		case domain.LedgerRebuy:
			rebuyCount++
			totalInRub += e.AmountRub
		case domain.LedgerCashOut:
			totalOutRub += e.AmountRub
			totalOutChips += e.AmountChips
		}
	}

	status := "⬛ вышел"
	if p.IsActive {
		status = "🟢 в игре"
	}

	sb := strings.Builder{}
	fmt.Fprintf(&sb, "👤 Ваши показатели в игре #%d:\n", game.ID)
	fmt.Fprintf(&sb, "Статус: %s\n", status)
	fmt.Fprintf(&sb, "Buy-in: %d × %d₽ = %d₽\n", buyInCount, game.BuyInRub, buyInCount*game.BuyInRub)
	if rebuyCount > 0 {
		fmt.Fprintf(&sb, "Rebuy: %d × %d₽ = %d₽\n", rebuyCount, game.RebuyRub, rebuyCount*game.RebuyRub)
	}
	fmt.Fprintf(&sb, "Вложено: %d₽\n", totalInRub)
	if totalOutRub > 0 {
		fmt.Fprintf(&sb, "Вышел: %d фишек = %d₽\n", totalOutChips, totalOutRub)
	}
	fmt.Fprintf(&sb, "Баланс: %+d₽\n", totalOutRub-totalInRub)

	return c.Reply(sb.String())
}

// handleHistory показывает последние N завершённых игр в чате (по умолчанию 10).
func (h *Handler) handleHistory(c *Context) error {
	ctx := context.Background()
	n := 10
	if args := c.Args(); len(args) == 1 {
		if v, err := parsePositiveInt(args[0]); err == nil {
			n = v
		}
	}

	games, err := h.game.GetHistory(ctx, c.Chat().ID, n)
	if err != nil {
		return err
	}
	if len(games) == 0 {
		return c.Reply("Завершённых игр ещё нет.")
	}

	sb := strings.Builder{}
	fmt.Fprintf(&sb, "📜 История игр (последние %d):\n", len(games))

	for _, g := range games {
		results, _ := h.game.GetResultsByGame(ctx, g.ID)

		date := g.StartedAt.Format("02.01.2006")
		if g.EndedAt != nil {
			date = g.EndedAt.Format("02.01.2006")
		}
		fmt.Fprintf(&sb, "\n#%d — %s | %d игроков\n", g.ID, date, len(results))

		var winner, loser *domain.GameResult
		for i := range results {
			r := &results[i]
			if winner == nil || r.NetRub > winner.NetRub {
				winner = r
			}
			if loser == nil || r.NetRub < loser.NetRub {
				loser = r
			}
		}
		if winner != nil {
			wp, _ := h.player.GetByID(ctx, winner.PlayerTgID)
			fmt.Fprintf(&sb, "🏆 %s %+d₽", formatPlayerByID(wp, winner.PlayerTgID), winner.NetRub)
		}
		if loser != nil && loser.PlayerTgID != winner.PlayerTgID {
			lp, _ := h.player.GetByID(ctx, loser.PlayerTgID)
			fmt.Fprintf(&sb, " | 💸 %s %+d₽", formatPlayerByID(lp, loser.PlayerTgID), loser.NetRub)
		}
		sb.WriteByte('\n')
	}

	return c.Reply(sb.String())
}

// gameLogLimit — сколько последних событий ledger показывать в /game (лимит сообщения Telegram).
const gameLogLimit = 40

// handleGame показывает детали игры этого чата: курс, итоги, переводы и лог событий (ТЗ §8.4).
// Синтаксис: /game <id>
func (h *Handler) handleGame(c *Context) error {
	ctx := context.Background()

	args := c.Args()
	if len(args) != 1 {
		return c.Reply("Использование: /game <id>  (номер игры есть в /history)")
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
	if err != nil || id <= 0 {
		return c.Reply("❌ Номер игры должен быть положительным числом.")
	}

	g, err := h.game.GameByID(ctx, id)
	// Игры других чатов не показываем, чтобы не раскрывать чужие данные.
	if errors.Is(err, service.ErrNotFound) || (err == nil && g.ChatID != c.Chat().ID) {
		return c.Reply(fmt.Sprintf("❌ Игра #%d в этом чате не найдена.", id))
	}
	if err != nil {
		return err
	}

	name := func(tgID int64) string {
		p, _ := h.player.GetByID(ctx, tgID)
		return formatPlayerByID(p, tgID)
	}

	sb := strings.Builder{}
	statusText := map[domain.GameStatus]string{
		domain.GameStatusActive:    "🟢 идёт",
		domain.GameStatusFinished:  "🏁 завершена",
		domain.GameStatusCancelled: "🚫 отменена",
	}[g.Status]
	fmt.Fprintf(&sb, "🃏 Игра #%d — %s\n", g.ID, statusText)
	fmt.Fprintf(&sb, "Дилер: %s\n", name(g.DealerTgID))
	fmt.Fprintf(&sb, "Buy-in: %d₽ → %d фишек, rebuy: %d₽ → %d фишек\n", g.BuyInRub, g.BuyInChips, g.RebuyRub, g.RebuyChips)
	fmt.Fprintf(&sb, "Начало: %s", g.StartedAt.Format("02.01.2006 15:04"))
	if g.EndedAt != nil {
		fmt.Fprintf(&sb, ", конец: %s", g.EndedAt.Format("02.01.2006 15:04"))
	}
	sb.WriteByte('\n')
	if g.BankDeltaRub != 0 {
		fmt.Fprintf(&sb, "⚠️ Расхождение банка: %+d₽\n", g.BankDeltaRub)
	}

	if g.Status == domain.GameStatusFinished {
		results, err := h.game.GetResultsByGame(ctx, g.ID)
		if err != nil {
			return err
		}
		if len(results) > 0 {
			sb.WriteString("\n📊 Итоги:\n")
			for _, r := range results {
				fmt.Fprintf(&sb, "• %s: внёс %d₽, вывел %d₽ → %+d₽\n", name(r.PlayerTgID), r.TotalInRub, r.TotalOutRub, r.NetRub)
			}
		}
		settlements, err := h.game.ListSettlements(ctx, g.ID)
		if err != nil {
			return err
		}
		if len(settlements) > 0 {
			sb.WriteString("\n💸 Переводы:\n")
			for _, s := range settlements {
				fmt.Fprintf(&sb, "• %s%s → %s%s: %d₽\n",
					settleMark(s.IsPaid), name(s.FromTgID), settleMark(s.IsReceived), name(s.ToTgID), s.AmountRub)
			}
		}
	}

	entries, err := h.game.GetLedger(ctx, g.ID)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		sb.WriteString("\nСобытий нет.")
		return c.Reply(sb.String())
	}
	sb.WriteString("\n📜 События:\n")
	if len(entries) > gameLogLimit {
		fmt.Fprintf(&sb, "… ещё %d ранних событий скрыто\n", len(entries)-gameLogLimit)
		entries = entries[len(entries)-gameLogLimit:]
	}
	for _, e := range entries {
		line := fmt.Sprintf("%s %s %s %d₽ / %d фишек",
			e.CreatedAt.Format("15:04"), name(e.PlayerTgID), e.Type, e.AmountRub, e.AmountChips)
		if e.IsVoid {
			line = "↩️ " + line + " (отменено)"
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	return c.Reply(sb.String())
}

// handleStats показывает статистику чата или конкретного игрока.
// /stats          — топ игроков чата по суммарному net_rub
// /stats @username — статистика конкретного игрока
func (h *Handler) handleStats(c *Context) error {
	ctx := context.Background()
	chatID := c.Chat().ID

	args := c.Args()
	if len(args) == 0 {
		return h.statsLeaderboard(c, ctx, chatID)
	}
	return h.statsPlayer(c, ctx, chatID, strings.TrimPrefix(args[0], "@"))
}

func (h *Handler) statsLeaderboard(c *Context, ctx context.Context, chatID int64) error {
	stats, err := h.game.GetLeaderboard(ctx, chatID)
	if err != nil {
		h.log.Error("leaderboard", zap.Error(err))
		return c.Reply("Ошибка при получении статистики.")
	}
	if len(stats) == 0 {
		return c.Reply("Ещё нет завершённых игр в этом чате.")
	}

	sb := strings.Builder{}
	sb.WriteString("📊 Топ игроков:\n")
	for i, s := range stats {
		player, _ := h.player.GetByID(ctx, s.PlayerTgID)
		name := formatPlayerByID(player, s.PlayerTgID)
		winRate := 0
		if s.GameCount > 0 {
			winRate = s.WinsCount * 100 / s.GameCount
		}
		fmt.Fprintf(&sb, "%d. %s — %+d₽ (%d игр, win-rate %d%%)\n",
			i+1, name, s.TotalNetRub, s.GameCount, winRate)
	}
	return c.Reply(sb.String())
}

func (h *Handler) statsPlayer(c *Context, ctx context.Context, chatID int64, username string) error {
	player, err := h.player.GetByUsername(ctx, username)
	if err != nil {
		return c.Reply(fmt.Sprintf("❌ Игрок @%s не найден.", username))
	}

	results, err := h.game.GetPlayerStats(ctx, player.TelegramUserID, chatID)
	if err != nil {
		h.log.Error("player stats", zap.Error(err))
		return c.Reply("Ошибка при получении статистики.")
	}
	if len(results) == 0 {
		return c.Reply(fmt.Sprintf("У @%s нет завершённых игр в этом чате.", username))
	}

	var totalNet, wins int
	for _, r := range results {
		totalNet += r.NetRub
		if r.NetRub > 0 {
			wins++
		}
	}
	games := len(results)
	avg := totalNet / games
	winRate := wins * 100 / games

	name := formatPlayerByID(player, player.TelegramUserID)
	return c.Reply(fmt.Sprintf(
		"📊 Статистика %s:\n• Игр: %d\n• Суммарно: %+d₽\n• Среднее: %+d₽/игру\n• Win-rate: %d%%",
		name, games, totalNet, avg, winRate,
	))
}

// handleCallback диспетчеризует нажатия инлайн-кнопок.
func (h *Handler) handleCallback(c *Context) error {
	data := c.Callback().Data
	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return c.Respond()
	}
	switch parts[0] {
	case "cancel":
		return h.handleCancelCallback(c, parts)
	case "endgame":
		return h.handleEndGameCallback(c, parts)
	case "pa":
		return h.handlePendingCallback(c, parts)
	case "djoin":
		return h.handleDealerJoinCallback(c, parts)
	case "drebuy":
		return h.handleDealerRebuyCallback(c, parts)
	case "dcashout":
		return h.handleDealerCashOutCallback(c, parts)
	case "settle":
		return h.handleSettleCallback(c, parts)
	}
	return c.Respond()
}

// handlePendingCallback обрабатывает подтверждение/отклонение запроса игрока.
func (h *Handler) handlePendingCallback(c *Context, parts []string) error {
	if len(parts) != 3 {
		return c.Respond()
	}
	paID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond()
	}

	ctx := context.Background()
	res, err := h.game.ResolveRequest(ctx, paID, c.Sender().ID, parts[2] == "yes")
	switch {
	case errors.Is(err, service.ErrAlreadyResolved):
		// Идемпотентность: решён раньше (в чате или в Mini App) — просто обновляем сообщение.
		_ = c.Edit(h.resolvedText(ctx, res.Action))
		return c.Respond(&CallbackResponse{Text: "Запрос уже обработан."})
	case errors.Is(err, service.ErrNotFound):
		return c.Respond(&CallbackResponse{Text: "Запрос не найден."})
	case errors.Is(err, service.ErrNotDealer):
		return c.Respond(&CallbackResponse{Text: "❌ Только дилер может подтверждать запросы."})
	case errors.Is(err, service.ErrGameNotActive):
		return c.Respond(&CallbackResponse{Text: "Игра уже не активна."})
	case errors.Is(err, service.ErrAlreadyInGame):
		return c.Respond(&CallbackResponse{Text: "Игрок уже в игре.", ShowAlert: true})
	case errors.Is(err, service.ErrNotInGame):
		return c.Respond(&CallbackResponse{Text: "Игрок уже не в игре.", ShowAlert: true})
	case err != nil:
		h.log.Error("решение по запросу", zap.Error(err), zap.Int64("pending_id", paID))
		return c.Respond(&CallbackResponse{Text: "Ошибка. Попробуй позже."})
	}

	h.RequestsClosed(ctx, res.Cancelled)
	_ = c.Edit(h.resolvedText(ctx, res.Action))
	return c.Respond()
}

// handleDealerJoinCallback выполняет buy-in после подтверждения дилером.
// Формат data: djoin:<game_id>:<target_tg_id>:yes|no
func (h *Handler) handleDealerJoinCallback(c *Context, parts []string) error {
	return h.handleDealerActionCallback(c, parts, func(ctx context.Context, game *domain.Game, targetID int64) error {
		return h.game.BuyIn(ctx, game, targetID, c.Sender().ID)
	}, func(game *domain.Game, target *domain.Player, targetID int64) string {
		return fmt.Sprintf("✅ %s добавлен (buy-in: %d₽ → %d фишек).",
			formatPlayerByID(target, targetID), game.BuyInRub, game.BuyInChips)
	})
}

// handleDealerRebuyCallback выполняет rebuy после подтверждения дилером.
// Формат data: drebuy:<game_id>:<target_tg_id>:yes|no
func (h *Handler) handleDealerRebuyCallback(c *Context, parts []string) error {
	return h.handleDealerActionCallback(c, parts, func(ctx context.Context, game *domain.Game, targetID int64) error {
		return h.game.Rebuy(ctx, game, targetID, c.Sender().ID)
	}, func(game *domain.Game, target *domain.Player, targetID int64) string {
		return fmt.Sprintf("✅ %s rebuy (%d₽ → %d фишек).",
			formatPlayerByID(target, targetID), game.RebuyRub, game.RebuyChips)
	})
}

// handleEndGameCallback завершает игру по нажатию кнопки из /status.
// Формат data: endgame:<game_id>
func (h *Handler) handleEndGameCallback(c *Context, parts []string) error {
	if len(parts) != 2 {
		return c.Respond()
	}
	gameID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond()
	}

	ctx := context.Background()
	game, err := h.game.GetGame(ctx, gameID)
	if err != nil {
		return c.Respond(&CallbackResponse{Text: "Игра не найдена."})
	}
	if game.Status != domain.GameStatusActive {
		_ = c.Edit("Игра уже не активна.")
		return c.Respond()
	}
	if game.DealerTgID != c.Sender().ID {
		return c.Respond(&CallbackResponse{Text: "❌ Только дилер может завершить игру."})
	}

	res, err := h.game.EndGame(ctx, game, c.Sender().ID, false)
	var active *service.ActiveParticipantsError
	var mismatch *service.BankMismatchError
	switch {
	case errors.As(err, &active):
		_ = c.Edit(h.buildActiveParticipantsList(ctx, active.PlayerIDs))
		return c.Respond()
	case errors.As(err, &mismatch):
		_ = c.Edit(fmt.Sprintf("⚠️ Расхождение: банк = %+d₽. Используй /endgame_force.", mismatch.BankRub))
		return c.Respond()
	case err != nil:
		h.log.Error("завершение игры (callback)", zap.Error(err))
		return c.Respond(&CallbackResponse{Text: "Ошибка при завершении игры."})
	}

	h.RequestsClosed(ctx, res.CancelledPending)
	if res.Cancelled {
		_ = c.Edit(emptyGameCancelledText(gameID))
		return c.Respond()
	}
	_ = c.Edit(h.buildEndGameMessage(ctx, gameID, res.BankRub, res.Settlements), settleMarkup(gameID, res.Settlements))
	if m := c.Callback().Message; m != nil {
		h.rememberSettleMessage(ctx, gameID, m.MessageID)
	}
	return c.Respond()
}

// handleSettleCallback отмечает переводы нажавшего: исходящие — «оплачено»,
// входящие — «получено». Формат data: settle:<game_id>
func (h *Handler) handleSettleCallback(c *Context, parts []string) error {
	if len(parts) != 2 {
		return c.Respond()
	}
	gameID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond()
	}

	ctx := context.Background()
	senderID := c.Sender().ID

	game, err := h.game.GetGame(ctx, gameID)
	if err != nil {
		return c.Respond(&CallbackResponse{Text: "Игра не найдена."})
	}
	if game.Status != domain.GameStatusFinished {
		return c.Respond(&CallbackResponse{Text: "Игра не завершена."})
	}

	changed, err := h.game.MarkSettlementsByPlayer(ctx, gameID, senderID)
	if err != nil {
		h.log.Error("отметка переводов", zap.Error(err), zap.Int64("game_id", gameID))
		return c.Respond(&CallbackResponse{Text: "Ошибка. Попробуй позже."})
	}

	settlements, err := h.game.ListSettlements(ctx, gameID)
	if err != nil {
		h.log.Error("список переводов", zap.Error(err), zap.Int64("game_id", gameID))
		return c.Respond(&CallbackResponse{Text: "Ошибка. Попробуй позже."})
	}

	if changed == 0 {
		for _, s := range settlements {
			if s.FromTgID == senderID || s.ToTgID == senderID {
				return c.Respond(&CallbackResponse{Text: "Уже отмечено."})
			}
		}
		return c.Respond(&CallbackResponse{Text: "Вас нет в переводах этой игры."})
	}

	// Когда все расчёты закрыты, settleMarkup == nil и правка без клавиатуры убирает кнопку.
	_ = c.Edit(h.buildEndGameMessage(ctx, gameID, game.BankDeltaRub, settlements), settleMarkup(gameID, settlements))
	return c.Respond(&CallbackResponse{Text: "Отмечено ✅"})
}

// handleDealerCashOutCallback выполняет cashout после подтверждения дилером.
// Формат data: dcashout:<game_id>:<target_tg_id>:<chips>:yes|no
func (h *Handler) handleDealerCashOutCallback(c *Context, parts []string) error {
	if len(parts) != 5 {
		return c.Respond()
	}
	gameID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond()
	}
	targetID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return c.Respond()
	}
	chips, err := strconv.Atoi(parts[3])
	if err != nil {
		return c.Respond()
	}

	ctx := context.Background()
	game, err := h.game.GetGame(ctx, gameID)
	if err != nil {
		return c.Respond(&CallbackResponse{Text: "Игра не найдена."})
	}
	if game.DealerTgID != c.Sender().ID {
		return c.Respond(&CallbackResponse{Text: "❌ Только дилер может подтверждать."})
	}

	if parts[4] == "no" {
		_ = c.Edit("Отменено.")
		return c.Respond()
	}

	cancelled, err := h.game.CashOut(ctx, game, targetID, chips, c.Sender().ID)
	if err != nil {
		h.log.Error("dealer cashout callback", zap.Error(err))
		return c.Respond(&CallbackResponse{Text: "Ошибка выполнения."})
	}
	h.editCancelledPending(cancelled)

	amountRub, _ := game.ChipsToRub(chips)
	target, _ := h.player.GetByID(ctx, targetID)
	_ = c.Edit(fmt.Sprintf("💸 %s вышел: %d фишек = %d₽.",
		formatPlayerByID(target, targetID), chips, amountRub))
	return c.Respond()
}

// handleDealerActionCallback — общий обработчик для djoin/drebuy callback-ов.
func (h *Handler) handleDealerActionCallback(
	c *Context,
	parts []string,
	execute func(ctx context.Context, game *domain.Game, targetID int64) error,
	successText func(game *domain.Game, target *domain.Player, targetID int64) string,
) error {
	if len(parts) != 4 {
		return c.Respond()
	}
	gameID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond()
	}
	targetID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return c.Respond()
	}

	ctx := context.Background()
	game, err := h.game.GetGame(ctx, gameID)
	if err != nil {
		return c.Respond(&CallbackResponse{Text: "Игра не найдена."})
	}
	if game.DealerTgID != c.Sender().ID {
		return c.Respond(&CallbackResponse{Text: "❌ Только дилер может подтверждать."})
	}

	if parts[3] == "no" {
		_ = c.Edit("Отменено.")
		return c.Respond()
	}

	if err := execute(ctx, game, targetID); err != nil {
		h.log.Error("dealer action callback", zap.Error(err))
		return c.Respond(&CallbackResponse{Text: "Ошибка выполнения."})
	}

	target, _ := h.player.GetByID(ctx, targetID)
	_ = c.Edit(successText(game, target, targetID))
	return c.Respond()
}

// handleCancelCallback обрабатывает подтверждение/отказ от отмены игры.
func (h *Handler) handleCancelCallback(c *Context, parts []string) error {
	if len(parts) != 3 {
		return c.Respond()
	}
	gameID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond()
	}

	ctx := context.Background()
	game, err := h.game.GetGame(ctx, gameID)
	if err != nil {
		return c.Respond(&CallbackResponse{Text: "Игра не найдена."})
	}
	if game.Status != domain.GameStatusActive {
		_ = c.Edit("Игра уже не активна.")
		return c.Respond()
	}
	if game.DealerTgID != c.Sender().ID {
		return c.Respond(&CallbackResponse{Text: "❌ Только дилер может отменить игру."})
	}

	if parts[2] == "no" {
		_ = c.Edit("Отмена игры отменена.")
		return c.Respond()
	}

	cancelled, err := h.game.CancelGame(ctx, game, c.Sender().ID)
	if err != nil {
		h.log.Error("отмена игры", zap.Error(err))
		return c.Respond(&CallbackResponse{Text: "Ошибка при отмене игры."})
	}
	h.RequestsClosed(ctx, cancelled)

	_ = c.Edit(fmt.Sprintf("🚫 Игра #%d отменена.", gameID))
	return c.Respond()
}

// --- вспомогательные методы ---

// getActiveGameForDealer возвращает активную игру, если вызывает дилер.
// Если условие не выполнено — сам отвечает пользователю и возвращает (nil, nil).
func (h *Handler) getActiveGameForDealer(c *Context, ctx context.Context) (*domain.Game, error) {
	game, err := h.game.GetActiveGame(ctx, c.Chat().ID)
	if errors.Is(err, service.ErrNoActiveGame) {
		return nil, c.Send("❌ Нет активной игры.")
	}
	if err != nil {
		return nil, err
	}
	if game.DealerTgID != c.Sender().ID {
		return nil, c.Send("❌ Только дилер может выполнить это действие.")
	}
	return game, nil
}

// buildActiveParticipantsList формирует сообщение со списком игроков, которым нужен cash-out.
func (h *Handler) buildActiveParticipantsList(ctx context.Context, playerIDs []int64) string {
	sb := strings.Builder{}
	sb.WriteString("❌ Сначала сделайте cash-out для:\n")
	for _, id := range playerIDs {
		player, _ := h.player.GetByID(ctx, id)
		sb.WriteString("• ")
		sb.WriteString(formatPlayerByID(player, id))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// buildEndGameMessage формирует итоговое сообщение с планом переводов.
// 🟢 перед именем отправителя — «оплатил», перед именем получателя — «получил».
func (h *Handler) buildEndGameMessage(ctx context.Context, gameID int64, bankDelta int, settlements []domain.Settlement) string {
	sb := strings.Builder{}
	if bankDelta != 0 {
		fmt.Fprintf(&sb, "⚠️ Расхождение %+d₽ распределено пропорционально.\n\n", bankDelta)
	}
	fmt.Fprintf(&sb, "🏁 Игра #%d завершена!\n", gameID)

	if len(settlements) == 0 {
		sb.WriteString("\nВсе квиты — переводов нет.")
		return sb.String()
	}

	sb.WriteString("\n💸 Переводы:\n")
	for _, s := range settlements {
		from, _ := h.player.GetByID(ctx, s.FromTgID)
		to, _ := h.player.GetByID(ctx, s.ToTgID)
		fmt.Fprintf(&sb, "• %s%s → %s%s: %d₽\n",
			settleMark(s.IsPaid), formatPlayerByID(from, s.FromTgID),
			settleMark(s.IsReceived), formatPlayerByID(to, s.ToTgID),
			s.AmountRub)
	}
	if allSettled(settlements) {
		sb.WriteString("\n✅ Все расчёты закрыты.")
	}
	return sb.String()
}

func settleMark(done bool) string {
	if done {
		return "🟢"
	}
	return ""
}

// allSettled сообщает, что каждый перевод отмечен и отправителем, и получателем.
func allSettled(settlements []domain.Settlement) bool {
	for _, s := range settlements {
		if !s.IsPaid || !s.IsReceived {
			return false
		}
	}
	return true
}

// settleMarkup возвращает кнопку отметки оплаты/получения или nil,
// если переводов нет или все они уже закрыты.
func settleMarkup(gameID int64, settlements []domain.Settlement) *Markup {
	if len(settlements) == 0 || allSettled(settlements) {
		return nil
	}
	return &Markup{InlineKeyboard: [][]Button{{
		{Text: "✅ Я оплатил / получил", CallbackData: fmt.Sprintf("settle:%d", gameID)},
	}},
	}
}

// --- форматирование ---

func playerName(u *tg.User, fallbackID int64) string {
	if u == nil {
		return fmt.Sprintf("id%d", fallbackID)
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return u.FirstName
}

func formatPlayerByID(p *domain.Player, fallbackID int64) string {
	if p == nil {
		return fmt.Sprintf("id%d", fallbackID)
	}
	if p.Username != "" {
		return "@" + p.Username
	}
	return p.DisplayName
}

// formatRate возвращает курс в виде строки: целый если без дроби, иначе с двумя знаками.
func formatRate(rub, chips int) string {
	if rub%chips == 0 {
		return strconv.Itoa(rub / chips)
	}
	return fmt.Sprintf("%.2f", float64(rub)/float64(chips))
}

// pendingMarkup возвращает инлайн-клавиатуру для запроса подтверждения.
func pendingMarkup(paID int64) *Markup {
	return &Markup{InlineKeyboard: [][]Button{{
		{Text: "✅ Подтвердить", CallbackData: fmt.Sprintf("pa:%d:yes", paID)},
		{Text: "❌ Отклонить", CallbackData: fmt.Sprintf("pa:%d:no", paID)},
	}},
	}
}

// pendingResolvedText возвращает текст для редактирования сообщения после резолва.
func pendingResolvedText(pa *domain.PendingAction) string {
	action := map[domain.ActionType]string{
		domain.ActionJoin:    "JOIN",
		domain.ActionRebuy:   "REBUY",
		domain.ActionCashOut: "CASHOUT",
	}[pa.ActionType]
	switch pa.Status {
	case domain.PendingStatusConfirmed:
		return fmt.Sprintf("✅ %s подтверждён.", action)
	case domain.PendingStatusRejected:
		return fmt.Sprintf("❌ %s отклонён.", action)
	case domain.PendingStatusExpired:
		return fmt.Sprintf("⏰ %s истёк.", action)
	case domain.PendingStatusCancelled:
		return fmt.Sprintf("🚫 %s отменён (игрок вышел).", action)
	default:
		return fmt.Sprintf("%s [%s].", action, pa.Status)
	}
}

// editCancelledPending редактирует сообщения отменённых запросов (убирает кнопки).
func (h *Handler) editCancelledPending(cancelled []domain.PendingAction) {
	h.RequestsClosed(context.Background(), cancelled)
}

func parsePositiveInt(s string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("не положительное целое")
	}
	return v, nil
}

func parseNonNegativeInt(s string) (int, error) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 0 {
		return 0, fmt.Errorf("не неотрицательное целое")
	}
	return v, nil
}

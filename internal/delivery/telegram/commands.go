package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/telebot.v3"

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
func (h *Handler) handleNewGame(c telebot.Context) error {
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
func (h *Handler) newGameStartFSM(c telebot.Context, ctx context.Context, chatID, senderID int64) error {
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
func (h *Handler) newGameFromArgs(c telebot.Context, ctx context.Context, chatID, senderID int64, args []string) error {
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
func (h *Handler) createGame(c telebot.Context, ctx context.Context, chatID, dealerID int64, buyInRub, buyInChips, rebuyRub, rebuyChips int) error {
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
func (h *Handler) handleText(c telebot.Context) error {
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

func (h *Handler) fsmStepBuyInRub(c telebot.Context, ctx context.Context, state *domain.FSMState) error {
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

func (h *Handler) fsmStepBuyInChips(c telebot.Context, ctx context.Context, state *domain.FSMState) error {
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

func (h *Handler) fsmStepRebuyRub(c telebot.Context, ctx context.Context, state *domain.FSMState) error {
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

func (h *Handler) fsmStepRebuyChips(c telebot.Context, ctx context.Context, state *domain.FSMState) error {
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

func (h *Handler) handleEndGame(c telebot.Context) error      { return h.doEndGame(c, false) }
func (h *Handler) handleEndGameForce(c telebot.Context) error { return h.doEndGame(c, true) }

// doEndGame — общая логика завершения игры.
// force=false: требует нулевой банк.
// force=true:  принимает расхождение, распределяет дельту пропорционально.
func (h *Handler) doEndGame(c telebot.Context, force bool) error {
	ctx := context.Background()

	game, err := h.getActiveGameForDealer(c, ctx)
	if err != nil {
		return err
	}
	if game == nil {
		return nil
	}

	active, err := h.game.GetActiveParticipants(ctx, game.ID)
	if err != nil {
		return err
	}
	if len(active) > 0 {
		return c.Reply(h.buildActiveParticipantsList(ctx, active))
	}

	bank, err := h.game.GetBank(ctx, game.ID)
	if err != nil {
		return err
	}
	if !force && bank != 0 {
		return c.Reply(fmt.Sprintf(
			"⚠️ Расхождение: банк = %+d₽. Чтобы продолжить — /endgame_force.",
			bank,
		))
	}

	// При force передаём delta; при обычном endgame bank гарантированно == 0
	settlements, err := h.game.Finish(ctx, game.ID, bank)
	if err != nil {
		h.log.Error("завершение игры", zap.Error(err))
		return c.Reply("Ошибка при завершении игры.")
	}

	msg := h.buildEndGameMessage(ctx, game.ID, settlements)
	if force && bank != 0 {
		msg = fmt.Sprintf("⚠️ Расхождение %+d₽ распределено пропорционально.\n\n", bank) + msg
	}
	return c.Reply(msg)
}

// handleCancel запрашивает подтверждение отмены игры у дилера.
func (h *Handler) handleCancel(c telebot.Context) error {
	ctx := context.Background()

	game, err := h.getActiveGameForDealer(c, ctx)
	if err != nil {
		return err
	}
	if game == nil {
		return nil
	}

	markup := &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{{
			{Text: "✅ Да, отменить", Data: fmt.Sprintf("cancel:%d:yes", game.ID)},
			{Text: "❌ Нет", Data: fmt.Sprintf("cancel:%d:no", game.ID)},
		}},
	}
	return c.Reply("⚠️ Отменить игру? Данные сохранятся, переводы не рассчитываются.", markup)
}

func (h *Handler) handleAdminCancel(_ telebot.Context) error { return nil }

// handleTransferDealer передаёт роль дилера активному участнику. Только текущий дилер.
// Синтаксис: /transfer_dealer @username
func (h *Handler) handleTransferDealer(c telebot.Context) error {
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
func (h *Handler) handleJoin(c telebot.Context) error {
	ctx := context.Background()
	chatID := c.Chat().ID
	senderID := c.Sender().ID

	game, err := h.game.GetActiveGame(ctx, chatID)
	if errors.Is(err, service.ErrNoActiveGame) {
		return c.Reply("❌ Нет активной игры.")
	}
	if err != nil {
		return err
	}

	// Проверяем статус участника в текущей игре
	p, err := h.game.GetParticipant(ctx, game.ID, senderID)
	if err == nil && p.IsActive {
		return c.Reply("❌ Вы уже в игре.")
	}

	// Проверяем, нет ли уже pending-запроса JOIN
	_, err = h.pending.GetPending(ctx, game.ID, senderID, domain.ActionJoin)
	if err == nil {
		return c.Reply("❌ У вас уже есть запрос на вход, ожидающий подтверждения.")
	}

	paID, err := h.pending.Create(ctx, &domain.PendingAction{
		GameID:        game.ID,
		ActionType:    domain.ActionJoin,
		RequesterTgID: senderID,
		TargetTgID:    senderID,
		Payload:       map[string]any{},
		ChatID:        chatID,
	})
	if err != nil {
		h.log.Error("создание pending JOIN", zap.Error(err))
		return c.Reply("Ошибка при создании запроса.")
	}

	markup := pendingMarkup(paID)
	_, err = h.bot.Send(c.Chat(), fmt.Sprintf(
		"🃏 %s просит JOIN (buy-in: %d₽ → %d фишек). Подтвердить?",
		playerName(c.Sender(), senderID), game.BuyInRub, game.BuyInChips,
	), markup)
	return err
}

// handleRebuy создаёт запрос на rebuy, ожидающий подтверждения дилера.
func (h *Handler) handleRebuy(c telebot.Context) error {
	ctx := context.Background()
	chatID := c.Chat().ID
	senderID := c.Sender().ID

	game, err := h.game.GetActiveGame(ctx, chatID)
	if errors.Is(err, service.ErrNoActiveGame) {
		return c.Reply("❌ Нет активной игры.")
	}
	if err != nil {
		return err
	}

	// Только активный участник может rebuy
	p, err := h.game.GetParticipant(ctx, game.ID, senderID)
	if err != nil || !p.IsActive {
		return c.Reply("❌ Вы не в игре. Сначала используйте /join.")
	}

	// Проверяем, нет ли уже pending-запроса REBUY
	_, err = h.pending.GetPending(ctx, game.ID, senderID, domain.ActionRebuy)
	if err == nil {
		return c.Reply("❌ У вас уже есть запрос на rebuy, ожидающий подтверждения.")
	}

	paID, err := h.pending.Create(ctx, &domain.PendingAction{
		GameID:        game.ID,
		ActionType:    domain.ActionRebuy,
		RequesterTgID: senderID,
		TargetTgID:    senderID,
		Payload:       map[string]any{},
		ChatID:        chatID,
	})
	if err != nil {
		h.log.Error("создание pending REBUY", zap.Error(err))
		return c.Reply("Ошибка при создании запроса.")
	}

	markup := pendingMarkup(paID)
	_, err = h.bot.Send(c.Chat(), fmt.Sprintf(
		"🃏 %s просит REBUY (%d₽ → %d фишек). Подтвердить?",
		playerName(c.Sender(), senderID), game.RebuyRub, game.RebuyChips,
	), markup)
	return err
}

// handleDealerJoin запрашивает у дилера подтверждение добавления игрока.
// Синтаксис: /dealer_join @username
func (h *Handler) handleDealerJoin(c telebot.Context) error {
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

	markup := &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{{
			{Text: "✅ Да", Data: fmt.Sprintf("djoin:%d:%d:yes", game.ID, target.TelegramUserID)},
			{Text: "❌ Нет", Data: fmt.Sprintf("djoin:%d:%d:no", game.ID, target.TelegramUserID)},
		}},
	}
	return c.Reply(fmt.Sprintf(
		"Добавить %s в игру? (buy-in: %d₽ → %d фишек)",
		formatPlayerByID(target, target.TelegramUserID), game.BuyInRub, game.BuyInChips,
	), markup)
}

// handleDealerRebuy запрашивает у дилера подтверждение rebuy для игрока.
// Синтаксис: /dealer_rebuy @username
func (h *Handler) handleDealerRebuy(c telebot.Context) error {
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

	markup := &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{{
			{Text: "✅ Да", Data: fmt.Sprintf("drebuy:%d:%d:yes", game.ID, target.TelegramUserID)},
			{Text: "❌ Нет", Data: fmt.Sprintf("drebuy:%d:%d:no", game.ID, target.TelegramUserID)},
		}},
	}
	return c.Reply(fmt.Sprintf(
		"Rebuy для %s? (%d₽ → %d фишек)",
		formatPlayerByID(target, target.TelegramUserID), game.RebuyRub, game.RebuyChips,
	), markup)
}

// handleCashOut создаёт запрос на выход из игры, ожидающий подтверждения дилера.
// Синтаксис: /cashout <chips>
func (h *Handler) handleCashOut(c telebot.Context) error {
	ctx := context.Background()
	senderID := c.Sender().ID

	game, err := h.game.GetActiveGame(ctx, c.Chat().ID)
	if errors.Is(err, service.ErrNoActiveGame) {
		return c.Reply("❌ Нет активной игры.")
	}
	if err != nil {
		return err
	}

	p, err := h.game.GetParticipant(ctx, game.ID, senderID)
	if err != nil || !p.IsActive {
		return c.Reply("❌ Вы не являетесь активным участником игры.")
	}

	args := c.Args()
	if len(args) != 1 {
		return c.Reply("Использование: /cashout <chips>")
	}

	chips, err := parseNonNegativeInt(args[0])
	if err != nil {
		return c.Reply("❌ Количество фишек должно быть целым числом (0 или больше).")
	}

	amountRub, ok := game.ChipsToRub(chips)
	if !ok {
		return c.Reply(fmt.Sprintf(
			"❌ %d фишек даёт нецелое число рублей. Курс: %s₽/фишку.",
			chips, formatRate(game.BuyInRub, game.BuyInChips),
		))
	}

	_, err = h.pending.GetPending(ctx, game.ID, senderID, domain.ActionCashOut)
	if err == nil {
		return c.Reply("❌ У вас уже есть запрос на выход, ожидающий подтверждения.")
	}

	paID, err := h.pending.Create(ctx, &domain.PendingAction{
		GameID:        game.ID,
		ActionType:    domain.ActionCashOut,
		RequesterTgID: senderID,
		TargetTgID:    senderID,
		Payload:       map[string]any{"chips": chips},
		ChatID:        c.Chat().ID,
	})
	if err != nil {
		h.log.Error("создание pending CASHOUT", zap.Error(err))
		return c.Reply("Ошибка при создании запроса.")
	}

	markup := pendingMarkup(paID)
	_, err = h.bot.Send(c.Chat(), fmt.Sprintf(
		"💸 %s хочет выйти: %d фишек = %d₽. Подтвердить?",
		playerName(c.Sender(), senderID), chips, amountRub,
	), markup)
	return err
}

// handleDealerCashOut делает cashout за игрока с подтверждением дилера.
// Синтаксис: /dealer_cashout @username <chips>
func (h *Handler) handleDealerCashOut(c telebot.Context) error {
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

	markup := &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{{
			{Text: "✅ Да", Data: fmt.Sprintf("dcashout:%d:%d:%d:yes", game.ID, target.TelegramUserID, chips)},
			{Text: "❌ Нет", Data: fmt.Sprintf("dcashout:%d:%d:%d:no", game.ID, target.TelegramUserID, chips)},
		}},
	}
	return c.Reply(fmt.Sprintf(
		"💸 Cashout для %s: %d фишек = %d₽?",
		formatPlayerByID(target, target.TelegramUserID), chips, amountRub,
	), markup)
}

// handleUndo аннулирует последние N записей леджера. Только дилер.
// Синтаксис: /undo [N] (по умолчанию N=1)
func (h *Handler) handleUndo(c telebot.Context) error {
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
func (h *Handler) handleStatus(c telebot.Context) error {
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
		markup := &telebot.ReplyMarkup{
			InlineKeyboard: [][]telebot.InlineButton{{
				{Text: "🏁 Завершить игру", Data: fmt.Sprintf("endgame:%d", game.ID)},
			}},
		}
		return c.Reply(sb.String(), markup)
	}

	return c.Reply(sb.String())
}

// handleMe показывает личные показатели пользователя в текущей игре.
func (h *Handler) handleMe(c telebot.Context) error {
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
func (h *Handler) handleHistory(c telebot.Context) error {
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

func (h *Handler) handleGame(_ telebot.Context) error { return nil }

// handleStats показывает статистику чата или конкретного игрока.
// /stats          — топ игроков чата по суммарному net_rub
// /stats @username — статистика конкретного игрока
func (h *Handler) handleStats(c telebot.Context) error {
	ctx := context.Background()
	chatID := c.Chat().ID

	args := c.Args()
	if len(args) == 0 {
		return h.statsLeaderboard(c, ctx, chatID)
	}
	return h.statsPlayer(c, ctx, chatID, strings.TrimPrefix(args[0], "@"))
}

func (h *Handler) statsLeaderboard(c telebot.Context, ctx context.Context, chatID int64) error {
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

func (h *Handler) statsPlayer(c telebot.Context, ctx context.Context, chatID int64, username string) error {
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
func (h *Handler) handleCallback(c telebot.Context) error {
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
	}
	return c.Respond()
}

// handlePendingCallback обрабатывает подтверждение/отклонение JOIN или REBUY.
func (h *Handler) handlePendingCallback(c telebot.Context, parts []string) error {
	if len(parts) != 3 {
		return c.Respond()
	}
	paID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond()
	}

	ctx := context.Background()
	senderID := c.Sender().ID

	pa, err := h.pending.GetByID(ctx, paID)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Запрос не найден."})
	}

	// Идемпотентность: если уже обработан — просто обновляем сообщение
	if pa.Status != domain.PendingStatusPending {
		_ = c.Edit(pendingResolvedText(pa))
		return c.Respond(&telebot.CallbackResponse{Text: "Запрос уже обработан."})
	}

	// Только дилер может подтверждать
	game, err := h.game.GetGame(ctx, pa.GameID)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Игра не найдена."})
	}
	if game.DealerTgID != senderID {
		return c.Respond(&telebot.CallbackResponse{Text: "❌ Только дилер может подтверждать запросы."})
	}

	newStatus := domain.PendingStatusRejected
	if parts[2] == "yes" {
		newStatus = domain.PendingStatusConfirmed
	}

	resolved, err := h.pending.Resolve(ctx, paID, newStatus, senderID)
	if err != nil {
		// 0 строк — кто-то успел раньше (гонка двойного клика)
		return c.Respond(&telebot.CallbackResponse{Text: "Запрос уже обработан."})
	}

	// Выполняем действие при подтверждении
	if resolved.Status == domain.PendingStatusConfirmed {
		switch resolved.ActionType {
		case domain.ActionJoin:
			if err := h.game.BuyIn(ctx, game, resolved.TargetTgID, senderID); err != nil {
				h.log.Error("buy-in после подтверждения", zap.Error(err))
			}
		case domain.ActionRebuy:
			if err := h.game.Rebuy(ctx, game, resolved.TargetTgID, senderID); err != nil {
				h.log.Error("rebuy после подтверждения", zap.Error(err))
			}
		case domain.ActionCashOut:
			chips := int(resolved.Payload["chips"].(float64))
			if err := h.game.CashOut(ctx, game, resolved.TargetTgID, chips, senderID); err != nil {
				h.log.Error("cashout после подтверждения", zap.Error(err))
			}
		}
	}

	target, _ := h.player.GetByID(ctx, resolved.TargetTgID)
	_ = c.Edit(pendingResolvedText(resolved) + " — " + formatPlayerByID(target, resolved.TargetTgID))
	return c.Respond()
}

// handleDealerJoinCallback выполняет buy-in после подтверждения дилером.
// Формат data: djoin:<game_id>:<target_tg_id>:yes|no
func (h *Handler) handleDealerJoinCallback(c telebot.Context, parts []string) error {
	return h.handleDealerActionCallback(c, parts, func(ctx context.Context, game *domain.Game, targetID int64) error {
		return h.game.BuyIn(ctx, game, targetID, c.Sender().ID)
	}, func(game *domain.Game, target *domain.Player, targetID int64) string {
		return fmt.Sprintf("✅ %s добавлен (buy-in: %d₽ → %d фишек).",
			formatPlayerByID(target, targetID), game.BuyInRub, game.BuyInChips)
	})
}

// handleDealerRebuyCallback выполняет rebuy после подтверждения дилером.
// Формат data: drebuy:<game_id>:<target_tg_id>:yes|no
func (h *Handler) handleDealerRebuyCallback(c telebot.Context, parts []string) error {
	return h.handleDealerActionCallback(c, parts, func(ctx context.Context, game *domain.Game, targetID int64) error {
		return h.game.Rebuy(ctx, game, targetID, c.Sender().ID)
	}, func(game *domain.Game, target *domain.Player, targetID int64) string {
		return fmt.Sprintf("✅ %s rebuy (%d₽ → %d фишек).",
			formatPlayerByID(target, targetID), game.RebuyRub, game.RebuyChips)
	})
}

// handleEndGameCallback завершает игру по нажатию кнопки из /status.
// Формат data: endgame:<game_id>
func (h *Handler) handleEndGameCallback(c telebot.Context, parts []string) error {
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
		return c.Respond(&telebot.CallbackResponse{Text: "Игра не найдена."})
	}
	if game.Status != domain.GameStatusActive {
		_ = c.Edit("Игра уже не активна.")
		return c.Respond()
	}
	if game.DealerTgID != c.Sender().ID {
		return c.Respond(&telebot.CallbackResponse{Text: "❌ Только дилер может завершить игру."})
	}

	bank, err := h.game.GetBank(ctx, gameID)
	if err != nil {
		return err
	}
	if bank != 0 {
		_ = c.Edit(fmt.Sprintf("⚠️ Расхождение: банк = %+d₽. Используй /endgame_force.", bank))
		return c.Respond()
	}

	settlements, err := h.game.Finish(ctx, gameID, 0)
	if err != nil {
		h.log.Error("завершение игры (callback)", zap.Error(err))
		return c.Respond(&telebot.CallbackResponse{Text: "Ошибка при завершении игры."})
	}

	_ = c.Edit(h.buildEndGameMessage(ctx, gameID, settlements))
	return c.Respond()
}

// handleDealerCashOutCallback выполняет cashout после подтверждения дилером.
// Формат data: dcashout:<game_id>:<target_tg_id>:<chips>:yes|no
func (h *Handler) handleDealerCashOutCallback(c telebot.Context, parts []string) error {
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
		return c.Respond(&telebot.CallbackResponse{Text: "Игра не найдена."})
	}
	if game.DealerTgID != c.Sender().ID {
		return c.Respond(&telebot.CallbackResponse{Text: "❌ Только дилер может подтверждать."})
	}

	if parts[4] == "no" {
		_ = c.Edit("Отменено.")
		return c.Respond()
	}

	if err := h.game.CashOut(ctx, game, targetID, chips, c.Sender().ID); err != nil {
		h.log.Error("dealer cashout callback", zap.Error(err))
		return c.Respond(&telebot.CallbackResponse{Text: "Ошибка выполнения."})
	}

	amountRub, _ := game.ChipsToRub(chips)
	target, _ := h.player.GetByID(ctx, targetID)
	_ = c.Edit(fmt.Sprintf("💸 %s вышел: %d фишек = %d₽.",
		formatPlayerByID(target, targetID), chips, amountRub))
	return c.Respond()
}

// handleDealerActionCallback — общий обработчик для djoin/drebuy callback-ов.
func (h *Handler) handleDealerActionCallback(
	c telebot.Context,
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
		return c.Respond(&telebot.CallbackResponse{Text: "Игра не найдена."})
	}
	if game.DealerTgID != c.Sender().ID {
		return c.Respond(&telebot.CallbackResponse{Text: "❌ Только дилер может подтверждать."})
	}

	if parts[3] == "no" {
		_ = c.Edit("Отменено.")
		return c.Respond()
	}

	if err := execute(ctx, game, targetID); err != nil {
		h.log.Error("dealer action callback", zap.Error(err))
		return c.Respond(&telebot.CallbackResponse{Text: "Ошибка выполнения."})
	}

	target, _ := h.player.GetByID(ctx, targetID)
	_ = c.Edit(successText(game, target, targetID))
	return c.Respond()
}

// handleCancelCallback обрабатывает подтверждение/отказ от отмены игры.
func (h *Handler) handleCancelCallback(c telebot.Context, parts []string) error {
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
		return c.Respond(&telebot.CallbackResponse{Text: "Игра не найдена."})
	}
	if game.Status != domain.GameStatusActive {
		_ = c.Edit("Игра уже не активна.")
		return c.Respond()
	}
	if game.DealerTgID != c.Sender().ID {
		return c.Respond(&telebot.CallbackResponse{Text: "❌ Только дилер может отменить игру."})
	}

	if parts[2] == "no" {
		_ = c.Edit("Отмена игры отменена.")
		return c.Respond()
	}

	if err := h.game.Cancel(ctx, gameID); err != nil {
		h.log.Error("отмена игры", zap.Error(err))
		return c.Respond(&telebot.CallbackResponse{Text: "Ошибка при отмене игры."})
	}

	_ = c.Edit(fmt.Sprintf("🚫 Игра #%d отменена.", gameID))
	return c.Respond()
}

// --- вспомогательные методы ---

// getActiveGameForDealer возвращает активную игру, если вызывает дилер.
// Если условие не выполнено — сам отвечает пользователю и возвращает (nil, nil).
func (h *Handler) getActiveGameForDealer(c telebot.Context, ctx context.Context) (*domain.Game, error) {
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
func (h *Handler) buildActiveParticipantsList(ctx context.Context, participants []domain.Participant) string {
	sb := strings.Builder{}
	sb.WriteString("❌ Сначала сделайте cash-out для:\n")
	for _, p := range participants {
		player, _ := h.player.GetByID(ctx, p.PlayerTgID)
		sb.WriteString("• ")
		sb.WriteString(formatPlayerByID(player, p.PlayerTgID))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// buildEndGameMessage формирует итоговое сообщение с планом переводов.
func (h *Handler) buildEndGameMessage(ctx context.Context, gameID int64, settlements []domain.Settlement) string {
	sb := strings.Builder{}
	fmt.Fprintf(&sb, "🏁 Игра #%d завершена!\n", gameID)

	if len(settlements) == 0 {
		sb.WriteString("\nВсе квиты — переводов нет.")
		return sb.String()
	}

	sb.WriteString("\n💸 Переводы:\n")
	for _, s := range settlements {
		from, _ := h.player.GetByID(ctx, s.FromTgID)
		to, _ := h.player.GetByID(ctx, s.ToTgID)
		fmt.Fprintf(&sb, "• %s → %s: %d₽\n",
			formatPlayerByID(from, s.FromTgID),
			formatPlayerByID(to, s.ToTgID),
			s.AmountRub)
	}
	return sb.String()
}

// --- форматирование ---

func playerName(u *telebot.User, fallbackID int64) string {
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
func pendingMarkup(paID int64) *telebot.ReplyMarkup {
	return &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{{
			{Text: "✅ Подтвердить", Data: fmt.Sprintf("pa:%d:yes", paID)},
			{Text: "❌ Отклонить", Data: fmt.Sprintf("pa:%d:no", paID)},
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
	default:
		return fmt.Sprintf("%s [%s].", action, pa.Status)
	}
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

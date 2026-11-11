package telegram

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/arrowwhi/infrastructure/observability/logger"
	tg "github.com/arrowwhi/infrastructure/telegram"

	"poker_bank/internal/domain"
	"poker_bank/internal/pkg/tgforum"
	"poker_bank/internal/service"
)

// Уведомления в чат игры. Их вызывают и обработчики бота, и Mini App (через
// интерфейс в delivery/http), поэтому сообщение в чате всегда отражает текущее
// состояние запроса, где бы его ни решили. Ошибки Telegram только логируются:
// данные уже записаны, а сообщение — вторично.

// threadFor возвращает топик форума, к которому привязан бот в чате (0 — без топика).
func (h *Handler) threadFor(ctx context.Context, chatID int64) int {
	b, err := h.chatTopic.Get(ctx, chatID)
	if err != nil || b == nil {
		return 0
	}
	return int(b.TopicID)
}

// sendToChat отправляет сообщение в чат игры (в привязанный топик).
func (h *Handler) sendToChat(ctx context.Context, chatID int64, text string, markup *Markup) (tg.Message, error) {
	req := tgforum.SendRequest{ChatID: chatID, Text: text, MessageThreadID: h.threadFor(ctx, chatID)}
	if markup != nil {
		req.ReplyMarkup = markup
	}
	return tgforum.Send(ctx, h.api, req)
}

func (h *Handler) editInChat(ctx context.Context, chatID int64, messageID int64, text string, markup *Markup) {
	err := h.api.EditMessageText(ctx, tg.EditMessageTextRequest{
		ChatID: chatID, MessageID: int(messageID), Text: text, ReplyMarkup: markup,
	})
	if err != nil && !tg.IsMessageNotModified(err) {
		logger.Warn(ctx).Err(err).Int64("chat_id", chatID).Int64("message_id", messageID).Msg("edit chat message")
	}
}

func (h *Handler) name(ctx context.Context, tgID int64) string {
	p, _ := h.player.GetByID(ctx, tgID)
	return formatPlayerByID(p, tgID)
}

// requestText — текст сообщения с запросом, ожидающим решения дилера.
func (h *Handler) requestText(ctx context.Context, g *domain.Game, pa *domain.PendingAction) string {
	who := h.name(ctx, pa.TargetTgID)
	rub, chips := service.RequestAmount(g, pa)
	switch pa.ActionType {
	case domain.ActionJoin:
		return fmt.Sprintf("🃏 %s просит JOIN (buy-in: %d₽ → %d фишек). Подтвердить?", who, rub, chips)
	case domain.ActionRebuy:
		return fmt.Sprintf("🃏 %s просит REBUY (%d₽ → %d фишек). Подтвердить?", who, rub, chips)
	default:
		return fmt.Sprintf("💸 %s хочет выйти: %d фишек = %d₽. Подтвердить?", who, chips, rub)
	}
}

// RequestCreated публикует запрос с кнопками и запоминает сообщение.
func (h *Handler) RequestCreated(ctx context.Context, g *domain.Game, pa *domain.PendingAction) {
	m, err := h.sendToChat(ctx, g.ChatID, h.requestText(ctx, g, pa), pendingMarkup(pa.ID))
	if err != nil {
		logger.Warn(ctx).Err(err).Int64("pending_id", pa.ID).Msg("send pending request")
		return
	}
	if err := h.game.SetPendingMessageID(ctx, pa.ID, int64(m.MessageID)); err != nil {
		logger.Warn(ctx).Err(err).Int64("pending_id", pa.ID).Msg("save pending message id")
	}
}

// RequestResolved убирает кнопки и пишет решение в сообщение запроса.
func (h *Handler) RequestResolved(ctx context.Context, pa *domain.PendingAction) {
	h.RequestsClosed(ctx, []domain.PendingAction{*pa})
}

// RequestsClosed правит сообщения закрытых запросов (подтверждены, отклонены, отменены, истекли).
func (h *Handler) RequestsClosed(ctx context.Context, actions []domain.PendingAction) {
	for i := range actions {
		pa := &actions[i]
		if pa.MessageID == nil {
			continue
		}
		h.editInChat(ctx, pa.ChatID, *pa.MessageID, h.resolvedText(ctx, pa), nil)
	}
}

func (h *Handler) resolvedText(ctx context.Context, pa *domain.PendingAction) string {
	text := pendingResolvedText(pa) + " — " + h.name(ctx, pa.TargetTgID)
	if pa.ResolvedByTgID != nil && *pa.ResolvedByTgID != pa.TargetTgID &&
		(pa.Status == domain.PendingStatusConfirmed || pa.Status == domain.PendingStatusRejected) {
		text += " (" + h.name(ctx, *pa.ResolvedByTgID) + ")"
	}
	if pa.Status == domain.PendingStatusCancelled && pa.ResolvedByTgID != nil && *pa.ResolvedByTgID == pa.RequesterTgID {
		text = fmt.Sprintf("↩️ Запрос отозван — %s", h.name(ctx, pa.TargetTgID))
	}
	return text
}

// Announce пишет в чат игры короткое сообщение о действии из Mini App.
func (h *Handler) Announce(ctx context.Context, chatID int64, text string) {
	if _, err := h.sendToChat(ctx, chatID, text, nil); err != nil {
		logger.Warn(ctx).Err(err).Int64("chat_id", chatID).Msg("announce")
	}
}

// GameFinished публикует итоги с кнопкой «Я оплатил / получил» и запоминает сообщение.
func (h *Handler) GameFinished(ctx context.Context, g *domain.Game, res *service.EndResult) {
	h.RequestsClosed(ctx, res.CancelledPending)
	if res.Cancelled {
		h.Announce(ctx, g.ChatID, emptyGameCancelledText(g.ID))
		return
	}
	m, err := h.sendToChat(ctx, g.ChatID, h.buildEndGameMessage(ctx, g.ID, res.BankRub, res.Settlements), h.settleMarkup(g.ID, res.Settlements))
	if err != nil {
		logger.Warn(ctx).Err(err).Int64("game_id", g.ID).Msg("send end game message")
		return
	}
	h.rememberSettleMessage(ctx, g.ID, m.MessageID)
}

func (h *Handler) rememberSettleMessage(ctx context.Context, gameID int64, messageID int) {
	if err := h.game.SetSettleMessageID(ctx, gameID, int64(messageID)); err != nil {
		logger.Warn(ctx).Err(err).Int64("game_id", gameID).Msg("save settle message id")
	}
}

// SettlementsChanged перерисовывает сообщение с планом переводов (отметки 🟢).
func (h *Handler) SettlementsChanged(ctx context.Context, g *domain.Game) {
	msgID, err := h.game.SettleMessageID(ctx, g.ID)
	if err != nil || msgID == nil {
		return
	}
	settlements, err := h.game.ListSettlements(ctx, g.ID)
	if err != nil {
		logger.Warn(ctx).Err(err).Int64("game_id", g.ID).Msg("list settlements")
		return
	}
	h.editInChat(ctx, g.ChatID, *msgID, h.buildEndGameMessage(ctx, g.ID, g.BankDeltaRub, settlements), h.settleMarkup(g.ID, settlements))
}

// RequestApplied сообщает о действии, записанном без подтверждения (дилер сам себе).
func (h *Handler) RequestApplied(ctx context.Context, g *domain.Game, pa *domain.PendingAction) {
	h.Announce(ctx, g.ChatID, h.appliedText(ctx, g, pa))
}

// GameCreated публикует в группе сводку игры, начатой из Mini App.
func (h *Handler) GameCreated(ctx context.Context, g *domain.Game) {
	if _, err := h.sendToChat(ctx, g.ChatID, h.gameStartedText(g, h.name(ctx, g.DealerTgID)), h.gameStartedMarkup(g)); err != nil {
		logger.Warn(ctx).Err(err).Int64("chat_id", g.ChatID).Msg("announce new game")
	}
}

// gameStartedText — сводка новой игры: одна и та же для /newgame и старта из Mini App.
func (h *Handler) gameStartedText(g *domain.Game, dealer string) string {
	entry := "Вход — /join"
	if h.webAppLink("") != "" {
		entry += " или в приложении 👇"
	}
	return fmt.Sprintf(
		"🎰 Игра #%d началась!\n\n🎩 Дилер: %s\n💵 Buy-in: %d₽ → %d фишек\n🔁 Rebuy: %d₽ → %d фишек\n⚖️ Курс: %s₽ за фишку\n\n%s",
		g.ID, dealer, g.BuyInRub, g.BuyInChips, g.RebuyRub, g.RebuyChips,
		formatRate(g.BuyInRub, g.BuyInChips), entry,
	)
}

// gameStartedMarkup — кнопка, открывающая стол этой игры в приложении.
func (h *Handler) gameStartedMarkup(g *domain.Game) *Markup {
	return h.withAppButton(nil, btnOpenTable, chatStartParam(g.ChatID))
}

// PlayerName — @username или имя игрока.
func (h *Handler) PlayerName(ctx context.Context, tgID int64) string { return h.name(ctx, tgID) }

// memberCacheTTL — сколько помнить ответ getChatMember.
const memberCacheTTL = 5 * time.Minute

type memberCacheEntry struct {
	at time.Time
	ok bool
}

// IsChatMember проверяет через Telegram, состоит ли пользователь в группе (с кешем).
//
// Ошибка означает «проверить не удалось» (нет клиента Bot API, бот не в группе, сеть),
// а не «не участник».
func (h *Handler) IsChatMember(ctx context.Context, chatID, userID int64) (bool, error) {
	if h.api == nil {
		return false, errNoBotAPI
	}
	key := [2]int64{chatID, userID}
	h.memberMu.Lock()
	if e, ok := h.memberCache[key]; ok && time.Since(e.at) < memberCacheTTL {
		h.memberMu.Unlock()
		return e.ok, nil
	}
	h.memberMu.Unlock()

	m, err := tgforum.GetChatMember(ctx, h.api, chatID, userID)
	if err != nil {
		// Бота нет в группе или группа не существует: проверить членство нельзя.
		// Вызывающий решает по тому, что знает сам (см. delivery/http canSeeChat).
		return false, fmt.Errorf("getChatMember %d/%d: %w", chatID, userID, err)
	}

	h.memberMu.Lock()
	h.memberCache[key] = memberCacheEntry{at: time.Now(), ok: m.InChat()}
	h.memberMu.Unlock()
	return m.InChat(), nil
}

// errNoBotAPI — бот ещё не зарегистрирован (нет клиента Bot API), членство не проверить.
var errNoBotAPI = errors.New("telegram bot api is not ready")

// forgetMember убирает участника из известных членов группы и из кэша getChatMember:
// он вышел или его удалили, и Mini App больше не должно пускать его в группу.
func (h *Handler) forgetMember(chatID, userID int64) {
	h.memberMu.Lock()
	delete(h.memberCache, [2]int64{chatID, userID})
	h.memberMu.Unlock()

	if err := h.game.ForgetMember(context.Background(), chatID, userID); err != nil {
		logger.Warn(context.Background()).Err(err).Int64("chat_id", chatID).Int64("tg_id", userID).Msg("forget chat member")
	}
}

package telegram

import (
	"context"
	"fmt"
	"time"

	tg "github.com/arrowwhi/infrastructure/telegram"
	"go.uber.org/zap"

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
		h.log.Warn("edit chat message", zap.Error(err), zap.Int64("chat_id", chatID), zap.Int64("message_id", messageID))
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
		h.log.Warn("send pending request", zap.Error(err), zap.Int64("pending_id", pa.ID))
		return
	}
	if err := h.game.SetPendingMessageID(ctx, pa.ID, int64(m.MessageID)); err != nil {
		h.log.Warn("save pending message id", zap.Error(err), zap.Int64("pending_id", pa.ID))
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
		h.log.Warn("announce", zap.Error(err), zap.Int64("chat_id", chatID))
	}
}

// GameFinished публикует итоги с кнопкой «Я оплатил / получил» и запоминает сообщение.
func (h *Handler) GameFinished(ctx context.Context, g *domain.Game, res *service.EndResult) {
	h.RequestsClosed(ctx, res.CancelledPending)
	if res.Cancelled {
		h.Announce(ctx, g.ChatID, fmt.Sprintf("🚫 Игра #%d отменена: никто не сделал buy-in.", g.ID))
		return
	}
	m, err := h.sendToChat(ctx, g.ChatID, h.buildEndGameMessage(ctx, g.ID, res.BankRub, res.Settlements), h.settleMarkup(g.ID, res.Settlements))
	if err != nil {
		h.log.Warn("send end game message", zap.Error(err), zap.Int64("game_id", g.ID))
		return
	}
	h.rememberSettleMessage(ctx, g.ID, m.MessageID)
}

func (h *Handler) rememberSettleMessage(ctx context.Context, gameID int64, messageID int) {
	if err := h.game.SetSettleMessageID(ctx, gameID, int64(messageID)); err != nil {
		h.log.Warn("save settle message id", zap.Error(err), zap.Int64("game_id", gameID))
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
		h.log.Warn("list settlements", zap.Error(err), zap.Int64("game_id", g.ID))
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
		h.log.Warn("announce new game", zap.Error(err), zap.Int64("chat_id", g.ChatID))
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
func (h *Handler) IsChatMember(ctx context.Context, chatID, userID int64) (bool, error) {
	if h.api == nil {
		return false, nil
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
		// Бота нет в группе или группа не существует — доступа нет.
		if tg.IsCode(err, 400) || tg.IsCode(err, 403) {
			return false, nil
		}
		return false, err
	}

	h.memberMu.Lock()
	h.memberCache[key] = memberCacheEntry{at: time.Now(), ok: m.InChat()}
	h.memberMu.Unlock()
	return m.InChat(), nil
}

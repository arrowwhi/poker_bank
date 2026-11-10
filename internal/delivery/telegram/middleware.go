package telegram

import (
	"context"

	"go.uber.org/zap"

	tg "github.com/arrowwhi/infrastructure/telegram"

	"poker_bank/internal/domain"
)

// UpsertPlayer upserts the sender on every incoming update.
func (h *Handler) UpsertPlayer(next tg.HandlerFunc) tg.HandlerFunc {
	return func(c *tg.Context) error {
		if u := c.User(); u != nil && !u.IsBot {
			p := &domain.Player{
				TelegramUserID: u.ID,
				Username:       u.Username,
				DisplayName:    u.FirstName + " " + u.LastName,
			}
			if err := h.player.Upsert(context.Background(), p); err != nil {
				h.log.Warn("upsert player", zap.Error(err), zap.Int64("tg_id", u.ID))
			}
			// Группа и её участник: из них Mini App строит список столов и picker игроков.
			if chat := c.Update.Chat(); chat != nil && !chat.IsPrivate() {
				err := h.game.RememberChat(context.Background(),
					&domain.Chat{ID: chat.ID, Title: chat.Title, Type: chat.Type}, u.ID)
				if err != nil {
					h.log.Warn("remember chat", zap.Error(err), zap.Int64("chat_id", chat.ID))
				}
			}
		}
		return next(c)
	}
}

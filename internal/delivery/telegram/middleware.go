package telegram

import (
	"context"

	"github.com/arrowwhi/infrastructure/observability/logger"

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
				logger.Warn(c).Err(err).Int64("tg_id", u.ID).Msg("upsert player")
			}
			// Группа и её участник: из них Mini App строит список столов и picker игроков.
			if chat := c.Update.Chat(); chat != nil && !chat.IsPrivate() {
				err := h.game.RememberChat(context.Background(),
					&domain.Chat{ID: chat.ID, Title: chat.Title, Type: chat.Type}, u.ID)
				if err != nil {
					logger.Warn(c).Err(err).Int64("chat_id", chat.ID).Msg("remember chat")
				}
				// Участник вышел или его удалили: доступ к группе в Mini App закрывается сразу.
				if left := h.forum.Info(c).LeftUserID; left != 0 {
					h.forgetMember(chat.ID, left)
				}
			}
		}
		return next(c)
	}
}

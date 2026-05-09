package telegram

import (
	"context"

	"go.uber.org/zap"
	"gopkg.in/telebot.v3"

	"poker_bank/internal/domain"
)

// UpsertPlayer upserts the sender on every incoming message.
func (h *Handler) UpsertPlayer(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		u := c.Sender()
		if u != nil {
			p := &domain.Player{
				TelegramUserID: u.ID,
				Username:       u.Username,
				DisplayName:    u.FirstName + " " + u.LastName,
			}
			if err := h.player.Upsert(context.Background(), p); err != nil {
				h.log.Warn("upsert player", zap.Error(err), zap.Int64("tg_id", u.ID))
			}
		}
		return next(c)
	}
}

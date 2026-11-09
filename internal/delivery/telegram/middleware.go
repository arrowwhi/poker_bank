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
		}
		return next(c)
	}
}

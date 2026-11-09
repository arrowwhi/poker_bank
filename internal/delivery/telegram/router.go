package telegram

import (
	tg "github.com/arrowwhi/infrastructure/telegram"
)

// callbackPrefixes — префиксы data inline-кнопок, которые разбирает handleCallback.
var callbackPrefixes = []string{"cancel", "endgame", "pa", "djoin", "drebuy", "dcashout", "settle"}

// Register attaches all bot middleware and command handlers to the given bot instance.
func (h *Handler) Register(b *tg.Bot) {
	h.api = b.API()

	b.Use(h.forum.Middleware(), h.UpsertPlayer, h.TopicGuard)

	commands := map[string]func(*Context) error{
		"start": h.handleStart,
		"help":  h.handleHelp,

		// Topic binding (chat admins only)
		"set_topic":   h.handleSetTopic,
		"unset_topic": h.handleUnsetTopic,

		// Game lifecycle
		"newgame":         h.handleNewGame,
		"endgame":         h.handleEndGame,
		"endgame_force":   h.handleEndGameForce,
		"cancel":          h.handleCancel,
		"admin_cancel":    h.handleAdminCancel,
		"transfer_dealer": h.handleTransferDealer,

		// Player commands (require dealer confirmation)
		"join":  h.handleJoin,
		"rebuy": h.handleRebuy,

		// Dealer commands (with inline confirmation)
		"dealer_join":    h.handleDealerJoin,
		"dealer_rebuy":   h.handleDealerRebuy,
		"dealer_cashout": h.handleDealerCashOut,
		"cashout":        h.handleCashOut,
		"undo":           h.handleUndo,

		// Read-only
		"status":  h.handleStatus,
		"me":      h.handleMe,
		"history": h.handleHistory,
		"game":    h.handleGame,
		"stats":   h.handleStats,
	}
	for name, fn := range commands {
		// Без описания: меню команд бот не публикует, как и раньше.
		b.Command(name, "", h.wrap(fn))
	}

	for _, prefix := range callbackPrefixes {
		b.Callback(prefix, h.wrap(h.handleCallback))
	}

	// FSM: пошаговый ввод (например, /newgame без аргументов)
	b.Text(h.wrap(h.handleText))

	// Остальные обновления тоже проходят middleware (учёт игроков в players).
	b.Default(func(*tg.Context) error { return nil })
}

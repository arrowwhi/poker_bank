package telegram

import "gopkg.in/telebot.v3"

func (h *Handler) Register(b *telebot.Bot) {
	b.Use(h.UpsertPlayer)

	b.Handle("/start", h.handleStart)
	b.Handle("/help", h.handleHelp)

	// Game lifecycle
	b.Handle("/newgame", h.handleNewGame)
	b.Handle("/endgame", h.handleEndGame)
	b.Handle("/endgame_force", h.handleEndGameForce)
	b.Handle("/cancel", h.handleCancel)
	b.Handle("/admin_cancel", h.handleAdminCancel)
	b.Handle("/transfer_dealer", h.handleTransferDealer)

	// Player commands (require dealer confirmation)
	b.Handle("/join", h.handleJoin)
	b.Handle("/rebuy", h.handleRebuy)

	// Dealer commands (with inline confirmation)
	b.Handle("/dealer_join", h.handleDealerJoin)
	b.Handle("/dealer_rebuy", h.handleDealerRebuy)
	b.Handle("/dealer_cashout", h.handleDealerCashOut)
	b.Handle("/cashout", h.handleCashOut)
	b.Handle("/undo", h.handleUndo)

	// Read-only
	b.Handle("/status", h.handleStatus)
	b.Handle("/me", h.handleMe)
	b.Handle("/history", h.handleHistory)
	b.Handle("/game", h.handleGame)
	b.Handle("/stats", h.handleStats)

	// FSM: пошаговый ввод (например, /newgame без аргументов)
	b.Handle(telebot.OnText, h.handleText)

	// Inline button callbacks
	b.Handle(telebot.OnCallback, h.handleCallback)
}

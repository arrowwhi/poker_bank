package telegram

import (
	"go.uber.org/zap"
	"gopkg.in/telebot.v3"

	"poker_bank/internal/interfaces"
	"poker_bank/internal/service"
)

// Handler holds the Telegram bot and all service dependencies used by command handlers.
type Handler struct {
	bot     *telebot.Bot
	game    *service.GameService
	player  *service.PlayerService
	pending *service.PendingService
	fsm     interfaces.FSMStateRepository
	log     *zap.Logger
}

// NewHandler creates a Handler wired up with the provided bot and services.
func NewHandler(
	bot *telebot.Bot,
	game *service.GameService,
	player *service.PlayerService,
	pending *service.PendingService,
	fsm interfaces.FSMStateRepository,
	log *zap.Logger,
) *Handler {
	return &Handler{
		bot:     bot,
		game:    game,
		player:  player,
		pending: pending,
		fsm:     fsm,
		log:     log,
	}
}

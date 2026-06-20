package telegram

import (
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/telebot.v3"

	"poker_bank/internal/interfaces"
	"poker_bank/internal/service"
)

// Handler holds the Telegram bot and all service dependencies used by command handlers.
type Handler struct {
	bot       *telebot.Bot
	game      *service.GameService
	player    *service.PlayerService
	pending   *service.PendingService
	fsm       interfaces.FSMStateRepository
	chatTopic interfaces.ChatTopicRepository
	log       *zap.Logger

	// lastHintAt throttles the "wrong topic" hint to at most once per hour per chat.
	hintMu     sync.Mutex
	lastHintAt map[int64]time.Time

	// botCommands is a set of all commands handled by this bot
	botCommands map[string]bool
}

// NewHandler creates a Handler wired up with the provided bot and services.
func NewHandler(
	bot *telebot.Bot,
	game *service.GameService,
	player *service.PlayerService,
	pending *service.PendingService,
	fsm interfaces.FSMStateRepository,
	chatTopic interfaces.ChatTopicRepository,
	log *zap.Logger,
) *Handler {
	return &Handler{
		bot:        bot,
		game:       game,
		player:     player,
		pending:    pending,
		fsm:        fsm,
		chatTopic:  chatTopic,
		log:        log,
		lastHintAt: make(map[int64]time.Time),
		botCommands: map[string]bool{
			"/start":           true,
			"/help":            true,
			"/set_topic":       true,
			"/unset_topic":     true,
			"/newgame":         true,
			"/endgame":         true,
			"/endgame_force":   true,
			"/cancel":          true,
			"/admin_cancel":    true,
			"/transfer_dealer": true,
			"/join":            true,
			"/rebuy":           true,
			"/dealer_join":     true,
			"/dealer_rebuy":    true,
			"/dealer_cashout":  true,
			"/cashout":         true,
			"/undo":            true,
			"/status":          true,
			"/me":              true,
			"/history":         true,
			"/game":            true,
			"/stats":           true,
		},
	}
}

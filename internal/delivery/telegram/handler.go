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
	}
}

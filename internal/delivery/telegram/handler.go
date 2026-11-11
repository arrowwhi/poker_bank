package telegram

import (
	"sync"
	"time"

	tg "github.com/arrowwhi/infrastructure/telegram"

	"poker_bank/internal/interfaces"
	"poker_bank/internal/pkg/tgforum"
	"poker_bank/internal/service"
)

// Handler holds the Telegram bot and all service dependencies used by command handlers.
type Handler struct {
	api       *tg.Client
	forum     *tgforum.Tracker
	game      *service.GameService
	player    *service.PlayerService
	pending   *service.PendingService
	fsm       interfaces.FSMStateRepository
	chatTopic interfaces.ChatTopicRepository

	// lastHintAt throttles the "wrong topic" hint to at most once per hour per chat.
	hintMu     sync.Mutex
	lastHintAt map[int64]time.Time

	// memberCache — ответы getChatMember для проверки доступа Mini App.
	memberMu    sync.Mutex
	memberCache map[[2]int64]memberCacheEntry

	// usedButtons — сообщения с кнопками djoin/drebuy/dcashout, по которым дилер уже
	// принял решение (dealer_buttons.go).
	buttonsMu   sync.Mutex
	usedButtons map[[2]int64]time.Time

	// webAppShortName и botUsername — из чего собирается ссылка на Mini App (webapp_link.go).
	webAppShortName string
	botUsername     func() string

	// botCommands is a set of all commands handled by this bot
	botCommands map[string]bool
}

// NewHandler creates a Handler wired up with the provided bot and services.
func NewHandler(
	forum *tgforum.Tracker,
	game *service.GameService,
	player *service.PlayerService,
	pending *service.PendingService,
	fsm interfaces.FSMStateRepository,
	chatTopic interfaces.ChatTopicRepository,
) *Handler {
	return &Handler{
		forum:       forum,
		game:        game,
		player:      player,
		pending:     pending,
		fsm:         fsm,
		chatTopic:   chatTopic,
		lastHintAt:  make(map[int64]time.Time),
		memberCache: make(map[[2]int64]memberCacheEntry),
		usedButtons: make(map[[2]int64]time.Time),
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

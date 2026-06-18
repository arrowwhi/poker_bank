package ep

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"gopkg.in/telebot.v3"

	"poker_bank/internal/config"
	"poker_bank/internal/delivery/telegram"
	"poker_bank/internal/repository/postgres"
	"poker_bank/internal/service"
)

// Run starts the bot application and blocks until an OS signal is received.
func Run() error {
	cfg, err := config.Load(".env")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log, err := buildLogger(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	defer log.Sync() //nolint:errcheck

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	// Repositories
	playerRepo := postgres.NewPlayerRepo(pool)
	gameRepo := postgres.NewGameRepo(pool)
	ledgerRepo := postgres.NewLedgerRepo(pool)
	participantRepo := postgres.NewParticipantRepo(pool)
	pendingRepo := postgres.NewPendingActionRepo(pool)
	resultRepo := postgres.NewGameResultRepo(pool)
	settlementRepo := postgres.NewSettlementRepo(pool)
	fsmRepo := postgres.NewFSMStateRepo(pool)
	chatTopicRepo := postgres.NewChatTopicRepo(pool)

	// Services
	playerSvc := service.NewPlayerService(playerRepo, log)
	gameSvc := service.NewGameService(
		gameRepo, ledgerRepo, participantRepo,
		resultRepo, settlementRepo, pendingRepo, log,
	)
	pendingSvc := service.NewPendingService(pendingRepo, log)

	// Telegram bot
	bot, err := telebot.NewBot(telebot.Settings{
		Token:  cfg.TelegramToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		return fmt.Errorf("create bot: %w", err)
	}

	h := telegram.NewHandler(bot, gameSvc, playerSvc, pendingSvc, fsmRepo, chatTopicRepo, log)
	h.Register(bot)

	// Background job: expire pending actions older than 30 minutes
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pendingSvc.ExpirePending(context.Background(), 30*time.Minute)
			}
		}
	}()

	log.Info("bot started")
	go bot.Start()

	<-ctx.Done()
	log.Info("shutting down")
	bot.Stop()

	return nil
}

func buildLogger(level string) (*zap.Logger, error) {
	switch level {
	case "debug":
		return zap.NewDevelopment()
	default:
		return zap.NewProduction()
	}
}

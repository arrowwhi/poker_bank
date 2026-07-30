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
	"poker_bank/internal/integrations/sheets"
	"poker_bank/internal/interfaces"
	"poker_bank/internal/repository/postgres"
	"poker_bank/internal/service"
)

// Run starts the bot application and blocks until an OS signal is received.
func Run(cfg *config.Config, log *zap.Logger) error {
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

	var exporter interfaces.SheetsExporter = service.NewNoopSheetsExporter()
	if cfg.SheetsEnabled() {
		sheetsClient, err := sheets.NewClient(ctx, cfg.GoogleSheetsCredentialsFile, cfg.GoogleSheetsSpreadsheetID)
		if err != nil {
			log.Error("инициализация Google Sheets клиента", zap.Error(err))
		} else {
			exporter = service.NewGoogleSheetsExporter(sheetsClient, gameRepo, ledgerRepo, settlementRepo, playerRepo, log)
		}
	}

	gameSvc := service.NewGameService(
		gameRepo, ledgerRepo, participantRepo,
		resultRepo, settlementRepo, pendingRepo, exporter, log,
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

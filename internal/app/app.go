// Package app собирает poker_bank на инфраструктуре github.com/arrowwhi/infrastructure:
// telegram.App отвечает за метрики, трейсинг, служебный порт и порядок остановки,
// здесь — только wiring: БД, сервисы, бот, HTTP-сервер и фоновые задачи.
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/arrowwhi/infrastructure/connections/postgres/migrate"
	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/observability/metrics"
	"github.com/arrowwhi/infrastructure/observability/tracing"
	tg "github.com/arrowwhi/infrastructure/telegram"

	"poker_bank/internal/config"
	httpdelivery "poker_bank/internal/delivery/http"
	"poker_bank/internal/delivery/telegram"
	"poker_bank/internal/integrations/sheets"
	"poker_bank/internal/interfaces"
	"poker_bank/internal/pkg/tgforum"
	"poker_bank/internal/repository/postgres"
	"poker_bank/internal/service"
	"poker_bank/migrations"
	"poker_bank/webapp"
)

const serviceName = "poker-bank"

// version прошивается линкером: -ldflags "-X poker_bank/internal/app.version=...".
var version = "dev"

// pendingTTL — через сколько неподтверждённый запрос переходит в expired (ТЗ §7).
const pendingTTL = 30 * time.Minute

// Run поднимает сервис и блокируется до SIGINT/SIGTERM.
func Run(ctx context.Context, cfg *config.Config) error {
	a, err := tg.NewApp(ctx, infraConfig(cfg))
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		_ = a.Shutdown(ctx)
		return err
	}
	a.OnShutdown(func(context.Context) error { pool.Close(); return nil })
	a.Health().Register("postgres", pool.Healthcheck)

	if err = migrate.Up(ctx, pool, migrations.FS, "."); err != nil {
		_ = a.Shutdown(ctx)
		return fmt.Errorf("run migrations: %w", err)
	}

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
	playerSvc := service.NewPlayerService(playerRepo)

	var exporter interfaces.SheetsExporter = service.NewNoopSheetsExporter()
	if cfg.SheetsEnabled() {
		sheetsClient, sErr := sheets.NewClient(ctx, cfg.GoogleSheetsCredentialsFile, cfg.GoogleSheetsSpreadsheetID)
		if sErr != nil {
			logger.Error(ctx).Err(sErr).Msg("инициализация Google Sheets клиента")
		} else {
			exporter = service.NewGoogleSheetsExporter(sheetsClient, gameRepo, ledgerRepo, settlementRepo, playerRepo)
		}
	}

	gameSvc := service.NewGameService(
		gameRepo, ledgerRepo, participantRepo,
		resultRepo, settlementRepo, pendingRepo,
		postgres.NewChatRepo(pool), postgres.NewTxManager(pool),
		exporter,
	)
	pendingSvc := service.NewPendingService(pendingRepo)

	// Telegram bot. Tracker достаёт топик форума из сырых обновлений (polling — через
	// транспорт клиента Bot API, webhook — через обёртку обработчика).
	forum := tgforum.NewTracker()

	botCfg := botConfig(cfg)

	client, err := tg.NewClient(botCfg.API, tg.WithHTTPClient(forum.HTTPClient(nil)))
	if err != nil {
		_ = a.Shutdown(ctx)
		return fmt.Errorf("create bot api client: %w", err)
	}

	bot, err := tg.New(botCfg,
		tg.WithAPIClient(client),
		tg.WithErrorHandler(func(c *tg.Context, err error) {
			logger.Error(c).Err(err).Int64("chat_id", c.ChatID()).Str("route", c.Route()).Msg("telegram handler")
		}),
	)
	if err != nil {
		_ = a.Shutdown(ctx)
		return fmt.Errorf("create bot: %w", err)
	}
	a.Health().Register("telegram", bot.Healthcheck)

	h := telegram.NewHandler(forum, gameSvc, playerSvc, pendingSvc, fsmRepo, chatTopicRepo)
	h.SetWebAppShortName(cfg.WebAppShortName)
	h.Register(bot)

	// HTTP: API Mini App, фронтенд и (в режиме webhook) обновления Telegram.
	assets, err := webapp.Assets()
	if err != nil {
		logger.Warn(ctx).Err(err).Msg("mini app assets are not embedded, serving api only")
		assets = nil
	}

	deps := httpdelivery.Deps{
		Games:           gameSvc,
		Players:         playerSvc,
		Notifier:        h,
		Health:          a.Health(),
		WebAssets:       assets,
		ServiceName:     serviceName,
		BotToken:        cfg.TelegramToken,
		WebhookPath:     cfg.TelegramWebhookPath,
		Port:            cfg.HTTPPort,
		ShutdownTimeout: cfg.ShutdownTimeout,
	}
	if cfg.IsWebhook() {
		deps.TelegramWebhook = forum.Webhook(bot.WebhookHandler())
	}

	srv, err := httpdelivery.New(deps)
	if err != nil {
		_ = a.Shutdown(ctx)
		return fmt.Errorf("create http server: %w", err)
	}

	logger.Info(ctx).Str("telegram_mode", cfg.TelegramMode).Str("http_port", cfg.HTTPPort).Msg("starting poker-bank")

	return a.Run(ctx, bot, srv, expirePendingJob(pendingSvc, h), publishMenuJob(client))
}

// expirePendingJob раз в минуту переводит старые pending-запросы в expired
// и убирает кнопки из их сообщений.
func expirePendingJob(pending *service.PendingService, h *telegram.Handler) tg.ComponentFunc {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				h.RequestsClosed(ctx, pending.ExpirePending(ctx, pendingTTL))
			}
		}
	}
}

// publishMenuJob публикует меню команд Telegram при старте. Меню — косметика:
// ошибка только логируется, компонент живёт до остановки сервиса.
func publishMenuJob(client *tg.Client) tg.ComponentFunc {
	return func(ctx context.Context) error {
		if err := client.SetMyCommands(ctx, telegram.MenuCommands()); err != nil && ctx.Err() == nil {
			logger.Warn(ctx).Err(err).Msg("set bot commands")
		}

		<-ctx.Done()

		return nil
	}
}

// infraConfig переводит конфиг poker_bank в конфиг инфраструктуры.
func infraConfig(cfg *config.Config) tg.AppConfig {
	ac := tg.AppConfig{
		ServiceName:     serviceName,
		Version:         version,
		Environment:     cfg.Env,
		ShutdownTimeout: cfg.ShutdownTimeout,
		Admin:           tg.AdminConfig{Host: "0.0.0.0", Port: cfg.AdminPort, Enabled: cfg.AdminPort != ""},
	}

	// Один логгер на весь сервис: инфраструктура и poker_bank пишут через observability/logger.
	ac.Logger.Level = cfg.LogLevel
	ac.Logger.Masking = true

	ac.Metrics = metrics.DefaultConfig()
	ac.Tracing = tracing.DefaultConfig() // OTEL выключен по умолчанию

	return ac
}

// botConfig собирает настройки бота инфраструктуры.
func botConfig(cfg *config.Config) tg.Config {
	bc := tg.DefaultConfig(cfg.TelegramToken)
	bc.API.BaseURL = cfg.TelegramAPIURL
	bc.Mode = cfg.TelegramMode
	// Меню публикует publishMenuJob — в своём порядке, а не по алфавиту, как библиотека.
	bc.SetCommands = false

	bc.Webhook.URL = cfg.PublicURL + cfg.TelegramWebhookPath
	bc.Webhook.Secret = cfg.TelegramWebhookSecret
	bc.Webhook.Path = cfg.TelegramWebhookPath
	// Встроенный webhook-сервер не нужен: обработчик смонтирован на публичный HTTP-сервер.
	bc.Webhook.Port = ""

	return bc
}

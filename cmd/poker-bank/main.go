package main

import (
	"context"

	"github.com/arrowwhi/infrastructure/observability/logger"

	"poker_bank/internal/app"
	"poker_bank/internal/config"
)

func main() {
	ctx := context.Background()

	// Логгер инфраструктуры настраивает app.Run (telegram.NewApp → logger.Init по LOG_LEVEL).
	// До этого ошибки конфигурации пишет его временный логгер.
	cfg, err := config.Load(".env")
	if err != nil {
		logger.Fatal(ctx).Err(err).Msg("load config")
	}

	if err := app.Run(ctx, cfg); err != nil {
		logger.Fatal(ctx).Err(err).Msg("run app")
	}
}

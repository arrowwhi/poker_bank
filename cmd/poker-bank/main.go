package main

import (
	"context"
	"log"

	"go.uber.org/zap"

	"poker_bank/internal/app"
	"poker_bank/internal/config"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatal(err)
	}

	logger, err := buildLogger(cfg.LogLevel)
	if err != nil {
		log.Fatal(err)
	}
	defer logger.Sync() //nolint:errcheck

	if err := app.Run(context.Background(), cfg, logger); err != nil {
		logger.Fatal("run app", zap.Error(err))
	}
}

func buildLogger(level string) (*zap.Logger, error) {
	switch level {
	case "debug":
		return zap.NewDevelopment()
	default:
		return zap.NewProduction()
	}
}

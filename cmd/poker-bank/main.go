package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"go.uber.org/zap"

	"poker_bank/internal/config"
	"poker_bank/internal/ep"
	"poker_bank/migrations"
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

	if err := runMigrations(cfg.DatabaseURL); err != nil {
		logger.Fatal("run migrations", zap.Error(err))
	}

	if err := ep.Run(cfg, logger); err != nil {
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

// runMigrations applies pending goose migrations embedded in the migrations package.
func runMigrations(databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}

	return nil
}

package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	TelegramToken               string
	DatabaseURL                 string
	LogLevel                    string
	GoogleSheetsSpreadsheetID   string
	GoogleSheetsCredentialsFile string
}

// SheetsEnabled reports whether Google Sheets export is configured.
func (c *Config) SheetsEnabled() bool {
	return c.GoogleSheetsSpreadsheetID != "" && c.GoogleSheetsCredentialsFile != ""
}

// Load reads config from envFile (if non-empty) and then from environment variables.
func Load(envFile string) (*Config, error) {
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil {
			return nil, fmt.Errorf("load env file: %w", err)
		}
	}

	cfg := &Config{
		TelegramToken:               os.Getenv("TELEGRAM_TOKEN"),
		DatabaseURL:                 os.Getenv("DATABASE_URL"),
		LogLevel:                    os.Getenv("LOG_LEVEL"),
		GoogleSheetsSpreadsheetID:   os.Getenv("GOOGLE_SHEETS_SPREADSHEET_ID"),
		GoogleSheetsCredentialsFile: os.Getenv("GOOGLE_SHEETS_CREDENTIALS_FILE"),
	}

	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("TELEGRAM_TOKEN is required")
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}

	return cfg, nil
}

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

// Load reads config from environment variables, falling back to envFile for
// any variable not already set in the environment. envFile is optional: if it
// doesn't exist or fails to parse, that error is ignored and only real
// environment variables are used.
func Load(envFile string) (*Config, error) {
	var fileVars map[string]string
	if envFile != "" {
		fileVars, _ = godotenv.Read(envFile)
	}

	getenv := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return fileVars[key]
	}

	cfg := &Config{
		TelegramToken:               getenv("TELEGRAM_TOKEN"),
		DatabaseURL:                 getenv("DATABASE_URL"),
		LogLevel:                    getenv("LOG_LEVEL"),
		GoogleSheetsSpreadsheetID:   getenv("GOOGLE_SHEETS_SPREADSHEET_ID"),
		GoogleSheetsCredentialsFile: getenv("GOOGLE_SHEETS_CREDENTIALS_FILE"),
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

// Package config читает настройки poker_bank из окружения (и .env). Имена переменных
// прежние, новые добавлены для HTTP-сервера и webhook; в app они переводятся в
// конфиги инфраструктуры (github.com/arrowwhi/infrastructure).
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Режимы получения обновлений Telegram.
const (
	ModePolling = "polling"
	ModeWebhook = "webhook"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	TelegramToken               string
	DatabaseURL                 string
	LogLevel                    string
	GoogleSheetsSpreadsheetID   string
	GoogleSheetsCredentialsFile string

	// Env — окружение в логах и метриках (local, prod).
	Env string

	// TelegramAPIURL — адрес Bot API: свой telegram-bot-api сервер или заглушка в тестах.
	TelegramAPIURL string

	// TelegramMode — polling (локально) или webhook (прод, через HTTP-сервер).
	TelegramMode string
	// TelegramWebhookSecret — секрет заголовка X-Telegram-Bot-Api-Secret-Token.
	TelegramWebhookSecret string
	// TelegramWebhookPath — путь webhook на публичном HTTP-сервере.
	TelegramWebhookPath string

	// PublicURL — внешний HTTPS-адрес сервиса (Mini App и webhook), без завершающего "/".
	PublicURL string
	// HTTPPort — порт публичного HTTP-сервера (API, Mini App, webhook).
	HTTPPort string
	// AdminPort — служебный порт /metrics, /healthz, /ready; наружу не публикуется.
	AdminPort string
	// WebAppShortName — short name Mini App из BotFather (/newapp): бот ставит под сообщениями
	// кнопки-ссылки t.me/<bot>/<app>.
	WebAppShortName string

	ShutdownTimeout time.Duration
}

// SheetsEnabled reports whether Google Sheets export is configured.
func (c *Config) SheetsEnabled() bool {
	return c.GoogleSheetsSpreadsheetID != "" && c.GoogleSheetsCredentialsFile != ""
}

// IsWebhook сообщает, что обновления приходят на HTTP-сервер.
func (c *Config) IsWebhook() bool { return c.TelegramMode == ModeWebhook }

// Load reads config from environment variables, falling back to envFile for
// any variable not already set in the environment. envFile is optional: if it
// doesn't exist or fails to parse, that error is ignored and only real
// environment variables are used.
func Load(envFile string) (*Config, error) {
	var fileVars map[string]string
	if envFile != "" {
		fileVars, _ = godotenv.Read(envFile)
	}

	getenv := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v := fileVars[key]; v != "" {
			return v
		}
		return def
	}

	cfg := &Config{
		TelegramToken:               getenv("TELEGRAM_TOKEN", ""),
		DatabaseURL:                 getenv("DATABASE_URL", ""),
		LogLevel:                    getenv("LOG_LEVEL", "info"),
		GoogleSheetsSpreadsheetID:   getenv("GOOGLE_SHEETS_SPREADSHEET_ID", ""),
		GoogleSheetsCredentialsFile: getenv("GOOGLE_SHEETS_CREDENTIALS_FILE", ""),

		Env:                   getenv("APP_ENV", "local"),
		TelegramAPIURL:        getenv("TELEGRAM_API_URL", "https://api.telegram.org"),
		TelegramMode:          getenv("TELEGRAM_MODE", ModePolling),
		TelegramWebhookSecret: getenv("TELEGRAM_WEBHOOK_SECRET", ""),
		TelegramWebhookPath:   getenv("TELEGRAM_WEBHOOK_PATH", "/telegram/webhook"),
		PublicURL:             strings.TrimSuffix(getenv("PUBLIC_URL", ""), "/"),
		HTTPPort:              getenv("HTTP_PORT", "8080"),
		AdminPort:             getenv("ADMIN_PORT", "9090"),
		WebAppShortName:       getenv("WEBAPP_SHORT_NAME", "poker_bank"),
	}

	timeout, err := time.ParseDuration(getenv("SHUTDOWN_TIMEOUT", "15s"))
	if err != nil {
		return nil, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err)
	}
	cfg.ShutdownTimeout = timeout

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	var problems []string

	if c.TelegramToken == "" {
		problems = append(problems, "TELEGRAM_TOKEN is required")
	}
	if c.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is required")
	}

	switch c.TelegramMode {
	case ModePolling:
	case ModeWebhook:
		if c.TelegramWebhookSecret == "" {
			problems = append(problems, "TELEGRAM_WEBHOOK_SECRET is required when TELEGRAM_MODE=webhook")
		}
		if c.PublicURL == "" {
			problems = append(problems, "PUBLIC_URL is required when TELEGRAM_MODE=webhook")
		}
	default:
		problems = append(problems, "TELEGRAM_MODE must be polling or webhook")
	}

	if len(problems) > 0 {
		return errors.New("invalid configuration:\n  - " + strings.Join(problems, "\n  - "))
	}

	return nil
}

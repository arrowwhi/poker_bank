package domain

import "time"

// Player represents a Telegram user who participates in games.
type Player struct {
	TelegramUserID int64
	Username       string
	DisplayName    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

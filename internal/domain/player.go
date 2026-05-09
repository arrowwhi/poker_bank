package domain

import "time"

type Player struct {
	TelegramUserID int64
	Username       string
	DisplayName    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

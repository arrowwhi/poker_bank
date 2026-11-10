package domain

import "time"

// Chat is a Telegram group where the bot has seen activity.
type Chat struct {
	ID        int64
	Title     string
	Type      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlayerGameResult is a player's result together with the game it belongs to
// (for personal history and statistics).
type PlayerGameResult struct {
	GameResult
	ChatID  int64
	EndedAt time.Time
}

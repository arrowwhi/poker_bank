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

// ChatActivity is a chat together with how much it has been played in:
// the source of the Mini App chat list, ordered from the most popular chat down.
type ChatActivity struct {
	Chat
	GamesCount int        // finished games in the chat
	LastGameAt *time.Time // end of the latest finished game; nil if none yet
}

// PlayerGameResult is a player's result together with the game it belongs to
// (for personal history and statistics).
type PlayerGameResult struct {
	GameResult
	ChatID  int64
	EndedAt time.Time
}

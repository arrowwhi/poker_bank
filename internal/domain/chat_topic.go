package domain

import "time"

// ChatTopic binds a chat to a single forum topic the bot is allowed to work in.
type ChatTopic struct {
	ChatID    int64
	TopicID   int64
	SetByTgID int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

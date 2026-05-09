package domain

import "time"

// Participant links a player to a game and tracks whether they are still active.
type Participant struct {
	GameID     int64
	PlayerTgID int64
	IsActive   bool
	JoinedAt   time.Time
}

package domain

import "time"

type Participant struct {
	GameID     int64
	PlayerTgID int64
	IsActive   bool
	JoinedAt   time.Time
}

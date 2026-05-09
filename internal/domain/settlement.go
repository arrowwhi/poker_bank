package domain

import "time"

// Settlement describes a debt payment owed from one player to another after a game.
type Settlement struct {
	ID        int64
	GameID    int64
	FromTgID  int64
	ToTgID    int64
	AmountRub int
	IsPaid    bool
	PaidAt    *time.Time
}

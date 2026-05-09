package domain

import "time"

type Settlement struct {
	ID        int64
	GameID    int64
	FromTgID  int64
	ToTgID    int64
	AmountRub int
	IsPaid    bool
	PaidAt    *time.Time
}

package domain

import "time"

type GameStatus string

const (
	GameStatusActive    GameStatus = "active"
	GameStatusFinished  GameStatus = "finished"
	GameStatusCancelled GameStatus = "cancelled"
)

type Game struct {
	ID           int64
	ChatID       int64
	DealerTgID   int64
	BuyInRub     int
	BuyInChips   int
	RebuyRub     int
	RebuyChips   int
	Status       GameStatus
	StartedAt    time.Time
	EndedAt      *time.Time
	BankDeltaRub int
}

// RateRub returns how many rubles one chip costs (buy-in rate).
func (g *Game) RateRub() float64 {
	if g.BuyInChips == 0 {
		return 0
	}
	return float64(g.BuyInRub) / float64(g.BuyInChips)
}

// ChipsToRub converts chips to rubles using the game rate.
// Returns (amount, ok) — ok is false if the conversion is not a whole number of rubles.
func (g *Game) ChipsToRub(chips int) (int, bool) {
	rub := chips * g.BuyInRub
	if rub%g.BuyInChips != 0 {
		return 0, false
	}
	return rub / g.BuyInChips, true
}

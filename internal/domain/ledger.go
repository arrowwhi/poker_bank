package domain

import "time"

// LedgerType identifies the kind of ledger transaction.
type LedgerType string

// LedgerBuyIn and related constants enumerate all ledger entry types.
const (
	LedgerBuyIn   LedgerType = "BUY_IN"
	LedgerRebuy   LedgerType = "REBUY"
	LedgerCashOut LedgerType = "CASH_OUT"
)

// LedgerEntry records a single financial transaction (buy-in, rebuy, or cash-out) in a game.
type LedgerEntry struct {
	ID            int64
	GameID        int64
	PlayerTgID    int64
	Type          LedgerType
	AmountRub     int
	AmountChips   int
	CreatedByTgID int64
	CreatedAt     time.Time
	IsVoid        bool
	VoidReason    *string
}

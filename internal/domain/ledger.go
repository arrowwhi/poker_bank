package domain

import "time"

type LedgerType string

const (
	LedgerBuyIn   LedgerType = "BUY_IN"
	LedgerRebuy   LedgerType = "REBUY"
	LedgerCashOut LedgerType = "CASH_OUT"
)

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

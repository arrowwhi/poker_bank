package domain

import "time"

type ActionType string

const (
	ActionJoin    ActionType = "JOIN"
	ActionRebuy   ActionType = "REBUY"
	ActionCashOut ActionType = "CASHOUT"
)

type PendingStatus string

const (
	PendingStatusPending   PendingStatus = "pending"
	PendingStatusConfirmed PendingStatus = "confirmed"
	PendingStatusRejected  PendingStatus = "rejected"
	PendingStatusExpired   PendingStatus = "expired"
	PendingStatusCancelled PendingStatus = "cancelled"
)

type PendingAction struct {
	ID             int64
	GameID         int64
	ActionType     ActionType
	RequesterTgID  int64
	TargetTgID     int64
	Payload        map[string]any
	Status         PendingStatus
	ChatID         int64
	MessageID      *int64
	CreatedAt      time.Time
	ResolvedAt     *time.Time
	ResolvedByTgID *int64
}

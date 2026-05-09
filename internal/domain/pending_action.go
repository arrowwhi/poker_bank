package domain

import "time"

// ActionType identifies the kind of pending player action.
type ActionType string

// ActionJoin and related constants enumerate all pending action types.
const (
	ActionJoin    ActionType = "JOIN"
	ActionRebuy   ActionType = "REBUY"
	ActionCashOut ActionType = "CASHOUT"
)

// PendingStatus represents the resolution state of a pending action.
type PendingStatus string

// PendingStatusPending and related constants enumerate all pending action statuses.
const (
	PendingStatusPending   PendingStatus = "pending"
	PendingStatusConfirmed PendingStatus = "confirmed"
	PendingStatusRejected  PendingStatus = "rejected"
	PendingStatusExpired   PendingStatus = "expired"
	PendingStatusCancelled PendingStatus = "cancelled"
)

// PendingAction represents a player action that requires dealer confirmation.
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

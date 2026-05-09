package domain

import "time"

// FSMState stores the finite-state-machine state for a user in a chat.
type FSMState struct {
	ChatID    int64
	UserTgID  int64
	State     string
	Data      map[string]any
	UpdatedAt time.Time
}

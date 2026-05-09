package domain

import "time"

type FSMState struct {
	ChatID    int64
	UserTgID  int64
	State     string
	Data      map[string]any
	UpdatedAt time.Time
}

package service

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by service methods.
var (
	ErrNoActiveGame       = errors.New("no active game in this chat")
	ErrGameAlreadyActive  = errors.New("game already active in this chat")
	ErrNotDealer          = errors.New("only the dealer can perform this action")
	ErrAlreadyInGame      = errors.New("player is already in the game")
	ErrNotInGame          = errors.New("player is not in the game")
	ErrPendingExists      = errors.New("pending request already exists")
	ErrInvalidChipsAmount = errors.New("chips amount does not convert to a whole number of rubles")
	ErrActiveParticipants = errors.New("there are still active participants; cash them out first")
	ErrBankNotZero        = errors.New("bank is not zero; use /endgame_force to override")

	ErrGameNotActive   = errors.New("game is not active")
	ErrNotFound        = errors.New("not found")
	ErrAlreadyResolved = errors.New("request is already resolved")
	ErrBadRate         = errors.New("rate must be positive and proportional")
	ErrNotRequester    = errors.New("only the requester can withdraw the request")
	ErrDealerNotIdle   = errors.New("dealer was active recently")
)

// ActiveParticipantsError — завершить игру нельзя: игроки ещё не сделали cash-out.
type ActiveParticipantsError struct {
	PlayerIDs []int64
}

func (e *ActiveParticipantsError) Error() string {
	return fmt.Sprintf("%d active participants must cash out first", len(e.PlayerIDs))
}

// Unwrap allows errors.Is(err, ErrActiveParticipants).
func (e *ActiveParticipantsError) Unwrap() error { return ErrActiveParticipants }

// BankMismatchError — банк не сошёлся; завершить можно только принудительно.
type BankMismatchError struct {
	BankRub int
}

func (e *BankMismatchError) Error() string { return fmt.Sprintf("bank mismatch: %+d", e.BankRub) }

// Unwrap allows errors.Is(err, ErrBankNotZero).
func (e *BankMismatchError) Unwrap() error { return ErrBankNotZero }

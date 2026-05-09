package service

import "errors"

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
)

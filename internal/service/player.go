package service

import (
	"context"

	"go.uber.org/zap"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

// PlayerService provides player lookup and registration operations.
type PlayerService struct {
	players interfaces.PlayerRepository
	log     *zap.Logger
}

// NewPlayerService creates a new PlayerService backed by the given repository.
func NewPlayerService(players interfaces.PlayerRepository, log *zap.Logger) *PlayerService {
	return &PlayerService{players: players, log: log}
}

// Upsert registers or updates a player from incoming Telegram message metadata.
func (s *PlayerService) Upsert(ctx context.Context, p *domain.Player) error {
	return s.players.Upsert(ctx, p)
}

// GetByID retrieves a player by their Telegram user ID.
func (s *PlayerService) GetByID(ctx context.Context, tgID int64) (*domain.Player, error) {
	return s.players.GetByID(ctx, tgID)
}

// GetByUsername retrieves a player by their Telegram username.
func (s *PlayerService) GetByUsername(ctx context.Context, username string) (*domain.Player, error) {
	return s.players.GetByUsername(ctx, username)
}

package service

import (
	"context"

	"go.uber.org/zap"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

type PlayerService struct {
	players interfaces.PlayerRepository
	log     *zap.Logger
}

func NewPlayerService(players interfaces.PlayerRepository, log *zap.Logger) *PlayerService {
	return &PlayerService{players: players, log: log}
}

// Upsert registers or updates a player from incoming Telegram message metadata.
func (s *PlayerService) Upsert(ctx context.Context, p *domain.Player) error {
	return s.players.Upsert(ctx, p)
}

func (s *PlayerService) GetByID(ctx context.Context, tgID int64) (*domain.Player, error) {
	return s.players.GetByID(ctx, tgID)
}

func (s *PlayerService) GetByUsername(ctx context.Context, username string) (*domain.Player, error) {
	return s.players.GetByUsername(ctx, username)
}

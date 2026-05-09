package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

type PendingService struct {
	pending interfaces.PendingActionRepository
	log     *zap.Logger
}

func NewPendingService(pending interfaces.PendingActionRepository, log *zap.Logger) *PendingService {
	return &PendingService{pending: pending, log: log}
}

func (s *PendingService) Create(ctx context.Context, a *domain.PendingAction) (int64, error) {
	return s.pending.Create(ctx, a)
}

func (s *PendingService) GetByID(ctx context.Context, id int64) (*domain.PendingAction, error) {
	return s.pending.GetByID(ctx, id)
}

func (s *PendingService) GetPending(ctx context.Context, gameID int64, targetTgID int64, actionType domain.ActionType) (*domain.PendingAction, error) {
	return s.pending.GetPending(ctx, gameID, targetTgID, actionType)
}

func (s *PendingService) ListByGame(ctx context.Context, gameID int64) ([]domain.PendingAction, error) {
	return s.pending.ListPendingByGame(ctx, gameID)
}

func (s *PendingService) Resolve(ctx context.Context, id int64, status domain.PendingStatus, resolvedByTgID int64) (*domain.PendingAction, error) {
	return s.pending.Resolve(ctx, id, status, resolvedByTgID)
}

// ExpirePending переводит в expired все pending-запросы старше ttl.
func (s *PendingService) ExpirePending(ctx context.Context, ttl time.Duration) {
	n, err := s.pending.ExpireOlderThan(ctx, ttl)
	if err != nil {
		s.log.Error("expire pending actions", zap.Error(err))
		return
	}
	if n > 0 {
		s.log.Info("expired pending actions", zap.Int64("count", n))
	}
}

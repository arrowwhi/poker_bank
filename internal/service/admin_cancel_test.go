package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"poker_bank/internal/domain"
)

// fakeLedger — LedgerRepository в памяти; нужен только ListByGame.
type fakeLedger struct{ entries []domain.LedgerEntry }

func (f *fakeLedger) Create(context.Context, *domain.LedgerEntry) (int64, error) { return 0, nil }
func (f *fakeLedger) ListByGame(context.Context, int64) ([]domain.LedgerEntry, error) {
	return f.entries, nil
}
func (f *fakeLedger) VoidLastN(context.Context, int64, int) ([]domain.LedgerEntry, error) {
	return nil, nil
}
func (f *fakeLedger) GetBank(context.Context, int64) (int, error) { return 0, nil }

func TestDealerLastActivity(t *testing.T) {
	start := time.Date(2026, 11, 10, 18, 0, 0, 0, time.UTC)
	g := &domain.Game{ID: 1, DealerTgID: 10, Status: domain.GameStatusActive, StartedAt: start}

	s := &GameService{ledger: &fakeLedger{entries: []domain.LedgerEntry{
		{PlayerTgID: 10, CreatedByTgID: 10, CreatedAt: start.Add(time.Hour)},
		// Запись, сделанная не дилером, не считается его активностью.
		{PlayerTgID: 20, CreatedByTgID: 20, CreatedAt: start.Add(3 * time.Hour)},
		{PlayerTgID: 20, CreatedByTgID: 10, CreatedAt: start.Add(2 * time.Hour)},
	}}}

	last, err := s.DealerLastActivity(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if want := start.Add(2 * time.Hour); !last.Equal(want) {
		t.Fatalf("last activity = %v, want %v", last, want)
	}

	// Без записей дилера — время начала игры.
	s.ledger = &fakeLedger{}
	last, _ = s.DealerLastActivity(context.Background(), g)
	if !last.Equal(start) {
		t.Fatalf("last activity without entries = %v, want %v", last, start)
	}
}

func TestAdminCancelGameTooEarly(t *testing.T) {
	start := time.Date(2026, 11, 10, 18, 0, 0, 0, time.UTC)
	g := &domain.Game{ID: 1, DealerTgID: 10, Status: domain.GameStatusActive, StartedAt: start}
	s := &GameService{ledger: &fakeLedger{entries: []domain.LedgerEntry{
		{PlayerTgID: 10, CreatedByTgID: 10, CreatedAt: start.Add(time.Hour)},
	}}}

	// 5ч59м после последнего действия дилера — ещё рано; до отмены дело не доходит.
	_, last, err := s.AdminCancelGame(context.Background(), g, start.Add(time.Hour+AdminIdleTimeout-time.Minute))
	if !errors.Is(err, ErrDealerNotIdle) {
		t.Fatalf("err = %v, want ErrDealerNotIdle", err)
	}
	if !last.Equal(start.Add(time.Hour)) {
		t.Fatalf("last = %v", last)
	}

	// Неактивную игру отменить нельзя.
	g.Status = domain.GameStatusFinished
	if _, _, err := s.AdminCancelGame(context.Background(), g, start.Add(24*time.Hour)); !errors.Is(err, ErrGameNotActive) {
		t.Fatalf("err = %v, want ErrGameNotActive", err)
	}
}

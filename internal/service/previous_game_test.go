package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

// fakeGames — GameRepository в памяти; реализован только GetLastByChatStatus,
// остальные методы не нужны и упадут на nil-интерфейсе, если их вдруг вызовут.
type fakeGames struct {
	interfaces.GameRepository
	games []domain.Game
	err   error
}

func (f *fakeGames) GetLastByChatStatus(_ context.Context, chatID int64, status domain.GameStatus) (*domain.Game, error) {
	if f.err != nil {
		return nil, f.err
	}
	var last *domain.Game
	for i := range f.games {
		g := &f.games[i]
		if g.ChatID != chatID || g.Status != status {
			continue
		}
		if last == nil || g.StartedAt.After(last.StartedAt) {
			last = g
		}
	}
	return last, nil
}

func TestPreviousGameInChat(t *testing.T) {
	const chat, otherChat = int64(-100), int64(-200)
	day := func(n int) time.Time { return time.Date(2026, 11, n, 20, 0, 0, 0, time.UTC) }
	game := func(id, chatID int64, status domain.GameStatus, started time.Time) domain.Game {
		return domain.Game{ID: id, ChatID: chatID, Status: status, StartedAt: started}
	}

	tests := []struct {
		name   string
		games  []domain.Game
		wantID int64 // 0 — прошлой игры нет
	}{
		{name: "в группе ещё не играли"},
		{
			name: "последняя завершённая по started_at",
			games: []domain.Game{
				game(1, chat, domain.GameStatusFinished, day(1)),
				game(3, chat, domain.GameStatusFinished, day(5)),
				game(2, chat, domain.GameStatusFinished, day(3)),
			},
			wantID: 3,
		},
		{
			name: "завершённая важнее более свежей отменённой",
			games: []domain.Game{
				game(1, chat, domain.GameStatusFinished, day(1)),
				game(2, chat, domain.GameStatusCancelled, day(7)),
			},
			wantID: 1,
		},
		{
			name: "только отменённые — берём последнюю из них",
			games: []domain.Game{
				game(1, chat, domain.GameStatusCancelled, day(1)),
				game(2, chat, domain.GameStatusCancelled, day(4)),
			},
			wantID: 2,
		},
		{
			name: "активная игра не считается прошлой",
			games: []domain.Game{
				game(1, chat, domain.GameStatusCancelled, day(1)),
				game(2, chat, domain.GameStatusActive, day(9)),
			},
			wantID: 1,
		},
		{
			name: "игры других групп не учитываются",
			games: []domain.Game{
				game(1, otherChat, domain.GameStatusFinished, day(8)),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &GameService{games: &fakeGames{games: tt.games}}
			g, err := s.PreviousGameInChat(context.Background(), chat)
			if err != nil {
				t.Fatal(err)
			}
			var got int64
			if g != nil {
				got = g.ID
			}
			if got != tt.wantID {
				t.Fatalf("previous game id = %d, want %d", got, tt.wantID)
			}
		})
	}
}

func TestPreviousGameInChatError(t *testing.T) {
	boom := errors.New("db down")
	s := &GameService{games: &fakeGames{err: boom}}
	if g, err := s.PreviousGameInChat(context.Background(), 1); !errors.Is(err, boom) || g != nil {
		t.Fatalf("got (%v, %v), want (nil, %v)", g, err, boom)
	}
}

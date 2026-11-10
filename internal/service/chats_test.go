package service

import (
	"context"
	"testing"
	"time"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
)

// fakeChats — ChatRepository в памяти; нужен только ListByMember.
type fakeChats struct {
	interfaces.ChatRepository
	list []domain.ChatActivity
}

func (f *fakeChats) ListByMember(context.Context, int64) ([]domain.ChatActivity, error) {
	return append([]domain.ChatActivity(nil), f.list...), nil
}

func TestChatsOfOrdersByPopularity(t *testing.T) {
	day := func(d int) *time.Time {
		at := time.Date(2026, 11, d, 20, 0, 0, 0, time.UTC)
		return &at
	}
	chat := func(id int64, games int, last *time.Time) domain.ChatActivity {
		return domain.ChatActivity{Chat: domain.Chat{ID: id}, GamesCount: games, LastGameAt: last}
	}

	// Порядок репозитория — по давности появления пользователя в чате.
	s := &GameService{chats: &fakeChats{list: []domain.ChatActivity{
		chat(1, 0, nil),     // без игр, но самый свежий по активности
		chat(2, 3, day(2)),  // 3 игры, последняя раньше, чем в чате 4
		chat(3, 24, day(1)), // больше всего игр
		chat(4, 3, day(9)),  // 3 игры, последняя позже
		chat(5, 0, nil),     // без игр
		chat(6, 11, day(5)),
	}}}

	got, err := s.ChatsOf(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{3, 6, 4, 2, 1, 5}
	if len(got) != len(want) {
		t.Fatalf("got %d chats, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			ids := make([]int64, len(got))
			for j := range got {
				ids[j] = got[j].ID
			}
			t.Fatalf("order = %v, want %v", ids, want)
		}
	}
}

func TestMorePopular(t *testing.T) {
	early := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	mk := func(games int, last *time.Time) *domain.ChatActivity {
		return &domain.ChatActivity{GamesCount: games, LastGameAt: last}
	}

	cases := []struct {
		name string
		a, b *domain.ChatActivity
		want bool
	}{
		{"больше игр — выше", mk(5, &early), mk(4, &late), true},
		{"меньше игр — ниже", mk(4, &late), mk(5, &early), false},
		{"игр поровну: свежая игра выше", mk(3, &late), mk(3, &early), true},
		{"игр поровну: давняя игра ниже", mk(3, &early), mk(3, &late), false},
		{"полное равенство — порядок не меняется", mk(3, &late), mk(3, &late), false},
		{"оба без игр — порядок не меняется", mk(0, nil), mk(0, nil), false},
		{"с датой выше, чем без даты", mk(0, &early), mk(0, nil), true},
		{"без даты ниже, чем с датой", mk(0, nil), mk(0, &early), false},
	}
	for _, c := range cases {
		if got := morePopular(c.a, c.b); got != c.want {
			t.Errorf("%s: morePopular = %v, want %v", c.name, got, c.want)
		}
	}
}

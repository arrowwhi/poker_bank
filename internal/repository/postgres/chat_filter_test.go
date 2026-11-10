package postgres_test

import (
	"context"
	"testing"

	"poker_bank/internal/domain"
	"poker_bank/internal/repository/postgres"
	"poker_bank/internal/repository/postgres/pgtest"
)

// Фильтр по группе в «Мой профиль → чат». Параметр из-за сравнения `$2 = 0` выводился
// как integer, и id супергруппы (-100xxxxxxxxxx) не кодировался в int4 — запрос падал.
func TestListHistory_FilterBySupergroupChat(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	const playerID int64 = 7000000001

	inChat := pgtest.SeedFinishedGame(t, pool, pgtest.SupergroupChatID, playerID, 500)
	pgtest.SeedFinishedGame(t, pool, -4000001, playerID, -200) // обычная группа: id помещается в int4

	repo := postgres.NewGameResultRepo(pool)

	all, err := repo.ListHistory(ctx, playerID, 0)
	if err != nil {
		t.Fatalf("history over all chats: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("history over all chats: got %d games, want 2", len(all))
	}

	got, err := repo.ListHistory(ctx, playerID, pgtest.SupergroupChatID)
	if err != nil {
		t.Fatalf("history filtered by supergroup: %v", err)
	}
	if len(got) != 1 || got[0].GameID != inChat || got[0].ChatID != pgtest.SupergroupChatID || got[0].NetRub != 500 {
		t.Fatalf("history filtered by supergroup: got %+v, want only game %d", got, inChat)
	}
}

// Тот же фильтр в списке игр (GET /api/games?chat_id=).
func TestListVisible_FilterBySupergroupChat(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	const playerID int64 = 7000000002

	inChat := pgtest.SeedFinishedGame(t, pool, pgtest.SupergroupChatID, playerID, 500)
	pgtest.SeedFinishedGame(t, pool, -4000002, playerID, -200)

	repo := postgres.NewGameRepo(pool)

	all, err := repo.ListVisible(ctx, playerID, 0, "", 50)
	if err != nil {
		t.Fatalf("visible over all chats: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("visible over all chats: got %d games, want 2", len(all))
	}

	got, err := repo.ListVisible(ctx, playerID, pgtest.SupergroupChatID, domain.GameStatusFinished, 50)
	if err != nil {
		t.Fatalf("visible filtered by supergroup: %v", err)
	}
	if len(got) != 1 || got[0].ID != inChat {
		t.Fatalf("visible filtered by supergroup: got %+v, want only game %d", got, inChat)
	}
}

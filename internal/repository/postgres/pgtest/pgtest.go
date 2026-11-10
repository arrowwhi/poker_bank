// Package pgtest поднимает настоящий Postgres для тестов репозиториев и API.
//
// Тесты запускаются, когда задан TEST_DATABASE_URL, иначе пропускаются:
//
//	TEST_DATABASE_URL=postgres://poker:poker@localhost:5432/poker_bank?sslmode=disable go test ./...
//
// Каждый тест работает в своей схеме, которая удаляется по его завершении, — данные базы не трогаются.
package pgtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	infrapg "github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/arrowwhi/infrastructure/connections/postgres/migrate"
	"github.com/jackc/pgx/v5"

	"poker_bank/internal/domain"
	"poker_bank/internal/interfaces"
	"poker_bank/internal/repository/postgres"
	"poker_bank/migrations"
)

// SupergroupChatID — id супергруппы Telegram: всегда вида -100xxxxxxxxxx и не помещается в int4.
const SupergroupChatID int64 = -1001234567890

// migrateLockKey — ключ advisory lock, которым тесты сериализуют запуск миграций.
const migrateLockKey int64 = 0x706b5f7467 // "pk_tg"

// Pool возвращает пул к отдельной схеме с применёнными миграциями.
func Pool(t *testing.T) *infrapg.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — тесты с Postgres пропущены")
	}

	ctx := context.Background()
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`)
		_ = admin.Close(ctx)
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	pool, err := postgres.NewPool(ctx, u.String())
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Мигратор берёт advisory lock без ожидания и падает, если он занят. go test запускает
	// пакеты параллельно, и все они ходят в одну базу, поэтому миграции разных схем
	// выстраиваем в очередь своим блокирующим локом.
	if _, err := admin.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockKey); err != nil {
		t.Fatalf("lock migrations: %v", err)
	}
	err = migrate.Up(ctx, pool, migrations.FS, ".")
	if _, unlockErr := admin.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrateLockKey); unlockErr != nil {
		t.Fatalf("unlock migrations: %v", unlockErr)
	}
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

// SeedFinishedGame создаёт игрока-дилера, группу, где он участник, и завершённую игру
// с его результатом netRub. Возвращает id игры.
func SeedFinishedGame(t *testing.T, pool *infrapg.Pool, chatID, playerID int64, netRub int) int64 {
	t.Helper()
	ctx := context.Background()

	player := &domain.Player{TelegramUserID: playerID, Username: "player", DisplayName: "Player"}
	if err := postgres.NewPlayerRepo(pool).Upsert(ctx, player); err != nil {
		t.Fatalf("upsert player: %v", err)
	}
	if err := postgres.NewChatRepo(pool).TouchMember(ctx, chatID, playerID); err != nil {
		t.Fatalf("touch chat member: %v", err)
	}

	games := postgres.NewGameRepo(pool)
	gameID, err := games.Create(ctx, &domain.Game{
		ChatID: chatID, DealerTgID: playerID,
		BuyInRub: 1000, BuyInChips: 1000, RebuyRub: 1000, RebuyChips: 1000,
	})
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	err = games.Finish(ctx, interfaces.FinishGameParams{
		GameID: gameID,
		Results: []domain.GameResult{{
			GameID: gameID, PlayerTgID: playerID, BuyInCount: 1,
			TotalInRub: 1000, TotalOutRub: 1000 + netRub, TotalOutChips: 1000 + netRub, NetRub: netRub,
		}},
	})
	if err != nil {
		t.Fatalf("finish game: %v", err)
	}
	return gameID
}

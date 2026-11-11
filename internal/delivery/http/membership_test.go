package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"poker_bank/internal/repository/postgres"
	"poker_bank/internal/repository/postgres/pgtest"
	"poker_bank/internal/service"
)

// memberNotifier отвечает на getChatMember заданным образом.
type memberNotifier struct {
	stubNotifier
	member bool
	err    error
}

func (n memberNotifier) IsChatMember(context.Context, int64, int64) (bool, error) {
	return n.member, n.err
}

// Ревью C4: кто вышел или удалён из группы, теряет доступ к ней в Mini App, даже если
// бот запомнил его раньше. Если Telegram недоступен, решают известные боту участники.
func TestChatAccessFollowsTelegramMembership(t *testing.T) {
	const (
		chatID   = pgtest.SupergroupChatID
		playerID = int64(7000000010)
	)
	path := "/api/chats/" + strconv.FormatInt(chatID, 10) + "/leaderboard"

	cases := []struct {
		name      string
		notifier  memberNotifier
		wantCode  int
		wantKnown bool
	}{
		{"still a member", memberNotifier{member: true}, http.StatusOK, true},
		{"left the group", memberNotifier{member: false}, http.StatusForbidden, false},
		{"telegram unavailable", memberNotifier{err: errors.New("timeout")}, http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := pgtest.Pool(t)
			pgtest.SeedFinishedGame(t, pool, chatID, playerID, 100)

			games := service.NewGameService(
				postgres.NewGameRepo(pool), postgres.NewLedgerRepo(pool), postgres.NewParticipantRepo(pool),
				postgres.NewGameResultRepo(pool), postgres.NewSettlementRepo(pool), postgres.NewPendingActionRepo(pool),
				postgres.NewChatRepo(pool), postgres.NewTxManager(pool), nil,
			)
			a := &api{games: games, players: service.NewPlayerService(postgres.NewPlayerRepo(pool)), notifier: tc.notifier}
			r := chi.NewRouter()
			r.Route("/api", func(r chi.Router) {
				r.Use(TelegramAuth(token, nil))
				a.routes(r)
			})

			if w := getAs(t, r, playerID, path); w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantCode, w.Body.String())
			}
			known, err := games.IsKnownMember(context.Background(), chatID, playerID)
			if err != nil {
				t.Fatal(err)
			}
			if known != tc.wantKnown {
				t.Fatalf("known member = %v, want %v", known, tc.wantKnown)
			}
		})
	}
}

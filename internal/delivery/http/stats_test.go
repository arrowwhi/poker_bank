package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	infrapg "github.com/arrowwhi/infrastructure/connections/postgres"
	"github.com/go-chi/chi/v5"

	"poker_bank/internal/domain"
	"poker_bank/internal/repository/postgres"
	"poker_bank/internal/repository/postgres/pgtest"
	"poker_bank/internal/service"
)

var errTelegramUnavailable = errors.New("telegram unavailable")

// stubNotifier: API в этих тестах ничего не пишет в чат и не ходит в Telegram.
type stubNotifier struct{}

func (stubNotifier) RequestCreated(context.Context, *domain.Game, *domain.PendingAction) {}
func (stubNotifier) RequestApplied(context.Context, *domain.Game, *domain.PendingAction) {}
func (stubNotifier) RequestResolved(context.Context, *domain.PendingAction)              {}
func (stubNotifier) RequestsClosed(context.Context, []domain.PendingAction)              {}
func (stubNotifier) Announce(context.Context, int64, string)                             {}
func (stubNotifier) GameCreated(context.Context, *domain.Game)                           {}
func (stubNotifier) GameFinished(context.Context, *domain.Game, *service.EndResult)      {}
func (stubNotifier) SettlementsChanged(context.Context, *domain.Game)                    {}

// IsChatMember: Telegram недоступен — доступ решают известные боту участники.
func (stubNotifier) IsChatMember(context.Context, int64, int64) (bool, error) {
	return false, errTelegramUnavailable
}
func (stubNotifier) PlayerName(context.Context, int64) string { return "" }

// apiOnPostgres собирает /api поверх настоящих репозиториев (нужен TEST_DATABASE_URL).
func apiOnPostgres(t *testing.T) (http.Handler, func(chatID, playerID int64, netRub int) int64) {
	t.Helper()
	pool := pgtest.Pool(t)
	seed := func(chatID, playerID int64, netRub int) int64 {
		return pgtest.SeedFinishedGame(t, pool, chatID, playerID, netRub)
	}
	return apiOnPool(pool), seed
}

// apiOnPool — то же поверх готового пула: тест сам наполняет базу.
func apiOnPool(pool *infrapg.Pool) http.Handler {
	games := service.NewGameService(
		postgres.NewGameRepo(pool), postgres.NewLedgerRepo(pool), postgres.NewParticipantRepo(pool),
		postgres.NewGameResultRepo(pool), postgres.NewSettlementRepo(pool), postgres.NewPendingActionRepo(pool),
		postgres.NewChatRepo(pool), postgres.NewTxManager(pool), nil,
	)
	a := &api{games: games, players: service.NewPlayerService(postgres.NewPlayerRepo(pool)), notifier: stubNotifier{}}

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(TelegramAuth(token, nil))
		a.routes(r)
	})
	return r
}

func getAs(t *testing.T, h http.Handler, userID int64, path string) *httptest.ResponseRecorder {
	t.Helper()
	auth := sign(t, url.Values{
		"auth_date": {strconv.FormatInt(time.Now().Unix(), 10)},
		"user":      {`{"id":` + strconv.FormatInt(userID, 10) + `,"username":"player","first_name":"Player"}`},
	})
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "tma "+auth)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Сценарий «Мой профиль → конкретный чат»: GET /api/stats/me?chat_id=<супергруппа>.
// До исправления фильтра в ListHistory отвечал 500 «Что-то пошло не так».
func TestMyStats_FilterBySupergroupChat(t *testing.T) {
	h, seed := apiOnPostgres(t)

	const playerID int64 = 7000000003

	inChat := seed(pgtest.SupergroupChatID, playerID, 500)
	seed(-4000003, playerID, -200)

	chat := strconv.FormatInt(pgtest.SupergroupChatID, 10)
	cases := []struct {
		name, path string
		games, net int
	}{
		{"все чаты", "/api/stats/me", 2, 300},
		{"супергруппа", "/api/stats/me?chat_id=" + chat, 1, 500},
		{"профиль игрока в супергруппе", "/api/stats/users/" + strconv.FormatInt(playerID, 10) + "?chat_id=" + chat, 1, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := getAs(t, h, playerID, tc.path)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			var got struct {
				GamesPlayed      int  `json:"games_played"`
				TotalNetRub      int  `json:"total_net_rub"`
				LeaderboardPlace *int `json:"leaderboard_place"`
				History          []struct {
					GameID int64 `json:"game_id"`
					ChatID int64 `json:"chat_id"`
				} `json:"history"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.GamesPlayed != tc.games || got.TotalNetRub != tc.net || len(got.History) != tc.games {
				t.Fatalf("stats = %+v, want %d games with net %d", got, tc.games, tc.net)
			}
			if tc.games == 1 {
				if got.History[0].GameID != inChat || got.History[0].ChatID != pgtest.SupergroupChatID {
					t.Fatalf("history = %+v, want only game %d", got.History, inChat)
				}
				if got.LeaderboardPlace == nil || *got.LeaderboardPlace != 1 {
					t.Fatalf("leaderboard place = %v, want 1", got.LeaderboardPlace)
				}
			}
		})
	}
}

// Список игр группы (GET /api/games?chat_id=) использует тот же фильтр.
func TestListGames_FilterBySupergroupChat(t *testing.T) {
	h, seed := apiOnPostgres(t)

	const playerID int64 = 7000000004

	inChat := seed(pgtest.SupergroupChatID, playerID, 500)
	seed(-4000004, playerID, -200)

	w := getAs(t, h, playerID, "/api/games?chat_id="+strconv.FormatInt(pgtest.SupergroupChatID, 10))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var got []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ID != inChat {
		t.Fatalf("games = %+v, want only game %d", got, inChat)
	}
}

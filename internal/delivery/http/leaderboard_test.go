package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"poker_bank/internal/domain"
	"poker_bank/internal/repository/postgres/pgtest"
)

// GET /api/chats/:id/leaderboard отдаёт в одном ответе всё, из чего клиент строит рейтинг
// в рублях, фишках и «% от входа». Порядок и место — по рублям при любом period.
func TestLeaderboard_AllUnitsInOneResponse(t *testing.T) {
	pool := pgtest.Pool(t)
	h := apiOnPool(pool)

	const (
		chatID int64 = -4000020
		alice  int64 = 7000000020
		bob    int64 = 7000000021
	)

	// Алиса: +500₽ при входе 1000₽ / 1000 фишек и выходе с 1500 фишками.
	g1 := pgtest.SeedFinishedGame(t, pool, chatID, alice, 500)
	pgtest.SeedLedger(t, pool, g1, alice, domain.LedgerBuyIn, 1000, 1000)
	// Боб: −200₽; вход 1000₽ — это 4000 фишек, вышел с 800.
	g2 := pgtest.SeedFinishedGame(t, pool, chatID, bob, -200)
	pgtest.SeedLedger(t, pool, g2, bob, domain.LedgerBuyIn, 1000, 4000)

	type entry struct {
		Place int `json:"place"`
		User  struct {
			ID int64 `json:"id"`
		} `json:"user"`
		GamesPlayed   int     `json:"games_played"`
		TotalNetRub   int     `json:"total_net_rub"`
		TotalNetChips int     `json:"total_net_chips"`
		TotalInRub    int     `json:"total_in_rub"`
		WinRate       float64 `json:"win_rate"`
	}

	for _, period := range []string{"", "all", "year", "month"} {
		t.Run("period="+period, func(t *testing.T) {
			path := "/api/chats/" + strconv.FormatInt(chatID, 10) + "/leaderboard"
			if period != "" {
				path += "?period=" + period
			}
			w := getAs(t, h, alice, path)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			var got struct {
				Entries []entry `json:"entries"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(got.Entries) != 2 {
				t.Fatalf("entries = %+v, want 2", got.Entries)
			}

			a, b := got.Entries[0], got.Entries[1]
			if a.User.ID != alice || a.Place != 1 || a.GamesPlayed != 1 || a.TotalNetRub != 500 ||
				a.TotalNetChips != 500 || a.TotalInRub != 1000 || a.WinRate != 1 {
				t.Errorf("first entry = %+v, want alice: +500₽, +500 chips, in 1000₽, win-rate 1", a)
			}
			if b.User.ID != bob || b.Place != 2 || b.GamesPlayed != 1 || b.TotalNetRub != -200 ||
				b.TotalNetChips != 800-4000 || b.TotalInRub != 1000 || b.WinRate != 0 {
				t.Errorf("second entry = %+v, want bob: −200₽, −3200 chips, in 1000₽, win-rate 0", b)
			}
		})
	}
}

// Package http — публичный HTTP-сервер poker_bank на http/server инфраструктуры:
// API Mini App (/api), собранный фронтенд и webhook Telegram.
package http

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	httpserver "github.com/arrowwhi/infrastructure/http/server"
	"github.com/arrowwhi/infrastructure/observability/health"
	"github.com/arrowwhi/infrastructure/observability/metrics"
	"github.com/arrowwhi/infrastructure/observability/tracing"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"poker_bank/internal/domain"
	"poker_bank/internal/service"
)

// Deps — зависимости сервера; собираются в app.
type Deps struct {
	Games   *service.GameService
	Players *service.PlayerService
	Log     *zap.Logger
	Health  *health.Registry
	// WebAssets — собранный Mini App; nil — только API.
	WebAssets fs.FS
	// TelegramWebhook монтируется на WebhookPath только в режиме webhook.
	TelegramWebhook http.Handler

	ServiceName     string
	BotToken        string
	WebhookPath     string
	Port            string
	ShutdownTimeout time.Duration
}

// New собирает сервер. Он реализует telegram.Component и запускается через App.Run.
func New(d Deps) (*httpserver.Server, error) {
	srv, err := httpserver.New(httpserver.Config{
		Host:              "0.0.0.0",
		Port:              d.Port,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		ShutdownTimeout:   d.ShutdownTimeout,
	},
		httpserver.WithHealthRegistry(d.Health),
		httpserver.WithMiddlewares(
			tracing.Middleware(d.ServiceName),
			metrics.Middleware(d.ServiceName),
		),
	)
	if err != nil {
		return nil, err
	}

	r := srv.Router()

	if d.TelegramWebhook != nil {
		r.Method(http.MethodPost, d.WebhookPath, d.TelegramWebhook)
	}

	h := &handlers{games: d.Games, log: d.Log}

	r.Route("/api", func(api chi.Router) {
		api.Use(TelegramAuth(d.BotToken, upsertPlayer(d.Players, d.Log)))
		api.Get("/me", h.me)
	})

	if d.WebAssets != nil {
		r.Handle("/*", spaHandler(d.WebAssets))
	}

	return srv, nil
}

// upsertPlayer обновляет ник и имя пользователя при каждом заходе в Mini App.
func upsertPlayer(players *service.PlayerService, log *zap.Logger) func(context.Context, Identity) {
	return func(ctx context.Context, id Identity) {
		err := players.Upsert(ctx, &domain.Player{
			TelegramUserID: id.TelegramID,
			Username:       id.Username,
			DisplayName:    strings.TrimSpace(id.FirstName + " " + id.LastName),
		})
		if err != nil {
			log.Warn("upsert player from mini app", zap.Error(err), zap.Int64("tg_id", id.TelegramID))
		}
	}
}

type handlers struct {
	games *service.GameService
	log   *zap.Logger
}

type meResponse struct {
	User        userDTO   `json:"user"`
	StartParam  string    `json:"start_param,omitempty"`
	ActiveGames []gameDTO `json:"active_games"`
}

type userDTO struct {
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name"`
	ID        int64  `json:"id"`
}

type gameDTO struct {
	StartedAt  time.Time `json:"started_at"`
	ID         int64     `json:"id"`
	ChatID     int64     `json:"chat_id"`
	DealerID   int64     `json:"dealer_id"`
	BuyInRub   int       `json:"buy_in_rub"`
	BuyInChips int       `json:"buy_in_chips"`
	RebuyRub   int       `json:"rebuy_rub"`
	RebuyChips int       `json:"rebuy_chips"`
	IsDealer   bool      `json:"is_dealer"`
}

// me — кто я и в каких активных играх участвую (стартовый экран Mini App).
func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	id, _ := IdentityFrom(r.Context())

	games, err := h.games.ListActiveGamesOfPlayer(r.Context(), id.TelegramID)
	if err != nil {
		h.log.Error("list active games of player", zap.Error(err), zap.Int64("tg_id", id.TelegramID))
		writeError(w, http.StatusInternalServerError, "internal", "Что-то пошло не так")
		return
	}

	resp := meResponse{
		User:        userDTO{ID: id.TelegramID, Username: id.Username, FirstName: id.FirstName},
		StartParam:  id.StartParam,
		ActiveGames: make([]gameDTO, 0, len(games)),
	}
	for _, g := range games {
		resp.ActiveGames = append(resp.ActiveGames, gameDTO{
			ID: g.ID, ChatID: g.ChatID, DealerID: g.DealerTgID, StartedAt: g.StartedAt,
			BuyInRub: g.BuyInRub, BuyInChips: g.BuyInChips, RebuyRub: g.RebuyRub, RebuyChips: g.RebuyChips,
			IsDealer: g.DealerTgID == id.TelegramID,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError — формат ошибок API: {"error":{"code","message"}}.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// spaHandler отдаёт Mini App; неизвестные пути — index.html (клиентский роутинг).
func spaHandler(assets fs.FS) http.Handler {
	files := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(assets, path); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}

		// Хэшированные ассеты кешируются надолго, index.html — никогда.
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		files.ServeHTTP(w, r)
	})
}

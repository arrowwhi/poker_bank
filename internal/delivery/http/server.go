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
	// Notifier отражает действия из Mini App в чате игры.
	Notifier Notifier
	Log      *zap.Logger
	Health   *health.Registry
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

	a := &api{games: d.Games, players: d.Players, notifier: d.Notifier, log: d.Log}

	r.Route("/api", func(r chi.Router) {
		r.Use(TelegramAuth(d.BotToken, upsertPlayer(d.Players, d.Log)))
		a.routes(r)
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

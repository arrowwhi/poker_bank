// Package http — публичный HTTP-сервер poker_bank:
// API Mini App (/api), собранный фронтенд и webhook Telegram.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/arrowwhi/infrastructure/http/server/middlewares"
	"github.com/arrowwhi/infrastructure/observability/health"
	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/arrowwhi/infrastructure/observability/metrics"
	"github.com/arrowwhi/infrastructure/observability/tracing"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"poker_bank/internal/domain"
	"poker_bank/internal/service"
)

// Deps — зависимости сервера; собираются в app.
type Deps struct {
	Games   *service.GameService
	Players *service.PlayerService
	// Notifier отражает действия из Mini App в чате игры.
	Notifier Notifier
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

// Server — HTTP-сервер poker_bank; реализует telegram.Component и запускается через App.Run.
//
// Собирается на net/http + chi, а не на http/server инфраструктуры: тот всегда ставит
// middlewares.Log, который пишет в лог тела запросов и ответов (персональные данные игроков),
// и отключить это нельзя. Остальное повторяет его поведение: RequestID, RealIP, /healthz, /ready.
type Server struct {
	srv             *http.Server
	shutdownTimeout time.Duration
}

// New собирает сервер.
func New(d Deps) (*Server, error) {
	r := chi.NewRouter()
	r.Use(
		middlewares.RequestID,
		middlewares.RealIP,
		accessLog,
		middleware.Recoverer, // внутри accessLog — паника попадёт в лог как 500
		tracing.Middleware(d.ServiceName),
		metrics.Middleware(d.ServiceName),
	)

	registry := d.Health
	if registry == nil {
		registry = health.New()
	}
	r.Method(http.MethodGet, "/healthz", health.LivenessHandler())
	r.Method(http.MethodGet, "/ready", registry.ReadinessHandler())

	if d.TelegramWebhook != nil {
		r.Method(http.MethodPost, d.WebhookPath, d.TelegramWebhook)
	}

	a := &api{games: d.Games, players: d.Players, notifier: d.Notifier}

	r.Route("/api", func(r chi.Router) {
		r.Use(TelegramAuth(d.BotToken, upsertPlayer(d.Players)))
		a.routes(r)
	})

	if d.WebAssets != nil {
		r.Handle("/*", spaHandler(d.WebAssets))
	}

	return &Server{
		srv: &http.Server{
			Addr:              "0.0.0.0:" + d.Port,
			Handler:           r,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
		},
		shutdownTimeout: d.ShutdownTimeout,
	}, nil
}

// Handler — корневой обработчик со всеми middleware (для тестов).
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Run слушает порт до отмены ctx, затем останавливается с ожиданием ShutdownTimeout.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info(ctx).Str("addr", s.srv.Addr).Msg("starting http server")
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.shutdownTimeout)
	defer cancel()
	logger.Info(ctx).Msg("shutting down http server")
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	return <-errCh
}

// upsertPlayer обновляет ник и имя пользователя при каждом заходе в Mini App.
func upsertPlayer(players *service.PlayerService) func(context.Context, Identity) {
	return func(ctx context.Context, id Identity) {
		err := players.Upsert(ctx, &domain.Player{
			TelegramUserID: id.TelegramID,
			Username:       id.Username,
			DisplayName:    strings.TrimSpace(id.FirstName + " " + id.LastName),
		})
		if err != nil {
			logger.Warn(ctx).Err(err).Int64("tg_id", id.TelegramID).Msg("upsert player from mini app")
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

package http

import (
	"net/http"
	"time"

	"github.com/arrowwhi/infrastructure/http/server/middlewares"
	"github.com/arrowwhi/infrastructure/observability/logger"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

// accessLog — одна запись «incoming HTTP request» на запрос: метод, путь, статус, латентность,
// request_id, адрес клиента. Тела запроса и ответа намеренно не пишутся: в API это имена
// игроков, суммы и переводы, в webhook — апдейты Telegram. Заменяет middlewares.Log из
// infrastructure, который логирует тела целиком и не настраивается.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		defer func() {
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK // обработчик ничего не записал — net/http ответит 200
			}

			ctx := r.Context()
			var evt *zerolog.Event
			switch {
			case status >= http.StatusInternalServerError:
				evt = logger.Error(ctx)
			case status >= http.StatusBadRequest:
				evt = logger.Warn(ctx)
			default:
				evt = logger.Info(ctx)
			}

			evt = evt.
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status_code", status).
				Dur("latency", time.Since(start)).
				Str("remote_addr", r.RemoteAddr).
				Int("bytes_out", ww.BytesWritten())
			if id, ok := ctx.Value(middlewares.RequestIDKey).(string); ok && id != "" {
				evt = evt.Str("request_id", id)
			}
			evt.Msg("incoming HTTP request")
		}()

		next.ServeHTTP(ww, r)
	})
}

package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// captureStdout перехватывает вывод observability/logger: без logger.Init он пишет в os.Stdout,
// открытый в момент вызова.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()
	return string(<-done)
}

// Access-лог не должен содержать тел запросов и ответов: в /api там имена игроков и суммы,
// в webhook — апдейты Telegram, у статики — весь index.html.
func TestAccessLogOmitsBodies(t *testing.T) {
	const (
		reqSecret  = "REQUEST-SECRET-Петя-1500"
		respSecret = "RESPONSE-SECRET-Вася-750"
	)

	srv, err := New(Deps{
		Notifier: stubNotifier{},
		BotToken: token,
		WebAssets: fstest.MapFS{
			"index.html": {Data: []byte("<html>" + respSecret + "</html>")},
		},
		TelegramWebhook: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			writeJSON(w, http.StatusOK, map[string]string{"echo": string(body), "secret": respSecret})
		}),
		WebhookPath: "/telegram/webhook",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	requests := []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/api/games/1/requests", http.StatusUnauthorized},
		{http.MethodPost, "/telegram/webhook", http.StatusOK},
		{http.MethodGet, "/", http.StatusOK},
	}

	out := captureStdout(t, func() {
		for _, rq := range requests {
			r := httptest.NewRequest(rq.method, rq.path, strings.NewReader(`{"name":"`+reqSecret+`"}`))
			r.Header.Set("X-Request-ID", "req-"+rq.path)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != rq.status {
				t.Fatalf("%s %s: status %d, want %d", rq.method, rq.path, w.Code, rq.status)
			}
		}
	})

	for _, forbidden := range []string{reqSecret, respSecret, "Требуется авторизация", "missing_init_data", "request_body", "response_body"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("access log contains %q:\n%s", forbidden, out)
		}
	}

	var entries []map[string]any
	for _, line := range bytes.Split([]byte(out), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		if e["message"] == "incoming HTTP request" {
			entries = append(entries, e)
		}
	}
	if len(entries) != len(requests) {
		t.Fatalf("got %d access log entries, want %d:\n%s", len(entries), len(requests), out)
	}
	for i, rq := range requests {
		e := entries[i]
		if e["method"] != rq.method || e["path"] != rq.path || e["status_code"] != float64(rq.status) ||
			e["request_id"] != "req-"+rq.path || e["remote_addr"] == nil || e["latency"] == nil {
			t.Errorf("entry %d = %v, want method/path/status/request_id/remote_addr/latency of %s %s", i, e, rq.method, rq.path)
		}
	}
}

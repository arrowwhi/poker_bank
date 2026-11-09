package tgforum

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arrowwhi/infrastructure/telegram"
)

const token = "1:test"

// fakeAPI отдаёт одно обновление из топика 77 и записывает тела sendMessage.
func fakeAPI(t *testing.T, sent chan<- map[string]any) *httptest.Server {
	t.Helper()

	var served atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		body, _ := io.ReadAll(r.Body)

		switch method {
		case "getMe":
			_, _ = io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"b","username":"b"}}`)
		case "getUpdates":
			if served.Swap(true) {
				_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)

				return
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":[{"update_id":5,"message":{"message_id":1,"date":1,
				"message_thread_id":77,"is_topic_message":true,"text":"/ping",
				"chat":{"id":-100,"type":"supergroup"},"from":{"id":9,"is_bot":false,"first_name":"u"}}}]}`)
		case "sendMessage":
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			sent <- m
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":2,"date":1,"chat":{"id":-100,"type":"supergroup"}}}`)
		default:
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestPollingKeepsTopic(t *testing.T) {
	sent := make(chan map[string]any, 1)
	srv := fakeAPI(t, sent)
	tr := NewTracker()

	cfg := telegram.DefaultConfig(token)
	cfg.API.BaseURL = srv.URL
	cfg.PollTimeout = time.Second
	cfg.SetCommands = false

	client, err := telegram.NewClient(cfg.API, telegram.WithHTTPClient(tr.HTTPClient(nil)))
	if err != nil {
		t.Fatal(err)
	}

	b, err := telegram.New(cfg, telegram.WithAPIClient(client))
	if err != nil {
		t.Fatal(err)
	}

	b.Use(tr.Middleware())
	b.Command("ping", "", func(c *telegram.Context) error {
		_, err := Send(c, c.API(), SendRequest{ChatID: c.ChatID(), Text: "pong", MessageThreadID: tr.ThreadID(c)})

		return err
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = b.Run(ctx) }()

	select {
	case m := <-sent:
		if m["message_thread_id"] != float64(77) {
			t.Fatalf("message_thread_id = %v, want 77", m["message_thread_id"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reply")
	}
}

func TestWebhookRecordsAndForwardsBody(t *testing.T) {
	tr := NewTracker()

	var forwarded string

	h := tr.Webhook(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		forwarded = string(b)
	}))

	body := `{"update_id":3,"callback_query":{"id":"x","from":{"id":1},"message":{"message_id":1,"message_thread_id":12,"is_topic_message":true,"chat":{"id":-1,"type":"supergroup"}}}}`
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))

	if forwarded != body {
		t.Fatalf("body not forwarded")
	}

	c := &telegram.Context{Update: telegram.Update{UpdateID: 3}}
	if got := tr.ThreadID(c); got != 12 {
		t.Fatalf("ThreadID = %d, want 12", got)
	}
}

func TestReplyThreadInPlainGroupIsNotTopic(t *testing.T) {
	tr := NewTracker()
	tr.record([]rawUpdate{{UpdateID: 1, Message: &rawMessage{MessageThreadID: 4}}})

	if got := tr.ThreadID(&telegram.Context{Update: telegram.Update{UpdateID: 1}}); got != 0 {
		t.Fatalf("ThreadID = %d, want 0", got)
	}
}

func TestChatMember(t *testing.T) {
	if !(ChatMember{Status: "creator"}).IsAdmin() || (ChatMember{Status: "member"}).IsAdmin() {
		t.Fatal("IsAdmin")
	}
	if (ChatMember{Status: "restricted"}).InChat() || !(ChatMember{Status: "restricted", IsMember: true}).InChat() {
		t.Fatal("InChat")
	}
}

package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	tg "github.com/arrowwhi/infrastructure/telegram"

	"poker_bank/internal/domain"
	"poker_bank/internal/pkg/tgforum"
	"poker_bank/internal/service"
)

type stubPlayers struct{}

func (stubPlayers) Upsert(context.Context, *domain.Player) error { return nil }
func (stubPlayers) GetByID(context.Context, int64) (*domain.Player, error) {
	return nil, nil
}
func (stubPlayers) GetByUsername(context.Context, string) (*domain.Player, error) {
	return nil, nil
}

type apiCall struct {
	method string
	body   map[string]any
}

// fakeBotAPI — заглушка Bot API: отдаёт заранее заданные обновления и запоминает вызовы.
type fakeBotAPI struct {
	mu      sync.Mutex
	updates []string
	served  bool
	calls   []apiCall
}

func (f *fakeBotAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := path.Base(r.URL.Path)
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)

	w.Header().Set("Content-Type", "application/json")
	switch method {
	case "getMe":
		_, _ = io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"Bank","username":"testbot"}}`)
	case "getUpdates":
		f.mu.Lock()
		first := !f.served
		f.served = true
		f.mu.Unlock()
		if !first {
			select {
			case <-r.Context().Done():
			case <-time.After(200 * time.Millisecond):
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":[`+strings.Join(f.updates, ",")+`]}`)
	case "sendMessage", "editMessageText":
		f.record(method, body)
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":99,"chat":{"id":5,"type":"private"}}}`)
	default:
		f.record(method, body)
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}
}

func (f *fakeBotAPI) record(method string, body map[string]any) {
	f.mu.Lock()
	f.calls = append(f.calls, apiCall{method: method, body: body})
	f.mu.Unlock()
}

func (f *fakeBotAPI) byMethod(method string) []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []apiCall
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// Свежий обработчик (как после рестарта) получает /start, /help и нажатия кнопок под
// старым сообщением справки: раздел берётся только из callback data.
func TestStartAndHelpFlow(t *testing.T) {
	const user = `{"id":5,"is_bot":false,"first_name":"Ivan <b>","username":""}`
	const chat = `{"id":5,"type":"private"}`
	oldMessage := `{"message_id":10,"date":1,"chat":` + chat + `,"text":"старое сообщение"}`

	api := &fakeBotAPI{updates: []string{
		`{"update_id":1,"message":{"message_id":1,"date":1,"from":` + user + `,"chat":` + chat + `,"text":"/start","entities":[{"type":"bot_command","offset":0,"length":6}]}}`,
		`{"update_id":2,"message":{"message_id":2,"date":1,"from":` + user + `,"chat":` + chat + `,"text":"/help","entities":[{"type":"bot_command","offset":0,"length":5}]}}`,
		`{"update_id":3,"callback_query":{"id":"a","from":` + user + `,"chat_instance":"x","data":"help:dealer","message":` + oldMessage + `}}`,
		`{"update_id":4,"callback_query":{"id":"b","from":` + user + `,"chat_instance":"x","data":"help:main","message":` + oldMessage + `}}`,
	}}
	srv := httptest.NewServer(api)
	defer srv.Close()

	cfg := tg.DefaultConfig("123:TEST")
	cfg.API.BaseURL = srv.URL
	cfg.Mode = "polling"
	cfg.SetCommands = false

	bot, err := tg.New(cfg)
	if err != nil {
		t.Fatalf("new bot: %v", err)
	}

	h := NewHandler(tgforum.NewTracker(), nil, service.NewPlayerService(stubPlayers{}), nil, nil, nil)
	h.SetWebAppShortName("poker_bank")
	h.Register(bot)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bot.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("бот не остановился")
		}
	}()

	deadline := time.Now().Add(10 * time.Second)
	for len(api.byMethod("sendMessage")) < 2 || len(api.byMethod("editMessageText")) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("не дождались ответов: %+v", api.calls)
		}
		time.Sleep(20 * time.Millisecond)
	}

	const appURL = "https://t.me/testbot/poker_bank"

	sent := api.byMethod("sendMessage")
	start, help := sent[0].body, sent[1].body
	if s, _ := start["text"].(string); !strings.Contains(s, "Привет, Ivan &lt;b&gt;!") || start["parse_mode"] != "HTML" {
		t.Errorf("/start: %v", start)
	}
	if got := buttons(t, start); len(got) != 2 || got[0].URL != appURL || got[1].CallbackData != "help:main" {
		t.Errorf("/start кнопки: %+v", got)
	}
	if s, _ := help["text"].(string); !strings.Contains(s, "Банкир — команды") || help["parse_mode"] != "HTML" {
		t.Errorf("/help: %v", help)
	}
	if got := buttons(t, help); len(got) != len(helpSections)+1 || got[len(got)-1].URL != appURL {
		t.Errorf("/help кнопки: %+v", got)
	}

	edits := api.byMethod("editMessageText")
	dealer, back := edits[0].body, edits[1].body
	if s, _ := dealer["text"].(string); !strings.Contains(s, "/dealer_cashout @user &lt;фишки&gt;") ||
		dealer["parse_mode"] != "HTML" || dealer["message_id"] != float64(10) {
		t.Errorf("раздел «Дилер»: %v", dealer)
	}
	if got := buttons(t, dealer); got[len(got)-2].CallbackData != "help:main" || got[len(got)-1].URL != appURL {
		t.Errorf("раздел «Дилер» кнопки: %+v", got)
	}
	if s, _ := back["text"].(string); !strings.Contains(s, "Банкир — команды") {
		t.Errorf("«Назад»: %v", back)
	}
}

// buttons возвращает кнопки inline-клавиатуры запроса одним списком.
func buttons(t *testing.T, body map[string]any) []Button {
	t.Helper()
	raw, err := json.Marshal(body["reply_markup"])
	if err != nil {
		t.Fatal(err)
	}
	var m Markup
	if err = json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("reply_markup: %v (%s)", err, raw)
	}
	var out []Button
	for _, row := range m.InlineKeyboard {
		out = append(out, row...)
	}
	return out
}

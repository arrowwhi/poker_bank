// Package tgforum добавляет к Telegram-боту инфраструктуры
// (github.com/arrowwhi/infrastructure/telegram) поддержку топиков форума, не меняя библиотеку.
//
// В telegram.Update нет полей message_thread_id / is_topic_message / migrate_to_chat_id,
// поэтому Tracker читает их из сырого JSON на входе — до того, как бот декодирует
// обновление в свои типы — и запоминает по update_id:
//
//   - polling: Tracker.HTTPClient оборачивает транспорт клиента Bot API и разбирает ответы getUpdates;
//   - webhook: Tracker.Webhook оборачивает Bot.WebhookHandler и разбирает тело запроса.
//
// Обработчики читают сведения через Tracker.Info(c), Tracker.Middleware удаляет запись
// после обработки. Отправка в топик — Send (через Client.Call с message_thread_id).
package tgforum

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/arrowwhi/infrastructure/telegram"
)

// Info — то, что бот теряет при декодировании обновления.
type Info struct {
	// ThreadID — топик форума (0 — General или обычная группа).
	ThreadID int
	// MigrateToChatID — группа стала супергруппой с этим id.
	MigrateToChatID int64
}

// ttl — сколько хранится запись, если обновление так и не дошло до Middleware
// (например, не нашлось маршрута).
const ttl = 10 * time.Minute

type entry struct {
	info Info
	at   time.Time
}

// Tracker запоминает Info входящих обновлений по update_id.
type Tracker struct {
	now     func() time.Time
	entries map[int64]entry
	mu      sync.Mutex
}

// NewTracker создаёт пустой Tracker.
func NewTracker() *Tracker {
	return &Tracker{entries: make(map[int64]entry), now: time.Now}
}

// Info возвращает сведения о текущем обновлении.
func (t *Tracker) Info(c *telegram.Context) Info {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.entries[c.Update.UpdateID].info
}

// ThreadID — топик текущего обновления (0 — не топик).
func (t *Tracker) ThreadID(c *telegram.Context) int { return t.Info(c).ThreadID }

// Middleware забывает обновление после обработки.
func (t *Tracker) Middleware() telegram.Middleware {
	return func(next telegram.HandlerFunc) telegram.HandlerFunc {
		return func(c *telegram.Context) error {
			defer t.forget(c.Update.UpdateID)

			return next(c)
		}
	}
}

func (t *Tracker) forget(updateID int64) {
	t.mu.Lock()
	delete(t.entries, updateID)
	t.mu.Unlock()
}

// rawMessage — поля сообщения, которых нет в telegram.Message.
type rawMessage struct {
	MessageThreadID int   `json:"message_thread_id"`
	MigrateToChatID int64 `json:"migrate_to_chat_id"`
	IsTopicMessage  bool  `json:"is_topic_message"`
}

type rawUpdate struct {
	Message       *rawMessage `json:"message"`
	EditedMessage *rawMessage `json:"edited_message"`
	CallbackQuery *struct {
		Message *rawMessage `json:"message"`
	} `json:"callback_query"`
	UpdateID int64 `json:"update_id"`
}

func (u rawUpdate) message() *rawMessage {
	switch {
	case u.Message != nil:
		return u.Message
	case u.EditedMessage != nil:
		return u.EditedMessage
	case u.CallbackQuery != nil:
		return u.CallbackQuery.Message
	default:
		return nil
	}
}

// record разбирает обновления и запоминает непустые Info.
func (t *Tracker) record(updates []rawUpdate) {
	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()

	for id, e := range t.entries {
		if now.Sub(e.at) > ttl {
			delete(t.entries, id)
		}
	}

	for _, u := range updates {
		m := u.message()
		if m == nil {
			continue
		}

		var info Info
		// В обычных группах message_thread_id бывает у веток ответов — это не топик.
		if m.IsTopicMessage {
			info.ThreadID = m.MessageThreadID
		}

		info.MigrateToChatID = m.MigrateToChatID

		if info != (Info{}) {
			t.entries[u.UpdateID] = entry{info: info, at: now}
		}
	}
}

// Webhook оборачивает Bot.WebhookHandler: читает тело, запоминает Info и передаёт тело дальше.
func (t *Tracker) Webhook(next http.Handler) http.Handler {
	const maxBody = 1 << 20

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		var u rawUpdate
		if json.Unmarshal(body, &u) == nil {
			t.record([]rawUpdate{u})
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

// HTTPClient возвращает клиент для telegram.WithHTTPClient: он разбирает ответы getUpdates.
func (t *Tracker) HTTPClient(base *http.Client) *http.Client {
	if base == nil {
		base = &http.Client{}
	}

	rt := base.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}

	c := *base
	c.Transport = roundTripper{next: rt, t: t}

	return &c
}

type roundTripper struct {
	next http.RoundTripper
	t    *Tracker
}

func (rt roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := rt.next.RoundTrip(r)
	if err != nil || !strings.HasSuffix(r.URL.Path, "/getUpdates") || resp.StatusCode != http.StatusOK {
		return resp, err
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if err != nil {
		return nil, err
	}

	var envelope struct {
		Result []rawUpdate `json:"result"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		rt.t.record(envelope.Result)
	}

	resp.Body = io.NopCloser(bytes.NewReader(body))

	return resp, nil
}

// SendRequest — sendMessage с топиком; nil-поля не отправляются.
type SendRequest struct {
	ReplyMarkup      telegram.ReplyMarkup `json:"reply_markup,omitempty"`
	Text             string               `json:"text"`
	ParseMode        string               `json:"parse_mode,omitempty"`
	ChatID           int64                `json:"chat_id"`
	MessageThreadID  int                  `json:"message_thread_id,omitempty"`
	ReplyToMessageID int                  `json:"reply_to_message_id,omitempty"`
}

// Send отправляет сообщение (в топик, если задан MessageThreadID).
func Send(ctx context.Context, api *telegram.Client, req SendRequest) (telegram.Message, error) {
	var m telegram.Message
	err := api.Call(ctx, "sendMessage", req.ChatID, req, &m)

	return m, err
}

// ChatMember — ответ getChatMember (только нужные поля).
type ChatMember struct {
	Status   string `json:"status"`
	IsMember bool   `json:"is_member"`
}

// IsAdmin — администратор или создатель чата.
func (m ChatMember) IsAdmin() bool { return m.Status == "creator" || m.Status == "administrator" }

// InChat — пользователь сейчас состоит в чате.
func (m ChatMember) InChat() bool {
	switch m.Status {
	case "creator", "administrator", "member":
		return true
	case "restricted":
		return m.IsMember
	default:
		return false
	}
}

// GetChatMember вызывает getChatMember.
func GetChatMember(ctx context.Context, api *telegram.Client, chatID, userID int64) (ChatMember, error) {
	var m ChatMember
	err := api.Call(ctx, "getChatMember", 0, map[string]int64{"chat_id": chatID, "user_id": userID}, &m)

	return m, err
}

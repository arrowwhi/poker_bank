package telegram

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"
	"gopkg.in/telebot.v3"
)

// ctxTopicID is the telebot context key under which TopicGuard stores the
// resolved topic id for the current chat, so outgoing helpers can target it.
const ctxTopicID = "topic_id"

// hintInterval limits how often the "wrong topic" hint is sent per chat.
const hintInterval = time.Hour

// TopicGuard restricts the bot to a single forum topic when the chat has a
// binding. Messages outside the bound topic are not handled; instead the bot
// posts a throttled hint pointing users to the correct topic.
func (h *Handler) TopicGuard(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		// Callbacks are tied to messages already published inside the topic.
		if c.Callback() != nil {
			return next(c)
		}

		chat := c.Chat()
		// Private chats are never restricted.
		if chat == nil || chat.Type == telebot.ChatPrivate {
			return next(c)
		}

		msg := c.Message()
		if msg == nil {
			return next(c)
		}

		// /set_topic and /unset_topic must always pass so an admin can
		// (re)bind the chat from any topic.
		if cmd := commandName(msg.Text); cmd == "/set_topic" || cmd == "/unset_topic" {
			return next(c)
		}

		ctx := context.Background()
		binding, err := h.chatTopic.Get(ctx, chat.ID)
		if err != nil {
			h.log.Warn("topic guard: get binding", zap.Error(err), zap.Int64("chat_id", chat.ID))
			return next(c) // fail open: do not break the bot on a DB hiccup
		}
		// No binding — behave as before (work everywhere).
		if binding == nil {
			return next(c)
		}

		if int64(msg.ThreadID) == binding.TopicID {
			c.Set(ctxTopicID, int(binding.TopicID))
			return next(c)
		}

		// Outside the bound topic: do not handle, but nudge users only if it's a known command (throttled).
		cmd := commandName(msg.Text)
		if cmd != "" && h.botCommands[cmd] {
			h.maybeSendTopicHint(chat, msg)
		}
		return nil
	}
}

// maybeSendTopicHint replies to the offending message, in the same topic it was
// posted in, at most once per hour per chat.
func (h *Handler) maybeSendTopicHint(chat *telebot.Chat, msg *telebot.Message) {
	h.hintMu.Lock()
	last := h.lastHintAt[chat.ID]
	now := time.Now()
	if now.Sub(last) < hintInterval {
		h.hintMu.Unlock()
		return
	}
	h.lastHintAt[chat.ID] = now
	h.hintMu.Unlock()

	_, err := h.bot.Send(chat,
		"🃏 Я работаю только в выделенном топике этого чата. Пишите команды банкира туда.",
		&telebot.SendOptions{ThreadID: msg.ThreadID, ReplyTo: msg},
	)
	if err != nil {
		h.log.Warn("send topic hint", zap.Error(err), zap.Int64("chat_id", chat.ID))
	}
}

// sendToChat sends a message to the current chat, routing it into the bound
// topic when TopicGuard has resolved one for this update. Topic routing is
// merged into the caller-supplied SendOptions/ReplyMarkup rather than
// appended as a separate option, because telebot.extractOptions processes
// options in order and a later *SendOptions overwrites ReplyMarkup set by an
// earlier one — appending used to silently drop inline keyboards.
func (h *Handler) sendToChat(c telebot.Context, what interface{}, opts ...interface{}) (*telebot.Message, error) {
	tid, hasTopic := c.Get(ctxTopicID).(int)
	if !hasTopic || tid == 0 {
		return h.bot.Send(c.Chat(), what, opts...)
	}

	merged := &telebot.SendOptions{ThreadID: tid}
	for _, opt := range opts {
		switch v := opt.(type) {
		case *telebot.ReplyMarkup:
			merged.ReplyMarkup = v
		case *telebot.SendOptions:
			threadID := merged.ThreadID
			merged = v
			merged.ThreadID = threadID
		}
	}
	return h.bot.Send(c.Chat(), what, merged)
}

// commandName extracts the leading "/command" token from message text,
// stripping any "@botname" suffix. Returns "" if the text is not a command.
func commandName(text string) string {
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	tok := strings.Fields(text)[0]
	if i := strings.IndexByte(tok, '@'); i >= 0 {
		tok = tok[:i]
	}
	return tok
}

package telegram

import (
	"context"
	"strings"
	"time"

	"github.com/arrowwhi/infrastructure/observability/logger"

	tg "github.com/arrowwhi/infrastructure/telegram"

	"poker_bank/internal/pkg/tgforum"
)

// hintInterval limits how often the "wrong topic" hint is sent per chat.
const hintInterval = time.Hour

// TopicGuard restricts the bot to a single forum topic when the chat has a
// binding. Messages outside the bound topic are not handled; instead the bot
// posts a throttled hint pointing users to the correct topic.
func (h *Handler) TopicGuard(next tg.HandlerFunc) tg.HandlerFunc {
	return func(c *tg.Context) error {
		// Callbacks are tied to messages already published inside the topic.
		if c.Callback() != nil {
			return next(c)
		}

		chat := c.Update.Chat()
		// Private chats are never restricted.
		if chat == nil || chat.IsPrivate() {
			return next(c)
		}

		msg := c.Update.Message
		if msg == nil {
			return next(c)
		}

		// /set_topic and /unset_topic must always pass so an admin can
		// (re)bind the chat from any topic.
		if cmd := commandName(msg.Text); cmd == "/set_topic" || cmd == "/unset_topic" {
			return next(c)
		}

		binding, err := h.chatTopic.Get(c, chat.ID)
		if err != nil {
			logger.Warn(c).Err(err).Int64("chat_id", chat.ID).Msg("topic guard: get binding")
			return next(c) // fail open: do not break the bot on a DB hiccup
		}
		// No binding — behave as before (work everywhere).
		if binding == nil {
			return next(c)
		}

		threadID := h.forum.ThreadID(c)
		if int64(threadID) == binding.TopicID {
			return next(c)
		}

		// Outside the bound topic: do not handle, but nudge users only if it's a known command (throttled).
		cmd := commandName(msg.Text)
		if cmd != "" && h.botCommands[cmd] {
			h.maybeSendTopicHint(c, chat.ID, threadID, msg.MessageID)
		}
		return nil
	}
}

// maybeSendTopicHint replies to the offending message, in the same topic it was
// posted in, at most once per hour per chat.
func (h *Handler) maybeSendTopicHint(ctx context.Context, chatID int64, threadID, messageID int) {
	h.hintMu.Lock()
	last := h.lastHintAt[chatID]
	now := time.Now()
	if now.Sub(last) < hintInterval {
		h.hintMu.Unlock()
		return
	}
	h.lastHintAt[chatID] = now
	h.hintMu.Unlock()

	_, err := tgforum.Send(ctx, h.api, tgforum.SendRequest{
		ChatID:           chatID,
		Text:             "🃏 Я работаю только в выделенном топике этого чата. Пишите команды банкира туда.",
		MessageThreadID:  threadID,
		ReplyToMessageID: messageID,
	})
	if err != nil {
		logger.Warn(ctx).Err(err).Int64("chat_id", chatID).Msg("send topic hint")
	}
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

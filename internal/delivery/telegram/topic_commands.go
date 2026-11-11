package telegram

import (
	"context"
	"fmt"

	"github.com/arrowwhi/infrastructure/observability/logger"

	"poker_bank/internal/pkg/tgforum"
)

// handleSetTopic binds the chat to the topic the command was sent in.
// Only chat administrators may run it, and it must be invoked inside a forum topic.
func (h *Handler) handleSetTopic(c *Context) error {
	chat := c.Chat()
	if chat == nil || (chat.Type != "group" && chat.Type != "supergroup") {
		return c.Reply("❌ Команду нужно отправить в нужном топике группового чата.")
	}

	if !h.isChatAdmin(c) {
		return c.Reply("❌ Привязать топик может только администратор чата.")
	}

	topicID := int64(c.ThreadID())
	if topicID == 0 {
		return c.Reply("❌ Отправьте /set_topic внутри нужного топика (не в General).")
	}

	if err := h.chatTopic.Set(context.Background(), chat.ID, topicID, c.Sender().ID); err != nil {
		logger.Error(c).Err(err).Int64("chat_id", chat.ID).Msg("set chat topic")
		return c.Reply("Внутренняя ошибка. Попробуй позже.")
	}

	return c.Reply(fmt.Sprintf("✅ Бот теперь работает только в этом топике (id=%d).", topicID))
}

// handleUnsetTopic removes the chat-to-topic binding so the bot works everywhere again.
// Only chat administrators may run it.
func (h *Handler) handleUnsetTopic(c *Context) error {
	chat := c.Chat()
	if chat == nil || (chat.Type != "group" && chat.Type != "supergroup") {
		return c.Reply("❌ Команду нужно отправить в групповом чате.")
	}

	if !h.isChatAdmin(c) {
		return c.Reply("❌ Снять ограничение может только администратор чата.")
	}

	if err := h.chatTopic.Delete(context.Background(), chat.ID); err != nil {
		logger.Error(c).Err(err).Int64("chat_id", chat.ID).Msg("delete chat topic")
		return c.Reply("Внутренняя ошибка. Попробуй позже.")
	}

	return c.Reply("✅ Ограничение по топику снято — бот снова работает во всём чате.")
}

// isChatAdmin reports whether the message sender is an administrator or creator of the chat.
func (h *Handler) isChatAdmin(c *Context) bool {
	member, err := tgforum.GetChatMember(c, c.API(), c.Chat().ID, c.Sender().ID)
	if err != nil {
		logger.Warn(c).Err(err).Int64("chat_id", c.Chat().ID).Msg("check chat admin")
		return false
	}
	return member.IsAdmin()
}

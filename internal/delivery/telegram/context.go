package telegram

import (
	"strings"

	tg "github.com/arrowwhi/infrastructure/telegram"

	"poker_bank/internal/pkg/tgforum"
)

// Markup — inline-клавиатура сообщения.
type Markup = tg.InlineKeyboardMarkup

// Button — кнопка inline-клавиатуры.
type Button = tg.InlineKeyboardButton

// CallbackResponse — всплывающий ответ на нажатие кнопки.
type CallbackResponse struct {
	Text      string
	ShowAlert bool
}

// Context — контекст обновления бота инфраструктуры с хелперами обработчиков:
// ответы уходят в топик входящего сообщения (tgforum), аргументы команды — словами.
type Context struct {
	*tg.Context
	h *Handler
}

func (h *Handler) wrap(fn func(*Context) error) tg.HandlerFunc {
	return func(c *tg.Context) error { return fn(&Context{Context: c, h: h}) }
}

// Sender — автор обновления.
func (c *Context) Sender() *tg.User { return c.User() }

// Chat — чат обновления.
func (c *Context) Chat() *tg.Chat { return c.Update.Chat() }

// Args — аргументы команды, разбитые по пробелам.
func (c *Context) Args() []string { return strings.Fields(c.Context.Args()) }

// ThreadID — топик форума текущего обновления (0 — не топик).
func (c *Context) ThreadID() int { return c.h.forum.ThreadID(c.Context) }

// Reply отвечает на текущее сообщение в его топике.
func (c *Context) Reply(text string, markup ...*Markup) error {
	req := c.request(text, markup)
	if m := c.Update.Message; m != nil {
		req.ReplyToMessageID = m.MessageID
	}

	_, err := tgforum.Send(c, c.API(), req)

	return err
}

// Send отправляет сообщение в текущий чат и топик.
func (c *Context) Send(text string, markup ...*Markup) error {
	_, err := tgforum.Send(c, c.API(), c.request(text, markup))

	return err
}

func (c *Context) request(text string, markup []*Markup) tgforum.SendRequest {
	req := tgforum.SendRequest{ChatID: c.Chat().ID, Text: text, MessageThreadID: c.ThreadID()}
	if len(markup) > 0 && markup[0] != nil {
		req.ReplyMarkup = markup[0]
	}

	return req
}

// Edit меняет сообщение с нажатой кнопкой; без markup клавиатура убирается.
func (c *Context) Edit(text string, markup ...*Markup) error {
	var m *Markup
	if len(markup) > 0 {
		m = markup[0]
	}

	return c.Context.Edit(text, m, "")
}

// Respond отвечает на нажатие кнопки.
func (c *Context) Respond(resp ...*CallbackResponse) error {
	if len(resp) == 0 || resp[0] == nil {
		return c.Answer("", false)
	}

	return c.Answer(resp[0].Text, resp[0].ShowAlert)
}

// Command topicfinder is a throwaway helper that prints the chat_id and
// message_thread_id (topic) of every incoming message, so you can discover the
// values needed to bind the bot to a topic.
//
// Usage:
//
//	go run ./cmd/topicfinder
//
// It reuses the project's config (.env -> TELEGRAM_TOKEN). IMPORTANT: stop the
// main bot first (or use a separate token) — Telegram getUpdates long polling
// allows only one consumer per token at a time.
package main

import (
	"fmt"
	"log"
	"time"

	"gopkg.in/telebot.v3"

	"poker_bank/internal/config"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	b, err := telebot.NewBot(telebot.Settings{
		Token:  cfg.TelegramToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatalf("create bot: %v", err)
	}

	report := func(c telebot.Context) error {
		m := c.Message()
		if m == nil {
			return nil
		}
		chat := c.Chat()

		title := ""
		if chat != nil {
			title = chat.Title
			if title == "" {
				title = chat.FirstName
			}
		}

		topicName := ""
		switch {
		case m.TopicCreated != nil:
			topicName = m.TopicCreated.Name
		case m.TopicEdited != nil:
			topicName = m.TopicEdited.Name
		}

		fmt.Printf("chat_id=%d type=%s title=%q message_thread_id=%d topic_name=%q\n",
			chatID(chat), chatType(chat), title, m.ThreadID, topicName)
		return nil
	}

	b.Handle(telebot.OnText, report)
	b.Handle(telebot.OnMedia, report)

	fmt.Println("topicfinder started — send messages in your topics, press Ctrl+C to stop")
	b.Start()
}

func chatID(c *telebot.Chat) int64 {
	if c == nil {
		return 0
	}
	return c.ID
}

func chatType(c *telebot.Chat) telebot.ChatType {
	if c == nil {
		return ""
	}
	return c.Type
}

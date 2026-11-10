package telegram

import (
	"context"
	"fmt"
	"html"
	"strings"

	tg "github.com/arrowwhi/infrastructure/telegram"

	"poker_bank/internal/pkg/tgforum"
)

// Справка: короткое сообщение и разделы на инлайн-кнопках. Нажатие правит то же
// сообщение; состояние не хранится — раздел целиком закодирован в callback data
// (help:<раздел>), поэтому кнопки работают и после рестарта.

const (
	helpPrefix = "help"
	helpMain   = "main"
)

// helpSection — раздел справки. body — HTML: угловые скобки аргументов экранированы.
type helpSection struct {
	key   string
	label string
	body  string
}

// helpSections — все команды бота по разделам. Тест следит, что здесь есть каждая
// зарегистрированная команда.
var helpSections = []helpSection{
	{
		key:   "game",
		label: "🎰 Игра",
		body: "/newgame — начать игру (спрошу ставки по шагам)\n" +
			"/newgame 1000 1000 500 500 — сразу: buy-in ₽, фишки, rebuy ₽, фишки\n" +
			"/status — банк, участники, запросы\n" +
			"/endgame — завершить и посчитать переводы · дилер\n" +
			"/endgame_force — завершить, если банк не сошёлся · дилер\n" +
			"/cancel — отменить игру без расчёта · дилер",
	},
	{
		key:   "player",
		label: "🙋 Игрок",
		body: "/join — войти в игру (buy-in)\n" +
			"/rebuy — докупиться\n" +
			"/cashout &lt;фишки&gt; — выйти с фишками\n" +
			"/me — мои показатели в игре\n\n" +
			"Запросы подтверждает дилер.",
	},
	{
		key:   "dealer",
		label: "🎩 Дилер",
		body: "/dealer_join @user — посадить игрока (buy-in)\n" +
			"/dealer_rebuy @user — rebuy игроку\n" +
			"/dealer_cashout @user &lt;фишки&gt; — cash-out игрока\n" +
			"/undo [N] — отменить последние N записей\n" +
			"/transfer_dealer @user — передать роль дилера\n\n" +
			"Запросы игроков дилер подтверждает кнопками под сообщением.",
	},
	{
		key:   "history",
		label: "📊 История",
		body: "/history [N] — последние игры чата\n" +
			"/game &lt;id&gt; — итоги, переводы и лог игры\n" +
			"/stats — топ игроков чата\n" +
			"/stats @user — статистика игрока",
	},
	{
		key:   "admin",
		label: "⚙️ Админу чата",
		body: "/set_topic — работать только в этом топике\n" +
			"/unset_topic — снять привязку к топику\n" +
			"/admin_cancel — отменить игру, если дилер молчит больше 6 ч",
	},
}

// MenuCommands — короткое меню команд Telegram (setMyCommands). Полный список — в /help.
func MenuCommands() []tg.BotCommand {
	return []tg.BotCommand{
		{Command: "newgame", Description: "Начать игру"},
		{Command: "join", Description: "Войти в игру"},
		{Command: "rebuy", Description: "Докупиться"},
		{Command: "cashout", Description: "Выйти: /cashout <фишки>"},
		{Command: "status", Description: "Банк и участники"},
		{Command: "me", Description: "Мои показатели"},
		{Command: "endgame", Description: "Завершить игру (дилер)"},
		{Command: "history", Description: "Последние игры"},
		{Command: "stats", Description: "Топ игроков"},
		{Command: "help", Description: "Все команды"},
	}
}

func findHelpSection(key string) *helpSection {
	for i := range helpSections {
		if helpSections[i].key == key {
			return &helpSections[i]
		}
	}
	return nil
}

func helpData(key string) string { return helpPrefix + ":" + key }

// helpMarkup — клавиатура справки: разделы по два в ряд (текущий помечен), внизу
// приложение, а в разделе ещё и «Назад».
func (h *Handler) helpMarkup(current string, chatID int64) *Markup {
	var rows [][]Button
	var row []Button
	for _, s := range helpSections {
		label := s.label
		if s.key == current {
			label = "• " + label + " •"
		}
		row = append(row, Button{Text: label, CallbackData: helpData(s.key)})
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}

	link := h.webAppLink(chatStartParam(chatID))
	if current == helpMain {
		if link != "" {
			rows = append(rows, []Button{{Text: btnOpenApp, URL: link}})
		}
		return &Markup{InlineKeyboard: rows}
	}

	last := []Button{{Text: "‹ Назад", CallbackData: helpData(helpMain)}}
	if link != "" {
		last = append(last, Button{Text: btnAppShort, URL: link})
	}
	return &Markup{InlineKeyboard: append(rows, last)}
}

// helpText возвращает HTML-текст главного экрана справки или раздела.
func (h *Handler) helpText(ctx context.Context, key string, chatID int64) string {
	if s := findHelpSection(key); s != nil {
		return "<b>" + s.label + "</b>\n\n" + s.body
	}

	sb := strings.Builder{}
	sb.WriteString("<b>🃏 Банкир — команды</b>\n\n")
	sb.WriteString(h.helpGameLine(ctx, chatID))
	sb.WriteString("\n\nВыберите раздел 👇")
	if h.webAppLink("") != "" {
		sb.WriteString("\nВсё то же есть в приложении — без команд.")
	}
	return sb.String()
}

// helpGameLine — что сейчас происходит в чате. Строка одинакова для всех участников:
// сообщение общее и кнопки под ним может нажимать любой.
func (h *Handler) helpGameLine(ctx context.Context, chatID int64) string {
	if chatID >= 0 {
		return "Команды работают в чате, где идёт игра."
	}
	game, err := h.game.GetActiveGame(ctx, chatID)
	if err != nil || game == nil {
		return "⚪️ Игры сейчас нет — начните с /newgame"
	}
	return fmt.Sprintf("🟢 Идёт игра #%d, дилер %s", game.ID, html.EscapeString(h.name(ctx, game.DealerTgID)))
}

func (h *Handler) handleHelp(c *Context) error {
	chatID := c.Chat().ID
	return c.replyHTML(h.helpText(context.Background(), helpMain, chatID), h.helpMarkup(helpMain, chatID))
}

// handleHelpCallback переключает раздел справки, правя сообщение с кнопками.
func (h *Handler) handleHelpCallback(c *Context, parts []string) error {
	chat := c.Chat()
	if chat == nil {
		return c.Respond()
	}

	key := parts[1]
	if findHelpSection(key) == nil {
		key = helpMain
	}
	if err := c.Context.Edit(h.helpText(context.Background(), key, chat.ID), h.helpMarkup(key, chat.ID), tg.ParseModeHTML); err != nil {
		return err
	}
	return c.Respond()
}

// handleStart отправляет приветствие с кнопками приложения и справки.
func (h *Handler) handleStart(c *Context) error {
	chatID := c.Chat().ID

	steps := []string{
		"Добавьте меня в чат, где играете",
		"Запустите игру — /newgame или в приложении",
		"Игроки входят через /join, дилер подтверждает",
	}
	if chatID < 0 {
		steps = steps[1:] // в группе бот уже добавлен
	}

	sb := strings.Builder{}
	fmt.Fprintf(&sb, "👋 Привет, %s!\n\n", html.EscapeString(playerName(c.Sender(), c.Sender().ID)))
	sb.WriteString("Я Банкир 🃏 — веду банк домашнего покера: записываю buy-in, rebuy и cash-out, " +
		"а в конце считаю, кто кому сколько переводит.\n\n")
	sb.WriteString("<b>Как начать</b>\n")
	for i, s := range steps {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, s)
	}

	markup := h.withAppButton(nil, btnOpenApp, chatStartParam(chatID))
	markup = &Markup{InlineKeyboard: append(markupRows(markup), []Button{
		{Text: "📖 Команды", CallbackData: helpData(helpMain)},
	})}
	return c.replyHTML(sb.String(), markup)
}

func markupRows(m *Markup) [][]Button {
	if m == nil {
		return nil
	}
	return m.InlineKeyboard
}

// replyHTML отвечает на текущее сообщение текстом с HTML-разметкой.
func (c *Context) replyHTML(text string, markup *Markup) error {
	req := c.request(text, []*Markup{markup})
	req.ParseMode = tg.ParseModeHTML
	if m := c.Update.Message; m != nil {
		req.ReplyToMessageID = m.MessageID
	}

	_, err := tgforum.Send(c, c.API(), req)

	return err
}

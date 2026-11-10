package telegram

import (
	"strconv"
	"strings"
)

// Ссылка на Mini App — https://t.me/<bot>/<short name>[?startapp=<param>]. Собирается только
// здесь: username бот узнаёт сам (getMe), short name приходит из конфига (WEBAPP_SHORT_NAME).
// В группах это URL-кнопка: кнопки web_app Telegram разрешает только в личке.

// Подписи кнопок, открывающих Mini App.
const (
	btnOpenApp    = "📱 Открыть приложение"
	btnOpenTable  = "📱 Открыть стол"
	btnAppResults = "📱 Итоги в приложении"
	btnAppShort   = "📱 Приложение"
)

// SetWebAppShortName задаёт short name Mini App из BotFather; пустой — кнопки приложения не ставятся.
func (h *Handler) SetWebAppShortName(name string) {
	h.webAppShortName = strings.TrimSpace(name)
}

// webAppLink возвращает ссылку на Mini App или "", если она не настроена
// либо бот ещё не узнал свой username.
func (h *Handler) webAppLink(startParam string) string {
	if h.webAppShortName == "" || h.botUsername == nil {
		return ""
	}
	username := h.botUsername()
	if username == "" {
		return ""
	}

	link := "https://t.me/" + username + "/" + h.webAppShortName
	if startParam != "" {
		link += "?startapp=" + startParam
	}
	return link
}

// chatStartParam — startapp для группы: приложение откроет активную игру этого чата
// (или создание новой). Для лички параметра нет — откроется главный экран.
func chatStartParam(chatID int64) string {
	if chatID >= 0 {
		return ""
	}
	return strconv.FormatInt(chatID, 10)
}

// gameStartParam — startapp для итогов завершённой игры.
func gameStartParam(gameID int64) string {
	return "g" + strconv.FormatInt(gameID, 10)
}

// withAppButton дописывает к клавиатуре строку с кнопкой Mini App. Если ссылка
// не настроена, клавиатура возвращается как есть (в том числе nil).
func (h *Handler) withAppButton(m *Markup, text, startParam string) *Markup {
	link := h.webAppLink(startParam)
	if link == "" {
		return m
	}

	rows := [][]Button{}
	if m != nil {
		rows = append(rows, m.InlineKeyboard...)
	}
	return &Markup{InlineKeyboard: append(rows, []Button{{Text: text, URL: link}})}
}

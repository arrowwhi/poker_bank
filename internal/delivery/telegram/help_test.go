package telegram

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func testHandler(username, shortName string) *Handler {
	h := NewHandler(nil, nil, nil, nil, nil, nil, nil)
	h.SetWebAppShortName(shortName)
	if username != "" {
		h.botUsername = func() string { return username }
	}
	return h
}

func TestWebAppLink(t *testing.T) {
	h := testHandler("our_cool_bank_bot", " poker_bank ")

	cases := map[string]string{
		"":                         "https://t.me/our_cool_bank_bot/poker_bank",
		chatStartParam(-100123456): "https://t.me/our_cool_bank_bot/poker_bank?startapp=-100123456",
		gameStartParam(42):         "https://t.me/our_cool_bank_bot/poker_bank?startapp=g42",
	}
	for param, want := range cases {
		if got := h.webAppLink(param); got != want {
			t.Errorf("webAppLink(%q) = %q, want %q", param, got, want)
		}
	}

	if p := chatStartParam(777); p != "" {
		t.Errorf("личка не должна давать startapp, получили %q", p)
	}
}

func TestWebAppLinkNotConfigured(t *testing.T) {
	for name, h := range map[string]*Handler{
		"нет short name":  testHandler("bot", ""),
		"username пустой": testHandler("", "poker_bank"),
		"бот не запущен": func() *Handler {
			h := testHandler("", "poker_bank")
			h.botUsername = func() string { return "" }
			return h
		}(),
		"ничего не задано": testHandler("", ""),
	} {
		if got := h.webAppLink(""); got != "" {
			t.Errorf("%s: webAppLink = %q, want пусто", name, got)
		}
		orig := &Markup{InlineKeyboard: [][]Button{{{Text: "x", CallbackData: "x"}}}}
		if got := h.withAppButton(orig, btnOpenApp, ""); got != orig {
			t.Errorf("%s: клавиатура без ссылки должна остаться прежней", name)
		}
		if got := h.withAppButton(nil, btnOpenApp, ""); got != nil {
			t.Errorf("%s: без ссылки и без клавиатуры ждём nil", name)
		}
	}
}

func TestWithAppButtonAppendsRow(t *testing.T) {
	h := testHandler("bot", "app")
	orig := &Markup{InlineKeyboard: [][]Button{{{Text: "x", CallbackData: "x"}}}}

	got := h.withAppButton(orig, btnOpenTable, "-5")
	if len(orig.InlineKeyboard) != 1 {
		t.Fatal("исходная клавиатура не должна меняться")
	}
	if len(got.InlineKeyboard) != 2 {
		t.Fatalf("ждём 2 ряда, получили %d", len(got.InlineKeyboard))
	}
	b := got.InlineKeyboard[1][0]
	if b.Text != btnOpenTable || b.URL != "https://t.me/bot/app?startapp=-5" || b.CallbackData != "" {
		t.Errorf("кнопка приложения: %+v", b)
	}
}

// Каждая зарегистрированная команда должна быть видна в каком-то разделе справки.
func TestHelpCoversAllCommands(t *testing.T) {
	h := testHandler("bot", "app")
	ctx := context.Background()

	var all strings.Builder
	for _, s := range helpSections {
		all.WriteString(h.helpText(ctx, s.key, -1))
		all.WriteByte('\n')
	}
	text := all.String()

	for cmd := range h.botCommands {
		if cmd == "/start" || cmd == "/help" {
			continue
		}
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(cmd) + `( |$)`).MatchString(text) {
			t.Errorf("команды %s нет в справке", cmd)
		}
	}
}

// Разделы — HTML: из тегов допустим только <b>, скобки аргументов экранированы.
func TestHelpSectionsAreValidHTML(t *testing.T) {
	h := testHandler("bot", "app")
	tag := regexp.MustCompile(`</?b>`)
	for _, s := range helpSections {
		rest := tag.ReplaceAllString(h.helpText(context.Background(), s.key, -1), "")
		if strings.ContainsAny(rest, "<>") {
			t.Errorf("раздел %s: неэкранированные угловые скобки", s.key)
		}
	}
}

// Клавиатура строится только из callback data — без состояния в памяти,
// поэтому кнопки старых сообщений работают и после рестарта.
func TestHelpMarkup(t *testing.T) {
	h := testHandler("bot", "app")

	main := h.helpMarkup(helpMain, -100)
	wantRows := (len(helpSections)+1)/2 + 1
	if len(main.InlineKeyboard) != wantRows {
		t.Fatalf("главный экран: %d рядов, want %d", len(main.InlineKeyboard), wantRows)
	}
	last := main.InlineKeyboard[wantRows-1]
	if len(last) != 1 || last[0].URL != "https://t.me/bot/app?startapp=-100" {
		t.Errorf("главный экран, последний ряд: %+v", last)
	}

	for _, s := range helpSections {
		m := h.helpMarkup(s.key, -100)
		marked := 0
		for _, row := range m.InlineKeyboard {
			for _, b := range row {
				if len(b.CallbackData) > 64 {
					t.Errorf("callback data длиннее 64 байт: %q", b.CallbackData)
				}
				if strings.HasPrefix(b.Text, "• ") {
					marked++
					if b.CallbackData != helpData(s.key) {
						t.Errorf("раздел %s: помечена чужая кнопка %q", s.key, b.CallbackData)
					}
				}
			}
		}
		if marked != 1 {
			t.Errorf("раздел %s: помечено кнопок %d, want 1", s.key, marked)
		}
		back := m.InlineKeyboard[len(m.InlineKeyboard)-1]
		if len(back) != 2 || back[0].CallbackData != helpData(helpMain) || back[1].URL == "" {
			t.Errorf("раздел %s: ряд «Назад»: %+v", s.key, back)
		}
	}

	// Без ссылки на приложение остаётся только «Назад».
	noApp := testHandler("", "")
	back := noApp.helpMarkup("game", -100).InlineKeyboard
	if l := back[len(back)-1]; len(l) != 1 || l[0].CallbackData != helpData(helpMain) {
		t.Errorf("без приложения: %+v", l)
	}
}

func TestHelpUnknownSectionFallsBackToMain(t *testing.T) {
	h := testHandler("bot", "app")
	// chatID >= 0 — личка: главный экран не ходит в БД.
	got := h.helpText(context.Background(), "nope", 1)
	if !strings.Contains(got, "Банкир — команды") {
		t.Errorf("ждём главный экран, получили %q", got)
	}
}

// Меню Telegram — подмножество зарегистрированных команд.
func TestMenuCommandsAreRegistered(t *testing.T) {
	h := testHandler("bot", "app")
	seen := map[string]bool{}
	for _, c := range MenuCommands() {
		if !h.botCommands["/"+c.Command] {
			t.Errorf("команда меню /%s не зарегистрирована", c.Command)
		}
		if seen[c.Command] {
			t.Errorf("команда меню /%s повторяется", c.Command)
		}
		seen[c.Command] = true
		if c.Description == "" || len([]rune(c.Description)) > 256 {
			t.Errorf("описание /%s: %q", c.Command, c.Description)
		}
	}
}

func TestSettleMarkupKeepsAppButton(t *testing.T) {
	h := testHandler("bot", "app")
	if m := h.settleMarkup(7, nil); m == nil || len(m.InlineKeyboard) != 1 || m.InlineKeyboard[0][0].URL != "https://t.me/bot/app?startapp=g7" {
		t.Errorf("без переводов ждём только кнопку итогов: %+v", m)
	}
	if m := testHandler("", "").settleMarkup(7, nil); m != nil {
		t.Errorf("без переводов и без приложения ждём nil: %+v", m)
	}
}

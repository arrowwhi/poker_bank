package telegram

import (
	"context"
	"errors"
	"strings"

	"gopkg.in/telebot.v3"

	"poker_bank/internal/service"
)

// role описывает роль пользователя относительно активной игры в чате.
type role int

const (
	roleNoGame   role = iota // нет активной игры
	roleOutsider             // есть игра, пользователь не участвует
	roleOut                  // участвовал, сделал кэшаут
	rolePlayer               // активный участник
	roleDealer               // дилер текущей игры
)

type commandInfo struct {
	cmd   string
	desc  string
	roles []role // какие роли видят эту команду
}

// botCommands — полный список команд с описанием и ролями.
var botCommands = []commandInfo{
	// Нет активной игры
	{"/newgame", "начать новую игру", []role{roleNoGame}},

	// Управление игрой (дилер)
	{"/endgame", "завершить игру", []role{roleDealer}},
	{"/endgame_force", "завершить с расхождением банка", []role{roleDealer}},
	{"/cancel", "отменить игру", []role{roleDealer}},
	{"/transfer_dealer @user", "передать роль дилера", []role{roleDealer}},
	{"/dealer_join @user", "добавить игрока без подтверждения", []role{roleDealer}},
	{"/dealer_rebuy @user", "rebuy для игрока без подтверждения", []role{roleDealer}},
	{"/dealer_cashout @user <chips>", "кэшаут для игрока", []role{roleDealer}},
	{"/undo [N]", "отменить последние N записей (по умолч. 1)", []role{roleDealer}},

	// Команды игрока
	{"/join", "войти в игру (ожидает подтверждения дилера)", []role{roleOutsider, roleOut, roleDealer}},
	{"/rebuy", "запросить rebuy (ожидает подтверждения дилера)", []role{rolePlayer}},
	{"/cashout <chips>", "запросить выход (ожидает подтверждения дилера)", []role{rolePlayer}},

	// Информация
	{"/status", "состояние текущей игры", []role{roleDealer, rolePlayer, roleOut, roleOutsider}},
	{"/me", "мои показатели в текущей игре", []role{roleDealer, rolePlayer, roleOut}},
	{"/history [N]", "последние N завершённых игр", []role{roleNoGame, roleDealer, rolePlayer, roleOut, roleOutsider}},
	{"/stats [@user]", "статистика чата или конкретного игрока", []role{roleNoGame, roleDealer, rolePlayer, roleOut, roleOutsider}},
}

var roleLabels = map[role]string{
	roleNoGame:   "🎮 Нет активной игры",
	roleOutsider: "👤 Вы не в игре",
	roleOut:      "⬛ Вы вышли из игры",
	rolePlayer:   "🟢 Вы в игре",
	roleDealer:   "🎩 Вы дилер",
}

func (h *Handler) handleHelp(c telebot.Context) error {
	ctx := context.Background()

	r := h.resolveRole(ctx, c.Chat().ID, c.Sender().ID)

	sb := strings.Builder{}
	sb.WriteString(roleLabels[r] + "\n\n")

	for _, cmd := range botCommands {
		if hasRole(cmd.roles, r) {
			sb.WriteString(cmd.cmd + " — " + cmd.desc + "\n")
		}
	}

	return c.Reply(sb.String())
}

// resolveRole определяет роль пользователя в текущем чате.
func (h *Handler) resolveRole(ctx context.Context, chatID, senderID int64) role {
	game, err := h.game.GetActiveGame(ctx, chatID)
	if errors.Is(err, service.ErrNoActiveGame) || err != nil {
		return roleNoGame
	}
	if game.DealerTgID == senderID {
		return roleDealer
	}
	p, err := h.game.GetParticipant(ctx, game.ID, senderID)
	if err != nil {
		return roleOutsider
	}
	if p.IsActive {
		return rolePlayer
	}
	return roleOut
}

func hasRole(roles []role, r role) bool {
	for _, rr := range roles {
		if rr == r {
			return true
		}
	}
	return false
}

// handleStart отправляет приветствие с подсказкой про /help.
func (h *Handler) handleStart(c telebot.Context) error {
	return c.Reply("Привет, " + playerName(c.Sender(), c.Sender().ID) + "! Я бот-банкир для покера.\nИспользуй /help для списка доступных команд.")
}

package http

import (
	"context"
	"strings"
	"time"

	"poker_bank/internal/domain"
	"poker_bank/internal/service"
)

// DTO Mini App: имена и форма полей совпадают с webapp/src/api/types.ts.

type userDTO struct {
	ID          int64   `json:"id"`
	Username    *string `json:"username"`
	DisplayName string  `json:"display_name"`
	PhotoURL    *string `json:"photo_url"`
}

type busyRefDTO struct {
	GameID    int64  `json:"game_id"`
	ChatID    int64  `json:"chat_id"`
	ChatTitle string `json:"chat_title"`
}

type chatMemberDTO struct {
	userDTO
	BusyIn *busyRefDTO `json:"busy_in"`
}

type rateDTO struct {
	BuyInRub   int `json:"buy_in_rub"`
	BuyInChips int `json:"buy_in_chips"`
	RebuyRub   int `json:"rebuy_rub"`
	RebuyChips int `json:"rebuy_chips"`
}

type chatActiveGameDTO struct {
	ID     int64   `json:"id"`
	Dealer userDTO `json:"dealer"`
}

type chatDTO struct {
	ID           int64              `json:"id"`
	Title        string             `json:"title"`
	MembersCount int                `json:"members_count"`
	ActiveGame   *chatActiveGameDTO `json:"active_game"`
	LastRate     *rateDTO           `json:"last_rate"`
}

type gameSummaryDTO struct {
	ID            int64      `json:"id"`
	Number        int        `json:"number"`
	ChatID        int64      `json:"chat_id"`
	ChatTitle     string     `json:"chat_title"`
	Status        string     `json:"status"`
	Dealer        userDTO    `json:"dealer"`
	Rate          rateDTO    `json:"rate"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at"`
	PlayersTotal  int        `json:"players_total"`
	PlayersActive int        `json:"players_active"`
	BankRub       int        `json:"bank_rub"`
	MyRole        string     `json:"my_role"`
}

type participantDTO struct {
	User          userDTO   `json:"user"`
	IsActive      bool      `json:"is_active"`
	BuyInCount    int       `json:"buy_in_count"`
	RebuyCount    int       `json:"rebuy_count"`
	TotalInRub    int       `json:"total_in_rub"`
	TotalOutRub   int       `json:"total_out_rub"`
	TotalOutChips int       `json:"total_out_chips"`
	NetRub        int       `json:"net_rub"`
	JoinedAt      time.Time `json:"joined_at"`
}

type pendingDTO struct {
	ID          int64      `json:"id"`
	GameID      int64      `json:"game_id"`
	Type        string     `json:"type"`
	Requester   userDTO    `json:"requester"`
	Target      userDTO    `json:"target"`
	AmountRub   int        `json:"amount_rub"`
	AmountChips int        `json:"amount_chips"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	ResolvedBy  *userDTO   `json:"resolved_by"`
}

type ledgerDTO struct {
	ID          int64     `json:"id"`
	Type        string    `json:"type"`
	Player      userDTO   `json:"player"`
	AmountRub   int       `json:"amount_rub"`
	AmountChips int       `json:"amount_chips"`
	CreatedBy   userDTO   `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	IsVoid      bool      `json:"is_void"`
}

type resultDTO struct {
	User          userDTO `json:"user"`
	BuyInCount    int     `json:"buy_in_count"`
	RebuyCount    int     `json:"rebuy_count"`
	TotalInRub    int     `json:"total_in_rub"`
	TotalOutRub   int     `json:"total_out_rub"`
	TotalOutChips int     `json:"total_out_chips"`
	NetRub        int     `json:"net_rub"`
}

type settlementDTO struct {
	ID         int64      `json:"id"`
	GameID     int64      `json:"game_id"`
	From       userDTO    `json:"from"`
	To         userDTO    `json:"to"`
	AmountRub  int        `json:"amount_rub"`
	IsPaid     bool       `json:"is_paid"`
	PaidAt     *time.Time `json:"paid_at"`
	IsReceived bool       `json:"is_received"`
	ReceivedAt *time.Time `json:"received_at"`
}

type gameDetailDTO struct {
	gameSummaryDTO
	Participants []participantDTO `json:"participants"`
	Pending      []pendingDTO     `json:"pending"`
	MyRequests   []pendingDTO     `json:"my_requests"`
	Ledger       []ledgerDTO      `json:"ledger"`
	BankDeltaRub int              `json:"bank_delta_rub"`
	Results      []resultDTO      `json:"results"`
	Settlements  []settlementDTO  `json:"settlements"`
	MyBusyIn     *busyRefDTO      `json:"my_busy_in"`
}

type meDTO struct {
	User        userDTO         `json:"user"`
	MemberSince time.Time       `json:"member_since"`
	GamesPlayed int             `json:"games_played"`
	TotalNetRub int             `json:"total_net_rub"`
	ActiveGame  *gameSummaryDTO `json:"active_game"`
}

type historyItemDTO struct {
	GameID      int64     `json:"game_id"`
	GameNumber  int       `json:"game_number"`
	ChatID      int64     `json:"chat_id"`
	ChatTitle   string    `json:"chat_title"`
	EndedAt     time.Time `json:"ended_at"`
	BuyInCount  int       `json:"buy_in_count"`
	RebuyCount  int       `json:"rebuy_count"`
	TotalInRub  int       `json:"total_in_rub"`
	TotalOutRub int       `json:"total_out_rub"`
	NetRub      int       `json:"net_rub"`
}

type openDebtDTO struct {
	settlementDTO
	ChatTitle   string    `json:"chat_title"`
	GameNumber  int       `json:"game_number"`
	GameEndedAt time.Time `json:"game_ended_at"`
	Direction   string    `json:"direction"`
}

type playerStatsDTO struct {
	User              userDTO          `json:"user"`
	MemberSince       time.Time        `json:"member_since"`
	ChatID            *int64           `json:"chat_id"`
	GamesPlayed       int              `json:"games_played"`
	TotalNetRub       int              `json:"total_net_rub"`
	WinRate           float64          `json:"win_rate"`
	AvgNetRub         int              `json:"avg_net_rub"`
	RebuyTotal        int              `json:"rebuy_total"`
	LeaderboardPlace  *int             `json:"leaderboard_place"`
	LeaderboardTotal  *int             `json:"leaderboard_total"`
	LeaderboardChatID *int64           `json:"leaderboard_chat_id"`
	History           []historyItemDTO `json:"history"`
	OpenDebts         []openDebtDTO    `json:"open_debts"`
}

type leaderboardEntryDTO struct {
	Place       int     `json:"place"`
	User        userDTO `json:"user"`
	GamesPlayed int     `json:"games_played"`
	TotalNetRub int     `json:"total_net_rub"`
	WinRate     float64 `json:"win_rate"`
}

type leaderboardDTO struct {
	ChatID    int64                 `json:"chat_id"`
	ChatTitle string                `json:"chat_title"`
	Period    string                `json:"period"`
	Entries   []leaderboardEntryDTO `json:"entries"`
}

// builder собирает DTO в рамках одного запроса, кешируя игроков, группы и номера игр.
type builder struct {
	games   *service.GameService
	players *service.PlayerService
	me      int64

	users   map[int64]userDTO
	titles  map[int64]string
	numbers map[int64]int
}

func (a *api) newBuilder(me int64) *builder {
	return &builder{
		games: a.games, players: a.players, me: me,
		users: map[int64]userDTO{}, titles: map[int64]string{}, numbers: map[int64]int{},
	}
}

func (b *builder) user(ctx context.Context, id int64) userDTO {
	if u, ok := b.users[id]; ok {
		return u
	}
	u := userDTO{ID: id, DisplayName: "id" + itoa(id)}
	if p, err := b.players.GetByID(ctx, id); err == nil {
		if p.Username != "" {
			name := p.Username
			u.Username = &name
		}
		if dn := strings.TrimSpace(p.DisplayName); dn != "" {
			u.DisplayName = dn
		} else if p.Username != "" {
			u.DisplayName = p.Username
		}
	}
	b.users[id] = u
	return u
}

func (b *builder) chatTitle(ctx context.Context, chatID int64) string {
	if t, ok := b.titles[chatID]; ok {
		return t
	}
	t := ""
	if c, err := b.games.Chat(ctx, chatID); err == nil && c != nil {
		t = c.Title
	}
	if t == "" {
		t = "Группа"
	}
	b.titles[chatID] = t
	return t
}

func (b *builder) number(ctx context.Context, g *domain.Game) int {
	if n, ok := b.numbers[g.ID]; ok {
		return n
	}
	n, err := b.games.GameNumber(ctx, g)
	if err != nil {
		n = 0
	}
	b.numbers[g.ID] = n
	return n
}

func rateOf(g *domain.Game) rateDTO {
	return rateDTO{BuyInRub: g.BuyInRub, BuyInChips: g.BuyInChips, RebuyRub: g.RebuyRub, RebuyChips: g.RebuyChips}
}

// aggregate считает участников по неотменённым записям ledger.
func aggregate(participants []domain.Participant, entries []domain.LedgerEntry) map[int64]*participantDTO {
	m := make(map[int64]*participantDTO, len(participants))
	for _, p := range participants {
		m[p.PlayerTgID] = &participantDTO{IsActive: p.IsActive, JoinedAt: p.JoinedAt}
	}
	for _, e := range entries {
		if e.IsVoid {
			continue
		}
		p, ok := m[e.PlayerTgID]
		if !ok {
			continue // buy-in отменён через /undo — игрока нет среди участников
		}
		switch e.Type {
		case domain.LedgerBuyIn:
			p.BuyInCount++
			p.TotalInRub += e.AmountRub
		case domain.LedgerRebuy:
			p.RebuyCount++
			p.TotalInRub += e.AmountRub
		case domain.LedgerCashOut:
			p.TotalOutRub += e.AmountRub
			p.TotalOutChips += e.AmountChips
		}
	}
	for _, p := range m {
		p.NetRub = p.TotalOutRub - p.TotalInRub
	}
	return m
}

func role(g *domain.Game, me int64, part *participantDTO) string {
	switch {
	case g.Status == domain.GameStatusActive && g.DealerTgID == me:
		return "dealer"
	case part == nil:
		return "outsider"
	case part.IsActive && g.Status == domain.GameStatusActive:
		return "player"
	default:
		return "out"
	}
}

type gameData struct {
	game         *domain.Game
	participants []domain.Participant
	entries      []domain.LedgerEntry
	agg          map[int64]*participantDTO
}

func (b *builder) load(ctx context.Context, g *domain.Game) (*gameData, error) {
	ps, err := b.games.GetAllParticipants(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	entries, err := b.games.GetLedger(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	// Участник без buy-in (отменён через /undo) не показывается.
	visible := ps[:0:0]
	for _, p := range ps {
		if p.IsActive || hasBuyIn(entries, p.PlayerTgID) {
			visible = append(visible, p)
		}
	}
	return &gameData{game: g, participants: visible, entries: entries, agg: aggregate(visible, entries)}, nil
}

func hasBuyIn(entries []domain.LedgerEntry, tgID int64) bool {
	for _, e := range entries {
		if !e.IsVoid && e.PlayerTgID == tgID && e.Type == domain.LedgerBuyIn {
			return true
		}
	}
	return false
}

func (b *builder) summary(ctx context.Context, d *gameData) gameSummaryDTO {
	g := d.game
	s := gameSummaryDTO{
		ID: g.ID, Number: b.number(ctx, g), ChatID: g.ChatID, ChatTitle: b.chatTitle(ctx, g.ChatID),
		Status: string(g.Status), Dealer: b.user(ctx, g.DealerTgID), Rate: rateOf(g),
		StartedAt: g.StartedAt, EndedAt: g.EndedAt, PlayersTotal: len(d.agg),
		MyRole: role(g, b.me, d.agg[b.me]),
	}
	totalIn, bank := 0, 0
	for _, p := range d.agg {
		if p.IsActive {
			s.PlayersActive++
		}
		totalIn += p.TotalInRub
		bank += p.TotalInRub - p.TotalOutRub
	}
	if g.Status == domain.GameStatusActive {
		s.BankRub = bank
	} else {
		s.BankRub = totalIn // для завершённых — сколько всего было внесено
	}
	return s
}

func (b *builder) pending(ctx context.Context, g *domain.Game, pa *domain.PendingAction) pendingDTO {
	rub, chips := service.RequestAmount(g, pa)
	t := string(pa.ActionType)
	if pa.ActionType == domain.ActionCashOut {
		t = "CASH_OUT"
	}
	d := pendingDTO{
		ID: pa.ID, GameID: pa.GameID, Type: t,
		Requester: b.user(ctx, pa.RequesterTgID), Target: b.user(ctx, pa.TargetTgID),
		AmountRub: rub, AmountChips: chips, Status: string(pa.Status),
		CreatedAt: pa.CreatedAt, ResolvedAt: pa.ResolvedAt,
	}
	if pa.ResolvedByTgID != nil {
		u := b.user(ctx, *pa.ResolvedByTgID)
		d.ResolvedBy = &u
	}
	return d
}

func (b *builder) settlement(ctx context.Context, s *domain.Settlement) settlementDTO {
	return settlementDTO{
		ID: s.ID, GameID: s.GameID, From: b.user(ctx, s.FromTgID), To: b.user(ctx, s.ToTgID),
		AmountRub: s.AmountRub, IsPaid: s.IsPaid, PaidAt: s.PaidAt, IsReceived: s.IsReceived, ReceivedAt: s.ReceivedAt,
	}
}

// busyIn — другая активная игра, где игрок дилер или сидит за столом.
func (b *builder) busyIn(ctx context.Context, tgID, exceptGameID int64) *busyRefDTO {
	games, err := b.games.ListActiveGamesOfPlayer(ctx, tgID)
	if err != nil {
		return nil
	}
	for i := range games {
		g := &games[i]
		if g.ID == exceptGameID {
			continue
		}
		busy := g.DealerTgID == tgID
		if !busy {
			busy, _ = b.games.IsActivePlayer(ctx, g.ID, tgID)
		}
		if busy {
			return &busyRefDTO{GameID: g.ID, ChatID: g.ChatID, ChatTitle: b.chatTitle(ctx, g.ChatID)}
		}
	}
	return nil
}

func (b *builder) detail(ctx context.Context, g *domain.Game) (*gameDetailDTO, error) {
	d, err := b.load(ctx, g)
	if err != nil {
		return nil, err
	}
	out := &gameDetailDTO{
		gameSummaryDTO: b.summary(ctx, d),
		Participants:   []participantDTO{},
		Pending:        []pendingDTO{},
		MyRequests:     []pendingDTO{},
		Ledger:         []ledgerDTO{},
		BankDeltaRub:   g.BankDeltaRub,
	}

	for _, p := range d.participants {
		a := d.agg[p.PlayerTgID]
		a.User = b.user(ctx, p.PlayerTgID)
		out.Participants = append(out.Participants, *a)
	}

	pending, err := b.games.ListPending(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	for i := range pending {
		out.Pending = append(out.Pending, b.pending(ctx, g, &pending[i]))
	}

	mine, err := b.games.ListMyRequests(ctx, g.ID, b.me, 5)
	if err != nil {
		return nil, err
	}
	for i := range mine {
		out.MyRequests = append(out.MyRequests, b.pending(ctx, g, &mine[i]))
	}

	for _, e := range d.entries {
		out.Ledger = append(out.Ledger, ledgerDTO{
			ID: e.ID, Type: string(e.Type), Player: b.user(ctx, e.PlayerTgID),
			AmountRub: e.AmountRub, AmountChips: e.AmountChips,
			CreatedBy: b.user(ctx, e.CreatedByTgID), CreatedAt: e.CreatedAt, IsVoid: e.IsVoid,
		})
	}

	if g.Status == domain.GameStatusFinished {
		results, err := b.games.GetResultsByGame(ctx, g.ID)
		if err != nil {
			return nil, err
		}
		out.Results = []resultDTO{}
		for _, r := range results {
			out.Results = append(out.Results, resultDTO{
				User: b.user(ctx, r.PlayerTgID), BuyInCount: r.BuyInCount, RebuyCount: r.RebuyCount,
				TotalInRub: r.TotalInRub, TotalOutRub: r.TotalOutRub, TotalOutChips: r.TotalOutChips, NetRub: r.NetRub,
			})
		}
		settlements, err := b.games.ListSettlements(ctx, g.ID)
		if err != nil {
			return nil, err
		}
		out.Settlements = []settlementDTO{}
		for i := range settlements {
			out.Settlements = append(out.Settlements, b.settlement(ctx, &settlements[i]))
		}
	}

	if g.Status == domain.GameStatusActive {
		out.MyBusyIn = b.busyIn(ctx, b.me, g.ID)
	}
	return out, nil
}

func (b *builder) gameSummary(ctx context.Context, g *domain.Game) (*gameSummaryDTO, error) {
	d, err := b.load(ctx, g)
	if err != nil {
		return nil, err
	}
	s := b.summary(ctx, d)
	return &s, nil
}

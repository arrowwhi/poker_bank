package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"poker_bank/internal/domain"
	"poker_bank/internal/service"
)

// Notifier отражает действия из Mini App в чате игры (реализация — telegram.Handler).
type Notifier interface {
	RequestCreated(ctx context.Context, g *domain.Game, pa *domain.PendingAction)
	RequestApplied(ctx context.Context, g *domain.Game, pa *domain.PendingAction)
	RequestResolved(ctx context.Context, pa *domain.PendingAction)
	RequestsClosed(ctx context.Context, actions []domain.PendingAction)
	Announce(ctx context.Context, chatID int64, text string)
	GameCreated(ctx context.Context, g *domain.Game)
	GameFinished(ctx context.Context, g *domain.Game, res *service.EndResult)
	SettlementsChanged(ctx context.Context, g *domain.Game)
	// IsChatMember проверяет членство через Telegram (getChatMember).
	IsChatMember(ctx context.Context, chatID, userID int64) (bool, error)
	// PlayerName — @username или имя для текстов в чате.
	PlayerName(ctx context.Context, tgID int64) string
}

type api struct {
	games    *service.GameService
	players  *service.PlayerService
	notifier Notifier
	log      *zap.Logger
}

func (a *api) routes(r chi.Router) {
	r.Get("/me", a.me)

	r.Get("/chats", a.listChats)
	r.Get("/chats/{chatID}/members", a.chatMembers)
	r.Get("/chats/{chatID}/leaderboard", a.leaderboard)

	r.Get("/games", a.listGames)
	r.Post("/games", a.createGame)
	r.Get("/games/{id}", a.getGame)
	r.Post("/games/{id}/join", a.join)
	r.Post("/games/{id}/rebuy", a.rebuy)
	r.Post("/games/{id}/cashout", a.cashout)
	r.Post("/games/{id}/pending/{pid}/cancel", a.cancelRequest)
	r.Post("/games/{id}/approve", a.approve)
	r.Post("/games/{id}/reject", a.reject)
	r.Post("/games/{id}/buyin", a.buyIn)
	r.Post("/games/{id}/undo", a.undo)
	r.Post("/games/{id}/end", a.end)
	r.Post("/games/{id}/cancel", a.cancelGame)
	r.Post("/games/{id}/transfer_dealer", a.transferDealer)
	r.Post("/games/{id}/settle", a.settle)

	r.Get("/stats/me", a.myStats)
	r.Get("/stats/users/{userID}", a.userStats)
}

// ── ошибки ──

type apiError struct {
	status  int
	code    string
	message string
	details map[string]any
}

func (e *apiError) Error() string { return e.code + ": " + e.message }

func errStatus(status int, code, message string) *apiError {
	return &apiError{status: status, code: code, message: message}
}

var (
	errBadRequest = errStatus(http.StatusBadRequest, "bad_request", "Некорректный запрос")
	errForbidden  = errStatus(http.StatusForbidden, "forbidden", "Нет доступа к этой группе")
	errNotFound   = errStatus(http.StatusNotFound, "not_found", "Не найдено")
)

// fail переводит ошибку сервиса в ответ API.
func (a *api) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	var active *service.ActiveParticipantsError
	var bank *service.BankMismatchError
	switch {
	case errors.As(err, &ae):
	case errors.As(err, &active):
		ae = &apiError{status: http.StatusConflict, code: "active_players", message: "Не все игроки сделали cash-out",
			details: map[string]any{"user_ids": active.PlayerIDs}}
	case errors.As(err, &bank):
		ae = &apiError{status: http.StatusConflict, code: "bank_mismatch",
			message: fmt.Sprintf("Расхождение: банк = %+d₽", bank.BankRub), details: map[string]any{"bank_rub": bank.BankRub}}
	case errors.Is(err, service.ErrNotFound):
		ae = errNotFound
	case errors.Is(err, service.ErrNotDealer):
		ae = errStatus(http.StatusForbidden, "forbidden", "Это может только дилер")
	case errors.Is(err, service.ErrGameNotActive):
		ae = errStatus(http.StatusConflict, "not_active", "Игра уже завершена")
	case errors.Is(err, service.ErrAlreadyInGame):
		ae = errStatus(http.StatusConflict, "already_in_game", "Игрок уже в игре")
	case errors.Is(err, service.ErrNotInGame):
		ae = errStatus(http.StatusConflict, "not_in_game", "Игрок не в игре")
	case errors.Is(err, service.ErrPendingExists):
		ae = errStatus(http.StatusConflict, "already_pending", "Запрос уже отправлен дилеру")
	case errors.Is(err, service.ErrInvalidChipsAmount):
		ae = errStatus(http.StatusUnprocessableEntity, "not_integer_rub", "Фишки не дают целого числа рублей")
	case errors.Is(err, service.ErrAlreadyResolved):
		ae = errStatus(http.StatusConflict, "already_resolved", "Запрос уже обработан")
	case errors.Is(err, service.ErrNotRequester):
		ae = errStatus(http.StatusForbidden, "forbidden", "Отозвать можно только свой запрос")
	case errors.Is(err, service.ErrGameAlreadyActive):
		ae = errStatus(http.StatusConflict, "game_exists", "В этой группе уже идёт игра")
	case errors.Is(err, service.ErrBadRate):
		ae = errStatus(http.StatusUnprocessableEntity, "bad_rate", "Курс rebuy не пропорционален buy-in")
	default:
		a.log.Error("mini app api", zap.Error(err), zap.String("path", r.URL.Path))
		ae = errStatus(http.StatusInternalServerError, "internal", "Что-то пошло не так")
	}

	body := map[string]any{"code": ae.code, "message": ae.message}
	if ae.details != nil {
		body["details"] = ae.details
	}
	writeJSON(w, ae.status, map[string]any{"error": body})
}

// ── помощники ──

func me(r *http.Request) int64 {
	id, _ := IdentityFrom(r.Context())
	return id.TelegramID
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func pathInt(r *http.Request, name string) (int64, error) {
	v, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		return 0, errBadRequest
	}
	return v, nil
}

func decode(r *http.Request, v any) error {
	if r.ContentLength == 0 {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(v); err != nil {
		return errBadRequest
	}
	return nil
}

// canSeeChat — пользователь состоит в группе: известен боту или подтверждён Telegram.
func (a *api) canSeeChat(ctx context.Context, chatID, userID int64) (bool, error) {
	ok, err := a.games.IsKnownMember(ctx, chatID, userID)
	if err != nil || ok {
		return ok, err
	}
	ok, err = a.notifier.IsChatMember(ctx, chatID, userID)
	if err != nil {
		a.log.Warn("telegram chat member check", zap.Error(err), zap.Int64("chat_id", chatID))
		return false, nil
	}
	if ok {
		if err := a.games.RememberMember(ctx, chatID, userID); err != nil {
			a.log.Warn("remember chat member", zap.Error(err))
		}
	}
	return ok, nil
}

func (a *api) requireChat(ctx context.Context, chatID, userID int64) error {
	ok, err := a.canSeeChat(ctx, chatID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return errForbidden
	}
	return nil
}

// game загружает игру из пути и проверяет доступ: участник, дилер или член группы.
func (a *api) game(r *http.Request) (*domain.Game, error) {
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	g, err := a.games.GameByID(r.Context(), id)
	if err != nil {
		return nil, err
	}
	uid := me(r)
	if g.DealerTgID == uid {
		return g, nil
	}
	if _, err := a.games.GetParticipant(r.Context(), g.ID, uid); err == nil {
		return g, nil
	}
	if err := a.requireChat(r.Context(), g.ChatID, uid); err != nil {
		return nil, err
	}
	return g, nil
}

// respondGame отдаёт свежее состояние игры.
func (a *api) respondGame(w http.ResponseWriter, r *http.Request, gameID int64) {
	g, err := a.games.GameByID(r.Context(), gameID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	d, err := a.newBuilder(me(r)).detail(r.Context(), g)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// ── профиль и группы ──

func (a *api) me(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	b := a.newBuilder(uid)

	out := meDTO{User: b.user(ctx, uid), MemberSince: time.Now()}
	if p, err := a.players.GetByID(ctx, uid); err == nil {
		out.MemberSince = p.CreatedAt
	}

	history, err := a.games.PlayerHistory(ctx, uid, 0)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out.GamesPlayed = len(history)
	for _, h := range history {
		out.TotalNetRub += h.NetRub
	}

	active, err := a.games.ListActiveGamesOfPlayer(ctx, uid)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	for i := range active {
		g := &active[i]
		playing := g.DealerTgID == uid
		if !playing {
			playing, _ = a.games.IsActivePlayer(ctx, g.ID, uid)
		}
		if playing {
			if s, err := b.gameSummary(ctx, g); err == nil {
				out.ActiveGame = s
			}
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) listChats(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	b := a.newBuilder(uid)

	chats, err := a.games.ChatsOf(ctx, uid)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := make([]chatDTO, 0, len(chats))
	for _, c := range chats {
		d := chatDTO{ID: c.ID, Title: b.chatTitle(ctx, c.ID), GamesCount: c.GamesCount, LastGameAt: c.LastGameAt}
		d.MembersCount, _ = a.games.CountChatMembers(ctx, c.ID)
		if g, err := a.games.GetActiveGame(ctx, c.ID); err == nil {
			d.ActiveGame = &chatActiveGameDTO{ID: g.ID, Dealer: b.user(ctx, g.DealerTgID)}
		}
		if g, err := a.games.PreviousGameInChat(ctx, c.ID); err == nil && g != nil {
			d.LastGame = &chatLastGameDTO{ID: g.ID, StartedAt: g.StartedAt, Rate: rateOf(g)}
			d.LastGame.Number, _ = a.games.GameNumber(ctx, g)
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) chatMembers(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	chatID, err := pathInt(r, "chatID")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.requireChat(ctx, chatID, uid); err != nil {
		a.fail(w, r, err)
		return
	}

	members, err := a.games.ChatMembers(ctx, chatID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var activeID int64
	if g, err := a.games.GetActiveGame(ctx, chatID); err == nil {
		activeID = g.ID
	}

	b := a.newBuilder(uid)
	out := make([]chatMemberDTO, 0, len(members))
	for _, p := range members {
		out = append(out, chatMemberDTO{userDTO: b.user(ctx, p.TelegramUserID), BusyIn: b.busyIn(ctx, p.TelegramUserID, activeID)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) leaderboard(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	chatID, err := pathInt(r, "chatID")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.requireChat(ctx, chatID, uid); err != nil {
		a.fail(w, r, err)
		return
	}

	period := r.URL.Query().Get("period")
	var since *time.Time
	switch period {
	case "year":
		t := time.Now().AddDate(-1, 0, 0)
		since = &t
	case "month":
		t := time.Now().AddDate(0, -1, 0)
		since = &t
	default:
		period = "all"
	}

	rows, err := a.games.LeaderboardSince(ctx, chatID, since)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	b := a.newBuilder(uid)
	out := leaderboardDTO{ChatID: chatID, ChatTitle: b.chatTitle(ctx, chatID), Period: period, Entries: []leaderboardEntryDTO{}}
	for i, s := range rows {
		out.Entries = append(out.Entries, leaderboardEntryDTO{
			Place: i + 1, User: b.user(ctx, s.PlayerTgID), GamesPlayed: s.GameCount,
			TotalNetRub: s.TotalNetRub, WinRate: ratio(s.WinsCount, s.GameCount),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// ── игры ──

func (a *api) listGames(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	q := r.URL.Query()

	var chatID int64
	if v := q.Get("chat_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			a.fail(w, r, errBadRequest)
			return
		}
		chatID = id
		// Открытие по ссылке из группы: подтверждаем членство, чтобы показать её игры.
		if _, err := a.canSeeChat(ctx, chatID, uid); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	status := domain.GameStatus(q.Get("status"))
	switch status {
	case "", domain.GameStatusActive, domain.GameStatusFinished, domain.GameStatusCancelled:
	default:
		a.fail(w, r, errBadRequest)
		return
	}

	games, err := a.games.ListVisibleGames(ctx, uid, chatID, status, 50)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	b := a.newBuilder(uid)
	out := make([]gameSummaryDTO, 0, len(games))
	for i := range games {
		s, err := b.gameSummary(ctx, &games[i])
		if err != nil {
			a.fail(w, r, err)
			return
		}
		out = append(out, *s)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) getGame(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.respondGame(w, r, g.ID)
}

func (a *api) createGame(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var req struct {
		ChatID      int64   `json:"chat_id"`
		Rate        rateDTO `json:"rate"`
		DealerBuyIn bool    `json:"dealer_buy_in"`
	}
	if err := decode(r, &req); err != nil || req.ChatID == 0 {
		a.fail(w, r, errBadRequest)
		return
	}
	if err := a.requireChat(ctx, req.ChatID, uid); err != nil {
		a.fail(w, r, err)
		return
	}

	g, err := a.games.CreateGameIn(ctx, req.ChatID, uid,
		req.Rate.BuyInRub, req.Rate.BuyInChips, req.Rate.RebuyRub, req.Rate.RebuyChips, req.DealerBuyIn)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.notifier.GameCreated(ctx, g)
	a.respondGame(w, r, g.ID)
}

// ── запросы игрока ──

func (a *api) requestAction(w http.ResponseWriter, r *http.Request, g *domain.Game, t domain.ActionType, chips int) {
	ctx := r.Context()
	res, err := a.games.CreateRequest(ctx, g, me(r), t, chips)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if res.Applied {
		a.notifier.RequestsClosed(ctx, res.Cancelled)
		a.notifier.RequestApplied(ctx, g, res.Action)
	} else {
		a.notifier.RequestCreated(ctx, g, res.Action)
	}
	writeJSON(w, http.StatusOK, a.newBuilder(me(r)).pending(ctx, g, res.Action))
}

func (a *api) join(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.requestAction(w, r, g, domain.ActionJoin, 0)
}

type targetReq struct {
	UserID *int64 `json:"user_id"`
	Chips  *int   `json:"chips"`
}

// rebuy: без user_id — запрос игрока; с user_id — дилер записывает сам.
func (a *api) rebuy(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var req targetReq
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if req.UserID == nil {
		a.requestAction(w, r, g, domain.ActionRebuy, 0)
		return
	}

	ctx := r.Context()
	cancelled, err := a.games.DealerRebuy(ctx, g, me(r), *req.UserID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.notifier.RequestsClosed(ctx, cancelled)
	a.notifier.Announce(ctx, g.ChatID, fmt.Sprintf("✅ %s rebuy (%d₽ → %d фишек).",
		a.notifier.PlayerName(ctx, *req.UserID), g.RebuyRub, g.RebuyChips))
	a.respondGame(w, r, g.ID)
}

// cashout: без user_id — запрос игрока; с user_id — дилер выводит игрока.
func (a *api) cashout(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var req targetReq
	if err := decode(r, &req); err != nil || req.Chips == nil {
		a.fail(w, r, errBadRequest)
		return
	}
	if req.UserID == nil {
		a.requestAction(w, r, g, domain.ActionCashOut, *req.Chips)
		return
	}

	ctx := r.Context()
	cancelled, err := a.games.DealerCashOut(ctx, g, me(r), *req.UserID, *req.Chips)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.notifier.RequestsClosed(ctx, cancelled)
	rub, _ := g.ChipsToRub(*req.Chips)
	a.notifier.Announce(ctx, g.ChatID, fmt.Sprintf("💸 %s вышел: %d фишек = %d₽.",
		a.notifier.PlayerName(ctx, *req.UserID), *req.Chips, rub))
	a.respondGame(w, r, g.ID)
}

func (a *api) cancelRequest(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	pid, err := pathInt(r, "pid")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	pa, err := a.games.CancelRequest(r.Context(), g.ID, pid, me(r))
	if err != nil && !errors.Is(err, service.ErrAlreadyResolved) {
		a.fail(w, r, err)
		return
	}
	if err == nil {
		a.notifier.RequestResolved(r.Context(), pa)
	}
	a.respondGame(w, r, g.ID)
}

// ── дилер ──

func (a *api) resolve(w http.ResponseWriter, r *http.Request, approve bool) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var req struct {
		PendingID int64 `json:"pending_id"`
	}
	if err := decode(r, &req); err != nil || req.PendingID == 0 {
		a.fail(w, r, errBadRequest)
		return
	}

	ctx := r.Context()
	res, err := a.games.ResolveRequest(ctx, req.PendingID, me(r), approve)
	switch {
	case errors.Is(err, service.ErrAlreadyResolved):
		// Уже решён (например, кнопкой в чате) — отдаём актуальное состояние.
		a.respondGame(w, r, g.ID)
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	if res.Action.GameID != g.ID {
		a.fail(w, r, errNotFound)
		return
	}
	a.notifier.RequestResolved(ctx, res.Action)
	a.notifier.RequestsClosed(ctx, res.Cancelled)
	a.respondGame(w, r, g.ID)
}

func (a *api) approve(w http.ResponseWriter, r *http.Request) { a.resolve(w, r, true) }
func (a *api) reject(w http.ResponseWriter, r *http.Request)  { a.resolve(w, r, false) }

func (a *api) buyIn(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var req targetReq
	if err := decode(r, &req); err != nil || req.UserID == nil {
		a.fail(w, r, errBadRequest)
		return
	}

	ctx := r.Context()
	// Игрока, который ни разу не писал боту и не открывал приложение, нет в players:
	// записать его в ledger нельзя (ТЗ §11 — просим его самого написать /join).
	if _, err := a.players.GetByID(ctx, *req.UserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			a.fail(w, r, errStatus(http.StatusConflict, "not_registered",
				"Игрок ещё не знаком боту — пусть напишет /join в группе или откроет приложение"))
			return
		}
		a.fail(w, r, err)
		return
	}
	// Дилер сажает игрока из списка группы — значит, тот её участник.
	if err := a.requireChat(ctx, g.ChatID, *req.UserID); err != nil {
		a.fail(w, r, errStatus(http.StatusConflict, "not_member", "Игрок не найден в группе"))
		return
	}
	cancelled, err := a.games.DealerBuyIn(ctx, g, me(r), *req.UserID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.notifier.RequestsClosed(ctx, cancelled)
	a.notifier.Announce(ctx, g.ChatID, fmt.Sprintf("✅ %s добавлен (buy-in: %d₽ → %d фишек).",
		a.notifier.PlayerName(ctx, *req.UserID), g.BuyInRub, g.BuyInChips))
	a.respondGame(w, r, g.ID)
}

func (a *api) undo(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var req struct {
		N int `json:"n"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if req.N <= 0 {
		req.N = 1
	}

	ctx := r.Context()
	voided, err := a.games.DealerUndo(ctx, g, me(r), req.N)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if len(voided) > 0 {
		text := fmt.Sprintf("↩️ Отменено записей: %d", len(voided))
		for _, e := range voided {
			text += fmt.Sprintf("\n• %s — %s %d₽", a.notifier.PlayerName(ctx, e.PlayerTgID), e.Type, e.AmountRub)
		}
		a.notifier.Announce(ctx, g.ChatID, text)
	}
	a.respondGame(w, r, g.ID)
}

func (a *api) end(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var req struct {
		Force bool `json:"force"`
	}
	if err := decode(r, &req); err != nil {
		a.fail(w, r, err)
		return
	}

	ctx := r.Context()
	res, err := a.games.EndGame(ctx, g, me(r), req.Force)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.notifier.GameFinished(ctx, g, res)
	a.respondGame(w, r, g.ID)
}

func (a *api) cancelGame(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	ctx := r.Context()
	cancelled, err := a.games.CancelGame(ctx, g, me(r))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.notifier.RequestsClosed(ctx, cancelled)
	a.notifier.Announce(ctx, g.ChatID, fmt.Sprintf("🚫 Игра #%d отменена.", g.ID))
	a.respondGame(w, r, g.ID)
}

func (a *api) transferDealer(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	var req targetReq
	if err := decode(r, &req); err != nil || req.UserID == nil {
		a.fail(w, r, errBadRequest)
		return
	}
	ctx := r.Context()
	if err := a.games.DealerTransfer(ctx, g, me(r), *req.UserID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.notifier.Announce(ctx, g.ChatID, "🎩 Роль дилера передана "+a.notifier.PlayerName(ctx, *req.UserID)+".")
	a.respondGame(w, r, g.ID)
}

// ── итоги ──

func (a *api) settle(w http.ResponseWriter, r *http.Request) {
	g, err := a.game(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	ctx, uid := r.Context(), me(r)
	if g.Status != domain.GameStatusFinished {
		a.fail(w, r, errStatus(http.StatusConflict, "not_finished", "Игра ещё не завершена"))
		return
	}

	changed, err := a.games.MarkSettlementsByPlayer(ctx, g.ID, uid)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if changed == 0 {
		settlements, err := a.games.ListSettlements(ctx, g.ID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		for _, s := range settlements {
			if s.FromTgID == uid || s.ToTgID == uid {
				a.fail(w, r, errStatus(http.StatusConflict, "already_marked", "Уже отмечено"))
				return
			}
		}
		a.fail(w, r, errStatus(http.StatusConflict, "not_in_settlements", "Вас нет в переводах этой игры"))
		return
	}
	a.notifier.SettlementsChanged(ctx, g)
	a.respondGame(w, r, g.ID)
}

// ── статистика ──

func (a *api) myStats(w http.ResponseWriter, r *http.Request) {
	a.stats(w, r, me(r))
}

func (a *api) userStats(w http.ResponseWriter, r *http.Request) {
	uid, err := pathInt(r, "userID")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.stats(w, r, uid)
}

func (a *api) stats(w http.ResponseWriter, r *http.Request, userID int64) {
	ctx, viewer := r.Context(), me(r)

	var chatID int64
	if v := r.URL.Query().Get("chat_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			a.fail(w, r, errBadRequest)
			return
		}
		if err := a.requireChat(ctx, id, viewer); err != nil {
			a.fail(w, r, err)
			return
		}
		chatID = id
	}

	history, err := a.games.PlayerHistory(ctx, userID, chatID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// Чужая статистика — только по общим группам.
	if userID != viewer {
		filtered := history[:0:0]
		for _, h := range history {
			if ok, _ := a.games.IsKnownMember(ctx, h.ChatID, viewer); ok {
				filtered = append(filtered, h)
			}
		}
		history = filtered
	}

	b := a.newBuilder(viewer)
	out := playerStatsDTO{User: b.user(ctx, userID), History: []historyItemDTO{}, GamesPlayed: len(history)}
	if chatID != 0 {
		out.ChatID = &chatID
	}
	if p, err := a.players.GetByID(ctx, userID); err == nil {
		out.MemberSince = p.CreatedAt
	}

	wins := 0
	chatGames := map[int64]int{}
	for _, h := range history {
		out.TotalNetRub += h.NetRub
		out.RebuyTotal += h.RebuyCount
		if h.NetRub > 0 {
			wins++
		}
		chatGames[h.ChatID]++
		g := &domain.Game{ID: h.GameID, ChatID: h.ChatID}
		out.History = append(out.History, historyItemDTO{
			GameID: h.GameID, GameNumber: b.number(ctx, g), ChatID: h.ChatID, ChatTitle: b.chatTitle(ctx, h.ChatID),
			EndedAt: h.EndedAt, BuyInCount: h.BuyInCount, RebuyCount: h.RebuyCount,
			TotalInRub: h.TotalInRub, TotalOutRub: h.TotalOutRub, NetRub: h.NetRub,
		})
	}
	out.WinRate = ratio(wins, len(history))
	if len(history) > 0 {
		out.AvgNetRub = out.TotalNetRub / len(history)
	}

	// Место в рейтинге: в выбранной группе или в группе, где сыграно больше всего игр.
	lbChat := chatID
	if lbChat == 0 {
		best := 0
		for id, n := range chatGames {
			if n > best || (n == best && id < lbChat) {
				lbChat, best = id, n
			}
		}
	}
	if lbChat != 0 {
		rows, err := a.games.LeaderboardSince(ctx, lbChat, nil)
		if err == nil {
			total := len(rows)
			out.LeaderboardTotal = &total
			out.LeaderboardChatID = &lbChat
			for i, s := range rows {
				if s.PlayerTgID == userID {
					place := i + 1
					out.LeaderboardPlace = &place
				}
			}
		}
	}

	if userID == viewer {
		debts, err := a.openDebts(ctx, b, userID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		out.OpenDebts = debts
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) openDebts(ctx context.Context, b *builder, userID int64) ([]openDebtDTO, error) {
	settlements, err := a.games.OpenSettlements(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []openDebtDTO{}
	games := map[int64]*domain.Game{}
	for i := range settlements {
		s := &settlements[i]
		g, ok := games[s.GameID]
		if !ok {
			if g, err = a.games.GameByID(ctx, s.GameID); err != nil {
				return nil, err
			}
			games[s.GameID] = g
		}
		ended := g.StartedAt
		if g.EndedAt != nil {
			ended = *g.EndedAt
		}
		dir := "in"
		if s.FromTgID == userID {
			dir = "out"
		}
		out = append(out, openDebtDTO{
			settlementDTO: b.settlement(ctx, s), ChatTitle: b.chatTitle(ctx, g.ChatID),
			GameNumber: b.number(ctx, g), GameEndedAt: ended, Direction: dir,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].GameEndedAt.After(out[j].GameEndedAt) })
	return out, nil
}

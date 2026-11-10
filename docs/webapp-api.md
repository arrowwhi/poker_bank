# Mini App «Банкир»: HTTP API, gap-анализ, миграции

**Статус:** дизайн, код не написан. **Дата:** 2026-11-09.
Источник истины по бизнес-логике — Go-код (`internal/service`, `internal/delivery/telegram`), а не CLAUDE.md (там устаревшие aiogram/Alembic).

---

## 0. TL;DR

- Сервис собран на скелете `github.com/arrowwhi/infrastructure`: `telegram.App` запускает бота (`telegram.Bot`), HTTP-сервер (`http/server`, chi) и фоновые задачи в одном процессе (`internal/app`). Пакет `internal/delivery/http`; фронт встроен в бинарь (`webapp/embed.go`) и отдаётся с того же origin → CORS не нужен.
- Авторизация: заголовок `Authorization: tma <initDataRaw>`, проверка HMAC по токену бота + `auth_date` не старше 24 ч. Доступ к чату — `getChatMember` с кешем на 5 мин.
- Вход: `t.me/<bot>/<app>?startapp=c<chat_id>` (ссылка на чат) или `startapp=g<game_id>` (ссылка на итоги игры). Ссылку бот ставит URL-кнопкой «Открыть банк» под своими сообщениями в группе.
- Синхронизация с чатом: новый интерфейс `Notifier` в сервисном слое (реализован в `delivery/telegram`). Сервис после коммита вызывает `PendingCreated/PendingResolved/GameCreated/GameFinished/SettlementsChanged`, а notifier публикует или редактирует сообщение в чате. **Бот и HTTP идут одним путём**: callback-хендлер бота тоже вызывает `service.ResolvePending`, а сообщение правит notifier.
- Транспорт обновлений: **короткий polling** `GET /api/games/{id}` раз в 3 с, пока приложение видно, с `ETag` → `304`. SSE — возможный апгрейд позже, не для MVP.
- Перед HTTP нужно исправить несколько багов в существующем коде (§2.3). Главный: **`pending_actions.message_id` никогда не записывается**, поэтому сообщение в чате нельзя отредактировать ни из приложения, ни из expire-джоба, ни при cash-out.
- Нужна миграция `006_webapp.sql`: таблицы `chats` и `chat_members`, `games.settle_message_id`, `players.photo_url`, два индекса. Серии считаются из `game_results` в Go, отдельная таблица не нужна.

---

## 1. Что уже есть в коде (переиспользовать)

| Возможность | Где | Годится для HTTP как есть? |
|---|---|---|
| Создание игры | `GameService.NewGame` | Нет, валидация (положительность, пропорциональный курс, «нет active игры») лежит в `telegram/commands.go: newGameFromArgs/createGame` |
| Buy-in / Rebuy / Cash-out записью в ledger | `GameService.BuyIn/Rebuy/CashOut` | Да, но без проверок статуса участника и без транзакции |
| Конвертация фишек в рубли с проверкой целочисленности | `domain.Game.ChipsToRub` | Да |
| Pending: create/get/resolve/expire, идемпотентный `Resolve ... WHERE status='pending'` | `PendingService`, `PendingActionRepo` | Частично: валидация запросов и исполнение подтверждения лежат в хендлерах |
| Undo последних N | `GameService.UndoLast` → `LedgerRepo.VoidLastN` | Только глобальный undo; per-player undo нет |
| Завершение игры (агрегаты + план переводов + транзакция) | `GameService.Finish`, `ComputeGameResults`, `ApplyBankDelta`, `ComputeSettlements` | Да. Предпроверки (active-участники, банк ≠ 0) лежат в `doEndGame` |
| Отметка «оплатил/получил» | `GameService.MarkSettlementsByPlayer` → `SettlementRepo.MarkByPlayer` | Да |
| Банк | `LedgerRepo.GetBank` | Да |
| Leaderboard чата | `GameResultRepo.GetLeaderboard` | Да |
| Результаты игрока в чате | `GameResultRepo.ListByPlayer(player, chat)` (сорт. `ended_at DESC`) | Да (тайлы, график, серии) |
| История | `GameRepo.ListFinishedByChat` | Да, но без пагинации и без победителя/проигравшего (их считает хендлер) |
| Топик чата | `ChatTopicRepository.Get` | Да. Топик входящего сообщения — `tgforum.Tracker.ThreadID(c)` |
| Роль пользователя | `telegram/help.go: resolveRole` | Нет, логика роли в delivery |

### 1.1. Логику из `delivery/telegram` нужно перенести в сервисы

Без этого HTTP-хендлеры либо продублируют её, либо разойдутся с ботом.

| Сейчас в хендлере | Новый сервисный метод | Возвращаемые ошибки |
|---|---|---|
| `newGameFromArgs` + `createGame` + проверка active-игры в `handleNewGame` | `GameService.Create(ctx, chatID, dealerID, Params) (*Game, error)` + `notifier.GameCreated` | `ErrGameAlreadyActive` (в т.ч. из unique violation `games_one_active_per_chat`), `ErrInvalidParams`, `ErrRateNotProportional` (новая) |
| `handleJoin/handleRebuy/handleCashOut` (проверки + `pending.Create` + отправка сообщения) | `RequestService.Request(ctx, gameID, actorID, type, chips)` → создаёт pending, вызывает `notifier.PendingCreated`, который сохраняет `message_id` | `ErrNoActiveGame`, `ErrAlreadyInGame`, `ErrNotInGame`, `ErrPendingExists` (в т.ч. из unique violation `pending_one_per_target`), `ErrInvalidChipsAmount` |
| `handlePendingCallback` (проверка дилера, resolve, исполнение, редактирование) | `RequestService.Resolve(ctx, paID, actorID, approve bool) (*PendingAction, error)`: **одна транзакция** (resolve + ledger + participants), повторная валидация (JOIN при уже active → reject с причиной), затем `notifier.PendingResolved` | `ErrNotDealer`, `ErrAlreadyResolved` (новая), `ErrGameNotActive` (новая) |
| `handleDealerJoin/Rebuy/CashOut` + callbacks `djoin/drebuy/dcashout` | `GameService.DealerBuyIn/DealerRebuy/DealerCashOut(ctx, gameID, actorID, targetID[, chips])` с проверкой дилера и статуса цели | `ErrNotDealer`, `ErrAlreadyInGame`, `ErrNotInGame`, `ErrInvalidChipsAmount` |
| `doEndGame` + `handleEndGameCallback` (дублируют друг друга) | `GameService.End(ctx, gameID, actorID, force bool) ([]Settlement, error)` + `notifier.GameFinished` (сохраняет `settle_message_id`) | `ErrNotDealer`, `*ActiveParticipantsError{Players}`, `*BankMismatchError{BankRub}` |
| `handleCancelCallback` | `GameService.CancelGame(ctx, gameID, actorID)`: отменяет pending (`CancelByGame`), правит их сообщения | `ErrNotDealer`, `ErrGameNotActive` |
| `handleSettleCallback` (подсчёт changed + «уже отмечено / вас нет в переводах») | `GameService.MarkSettlements(ctx, gameID, actorID) (MarkResult, error)` + `notifier.SettlementsChanged` | `ErrNotInSettlements`, `ErrAlreadyMarked` |
| Агрегаты по игрокам в `handleStatus` и `handleMe`, повтор `ComputeGameResults` | `GameService.Snapshot(ctx, gameID, viewerID) (*GameSnapshot, error)`: игра, банк, участники с net/in/out/buy-in/rebuy, pending, роль зрителя, `CanFinish`/блокер | — |
| Победитель и проигравший в `handleHistory`, winrate/avg в `statsPlayer`, winrate в `statsLeaderboard` | `StatsService.History`, `StatsService.PlayerProfile`, `StatsService.Leaderboard` | — |
| `resolveRole` | `GameSnapshot.ViewerRole` | — |
| `handleTransferDealer` (проверка active-участника) | `GameService.TransferDealer(ctx, gameID, actorID, targetID)` с проверками | `ErrNotDealer`, `ErrNotInGame` |

Текстовые шаблоны сообщений (`pendingResolvedText`, `buildEndGameMessage`, `settleMarkup`, `pendingMarkup`) остаются в `delivery/telegram`, их вызывает notifier.

```go
// internal/interfaces/notifier.go
type Notifier interface {
    GameCreated(ctx context.Context, g *domain.Game)                       // сводка в группу/топик + кнопка «Открыть банк»
    PendingCreated(ctx context.Context, pa *domain.PendingAction)          // сообщение с pa:<id>:yes|no, затем SetMessageID
    PendingResolved(ctx context.Context, pa *domain.PendingAction)         // edit текста, снятие клавиатуры (confirmed/rejected/expired/cancelled)
    LedgerChanged(ctx context.Context, g *domain.Game, e []domain.LedgerEntry, undo bool) // инфо-сообщение о прямом действии дилера
    GameFinished(ctx context.Context, g *domain.Game, s []domain.Settlement) // план + кнопка settle, затем SetSettleMessageID
    GameCancelled(ctx context.Context, g *domain.Game)
    SettlementsChanged(ctx context.Context, g *domain.Game, s []domain.Settlement) // edit по games.settle_message_id
}
```

Правила для notifier:
- Его вызывают **после коммита**. Ошибки Telegram логируются и не откатывают бизнес-операцию.
- Топик берётся из `ChatTopicRepository.Get(chatID)`, а не из входящего апдейта (у HTTP-запроса его нет). Отправка — `tgforum.Send` с `MessageThreadID`.
- Для теста и ранней разработки нужен `NoopNotifier`, по аналогии с `NoopSheetsExporter`.
- Колбэк бота после перехода на сервис отвечает только `c.Respond(...)`. Сообщение правит notifier, иначе будет двойное редактирование.

### 1.2. Транзакции

Сейчас транзакционен только `GameRepo.Finish`. Подтверждение pending (`Resolve` → `BuyIn` → `participants.Create`) идёт тремя отдельными запросами. Если упасть между ними, запрос останется `confirmed` без записи в ledger. Предлагается `TxManager`:

```go
type TxManager interface { WithinTx(ctx context.Context, fn func(ctx context.Context) error) error }
```

`pgx.Tx` кладётся в ctx, а репозитории берут `tx` из ctx или `pool` (маленький хелпер `postgres.conn(ctx)`). Это нужно для `Resolve`, `DealerBuyIn` (ledger + participants), `DealerCashOut` (ledger + participants + cancel pending) и `Undo`.

---

## 2. Gap-анализ

### 2.1. Нужно для API и сейчас отсутствует

| # | Что | Зачем |
|---|---|---|
| G1 | Знание о чатах: название, бот ещё в чате | Выбор игры (A), выбор стола (E), заголовки экранов |
| G2 | Связь «пользователь ↔ чат» | Выбор игры, picker игроков у дилера, проверка доступа без лишних `getChatMember` |
| G3 | `PendingActionRepository.SetMessageID(id, msgID)` | Правка сообщения в чате при approve из приложения (§2.3 B1) |
| G4 | `PendingActionRepository.ExpireOlderThan` должен возвращать строки (`RETURNING`) | Expire-джоб должен править сообщения через notifier |
| G5 | `PendingActionRepository.CancelByGame(gameID)` | При `/endgame`/`/cancel` висящие pending сейчас остаются `pending` навсегда |
| G6 | `PendingActionRepository.ListByRequester(gameID, tgID, since)` или последний resolved по игроку | Тосты «подтверждено/отклонено» у игрока |
| G7 | `games.settle_message_id` | Отметка «оплатил/получил» из приложения должна править сообщение с планом в чате |
| G8 | `LedgerRepository.VoidLastForPlayer(gameID, tgID)` | Per-player undo в карточке игрока (C) |
| G9 | `GameRepository.ListActiveByUser(tgID)` (join `participants`/`chat_members`) | Экран «вы уже в игре» и список активных игр |
| G10 | `SettlementRepository.ListOpenByPlayer(tgID)` (все чаты) | «Мои открытые долги» в профиле |
| G11 | `PlayerRepository.SearchInChat(chatID, q, limit)` | Picker игроков с поиском |
| G12 | `GameRepository.ListFinishedByChat` с курсором (`before_id`) и с выжимкой (кол-во игроков, топ+/топ−) одним запросом | История без N+1 (сейчас N+1 в `handleHistory`) |
| G13 | Проверка членства в чате (Bot API `getChatMember`) — сейчас есть только `isChatAdmin` в telegram-слое | Авторизация chat-scoped эндпоинтов |
| G14 | Перенос group → supergroup (`migrate_to_chat_id`) не обрабатывается | При апгрейде группы теряется связь `games.chat_id`, `chat_topics`, `chats` |

### 2.2. Расхождения со спецификацией, которые важны для приложения

- Дилерские команды в боте (`/dealer_join`, `/dealer_rebuy`, `/dealer_cashout`) идут через inline-подтверждение (`djoin:`/`drebuy:`/`dcashout:`), хотя спецификация говорит «без подтверждения». В приложении подтверждением служит локальный confirm-диалог, а API исполняет действие сразу.
- В коде есть игровой `/cashout <chips>`: pending с типом `CASHOUT` и `payload.chips`. В CLAUDE.md его нет, но экран B («выйти = попросить дилера о cash-out» с вводом фишек) ложится именно на него. Cash-out с 0 фишек разрешён (`parseNonNegativeInt`), и API должен это сохранить.
- `UndoLast` при отмене `BUY_IN` ставит `is_active=false`, а не удаляет участника, как требует спецификация. В приложении такой игрок будет выглядеть как «вышел, net 0». Предлагается пересчитывать статус участника из ledger (см. §2.3 B6).
- `/game <id>` и `/admin_cancel` — заглушки (`return nil`).

### 2.3. Баги, которые надо починить до HTTP

| # | Баг | Где | Последствие |
|---|---|---|---|
| B1 | `message_id` у pending не сохраняется: `sendToChat` возвращает сообщение, но результат выбрасывается, а `Create` вызывается с `MessageID=nil` | `handleJoin/Rebuy/CashOut` | `editCancelledPending` никогда ничего не правит. Синхронизация приложения с чатом невозможна |
| B2 | Expire-джоб меняет статус, но не трогает сообщения | `PendingService.ExpirePending` | В чате остаются «живые» кнопки у истёкших запросов (нажатие ответит «уже обработан») |
| B3 | `Resolve` и запись в ledger не в одной транзакции | `handlePendingCallback` | Возможен `confirmed` без ledger. Ошибка исполнения только логируется, сообщение всё равно «✅ подтверждён» |
| B4 | Нет повторной валидации при подтверждении: JOIN одобряется, даже если дилер уже добавил игрока напрямую | то же | Двойной buy-in |
| B5 | `resolved.Payload["chips"].(float64)` — паника при неожиданном payload | то же | Panic в хендлере |
| B6 | `VoidLastN` сортирует по `created_at` без `id` (одинаковые timestamp в одной транзакции) | `LedgerRepo` | Недетерминированный undo. Нужно `ORDER BY created_at DESC, id DESC` |
| B7 | `Finish`/`Cancel` не отменяют висящие pending | `GameService` | Висящие кнопки, а приложение покажет pending в завершённой игре |
| B8 | `CashOut` не проверяет, что участник active (проверка только в хендлере) | `GameService.CashOut` | Через callback со старыми данными можно сделать двойной cash-out |

---

## 3. Авторизация

### 3.1. initData

Клиент шлёт `Authorization: tma <window.Telegram.WebApp.initData>` (сырая query-строка) в каждом запросе.

Middleware `auth.InitData`:
1. Распарсить строку как `url.Values` и забрать `hash`.
2. `data_check_string` = все пары, кроме `hash`, отсортированные по ключу, в формате `key=value` через `\n`. Поле `signature` (Ed25519, Bot API 8.0+) **остаётся** в data_check_string и, как и все остальные поля, участвует в HMAC.
3. `secret = HMAC_SHA256(key="WebAppData", msg=bot_token)`, `expected = hex(HMAC_SHA256(key=secret, msg=data_check_string))`. Сравнивать через `hmac.Equal`.
4. `auth_date` не старше `INIT_DATA_MAX_AGE` (по умолчанию 24 ч) и не из будущего (допуск 1 мин).
5. `user` (JSON) → `{id, username, first_name, last_name, photo_url}`. Делается `players.Upsert` (как `UpsertPlayer` в боте) и сохраняется `photo_url`. Чтобы не нагружать БД, апсерт делать не чаще раза в 10 мин на пользователя (in-memory map).
6. В контекст кладутся `UserID` и `StartParam`.

Ошибки: `401 {"error":{"code":"unauthorized"}}`. Сам initData в логи не пишется.
Пакет `internal/delivery/http/initdata` — чистые функции без зависимостей, покрытые табличными тестами (валидный, протухший, подменённое поле, пустой hash).

**Dev-режим:** `DEV_AUTH_USER_ID=<tg_id>` при пустом заголовке подставляет пользователя. Работает **только** при `APP_ENV=dev`, при `APP_ENV=prod` сервер с такой переменной отказывается стартовать.

### 3.2. Доступ к чату

В initData для direct-link Mini App **нет `chat_id`**: есть только `chat_instance`/`chat_type`, и то не всегда. Поэтому чат приходит из `start_param` или из пути запроса и проверяется так:

`member.Require(chatID)` (для `/api/chats/{chat_id}/...`, а для `/api/games/{id}/...` чат берётся из `games.chat_id`):
1. Запись в кеше `(chat_id, user_id) → ok/deny, TTL 5 мин` (in-memory, `sync.Map` с временем жизни; инстанс один).
2. Иначе вызывается `bot.ChatMemberOf(chat, user)`. Пропускаются `creator`, `administrator`, `member` и `restricted` с `is_member=true`. Отказ по `left`/`kicked` или ошибке `chat not found` (бот не в чате) → `403 not_chat_member`.
3. При успехе делается upsert в `chat_members` (источник `webapp`).

Кто дилер, решает сервис (`ErrNotDealer` → `403 not_dealer`), а не middleware.

### 3.3. Формат start_param

`startapp` допускает `[A-Za-z0-9_-]{1,512}`:
- `c-1001234567890` → открыть чат: активная игра, иначе история или пустое состояние.
- `g42` → открыть игру 42 (итоги и отметка оплаты). Так выглядит ссылка в сообщении с планом переводов.
- пусто (menu button в личке) → выбор игры по `chat_members`.

Клиент сам разбирает `start_param` из `initDataUnsafe` и вызывает нужный эндпоинт. Сервер разбирать его не обязан.

Ссылки в группе — **URL-кнопки** (`InlineButton{URL: "https://t.me/<bot>/<app>?startapp=c<chat_id>"}`), их можно ставить в групповых чатах, в отличие от `web_app`. Их нужно добавить в сводку новой игры, сообщения pending и план переводов. В личке с ботом — `setChatMenuButton(type=web_app)` при старте.

---

## 4. REST API

Общие правила:
- Префикс `/api`, JSON, `Content-Type: application/json; charset=utf-8`.
- Деньги — целые рубли (`*_rub`), фишки — целые (`*_chips`). Telegram ID — JSON-числа (≤ 2^53, безопасно для JS).
- Время — RFC 3339 UTC.
- Ошибка: `{"error": {"code": "snake_case", "message": "человекочитаемо по-русски", "details": {...}}}`.

### 4.1. Коды ошибок

| HTTP | code | Источник |
|---|---|---|
| 400 | `bad_request` | невалидный JSON или путь |
| 401 | `unauthorized` | initData |
| 403 | `not_chat_member` | §3.2 |
| 403 | `not_dealer` | `ErrNotDealer` |
| 404 | `not_found` | игра, запрос или игрок |
| 409 | `game_already_active` | `ErrGameAlreadyActive`, `details.game_id` |
| 409 | `game_not_active` | действие в finished/cancelled |
| 409 | `already_in_game` / `not_in_game` | `ErrAlreadyInGame` / `ErrNotInGame` |
| 409 | `pending_exists` | `ErrPendingExists`, `details.request_id` |
| 409 | `already_resolved` | гонка approve (чат vs приложение), `details.status` |
| 409 | `active_participants` | `details.players: [PlayerRef]` |
| 409 | `bank_mismatch` | `details.bank_rub` (+ подсказка `force`) |
| 409 | `not_in_settlements` / `already_marked` | отметка оплаты |
| 422 | `invalid_params` | неположительные числа и т.п., `details.field` |
| 422 | `rate_not_proportional` | `buy_in_rub*rebuy_chips != rebuy_rub*buy_in_chips` |
| 422 | `invalid_chips` | `ChipsToRub` не целое, `details.rate = {rub, chips}` |
| 500 | `internal` | — |

### 4.2. Общие DTO

```jsonc
// PlayerRef
{ "tg_id": 123, "username": "petya", "display_name": "Пётр И.", "photo_url": null }

// ChatRef
{ "chat_id": -1001234567890, "title": "Покер по пятницам", "has_topic": true }

// GameSummary — карточка в выборе игры
{
  "id": 42, "chat": ChatRef, "status": "active",
  "dealer": PlayerRef,
  "buy_in_rub": 1000, "buy_in_chips": 1000, "rebuy_rub": 500, "rebuy_chips": 500,
  "players_active": 5, "players_total": 7, "bank_rub": 6500,
  "started_at": "...", "ended_at": null,
  "my_role": "dealer|player|out|outsider"
}

// Participant (внутри GameState)
{
  "player": PlayerRef, "status": "active|out",
  "buy_in_count": 1, "rebuy_count": 2,
  "in_rub": 2000, "in_chips": 2000, "out_rub": 0, "out_chips": 0,
  "net_rub": -2000,              // out - in (как /status)
  "is_dealer": false,
  "last_entry": LedgerEntry|null // для per-player undo: что отменится
}

// LedgerEntry
{ "id": 901, "player": PlayerRef, "type": "BUY_IN|REBUY|CASH_OUT",
  "amount_rub": 1000, "amount_chips": 1000, "created_by": PlayerRef,
  "created_at": "...", "is_void": false }

// Request (pending_action)
{ "id": 77, "type": "JOIN|REBUY|CASHOUT", "player": PlayerRef,
  "chips": 1500, "amount_rub": 1500,   // для CASHOUT; для JOIN/REBUY из курса игры
  "status": "pending|confirmed|rejected|expired|cancelled",
  "created_at": "...", "resolved_at": null, "resolved_by": null }

// Settlement
{ "id": 5, "from": PlayerRef, "to": PlayerRef, "amount_rub": 700,
  "is_paid": true, "paid_at": "...", "is_received": false, "received_at": null }
```

### 4.3. Эндпоинты

#### Сессия и навигация

**`GET /api/me`** — auth. Стартовый экран (A).
```jsonc
{
  "user": PlayerRef,
  "active_games": [GameSummary],   // мои активные игры: участник (любой статус) или дилер
  "chats": [ { "chat": ChatRef, "active_game": GameSummary|null } ] // известные мне чаты, где есть бот
}
```
- `active_games` с `my_role in (dealer, player)` → экран «вы уже в игре».
- `chats[].active_game != null` → выбор игры. Пусто → пустое состояние с подсказкой «откройте приложение по кнопке в чате».
- `chats` нужен также для выбора стола в E: клиент фильтрует `active_game == null`.

Это объединяет экраны A и E-«выбор стола» в один эндпоинт.

> **Правило: у игрока одновременно максимум одна активная игра** (во всех чатах, в любой роли — активный участник или дилер). Следствия:
> - `active_games` → заменяется на `"current_game": GameSummary|null`. Если не null — приложение сразу открывает эту игру; «Мои столы» / выбор игры показываются только когда `current_game == null`. Экран «вы уже в игре» = редирект.
> - `POST requests {JOIN}`, `POST entries {BUY_IN}` (дилер за игрока), `POST /api/chats/{id}/games` → `409 busy_in_other_game` с `details: { game_id, chat_title }`, если у целевого игрока (или создателя) уже есть другая активная игра. Out-игрок (сделал cash-out) **не** занят — может войти в другую игру.
> - Повторная валидация при approve JOIN: если игрок успел занять другую игру — запрос `rejected` с этой причиной.
> - Picker дилера: участники, занятые в другой игре, показываются серыми с подписью «в игре в …».
> - `/transfer_dealer` — новому дилеру нельзя быть дилером другой активной игры.
> - Гарантия в БД (миграция 006): `CREATE UNIQUE INDEX participants_one_active_per_player ON participants(player_tg_id) WHERE is_active;` — для этого `/cancel` должен сбрасывать `is_active=false` всем участникам (сейчас не сбрасывает), а миграция перед созданием индекса делает то же для уже отменённых игр: `UPDATE participants p SET is_active=false FROM games g WHERE g.id=p.game_id AND g.status<>'active' AND p.is_active;`. Ограничение «дилер ≠ занят» индексом не выразить — проверка в сервисе под `SELECT … FOR UPDATE` строки игрока.

**`GET /api/chats/{chat_id}`** — auth + member. Вход по `startapp=c…`.
```jsonc
{ "chat": ChatRef, "active_game": GameSummary|null, "last_finished": GameSummary|null }
```

#### Игра

**`POST /api/chats/{chat_id}/games`** — auth + member. Экран E. → `201 GameSummary`.
```jsonc
{ "buy_in_rub": 1000, "buy_in_chips": 1000, "rebuy_rub": 500, "rebuy_chips": 500 }
```
Бот публикует сводку в группу (в топик, если он задан) с кнопкой «Открыть банк». Ошибки: `game_already_active`, `invalid_params`, `rate_not_proportional`. Членство обязательно: нельзя создать игру в чужом чате. Бот должен состоять в чате (`chats.bot_status`), иначе `409 bot_not_in_chat`.

**`GET /api/games/{game_id}`** — auth + member(game.chat). Основной polling-эндпоинт для B, C и D.
Заголовки: ответ с `ETag: W/"<hash>"`, `Cache-Control: no-cache`; запрос `If-None-Match` → `304`.
```jsonc
{
  "game": GameSummary,
  "me": {
    "role": "dealer|player|out|outsider",
    "is_participant": true, "participant": Participant|null,
    "requests": [Request],        // мои pending + resolved за последние 10 мин (для тостов)
    "can": { "join": false, "rebuy": true, "cashout_request": true }
  },
  "bank_rub": 6500,
  "participants": [Participant],   // active сначала
  "pending": [Request],            // все pending игры (видны всем, как в /status)
  "finish": {                      // только для дилера, иначе null
    "ready": false,
    "blocker": "active_participants|bank_mismatch|null",
    "active_players": [PlayerRef], "bank_rub": 0
  },
  // finished/cancelled:
  "results": [ { "player": PlayerRef, "in_rub": 2000, "out_rub": 2700, "net_rub": 700,
                 "buy_in_count": 1, "rebuy_count": 1 } ] | null,
  "settlements": [Settlement] | null,
  "all_settled": false,
  "bank_delta_rub": 0
}
```
Тост у игрока клиент показывает по переходу `requests[i].status` из `pending` в `confirmed`/`rejected`/`expired`. Бейдж дилера равен `pending.length`. Новый запрос клиент находит по `id`, которого не было в прошлом ответе.

**`GET /api/games/{game_id}/ledger`** — auth + member. Полный лог (`/game <id>`). Нужен не для MVP: экраны лог не показывают, для undo хватает `participants[].last_entry`.

#### Запросы игрока (с подтверждением дилера)

**`POST /api/games/{game_id}/requests`** — auth + member.
```jsonc
{ "type": "JOIN" }                     // Buy-in (новый или повторный вход после cash-out)
{ "type": "REBUY" }
{ "type": "CASHOUT", "chips": 1500 }   // chips >= 0, chips*rate должно давать целые рубли
```
→ `201 Request`. Бот публикует сообщение с `pa:<id>:yes|no` в чат/топик и сохраняет `message_id`.
Ошибки: `game_not_active`, `already_in_game` (JOIN), `not_in_game` (REBUY/CASHOUT), `pending_exists`, `invalid_chips`.

**`DELETE /api/games/{game_id}/requests/{request_id}`** — auth + member, только автор запроса. Отзыв своего запроса: `status=cancelled`, сообщение в чате правится. → `204`. Ошибки: `already_resolved`, `403 forbidden`. *Новая функция, нужна экрану «ждём дилера».*

**`POST /api/games/{game_id}/requests/{request_id}/approve`** и **`.../reject`** — auth + member, дилер.
→ `200 Request` (с итоговым статусом). Внутри `RequestService.Resolve`: одна транзакция, затем notifier правит сообщение в чате («✅ Подтверждено @dealer»).
Ошибки: `not_dealer`, `already_resolved` (сюда попадает и одобрение в чате, сделанное раньше), `game_not_active`, а также `already_in_game` при повторной валидации (тогда запрос автоматически `rejected` с причиной).

#### Прямые действия дилера

**`POST /api/games/{game_id}/entries`** — auth + member, дилер. Без pending: подтверждение — confirm-диалог в UI.
```jsonc
{ "type": "BUY_IN",   "player_tg_id": 123 }               // в т.ч. дилер сам себе
{ "type": "REBUY",    "player_tg_id": 123 }
{ "type": "CASH_OUT", "player_tg_id": 123, "chips": 2700 }
```
→ `201 { "entry": LedgerEntry, "cancelled_requests": [Request] }`. При CASH_OUT висящие запросы игрока отменяются (как сейчас) и их сообщения правятся. Бот пишет в чат инфо-строку («💸 @x вышел: 2700 фишек = 2700₽»), как в существующих callback-ах.
Ошибки: `not_dealer`, `already_in_game` (BUY_IN active-игроку), `not_in_game`, `invalid_chips`, `404` (игрок неизвестен).
Если у игрока был pending того же типа (JOIN при BUY_IN, REBUY при REBUY), он автоматически `confirmed` с `resolved_by=dealer`, без второй записи в ledger. Это закрывает гонку B4.

**`POST /api/games/{game_id}/entries/{entry_id}/void`** — auth + member, дилер. Per-player undo.
Разрешено, только если `entry_id` — **последняя не-void запись этого игрока** (иначе `409 not_last_entry`). После отмены статус участника пересчитывается из ledger: active, если последняя не-void запись BUY_IN/REBUY; out, если CASH_OUT; удаление из `participants`, если записей нет. → `200 { "voided": LedgerEntry }`.
Глобальный «undo последней записи» = клиент берёт последнюю запись по `created_at`. Отдельный эндпоинт не нужен.

#### Завершение

**`POST /api/games/{game_id}/finish`** — auth + member, дилер.
```jsonc
{ "force": false }
```
→ `200 { "game": GameSummary, "settlements": [Settlement], "bank_delta_rub": 0 }`. Бот публикует план с кнопкой `settle:<id>` и URL-кнопкой `startapp=g<id>`, сохраняя `games.settle_message_id`.
Ошибки: `active_participants` (`details.players`), `bank_mismatch` (`details.bank_rub`; клиент показывает экран «расхождение» и повторяет с `force: true`), `not_dealer`, `game_not_active`.
Игра без buy-in ведёт себя как `cancel` (§11 спецификации): ответ `200` с `game.status = "cancelled"`.

**`POST /api/games/{game_id}/cancel`** — auth + member, дилер. → `200 GameSummary`. *В макетах нет, но без него дилер в приложении не выберется из ошибочно созданной игры. Дешёвый эндпоинт.*

#### Расчёты

**`POST /api/games/{game_id}/settlements/mark`** — auth + member. Аналог кнопки «Я оплатил / получил». Отмечает все мои исходящие (`is_paid`) и входящие (`is_received`) переводы.
→ `200 { "settlements": [Settlement], "all_settled": true, "changed": 2 }`. Сообщение в чате правится по `settle_message_id`. Когда `all_settled`, кнопка убирается и добавляется «✅ Все расчёты закрыты».
Ошибки: `409 game_not_finished`, `not_in_settlements`, `already_marked`.
Тело пустое. Отметка по одному переводу (`/settlements/{id}/mark`) — не для MVP, семантика бота «всё моё разом».

**`GET /api/me/debts`** — auth. Открытые переводы по всем чатам, где я отправитель с `!is_paid` или получатель с `!is_received`. Чаты фильтруются по членству (проверка кешем/`chat_members`).
```jsonc
{ "items": [ { "settlement": Settlement, "game_id": 42, "chat": ChatRef, "ended_at": "...",
               "direction": "out|in" } ],
  "total_out_rub": 700, "total_in_rub": 0 }
```

#### Игроки, статистика, история (`members` — MVP, остальное phase 2)

**`GET /api/chats/{chat_id}/members?q=пет&limit=20`** — auth + member, для picker'а дилера.
→ `{ "items": [ { "player": PlayerRef, "in_game": "active|out|null" } ] }`. Источник — `chat_members ⋈ players`, `ILIKE` по `username`/`display_name`, порядок по `last_seen_at DESC`. Пустой `q` даёт недавних. Игрок, которого бот никогда не видел, найден не будет: показать подсказку «пусть откроет приложение или напишет /join» (как `/dealer_join` сейчас).

**`GET /api/chats/{chat_id}/leaderboard`** — auth + member.
→ `{ "items": [ { "place": 1, "player": PlayerRef, "games": 12, "total_net_rub": 5400, "avg_net_rub": 450, "win_rate": 0.58 } ] }`. Источник — `GetLeaderboard`.

**`GET /api/chats/{chat_id}/players/{tg_id}/profile`** — auth + member. И «мой профиль», и «профиль другого игрока».
```jsonc
{
  "player": PlayerRef,
  "tiles": { "games": 12, "total_net_rub": 5400, "avg_net_rub": 450, "win_rate": 0.58 },
  "chart": [ { "game_id": 40, "ended_at": "...", "net_rub": 700, "cumulative_rub": 5400 } ], // по возрастанию
  "records": {
    "best_game":  { "game_id": 31, "net_rub": 4200, "ended_at": "..." },
    "worst_game": { "game_id": 28, "net_rub": -3000, "ended_at": "..." },
    "longest_plus_streak": { "length": 4, "from_game_id": 20, "to_game_id": 24 },
    "current_plus_streak": 2
  },
  "place": { "rank": 3, "of": 9 },
  "history": [ { "game_id": 40, "ended_at": "...", "net_rub": 700, "players": 7 } ]
}
```
Всё считается в Go из `ListByPlayer(player, chat)` и `GetLeaderboard(chat)`. Серия плюсовых игр — подряд идущие игры с `net_rub > 0` по `ended_at`; `0` и минус серию обрывают. Текущая серия считается от самой свежей игры. Для N ≤ сотен игр это O(n), SQL с оконными функциями не нужен. Таблица под серии не нужна. Долги в профиль не входят: для себя это `GET /api/me/debts`, а чужие долги показывать не нужно.

**`GET /api/chats/{chat_id}/games?status=finished&before_id=40&limit=20`** — auth + member. История.
→ `{ "items": [ GameSummary + { "players": 7, "top_winner": {player, net_rub}, "top_loser": {...} } ], "next_before_id": 20 }`.

#### Служебные

- `GET /healthz` → `200 ok` (без auth; ping БД). Для healthcheck в Coolify.
- `GET /` и `GET /{path...}` → статика фронта с SPA-fallback на `index.html`.

### 4.4. Сводка прав

| Эндпоинт | Член чата | Участник | Дилер |
|---|:-:|:-:|:-:|
| GET me / chats / games / leaderboard / profile / members | ✓ | ✓ | ✓ |
| POST games (создать) | ✓ | ✓ | ✓ |
| POST requests JOIN | ✓ (не active) | out | ✓ (дилер обычно делает себе BUY_IN напрямую) |
| POST requests REBUY/CASHOUT | — | active | active |
| DELETE requests/{id} | автор | автор | автор |
| approve/reject, entries, void, finish, cancel | — | — | ✓ |
| settlements/mark | в переводах | в переводах | в переводах |

---

## 5. Синхронизация приложения с чатом

```
  приложение (approve)                        чат (кнопка pa:77:yes)
          │                                            │
  POST /requests/77/approve               callback pa:* → handlePendingCallback
          │                                            │
          └──────────► RequestService.Resolve(77, actor, approve) ◄─┘
                         tx: UPDATE pending … WHERE status='pending' RETURNING
                             INSERT ledger, UPSERT participants
                         commit
                         notifier.PendingResolved(pa) → bot.Edit(chat_id, message_id,
                                                        "✅ JOIN подтверждён @dealer — @petya", без клавиатуры)
          ▲                                            ▲
  второй клиент: 0 строк → ErrAlreadyResolved → 409 / «Запрос уже обработан»
```

- Запрос создан в приложении → `notifier.PendingCreated` публикует сообщение с той же клавиатурой `pa:<id>:yes|no` и делает `SetMessageID`. Значит, дилер может одобрить хоть в чате, хоть в приложении.
- Одобрено в чате → в БД `confirmed` → приложение увидит это на следующем poll (≤ 3 с).
- Истёк (джоб, 30 мин) → `ExpireOlderThan … RETURNING` → `notifier.PendingResolved` для каждого.
- Отметка оплаты в приложении → `notifier.SettlementsChanged` правит план по `games.settle_message_id`. Нажатие `settle:` в чате правит то же сообщение, а приложение увидит изменение на poll.
- Правка сообщения, которое старше 48 ч или удалено, падает с ошибкой Telegram: её логировать и игнорировать.
- Уведомление дилеру «новый запрос» в приложении = появление нового `pending[].id` в ответе poll (бейдж на сегменте «Дилер», `HapticFeedback.notificationOccurred`). В чате сообщение уже есть, отдельный push не нужен.

### 5.1. Транспорт: рекомендация — короткий polling

| | Polling 3 с + ETag | Long-poll | SSE |
|---|---|---|---|
| Сложность сервера | минимальная, без состояния | брокер событий + таймауты | брокер + удержание соединений + heartbeat |
| Переживает рестарт/деплой | да | переподключение | переподключение, потеря событий без `Last-Event-ID` |
| Прокси Coolify (Traefik) | без проблем | таймауты | нужно отключать буферизацию, следить за idle timeout |
| События из чата (callback бота) | видны автоматически | нужно публиковать в брокер из того же процесса | то же |
| Задержка | ≤ 3 с | ~0 | ~0 |
| Нагрузка | ~10 игроков × 0,33 rps ≈ 3 rps на игру — копейки | — | — |

WebView Telegram в фоне всё равно замораживает JS, поэтому выигрыш SSE проявляется только при открытом экране, а там 3 с незаметны. **Решение:**
- `GET /api/games/{id}` каждые 3 с при `document.visibilityState === 'visible'`, на экране итогов — каждые 10 с.
- Немедленный refetch после собственной мутации и на `visibilitychange`/`activated`.
- ETag = хеш сериализованного ответа, считается после сборки. Серверу это ничего не экономит, но экономит трафик и ре-рендеры. Если понадобится дешевле — добавить `games.rev` (счётчик, инкрементируемый в каждой мутирующей транзакции) и отвечать 304 до сборки снапшота.
- Апгрейд-путь: `GET /api/games/{id}/events` (SSE). Notifier дополнительно публикует `game_id` в in-process `Broker`, клиент по событию делает тот же `GET`. Контракт данных не меняется.

---

## 6. Критический разбор экранов (MVP или нет)

**MVP (без этого приложение бессмысленно):**
- A: вход по `startapp=c…` → `GET /api/chats/{id}` → экран игры. «Вы уже в игре» входит в `GET /api/me`, отдельный экран можно не делать: если активная игра одна, сразу редирект.
- B целиком: Buy-in-запрос, Rebuy, ожидание, cash-out-запрос с вводом фишек, тосты.
- C: pending с approve/reject, участники, buy-in/rebuy/cash-out за игрока, picker, завершение, «расхождение» → force. Сегмент «Игрок | Дилер» — чисто клиентский, оба вида строятся из одного `GET /api/games/{id}`.
- D: итоги + «Я оплатил / получил».

**Phase 2:**
- Профиль, график, рекорды и серии, leaderboard, история, чужой профиль. Данные есть, но это 3–4 экрана и отдельный сервис. Чужой профиль — тот же эндпоинт, бесплатен, если делается свой.
- E (новая игра из приложения): `/newgame` в чате уже работает. E требует `chats`, выбора стола и notifier.GameCreated. Ценно, но не блокирует.
- Выбор игры по всем чатам без deep link: зависит от `chat_members`, которая из-за privacy mode бота заполняется неполно (бот видит только команды и ответы ему). Пустое состояние должно учить «откройте по кнопке в чате».
- «Мои долги по всем играм».

**Вырезать или упростить:**
- Per-player undo можно заменить одним «Отменить последнее действие (@petya: REBUY 500₽)», что дешевле и ближе к `/undo`. Если оставлять per-player — только «последняя запись игрока» (§4.3), а не произвольная.
- Аватары других игроков: Bot API отдаёт фото только через `getFile` с токеном в URL, значит нужен прокси и кеш. В MVP — инициалы. Своё фото есть в `initData.user.photo_url`.
- Фото чата: аналогично, в MVP — инициалы и название.
- `/transfer_dealer`, `/admin_cancel` в приложении не нужны, остаются командами бота.
- Отдельный экран лога (`/ledger`) не нужен.

**Не хватает в макетах:**
1. Отзыв своего запроса игроком (кнопка на экране ожидания) → `DELETE /requests/{id}`.
2. Состояние `expired` (тост «запрос истёк, отправьте снова»).
3. Отмена игры дилером (`/cancel`).
4. 403 «Вы не участник этого чата» / «бот удалён из чата».
5. Игра завершилась или отменилась, пока экран открыт: poll отдаёт `status` → переход на итоги или «игра отменена».
6. Дилер как игрок: его собственный buy-in — прямое действие `entries` с `player_tg_id = me`, а не JOIN-запрос самому себе. Это нужно отразить в UI сегмента «Игрок».
7. Cash-out 0 фишек («проигрался в ноль») — валиден, UI не должен блокировать.
8. «Все квиты — переводов нет» (`settlements = []`) на экране итогов.
9. Игрок, которого бот не знает (нет в `players`), в picker не найдётся. Нужна подсказка и кнопка «скопировать ссылку на приложение».
10. Кнопка «Перейти в чат» (`openTelegramLink(t.me/c/...)` недоступна для приватных групп без username, так что только если у чата есть `username`; иначе просто `close()`).

---

## 7. Миграции

Существующие: `001_init` (все базовые таблицы), `002_add_pending_fsm` (идемпотентный повтор pending/fsm), `003_cashout_action_type` (CASHOUT), `004_chat_topics`, `005_settlement_received`. Дублей ниже нет.

### 7.1. Откуда берутся данные

- **`chats`**:
  - (а) middleware `UpsertChat` на каждом апдейте из group/supergroup: `title`, `username`, `is_forum`, `type`. Апсерт дросселировать in-memory, раз в 10 мин на чат или при смене title;
  - (б) `my_chat_member` (бот добавлен, удалён или повышен): `bot_status`, `added_by`. **Внимание:** бот инфраструктуры запрашивает только `message` и `callback_query` (`allowed_updates`), поэтому этот источник недоступен без изменения библиотеки. Вместо него: служебные сообщения о добавлении бота (`new_chat_members`, приходят как `message`, разбираются в `tgforum` из сырого JSON) и проверка `getChatMember` при входе в приложение;
  - (в) `OnMigration`: `migrate_to_chat_id` → в одной транзакции перенести `games.chat_id`, `chat_topics.chat_id`, `chats`, `chat_members`, `pending_actions.chat_id`;
  - (г) backfill из `games`.
- **`chat_members`**:
  - (а) тот же middleware: отправитель любого сообщения или callback в группе (`source='message'`);
  - (б) успешная проверка `getChatMember` из HTTP (`source='webapp'`);
  - (в) buy-in в игре (`source='game'`);
  - (г) backfill из `participants`/`pending_actions`;
  - (д) опционально апдейты `chat_member` — только если бот админ и `allowed_updates` включает `chat_member`. Для MVP не нужно.

  Выход пользователя из чата отслеживается через `OnUserLeft` (`left_chat_member`, если бот видит сервисные сообщения) → `status='left'`. Авторитетная проверка всё равно `getChatMember`, таблица служит только подсказкой для списков.
- **Серии и рекорды** считаются из `game_results ⋈ games` (§4.3), таблица не нужна.
- **`games.settle_message_id`** записывает `notifier.GameFinished`. Для старых игр он NULL, и отметка из приложения тогда просто не правит сообщение.
- **`pending_actions.message_id`** — колонка уже есть, нужен только `SetMessageID` (B1).

### 7.2. SQL

```sql
-- migrations/006_webapp.sql
-- +goose Up

CREATE TABLE IF NOT EXISTS chats (
    chat_id       BIGINT      PRIMARY KEY,
    type          TEXT        NOT NULL DEFAULT 'supergroup'
                              CHECK (type IN ('group', 'supergroup', 'private', 'channel')),
    title         TEXT,
    username      TEXT,                               -- публичный @username чата, если есть
    is_forum      BOOLEAN     NOT NULL DEFAULT false,
    bot_status    TEXT        NOT NULL DEFAULT 'member'
                              CHECK (bot_status IN ('member', 'administrator', 'left', 'kicked')),
    bot_added_by  BIGINT      REFERENCES players (telegram_user_id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chat_members (
    chat_id       BIGINT      NOT NULL REFERENCES chats (chat_id) ON DELETE CASCADE,
    player_tg_id  BIGINT      NOT NULL REFERENCES players (telegram_user_id),
    status        TEXT        NOT NULL DEFAULT 'member'
                              CHECK (status IN ('member', 'administrator', 'creator', 'left', 'kicked')),
    source        TEXT        NOT NULL DEFAULT 'message'
                              CHECK (source IN ('message', 'webapp', 'game', 'chat_member', 'backfill')),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_id, player_tg_id)
);

-- «мои чаты» для GET /api/me
CREATE INDEX IF NOT EXISTS chat_members_player
    ON chat_members (player_tg_id) WHERE status IN ('member', 'administrator', 'creator');
-- picker: недавние участники чата
CREATE INDEX IF NOT EXISTS chat_members_recent
    ON chat_members (chat_id, last_seen_at DESC);

-- сообщение с планом переводов — чтобы править его при отметке из приложения
ALTER TABLE games   ADD COLUMN IF NOT EXISTS settle_message_id BIGINT;

-- фото из initData (только своё; чужие — нет)
ALTER TABLE players ADD COLUMN IF NOT EXISTS photo_url TEXT;

-- «мои открытые долги»: входящие неполученные (исходящие покрывает settlements_from из 001,
-- но его предикат is_paid=false не учитывает is_received — для входящих нужен свой)
CREATE INDEX IF NOT EXISTS settlements_to_open
    ON settlements (to_tg_id) WHERE is_received = false;

-- мои активные игры: participants ищется по игроку (PK начинается с game_id)
CREATE INDEX IF NOT EXISTS participants_player
    ON participants (player_tg_id);

-- Backfill
INSERT INTO chats (chat_id)
SELECT DISTINCT chat_id FROM games
ON CONFLICT (chat_id) DO NOTHING;

INSERT INTO chat_members (chat_id, player_tg_id, source, first_seen_at, last_seen_at)
SELECT g.chat_id, p.player_tg_id, 'backfill', min(p.joined_at), max(p.joined_at)
FROM participants p
JOIN games g ON g.id = p.game_id
GROUP BY g.chat_id, p.player_tg_id
ON CONFLICT (chat_id, player_tg_id) DO NOTHING;

INSERT INTO chat_members (chat_id, player_tg_id, source)
SELECT DISTINCT g.chat_id, g.dealer_tg_id, 'backfill'
FROM games g
ON CONFLICT (chat_id, player_tg_id) DO NOTHING;

-- +goose Down

DROP INDEX IF EXISTS participants_player;
DROP INDEX IF EXISTS settlements_to_open;
ALTER TABLE players DROP COLUMN IF EXISTS photo_url;
ALTER TABLE games   DROP COLUMN IF EXISTS settle_message_id;
DROP INDEX IF EXISTS chat_members_recent;
DROP INDEX IF EXISTS chat_members_player;
DROP TABLE IF EXISTS chat_members;
DROP TABLE IF EXISTS chats;
```

Чего сознательно **нет**:
- `games.chat_id REFERENCES chats`. FK добавил бы требование «сначала upsert чата» во все пути создания игры. Можно включить отдельной миграцией после стабилизации backfill.
- Таблицы под серии и сессии — initData stateless.
- `games.rev` — только если polling упрётся в нагрузку (§5.1).
- trigram-индекс для поиска: поиск идёт по `chat_members` одного чата (десятки строк), хватит `ILIKE`.

Ограничение: `chat_members.player_tg_id REFERENCES players` требует, чтобы игрок был в `players`. Middleware уже апсертит отправителя до остальной обработки, порядок сохраняется.

---

## 8. Размещение HTTP-сервера (сделано)

Сервис переведён на скелет `github.com/arrowwhi/infrastructure` (как kinolist). Библиотека не менялась: недостающее (топики форума, `getChatMember`) сделано подмодулем `internal/pkg/tgforum`.

### 8.1. Пакеты

```
cmd/poker-bank/main.go       // конфиг + zap → app.Run
internal/
  app/app.go                 // wiring: telegram.NewApp, пул, миграции, сервисы, бот, HTTP, expire-джоб; a.Run(bot, http, job)
  config/                    // прежние env + HTTP/webhook (см. 8.3)
  pkg/tgforum/               // топики форума поверх telegram инфраструктуры (см. 8.2)
  delivery/
    telegram/                // обработчики на *telegram.Context (адаптер context.go), middleware: tgforum, UpsertPlayer, TopicGuard
    http/
      server.go              // httpserver.New(...) + chi-роутер: webhook, /api/*, SPA
      auth.go                // TelegramAuth: Authorization: tma <initData>, HMAC, auth_date ≤ 24 ч
  repository/postgres/       // пул connections/postgres; транзакции — pool.InTx (транзакция в контексте)
webapp/embed.go              // go:embed all:dist (в git только dist/.gitkeep)
```

- **HTTP-сервер** — `http/server` инфраструктуры (chi): recovery, request-id, real IP, access-лог, `/healthz` и `/ready` на общем реестре `a.Health()`; подключены `tracing.Middleware` и `metrics.Middleware`. Роуты: `POST $TELEGRAM_WEBHOOK_PATH` (только webhook), `/api/*` под `TelegramAuth`, остальное — SPA (`index.html` с `no-cache`, `assets/*` — `immutable`).
- **Служебный порт** `ADMIN_PORT` (9090) поднимает `telegram.App`: `/metrics`, `/healthz`, `/ready`. Наружу не публикуется.
- **Логи**: код poker_bank пишет в zap, как раньше; логгер инфраструктуры (zerolog) — только её собственные события.
- **Реализовано в API пока** только `GET /api/me`: пользователь, `start_param`, активные игры, где он дилер или участник. Остальные эндпоинты из §4 — следующий этап.

### 8.2. Топики форума: `internal/pkg/tgforum`

В `telegram.Update` библиотеки нет `message_thread_id`, `is_topic_message`, `migrate_to_chat_id`. `tgforum.Tracker` читает их из сырого JSON до декодирования и хранит по `update_id`:

- polling — `Tracker.HTTPClient` подставляется в клиент Bot API (`telegram.WithHTTPClient`) и разбирает ответы `getUpdates`;
- webhook — `Tracker.Webhook(bot.WebhookHandler())`;
- обработчики берут `tracker.ThreadID(c)`; `Tracker.Middleware()` удаляет запись после обработки;
- отправка в топик — `tgforum.Send` через `Client.Call` с `message_thread_id`; проверка админа — `tgforum.GetChatMember`.

Покрыто тестом с настоящим `telegram.Bot` в режиме polling (ответ уходит в топик входящего сообщения).

### 8.3. Конфиг (`internal/config`)

Прежние переменные не менялись (`TELEGRAM_TOKEN`, `DATABASE_URL`, `LOG_LEVEL`, `GOOGLE_SHEETS_*`). Новые:

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `TELEGRAM_MODE` | `polling` | `webhook` — обновления приходят на HTTP-сервер |
| `PUBLIC_URL` | — | внешний HTTPS-адрес; обязателен для webhook и Mini App |
| `TELEGRAM_WEBHOOK_SECRET` | — | секрет `X-Telegram-Bot-Api-Secret-Token`, обязателен для webhook |
| `TELEGRAM_WEBHOOK_PATH` | `/telegram/webhook` | путь webhook |
| `TELEGRAM_API_URL` | `https://api.telegram.org` | свой Bot API или заглушка в тестах |
| `HTTP_PORT` | `8080` | API, Mini App, webhook |
| `ADMIN_PORT` | `9090` | `/metrics`, `/ready`; пусто — выключен |
| `APP_ENV` | `local` | окружение в логах и метриках |
| `WEBAPP_SHORT_NAME` | `poker_bank` | short name Mini App из BotFather: бот ставит под сообщениями кнопки-ссылки `t.me/<bot>/<app>` (username бота берётся из `getMe`) |
| `SHUTDOWN_TIMEOUT` | `15s` | остановка компонентов |

### 8.4. Docker и Coolify

`docker/Dockerfile`: стадия `node:22` собирает `webapp/dist`, Go-стадия кладёт его в `webapp/dist` перед `go build` (встраивается в бинарь). `EXPOSE 8080 9090`. CI берёт версию Go из `go.mod` (1.26, требование инфраструктуры).

Coolify:
1. Ports Exposes `8080`, домен с HTTPS (Telegram требует HTTPS для Mini App и webhook).
2. Health check `GET /healthz`.
3. Env: `PUBLIC_URL`, затем `TELEGRAM_MODE=webhook` + `TELEGRAM_WEBHOOK_SECRET`. Можно оставить `polling` — тогда HTTP нужен только для Mini App.
4. Одна реплика.

BotFather: `/newapp` → short name (`WEBAPP_SHORT_NAME`), URL = `PUBLIC_URL`.

Фронт в прод-сборке пока работает на моках: без `VITE_API_URL` включается `MockApi`. Переключить на настоящий API, когда будут готовы эндпоинты §4.

---

## 9. Порядок работ

1. **Подготовка без HTTP** (видимый эффект и в боте):
   - починить B1–B8;
   - добавить `TxManager`;
   - вынести `RequestService.Request/Resolve`, `GameService.Create/End/CancelGame/Dealer*`, `Snapshot`;
   - добавить `Notifier`. Колбэки бота переводятся на сервисы.
2. Миграция `006_webapp.sql`, middleware `UpsertChat`/`chat_members`, `OnMyChatMember`, `OnMigration`.
3. `delivery/http`: initdata, membership, `GET /api/me`, `GET /api/chats/{id}`, `GET /api/games/{id}`, requests, approve/reject, entries, finish, cancel, settlements/mark. Это MVP.
4. URL-кнопки «Открыть банк» в сообщениях бота, menu button, Dockerfile + Coolify.
5. Phase 2: `StatsService` (profile/leaderboard/history), `members` picker-поиск (picker нужен уже в MVP для дилерского buy-in — эндпоинт `members` поднять в шаг 3), `POST /api/chats/{id}/games`, `GET /api/me/debts`, void per-player.

Тесты: `initdata` (табличные), `StatsService` серии (пустой, все плюс, ноль обрывает, текущая vs максимальная), `RequestService.Resolve` (гонка двойного approve, повторная валидация), маппинг ошибок → HTTP-кодов.

# ТЗ: Telegram-бот «Банкир» для покера

**Версия:** 1.0
**Дата:** 2026-05-09

---

## 1. Цель

Telegram-бот, который ведёт банк домашней игры в покер: фиксирует buy-in, rebuy, cash-out игроков, в конце игры рассчитывает план переводов друг другу с минимальным числом транзакций и сохраняет агрегированные результаты для статистики.

---

## 2. Архитектура

### 2.1. Стек

- **Язык:** Go
- **Telegram-фреймворк:** aiogram 3.x (async)
- **БД:** PostgreSQL 15
- **Ммиграции:** Сырой SQL, goose

### 2.2. Базовый паттерн хранения

**Эфемерный event-log + материализованный результат:**

| Слой | Что хранит | Когда живёт |
|---|---|---|
| `ledger` | Подробный поток событий (BUY_IN/REBUY/CASH_OUT) | Постоянно. Служит полным аудитом игры. |
| `game_results` | Одна строка на игрока на игру: `total_in`, `total_out`, `net` | Создаётся при `/endgame`. Постоянно. Источник для `/history`, `/stats`. |
| `settlements` | План переводов после игры | Создаётся при `/endgame`. Постоянно. |
| `participants` | Участники игры и их статус | Постоянно. После завершения отражает итоговый состав. |
| `pending_actions` | Запросы, ожидающие подтверждения дилера | Постоянно. После завершения игры остаются для аудита. |
| `fsm[poker-bot-tz.md](poker-bot-tz.md)_states` | Состояния пошагового ввода (например, `/newgame`) | Операционные. |

Все данные игры хранятся бессрочно — `ledger`, `participants`, `pending_actions` не удаляются при завершении. Агрегаты в `game_results` вычисляются из `ledger` и кешируются при `/endgame` для быстрых запросов.

---

## 3. Жизненный цикл игры

```
                  /newgame              /endgame
        (нет игры) ────► active ─────────────────► finished
                          │                          (только агрегаты)
                          │       /cancel
                          └─────────────────► cancelled
```

В одном Telegram-чате одновременно может существовать только одна игра в статусе `active`. Завершённых и отменённых — сколько угодно.

---

## 4. Роли пользователей

Роль определяется относительно текущей активной игры в чате:

- **Dealer** — пользователь, запустивший `/newgame`. Один на игру. Имеет полные права: подтверждать запросы игроков, добавлять buy-in/rebuy за игроков напрямую, делать cash-out, завершать игру. Может передать роль через `/transfer_dealer`.
- **Player (active)** — игрок с незакрытым BUY_IN. Может: `/rebuy` (с подтверждением дилера), `/me`, `/status`. Может выйти только через cash-out, который делает дилер.
- **Player (out)** — игрок, сделавший cash-out в текущей игре. Может вернуться через `/join`.
- **Outsider** — пользователь чата вне игры. Может: `/join` (с подтверждением), `/status`, `/history`, `/stats`.
- **Chat admin** — Telegram-администратор чата. Особых прав не имеет, кроме `/admin_cancel` для разблокировки чата, если дилер недоступен.

---

## 5. Модель данных (PostgreSQL)

### 5.1. `players` — игроки

Накапливаются между играми. `telegram_user_id` — первичный ключ.

```sql
CREATE TABLE players (
  telegram_user_id  BIGINT       PRIMARY KEY,
  username          TEXT,                        -- актуальный @ник
  display_name      TEXT,                        -- first_name + last_name
  created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);
```

`username` и `display_name` обновляются при каждом сообщении от пользователя.

### 5.2. `games` — игры (одна таблица для всех статусов)

```sql
CREATE TABLE games (
  id              BIGSERIAL    PRIMARY KEY,
  chat_id         BIGINT       NOT NULL,
  dealer_tg_id    BIGINT       NOT NULL REFERENCES players(telegram_user_id),
  buy_in_rub      INTEGER      NOT NULL CHECK (buy_in_rub > 0),
  buy_in_chips    INTEGER      NOT NULL CHECK (buy_in_chips > 0),
  rebuy_rub       INTEGER      NOT NULL CHECK (rebuy_rub > 0),
  rebuy_chips     INTEGER      NOT NULL CHECK (rebuy_chips > 0),
  status          TEXT         NOT NULL CHECK (status IN ('active','finished','cancelled')),
  started_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
  ended_at        TIMESTAMPTZ,
  bank_delta_rub  INTEGER      NOT NULL DEFAULT 0,    -- расхождение при /endgame_force
  CONSTRAINT proportional_rate
    CHECK (buy_in_rub * rebuy_chips = rebuy_rub * buy_in_chips)
);

-- Быстрый поиск активной игры в чате + гарантия «одна активная на чат»
CREATE UNIQUE INDEX games_one_active_per_chat
  ON games(chat_id) WHERE status = 'active';

CREATE INDEX games_chat_history
  ON games(chat_id, ended_at DESC) WHERE status IN ('finished','cancelled');
```

Курс игры неизменен и един для buy-in и rebuy:
`rate = buy_in_rub / buy_in_chips = rebuy_rub / rebuy_chips`.

### 5.3. `ledger` — события активной игры (эфемерные)

```sql
CREATE TABLE ledger (
  id               BIGSERIAL    PRIMARY KEY,
  game_id          BIGINT       NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  player_tg_id     BIGINT       NOT NULL REFERENCES players(telegram_user_id),
  type             TEXT         NOT NULL CHECK (type IN ('BUY_IN','REBUY','CASH_OUT')),
  amount_rub       INTEGER      NOT NULL,
  amount_chips     INTEGER      NOT NULL,
  created_by_tg_id BIGINT       NOT NULL REFERENCES players(telegram_user_id),
  created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
  is_void          BOOLEAN      NOT NULL DEFAULT false,
  void_reason      TEXT
);
CREATE INDEX ledger_game ON ledger(game_id, created_at);
```

`is_void=true` — мягкое удаление через `/undo`. Строки **не удаляются** при завершении игры.

### 5.4. `participants` — участники активной игры (эфемерные)

```sql
CREATE TABLE participants (
  game_id      BIGINT       NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  player_tg_id BIGINT       NOT NULL REFERENCES players(telegram_user_id),
  is_active    BOOLEAN      NOT NULL DEFAULT true,    -- false после CASH_OUT
  joined_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
  PRIMARY KEY (game_id, player_tg_id)
);
```

**Не удаляется** при завершении игры — хранится постоянно как полный состав участников.

### 5.5. `game_results` — итоги игры (постоянные)

Создаются один раз при `/endgame`. Источник для `/history`, `/stats`, `/game <id>`.

```sql
CREATE TABLE game_results (
  game_id        BIGINT       NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  player_tg_id   BIGINT       NOT NULL REFERENCES players(telegram_user_id),
  buy_in_count   INTEGER      NOT NULL DEFAULT 0,    -- обычно 1
  rebuy_count    INTEGER      NOT NULL DEFAULT 0,
  total_in_rub   INTEGER      NOT NULL,              -- Σ всех buy-in и rebuy
  total_out_rub  INTEGER      NOT NULL,              -- Σ всех cash-out
  total_out_chips INTEGER     NOT NULL,
  net_rub        INTEGER      NOT NULL,              -- total_out - total_in
  PRIMARY KEY (game_id, player_tg_id)
);
CREATE INDEX game_results_player ON game_results(player_tg_id);
```

### 5.6. `settlements` — план переводов

```sql
CREATE TABLE settlements (
  id            BIGSERIAL    PRIMARY KEY,
  game_id       BIGINT       NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  from_tg_id    BIGINT       NOT NULL REFERENCES players(telegram_user_id),
  to_tg_id      BIGINT       NOT NULL REFERENCES players(telegram_user_id),
  amount_rub    INTEGER      NOT NULL CHECK (amount_rub > 0),
  is_paid       BOOLEAN      NOT NULL DEFAULT false,
  paid_at       TIMESTAMPTZ
);
CREATE INDEX settlements_from ON settlements(from_tg_id) WHERE is_paid = false;
CREATE INDEX settlements_game ON settlements(game_id);
```

### 5.7. `pending_actions` — запросы на подтверждение

```sql
CREATE TABLE pending_actions (
  id                BIGSERIAL    PRIMARY KEY,
  game_id           BIGINT       NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  action_type       TEXT         NOT NULL CHECK (action_type IN ('JOIN','REBUY')),
  requester_tg_id   BIGINT       NOT NULL REFERENCES players(telegram_user_id),
  target_tg_id      BIGINT       NOT NULL REFERENCES players(telegram_user_id),
  payload           JSONB        NOT NULL DEFAULT '{}',
  status            TEXT         NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','confirmed','rejected','expired','cancelled')),
  chat_id           BIGINT       NOT NULL,
  message_id        BIGINT,
  created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
  resolved_at       TIMESTAMPTZ,
  resolved_by_tg_id BIGINT       REFERENCES players(telegram_user_id)
);
CREATE UNIQUE INDEX pending_one_per_target
  ON pending_actions(game_id, target_tg_id, action_type)
  WHERE status = 'pending';
```

### 5.8. `fsm_states` — состояния пошагового ввода

```sql
CREATE TABLE fsm_states (
  chat_id     BIGINT       NOT NULL,
  user_tg_id  BIGINT       NOT NULL,
  state       TEXT,
  data        JSONB        NOT NULL DEFAULT '{}',
  updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
  PRIMARY KEY (chat_id, user_tg_id)
);
```

### 5.9. Производные значения

```sql
-- Текущий банк активной игры (по неудалённым записям)
bank(game_id) =
  SUM(CASE WHEN type IN ('BUY_IN','REBUY') THEN amount_rub ELSE 0 END)
  - SUM(CASE WHEN type = 'CASH_OUT' THEN amount_rub ELSE 0 END)
  FROM ledger WHERE game_id = $1 AND is_void = false;

-- Чистый баланс игрока в активной игре
player_net(game_id, player_tg_id) =
  SUM(CASE WHEN type = 'CASH_OUT' THEN amount_rub ELSE -amount_rub END)
  FROM ledger WHERE game_id = $1 AND player_tg_id = $2 AND is_void = false;
```

---

## 6. Завершение игры: переход в постоянное хранение

При `/endgame` (или `/endgame_force`) выполняется одна транзакция:

```sql
BEGIN;

-- 1. Считаем агрегаты на стороне БД
INSERT INTO game_results (game_id, player_tg_id, buy_in_count, rebuy_count,
                          total_in_rub, total_out_rub, total_out_chips, net_rub)
SELECT
  game_id,
  player_tg_id,
  COUNT(*) FILTER (WHERE type = 'BUY_IN'),
  COUNT(*) FILTER (WHERE type = 'REBUY'),
  COALESCE(SUM(amount_rub) FILTER (WHERE type IN ('BUY_IN','REBUY')), 0),
  COALESCE(SUM(amount_rub) FILTER (WHERE type = 'CASH_OUT'), 0),
  COALESCE(SUM(amount_chips) FILTER (WHERE type = 'CASH_OUT'), 0),
  COALESCE(SUM(amount_rub) FILTER (WHERE type = 'CASH_OUT'), 0)
    - COALESCE(SUM(amount_rub) FILTER (WHERE type IN ('BUY_IN','REBUY')), 0)
FROM ledger
WHERE game_id = $1 AND is_void = false
GROUP BY game_id, player_tg_id;

-- 2. Записываем settlements (план уже посчитан в Python, см. п.9)
INSERT INTO settlements (game_id, from_tg_id, to_tg_id, amount_rub) VALUES …;

-- 3. Обновляем игру
UPDATE games SET status = 'finished', ended_at = now(), bank_delta_rub = $delta
WHERE id = $1;

COMMIT;
```

`/cancel` — то же самое, но без шагов 1 и 2 и со `status='cancelled'`.

При падении бота посреди транзакции Postgres откатит всё → можно повторить `/endgame`.

---

## 7. Механизм подтверждений

Любое действие игрока, требующее одобрения дилера, проходит унифицированный флоу.

1. Игрок вызывает команду (`/join`, `/rebuy`).
2. Бот валидирует и **создаёт строку в `pending_actions`** со `status='pending'`.
3. Бот публикует в чат сообщение с инлайн-клавиатурой:
   > 🃏 @petya просит **REBUY** (300₽ → 300 фишек). Подтвердить?
   > [✅ Подтвердить] [❌ Отклонить]
4. `callback_data` кнопок: `pa:<pending_action_id>:yes` / `pa:<pending_action_id>:no`.
5. На нажатие бот проверяет `from_user.id == games.dealer_tg_id`. Если нет — `answer_callback("Только дилер")`.
6. Идемпотентное обновление статуса:
   ```sql
   UPDATE pending_actions
   SET status = 'confirmed', resolved_at = now(), resolved_by_tg_id = $dealer
   WHERE id = $id AND status = 'pending'
   RETURNING *;
   ```
   Если 0 строк — действие уже разрешено, повторное нажатие игнорируется.
7. На `confirmed` — в той же транзакции пишется `ledger`-запись и обновляется `participants.is_active`.
8. Сообщение редактируется: клавиатура убирается, текст дополняется «✅ Подтверждено @dealer» / «❌ Отклонено».
9. Фоновый джоб раз в минуту переводит pending старше **30 минут** в `expired` и убирает клавиатуру.

Команды дилера (`/dealer_join`, `/dealer_rebuy`, `/cashout`) выполняются **без** этого флоу — пишутся напрямую.

---

## 8. Команды бота

Везде `<>` — обязательный параметр, `[]` — опциональный. Все команды работают **в групповом чате**, где идёт игра. Исключения: `/start`, `/help`, `/stats` могут работать в личке.

### 8.1. Жизненный цикл игры

#### `/newgame [buy_in_rub buy_in_chips rebuy_rub rebuy_chips]`

- **Кто:** любой пользователь.
- **Препроверка:** в этом `chat_id` нет игры со `status='active'`.
- **Параметры:** четыре положительных целых. Курс должен быть пропорционален: `buy_in_rub * rebuy_chips == rebuy_rub * buy_in_chips`.
- **Если параметры не указаны** — пошаговый ввод (FSM, 4 шага), состояние хранится в `fsm_states`.
- **Эффект:** создаётся `games` row со `status='active'`, инициатор становится дилером. В чат публикуется сводка.

#### `/endgame`

- **Кто:** только дилер.
- **Препроверка:** нет участников с `is_active=true` в `participants`. Если есть — список, нужно cash-out.
- **Логика:**
  1. Считается `bank(game_id)`. Если ≠ 0:
     > ⚠️ Расхождение: банк = +120₽. Чтобы продолжить — `/endgame_force`.
     На этом останавливаемся.
  2. Считается план `settlements` (см. §9).
  3. Транзакция из §6 — пишутся агрегаты, settlements, удаляется лог.
  4. В чат публикуется план переводов с кнопкой «✅ Я заплатил» под каждой строкой.

#### `/endgame_force`

- **Кто:** только дилер. Только если предыдущий `/endgame` отказал из-за расхождения.
- **Эффект:** считаем переводы как есть; `bank_delta_rub` сохраняется в `games`. Дельта **распределяется по кредиторам пропорционально их net_rub** (если банк положительный — кредиторы получают чуть меньше; отрицательный — должники платят чуть больше).

#### `/cancel`

- **Кто:** только дилер. Запрашивает подтверждение через инлайн-кнопку.
- **Эффект:** `status='cancelled'`. Данные `ledger`, `participants`, `pending_actions` **сохраняются**. `game_results` и `settlements` **не создаются**.

#### `/admin_cancel`

- **Кто:** Telegram-админ чата (проверяется через `getChatMember`). Срабатывает только если дилер не делал действий **более 6 часов**.
- **Эффект:** как `/cancel`.

#### `/transfer_dealer @user`

- **Кто:** текущий дилер. `@user` должен быть active-участником игры.
- **Эффект:** `games.dealer_tg_id = @user.tg_id`. В чат — оповещение.

### 8.2. Команды игрока (с подтверждением дилера)

#### `/join`

- **Кто:** любой пользователь чата.
- **Проверки:** есть active-игра; нет pending-`JOIN` от него; он не active в игре.
- **Эффект:** `pending_actions(JOIN)`. Дилер подтверждает → `BUY_IN` записывается, `participants.is_active = true`.

#### `/rebuy`

- **Кто:** active-игрок.
- **Проверки:** active в игре, нет pending-`REBUY` от него.
- **Эффект:** аналогично, через подтверждение → `REBUY`.

### 8.3. Команды дилера (без подтверждения)

#### `/dealer_join @user`

- **Кто:** дилер.
- **Что:** мгновенно добавляет `@user` в игру с `BUY_IN`. Если в `players` нет записи — создаётся (нужен `telegram_user_id` — резолвится из чата; если только @username без id — ошибка с просьбой к игроку написать `/join`).

#### `/dealer_rebuy @user`

- **Кто:** дилер. `@user` должен быть active.

#### `/cashout @user <chips>`

- **Кто:** **только дилер**.
- **Параметры:** `chips` — положительное целое.
- **Валидация:** `chips * rate` должно давать целое число рублей. Иначе — отказ с пояснением. (Курс задан так, что это всегда выполнимо для кратных значений.)
- **Эффект:** `CASH_OUT`(`amount_chips=chips`, `amount_rub = chips * rate`). `participants.is_active = false`. В чат — «@user вышел: 750 фишек = 750₽».

#### `/undo [N]`

- **Кто:** дилер. По умолчанию `N=1`.
- **Эффект:** последние N не-void записей `ledger` помечаются `is_void=true`. Если откатывается `BUY_IN` — игрок удаляется из `participants`. Если `CASH_OUT` — `is_active` возвращается в `true`. В чат — лог отмены.
- **Только в active-игре.** В finished/cancelled запрещён.

### 8.4. Read-only

#### `/status`

- Информация о текущей игре: курс, банк, список active/out участников с их net_rub, список pending.

#### `/me`

- Личные показатели в текущей игре.

#### `/history [N]`

- Последние N (по умолчанию 10) `finished` игр в этом чате. Краткая выжимка: дата, число игроков, топ-1 победитель, топ-1 проигравший.

#### `/game <id>`

- Детали конкретной игры. Для `active` — список событий из `ledger`. Для `finished`/`cancelled` — `game_results` + `settlements` + полный лог событий из `ledger`.

#### `/stats [@user]`

- Без аргумента: топ игроков чата по сумме net_rub за все finished-игры.
- С `@user`: число игр, суммарный профит/убыток, средний за игру, win-rate.

### 8.5. Технические

- `/start` — приветствие, ссылка на `/help`. Регистрирует пользователя в `players`.
- `/help` — список команд по ролям.

### 8.6. Сводная таблица прав

| Команда | Outsider | Active player | Out player | Dealer | Chat admin |
|---|:-:|:-:|:-:|:-:|:-:|
| `/newgame` | ✓ (если нет active) | — | — | — | — |
| `/join` | ✓* | — | ✓* | — | — |
| `/rebuy` | — | ✓* | — | — | — |
| `/dealer_join @u` | — | — | — | ✓ | — |
| `/dealer_rebuy @u` | — | — | — | ✓ | — |
| `/cashout @u N` | — | — | — | ✓ | — |
| `/undo` | — | — | — | ✓ | — |
| `/transfer_dealer` | — | — | — | ✓ | — |
| `/endgame`, `/cancel` | — | — | — | ✓ | — |
| `/admin_cancel` | — | — | — | — | ✓ (после 6ч idle) |
| `/status`, `/history`, `/stats`, `/me`, `/game` | ✓ | ✓ | ✓ | ✓ | ✓ |

`*` — с подтверждением дилера.

### 8.7. Кнопки

- `pa:<pending_action_id>:yes|no` — подтверждение запроса (только дилер).
- `cancel:<game_id>:yes|no` — подтверждение отмены игры (только дилер).
- `paid:<settlement_id>` — отметка «оплачено». Жмёт **отправитель** перевода (`from_tg_id`).

---

## 9. Алгоритм расчёта переводов

**Вход:** для каждого игрока `net_i = total_out_rub − total_in_rub`. Сумма всех net == 0 (если банк сошёлся).

**Цель:** минимум переводов; вторично — стремиться к ≤ 2 получателям на отправителя (мягкое правило).

**Шаги:**

### Шаг 1 — точные совпадения

Для каждой пары `(должник X с долгом D, кредитор Y с профитом D)` где `D` совпадает копейка-в-копейку — записать перевод X → Y, обоих исключить из дальнейшей обработки. Это минимизирует переводы у максимального числа участников до 1.

### Шаг 2 — жадный матчинг «крупнейший должник → крупнейший кредитор»

```
debtors   = отсортированный по убыванию |net| список тех, у кого net < 0
creditors = отсортированный по убыванию net список тех, у кого net > 0

while debtors не пуст и creditors не пуст:
    d = debtors[0]
    c = creditors[0]
    amount = min(|d.net|, c.net)
    добавить перевод d → c на amount
    d.net += amount
    c.net -= amount
    если d.net == 0: убрать d
    если c.net == 0: убрать c
    пересортировать
```

Каждая итерация обнуляет минимум одного игрока → итоговое число переводов ≤ N − 1.

### Шаг 3 — оптимизация под «≤ 2 получателей на отправителя»

Если после шага 2 у кого-то получилось > 2 исходящих перевода:

- для каждого такого должника D перебрать подмножества кредиторов размера ≤ 2 с суммой net == |D.net|;
- если такая комбинация найдена — заменить переводы D на эти 1–2 транзакции;
- обновить остатки кредиторов и пересчитать остальное.

Для N ≤ 10 (типичный размер домашней игры) перебор копеечный по времени. Если не нашли — оставляем результат шага 2 как есть (мягкое правило).

### Расхождение банка (`/endgame_force`)

Дельта `bank_delta_rub = bank(game_id)` распределяется среди кредиторов пропорционально их net (если положительная — уменьшает их получки) или среди должников (если отрицательная — увеличивает их выплаты), после чего запускается алгоритм. Сохраняется в `games.bank_delta_rub` для прозрачности.

---

## 10. Восстановление после рестарта

**Принцип:** в памяти бота нет ничего, что нельзя восстановить из БД.

- FSM для `/newgame` — в `fsm_states`.
- Pending-подтверждения — в `pending_actions`. Инлайн-кнопки в чате остаются валидными после рестарта, потому что обработчик `pa:*` идёт в БД.
- Активная игра — в `games(status='active')`.
- Settlements и их статус оплаты — в `settlements`.

**Старт бота:**

1. Подключение к Postgres, прогон Alembic-миграций.
2. Никакой «гидрации» в RAM — все хендлеры работают через репозитории.
3. Запуск фонового джоба expire-pending (каждые 60 секунд).

Транзакционная атомарность завершения игры (§6) гарантирует, что падение посреди `/endgame` либо откатит всё, либо завершит — промежуточного состояния не бывает.

---

## 11. Валидация и крайние случаи

| Ситуация | Поведение |
|---|---|
| `/newgame` при уже существующей active-игре | Отказ, ссылка на текущую |
| Курс при `/newgame` непропорционален | Отказ, предложение начать заново |
| `/join` от уже active-игрока | «Вы уже в игре» |
| `/rebuy` от не-участника | «Сначала /join» |
| `/cashout` дилером для не-active | Отказ |
| `/cashout` с фишками, дающими нецелое число рублей | Отказ |
| `/dealer_join @user` где у `@user` нет известного `tg_id` | Отказ; просим игрока самого написать `/join` |
| Дилер вышел из чата | Игра остаётся active, дилер не меняется. Telegram-админ может через 6 часов сделать `/admin_cancel` |
| Двойной клик на confirm-кнопке | Идемпотентность через `UPDATE ... WHERE status='pending'` |
| Бот лежал во время FSM `/newgame` | Пользователь продолжает с того же шага |
| `/undo` после `/endgame` | Запрещён |
| `/endgame` при active-участниках | Отказ, список тех, кому нужен cash-out |
| Расхождение банка ≠ 0 | Отказ; решается через `/endgame_force` |
| Игра пустая (никто не сделал buy-in) и `/endgame` | Эквивалентно `/cancel` |

**Ограничения по суммам:** только целые рубли и целые фишки. Минимум `buy_in_rub = 1`, `buy_in_chips = 1`.

---

## 12. Принятые решения по спорным вопросам

| Вопрос | Решение |
|---|---|
| Где хранится план переводов | `settlements`, постоянно |
| Что с расхождением банка | `/endgame_force` распределяет дельту пропорционально кредиторам |
| Передача дилерства | `/transfer_dealer @user`, активному игроку |
| Дилер ушёл из чата | Через 6 часов простоя — `/admin_cancel` от админа чата |
| Минимальный шаг рубля | Целые рубли |
| Авто-cashout на `/endgame` | Нет — дилер делает cashout вручную; `/endgame` блокируется до закрытия всех |
| Полный лог игры в `/game <id>` | Для всех статусов — события из `ledger` + агрегаты из `game_results` |
| Личные нотификации о долгах | Опционально: бот пишет в личку отправителю плана, если тот хоть раз делал `/start` |
| Хранение событий завершённых игр | Хранятся постоянно в `ledger`; агрегаты в `game_results` — кеш для быстрых запросов |

---

## 13. План реализации по этапам

### Этап 1 — каркас
- Проект, dependencies, docker-compose, Alembic
- Модели SQLAlchemy для всех таблиц
- Подключение aiogram, базовый `/start`, `/help`
- Кастомный FSM-storage поверх `fsm_states`

### Этап 2 — игра без подтверждений
- `/newgame` (FSM + одной строкой)
- Команды дилера: `/dealer_join`, `/dealer_rebuy`, `/cashout`
- `/status`, `/me`
- `/undo`
- Учёт `players` при каждом сообщении

### Этап 3 — подтверждения и команды игроков
- `pending_actions` + универсальный confirm-флоу
- `/join`, `/rebuy`
- Фоновый expire-джоб

### Этап 4 — завершение и расчёт
- Алгоритм settlements (3 шага)
- Транзакция `/endgame`, `/endgame_force`, `/cancel`
- Кнопка «Я заплатил»
- `/transfer_dealer`, `/admin_cancel`

### Этап 5 — история и статистика
- `/history`, `/game <id>`, `/stats`
- Опциональные личные нотификации

### Этап 6 — устойчивость и операционка
- Логирование, метрики
- Тесты на алгоритм расчёта (свойства: сумма == 0, число переводов ≤ N−1)
- Тесты на восстановление после рестарта

---

## 14. Вне scope (на будущее)

- Поддержка нескольких валют
- Ручная корректировка фишек без cash-out
- Учёт аренды/комиссии стола (rake)
- Веб-интерфейс/просмотр истории
- Партиционирование таблиц (имеет смысл при сотнях тысяч игр)
- Экспорт результатов в CSV/Excel

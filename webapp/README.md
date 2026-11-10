# Банкир · Telegram Mini App

Фронтенд Mini App для покерного бота «Банкир»: Vite + React 18 + TypeScript, без UI-китов.
Дизайн и токены: Figma «Дизайны · Иван» → «Дизайны · проекты» (секции 0–3).

## Запуск

```bash
cd webapp
npm install
npm run dev        # http://localhost:5173 — мок API + мок Telegram (VITE_API_URL пуст)
npm run build      # tsc --noEmit + сборка в dist/
npm run typecheck
```

В браузере (вне Telegram) подставляется мок `window.Telegram.WebApp`: пользователь @ivan,
вместо нативных кнопок — HTML-шапка «‹ Назад» и нижняя MainButton.

Полезные query-параметры в dev:

- `?startapp=-1001001` — эмуляция deep-link (start_param = chat_id): сразу открывает активную игру чата
  (в моке: `-1001001` — Иван дилер и играет; `-1001002` и `-1001003` (курс 1₽ = 2 фишки) — чужие игры, войти нельзя:
  у игрока только одна активная игра; `-1001004` — нет игры, но создать её тоже нельзя, пока Иван занят в #13.
  В picker'е buy-in занятые в других играх (@masha, @dima, @kolya, @petya_k) показаны как «Занят»);
- без `startapp` приложение сразу открывает текущую игру пользователя, если она есть;
- `?fullscreen=1` — эмуляция fullscreen-режима телефона (статус-бар 47px + зона кнопок Telegram 46px);
- `?theme=dark|light` — «системная» тема в браузере (вместо темы Telegram); «···» в шапке переключает тему на лету.

Тему выбирает пользователь кнопкой на главном экране: как в Telegram / светлая / тёмная. Выбор лежит в `localStorage`
(`bankir.theme`, читается inline-скриптом в `index.html` до первого рендера) и в `Telegram.WebApp.CloudStorage`.
themeParams Telegram применяются, только пока итоговая схема совпадает с темой Telegram; иначе — палитра из `tokens.css`.

Мок живёт в памяти вкладки: через ~20 с в игре #13 приходит запрос @serega (in-app баннер дилеру),
свои запросы игрока «чужой» дилер подтверждает через ~6 с (15% — отказ).

## Переменные окружения (`.env.local`)

| Переменная | Назначение |
|---|---|
| `VITE_API_URL` | База Go-бэкенда. Пусто: в `npm run dev` — in-memory мок, в прод-сборке — тот же origin (`/api` отдаёт Go-сервер вместе с фронтом). |
| `VITE_USE_MOCK=1` | Принудительно мок (в том числе в сборке). |
| `VITE_BOT_USERNAME` | Username бота без @ — ссылка «Добавить бота в группу». |
| `VITE_APP_SHORT_NAME` | Short name Mini App — ссылка `t.me/<bot>/<app>?startapp=<chat_id>`. |

Реальный клиент (`src/api/http.ts`) шлёт `Authorization: tma <initData>`; бэкенд обязан проверить
подпись initData ботовым токеном. Ошибки ожидаются в виде `{"error":{"code","message","details"}}`.

## Открыть в Telegram

1. Соберите (`npm run build`) и разместите `dist/` по HTTPS (Coolify / за Go-сервером; `base: './'`,
   поэтому подходит любой путь). Для отладки — `npm run dev` + туннель (`cloudflared tunnel --url http://localhost:5173`).
2. BotFather → `/mybots` → бот → **Bot Settings → Configure Mini App → Enable Mini App** → указать URL.
   Либо `/newapp` — создать Mini App с short name (например `app`) для deep-link'ов.
3. (Опц.) **Menu Button** → тот же URL, чтобы открывать из чата с ботом.
4. Ссылка на стол: `https://t.me/<bot>/<short_name>?startapp=<chat_id>` — бот может класть её
   в сообщение `/newgame` кнопкой. chat_id групп отрицательный — допустимо (символы `-0-9`).

## Структура

```
src/
  api/        types.ts (DTO), client.ts (интерфейс BankirApi + ApiError), http.ts (fetch), mock.ts, index.ts (выбор по env)
  telegram/   telegram.ts (обёртка + мок WebApp, haptic, start_param), theme.ts (выбор темы, themeParams → токены, safe-area),
              buttons.tsx (MainButton/BackButton через стек владельцев, браузерные заглушки)
  lib/        format.ts, rate.ts (пропорциональность, chips*rate ∈ ℤ), stats.ts (серии, график), settle.ts (§9 для мока), hooks.ts
  components/ ui.tsx (базовые: Sheet, Confirm, Toast, Segmented…), domain.tsx (PlayerRow, RequestCard, GameCard, NetChart…)
  screens/    Welcome, GamePicker, Game (+GameSheets: игрок/дилер), Results, Profile, Leaderboard, NewGame
  router.tsx  стековый роутер; «Назад» = pop
```

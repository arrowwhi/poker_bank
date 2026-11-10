// In-memory мок API с реалистичными данными. Живёт в памяти вкладки (сбрасывается на reload).
// Имитирует: задержку сети, авто-решение запросов чужим дилером, входящие запросы для дилера.
import { ApiError, type BankirApi } from './client';
import type {
  BusyRef,
  ChatMember,
  Chat,
  CreateGameRequest,
  GameDetail,
  GameResult,
  GameRole,
  GameStatus,
  GameSummary,
  HistoryItem,
  Leaderboard,
  LeaderboardEntry,
  LeaderboardPeriod,
  LedgerEntry,
  LedgerType,
  Me,
  OpenDebt,
  Participant,
  PendingAction,
  PendingType,
  PlayerStats,
  Rate,
  Settlement,
  User,
} from './types';
import { adjustForDelta, computeSettlements } from '../lib/settle';
import { isProportional, rubForChips } from '../lib/rate';

const ME = 1;
const MIN = 60_000;
const DAY = 24 * 60 * MIN;

const USERS: User[] = [
  { id: 1, username: 'ivan', display_name: 'Иван Захаров' },
  { id: 2, username: 'vasya', display_name: 'Василий Петров' },
  { id: 3, username: 'petya', display_name: 'Пётр Смирнов' },
  { id: 4, username: 'masha', display_name: 'Мария Ковалёва' },
  { id: 5, username: 'kolya', display_name: 'Николай Орлов' },
  { id: 6, username: 'dima', display_name: 'Дмитрий Лебедев' },
  { id: 7, username: 'anya', display_name: 'Анна Соколова' },
  { id: 8, username: 'serega', display_name: 'Сергей Волков' },
  { id: 9, username: 'petya_k', display_name: 'Пётр Кожевников' },
  { id: 10, username: null, display_name: 'Олег Новиков' },
];
const u = (id: number): User => USERS.find((x) => x.id === id)!;

interface MockChat { id: number; title: string; members: number[] }
const CHATS: MockChat[] = [
  { id: -1001001, title: 'Покер по пятницам', members: [1, 2, 3, 4, 5, 6, 8, 9, 10] },
  { id: -1001002, title: 'Офис · покер-клуб', members: [1, 4, 6, 7, 8] },
  { id: -1001003, title: 'Дача 2026', members: [1, 2, 5, 7, 9] },
  { id: -1001004, title: 'Семейный стол', members: [1, 7, 10] },
];

interface MockGame {
  id: number;
  number: number;
  chat_id: number;
  dealer: number;
  rate: Rate;
  status: GameStatus;
  started_at: number;
  ended_at: number | null;
  bank_delta_rub: number;
  ledger: { id: number; type: LedgerType; player: number; rub: number; chips: number; by: number; at: number; void: boolean }[];
  pending: { id: number; type: PendingType; requester: number; target: number; rub: number; chips: number; status: PendingAction['status']; at: number; resolved_at: number | null; resolved_by: number | null }[];
  results: GameResult[] | null;
  settlements: Settlement[] | null;
}

let seq = 1000;
const nextId = () => ++seq;

function rng(seed: number) {
  return () => {
    seed |= 0; seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const R1000: Rate = { buy_in_rub: 1000, buy_in_chips: 1000, rebuy_rub: 500, rebuy_chips: 500 };
const R500: Rate = { buy_in_rub: 500, buy_in_chips: 500, rebuy_rub: 500, rebuy_chips: 500 };
const RDACHA: Rate = { buy_in_rub: 1000, buy_in_chips: 2000, rebuy_rub: 1000, rebuy_chips: 2000 };

function buildState(): MockGame[] {
  const now = Date.now();
  const games: MockGame[] = [];
  const rand = rng(42);

  // ── история: сгенерированные завершённые игры ──
  const genHistory = (chat: MockChat, count: number, spacingDays: number, idBase: number, rate: Rate, ivanBias: number) => {
    for (let k = 1; k <= count; k++) {
      const ended = now - (count - k + 1) * spacingDays * DAY - 3 * DAY;
      const pool = chat.members.filter((m) => m !== ME).sort(() => rand() - 0.5);
      const players = [ME, ...pool.slice(0, 3 + Math.floor(rand() * 3))];
      if (rand() < 0.12) players.shift(); // иногда Иван пропускает
      const nets = new Map<number, number>();
      let sum = 0;
      players.forEach((p, i) => {
        if (i === players.length - 1) return;
        let v = Math.round((rand() * 5000 - 2600) / 100) * 100;
        if (p === ME) v += ivanBias;
        nets.set(p, v);
        sum += v;
      });
      nets.set(players[players.length - 1], -sum);
      games.push(finishedGame(idBase + k, k, chat.id, players[1] ?? ME, rate, ended, nets, rand, true));
    }
  };
  genHistory(CHATS[0], 11, 52, 100, R1000, 450);
  genHistory(CHATS[1], 9, 60, 200, R500, 150);

  // ── «Покер по пятницам» #12: незакрытые расчёты Ивана ──
  {
    const nets = new Map([[ME, -500], [2, 500], [3, 1000], [4, -1000], [5, 0]]);
    const g = finishedGame(112, 12, CHATS[0].id, 2, R1000, now - 3 * DAY, nets, rand, false);
    g.settlements = [
      st(g.id, ME, 2, 500, false, false),
      st(g.id, 4, 3, 1000, true, true),
    ];
    games.push(g);
  }
  // @kolya должен Ивану 300 — из игры #11 (сгенерированной): подменим её расчёты
  {
    const g = games.find((x) => x.id === 111)!;
    const nets = new Map([[ME, 300], [5, -300], [6, 700], [9, -700]]);
    Object.assign(g, finishedGame(111, 11, CHATS[0].id, 2, R1000, g.ended_at!, nets, rand, true));
    g.settlements = [st(g.id, 5, ME, 300, true, false), st(g.id, 9, 6, 700, true, true)];
  }

  // ── активная игра #13: Иван — дилер ──
  const g13: MockGame = {
    id: 113, number: 13, chat_id: CHATS[0].id, dealer: ME, rate: R1000, status: 'active',
    started_at: now - 130 * MIN, ended_at: null, bank_delta_rub: 0, ledger: [], pending: [], results: null, settlements: null,
  };
  const L = (g: MockGame, type: LedgerType, player: number, rub: number, chips: number, by: number, at: number) =>
    g.ledger.push({ id: nextId(), type, player, rub, chips, by, at, void: false });
  L(g13, 'BUY_IN', ME, 1000, 1000, ME, now - 128 * MIN);
  L(g13, 'BUY_IN', 2, 1000, 1000, ME, now - 126 * MIN);
  L(g13, 'BUY_IN', 3, 1000, 1000, ME, now - 125 * MIN);
  L(g13, 'BUY_IN', 5, 1000, 1000, ME, now - 120 * MIN);
  L(g13, 'REBUY', 3, 500, 500, ME, now - 80 * MIN);
  L(g13, 'REBUY', 2, 500, 500, ME, now - 60 * MIN);
  L(g13, 'CASH_OUT', 5, 2300, 2300, ME, now - 30 * MIN);
  L(g13, 'REBUY', 3, 500, 500, ME, now - 12 * MIN);
  g13.pending.push(
    { id: nextId(), type: 'REBUY', requester: 3, target: 3, rub: 500, chips: 500, status: 'pending', at: now - 2 * MIN, resolved_at: null, resolved_by: null },
    { id: nextId(), type: 'JOIN', requester: 10, target: 10, rub: 1000, chips: 1000, status: 'pending', at: now - 10_000, resolved_at: null, resolved_by: null },
  );
  games.push(g13);

  // ── активная игра #10 в офисе: дилер @masha (играет), Иван НЕ в игре (он занят в #13) ──
  const g41: MockGame = {
    id: 210, number: 10, chat_id: CHATS[1].id, dealer: 4, rate: R500, status: 'active',
    started_at: now - 85 * MIN, ended_at: null, bank_delta_rub: 0, ledger: [], pending: [], results: null, settlements: null,
  };
  L(g41, 'BUY_IN', 4, 500, 500, 4, now - 84 * MIN);
  L(g41, 'BUY_IN', 6, 500, 500, 4, now - 80 * MIN);
  L(g41, 'BUY_IN', 7, 500, 500, 4, now - 79 * MIN);
  L(g41, 'REBUY', 6, 500, 500, 4, now - 20 * MIN);
  games.push(g41);

  // ── «Дача 2026» #7: дилер @kolya, Иван не в игре, курс 1₽ = 2 фишки.
  //    Игроки не пересекаются с #13 и #10: у каждого максимум одна активная игра. ──
  const g7: MockGame = {
    id: 307, number: 7, chat_id: CHATS[2].id, dealer: 5, rate: RDACHA, status: 'active',
    started_at: now - 200 * MIN, ended_at: null, bank_delta_rub: 0, ledger: [], pending: [], results: null, settlements: null,
  };
  L(g7, 'BUY_IN', 5, 1000, 2000, 5, now - 199 * MIN);
  L(g7, 'BUY_IN', 9, 1000, 2000, 5, now - 185 * MIN);
  L(g7, 'REBUY', 9, 1000, 2000, 5, now - 100 * MIN);
  games.push(g7);

  // «Семейный стол» — прошлая игра для пресета «как в прошлый раз»
  games.push(finishedGame(401, 1, CHATS[3].id, 7, R500, now - 40 * DAY, new Map([[ME, 200], [7, -500], [10, 300]]), rand, true));

  return games;
}

function st(gameId: number, from: number, to: number, amount: number, paid: boolean, received: boolean): Settlement {
  const at = new Date(Date.now() - DAY).toISOString();
  return {
    id: nextId(), game_id: gameId, from: u(from), to: u(to), amount_rub: amount,
    is_paid: paid, paid_at: paid ? at : null, is_received: received, received_at: received ? at : null,
  };
}

function finishedGame(id: number, number: number, chat_id: number, dealer: number, rate: Rate, ended: number,
  nets: Map<number, number>, rand: () => number, closed: boolean): MockGame {
  const results: GameResult[] = [];
  for (const [pid, net] of nets) {
    let rebuys = Math.floor(rand() * 3);
    let tin = rate.buy_in_rub + rebuys * rate.rebuy_rub;
    while (tin + net < 0) { rebuys++; tin += rate.rebuy_rub; }
    const tout = tin + net;
    results.push({
      user: u(pid), buy_in_count: 1, rebuy_count: rebuys, total_in_rub: tin, total_out_rub: tout,
      total_out_chips: Math.round((tout * rate.buy_in_chips) / rate.buy_in_rub), net_rub: net,
    });
  }
  const settlements = computeSettlements(nets).map((t) => st(id, t.from, t.to, t.amount, closed, closed));
  return {
    id, number, chat_id, dealer, rate, status: 'finished', started_at: ended - 4 * 60 * MIN, ended_at: ended,
    bank_delta_rub: 0, ledger: [], pending: [], results, settlements,
  };
}

// ────────────────────────────────────────────────────────────────

const delay = (ms = 220) => new Promise((r) => setTimeout(r, ms + Math.random() * 120));
const clone = <T,>(x: T): T => structuredClone(x);
const iso = (t: number) => new Date(t).toISOString();
const fail = (status: number, code: string, message: string, details?: Record<string, unknown>): never => {
  throw new ApiError(status, { code, message, details });
};

export class MockApi implements BankirApi {
  private games = buildState();
  private timers: number[] = [];

  constructor() {
    // Имитация входящих запросов в игре, где Иван — дилер (для in-app баннера).
    this.timers.push(window.setTimeout(() => this.incoming(113, 'JOIN', 8), 20_000));
    this.timers.push(window.setTimeout(() => this.incoming(113, 'REBUY', 2), 75_000));
  }

  private incoming(gameId: number, type: PendingType, who: number) {
    const g = this.games.find((x) => x.id === gameId);
    if (!g || g.status !== 'active') return;
    if (g.pending.some((p) => p.status === 'pending' && p.target === who && p.type === type)) return;
    const active = this.isActive(g, who);
    if (type === 'JOIN' && (active || this.busyIn(who, g.id))) return;
    if (type === 'REBUY' && !active) return;
    const [rub, chips] = type === 'JOIN' ? [g.rate.buy_in_rub, g.rate.buy_in_chips] : [g.rate.rebuy_rub, g.rate.rebuy_chips];
    g.pending.push({ id: nextId(), type, requester: who, target: who, rub, chips, status: 'pending', at: Date.now(), resolved_at: null, resolved_by: null });
  }

  // ── вычисления ──
  private chat(id: number) { return CHATS.find((c) => c.id === id) ?? fail(404, 'not_found', 'Чат не найден'); }
  private game(id: number) { return this.games.find((g) => g.id === id) ?? fail(404, 'not_found', 'Игра не найдена'); }
  private live(g: MockGame) { return g.ledger.filter((e) => !e.void); }
  private isActive(g: MockGame, pid: number) {
    const es = this.live(g).filter((e) => e.player === pid);
    return es.length > 0 && es[es.length - 1].type !== 'CASH_OUT';
  }
  /** Другая активная игра, где пользователь дилер или active-игрок (правило «одна игра на игрока»). */
  private busyIn(pid: number, exceptGameId?: number): BusyRef | null {
    const g = this.games.find((x) => x.status === 'active' && x.id !== exceptGameId && (x.dealer === pid || this.isActive(x, pid)));
    return g ? { game_id: g.id, chat_id: g.chat_id, chat_title: this.chat(g.chat_id).title } : null;
  }
  private assertFree(pid: number, gameId: number | undefined, self: boolean) {
    const b = this.busyIn(pid, gameId);
    if (!b) return;
    const msg = self ? `Вы уже в игре в «${b.chat_title}» — сначала выйдите из неё` : `${u(pid).username ? '@' + u(pid).username : u(pid).display_name} уже в игре в «${b.chat_title}»`;
    fail(409, 'busy_in_other_game', msg, { ...b });
  }
  private bank(g: MockGame) {
    return this.live(g).reduce((s, e) => s + (e.type === 'CASH_OUT' ? -e.rub : e.rub), 0);
  }
  private participants(g: MockGame): Participant[] {
    if (g.status === 'finished' && g.results && !g.ledger.length) {
      return g.results.map((r) => ({ ...r, is_active: false, joined_at: iso(g.started_at) }));
    }
    const map = new Map<number, Participant>();
    for (const e of this.live(g)) {
      let p = map.get(e.player);
      if (!p) {
        p = { user: u(e.player), is_active: true, buy_in_count: 0, rebuy_count: 0, total_in_rub: 0, total_out_rub: 0, total_out_chips: 0, net_rub: 0, joined_at: iso(e.at) };
        map.set(e.player, p);
      }
      if (e.type === 'BUY_IN') { p.buy_in_count++; p.total_in_rub += e.rub; p.is_active = true; }
      if (e.type === 'REBUY') { p.rebuy_count++; p.total_in_rub += e.rub; }
      if (e.type === 'CASH_OUT') { p.total_out_rub += e.rub; p.total_out_chips += e.chips; p.is_active = false; }
      p.net_rub = p.total_out_rub - p.total_in_rub;
    }
    return [...map.values()];
  }
  private role(g: MockGame): GameRole {
    if (g.status === 'active' && g.dealer === ME) return 'dealer';
    const p = this.participants(g).find((x) => x.user.id === ME);
    if (!p) return 'outsider';
    return p.is_active && g.status === 'active' ? 'player' : 'out';
  }
  private pa(g: MockGame, p: MockGame['pending'][number]): PendingAction {
    return {
      id: p.id, game_id: g.id, type: p.type, requester: u(p.requester), target: u(p.target), amount_rub: p.rub, amount_chips: p.chips,
      status: p.status, created_at: iso(p.at), resolved_at: p.resolved_at ? iso(p.resolved_at) : null, resolved_by: p.resolved_by ? u(p.resolved_by) : null,
    };
  }
  private summary(g: MockGame): GameSummary {
    const ps = this.participants(g);
    return {
      id: g.id, number: g.number, chat_id: g.chat_id, chat_title: this.chat(g.chat_id).title, status: g.status, dealer: u(g.dealer), rate: g.rate,
      started_at: iso(g.started_at), ended_at: g.ended_at ? iso(g.ended_at) : null,
      players_total: ps.length, players_active: ps.filter((p) => p.is_active).length,
      bank_rub: g.status === 'finished' && !g.ledger.length ? ps.reduce((s, p) => s + p.total_in_rub, 0) : this.bank(g),
      my_role: this.role(g),
    } as GameSummary;
  }
  private detail(g: MockGame): GameDetail {
    const ledger: LedgerEntry[] = g.ledger.map((e) => ({
      id: e.id, type: e.type, player: u(e.player), amount_rub: e.rub, amount_chips: e.chips, created_by: u(e.by), created_at: iso(e.at), is_void: e.void,
    }));
    return clone({
      ...this.summary(g),
      participants: this.participants(g),
      pending: g.pending.filter((p) => p.status === 'pending').map((p) => this.pa(g, p)),
      my_busy_in: this.busyIn(ME, g.id),
      my_requests: g.pending.filter((p) => p.target === ME).slice(-5).map((p) => this.pa(g, p)),
      ledger,
      bank_delta_rub: g.bank_delta_rub,
      results: g.results,
      settlements: g.settlements,
    });
  }
  private requireDealer(g: MockGame) {
    if (g.status !== 'active') fail(409, 'not_active', 'Игра уже завершена');
    if (g.dealer !== ME) fail(403, 'forbidden', 'Только дилер');
  }
  private add(g: MockGame, type: LedgerType, player: number, rub: number, chips: number, by = ME) {
    g.ledger.push({ id: nextId(), type, player, rub, chips, by, at: Date.now(), void: false });
  }

  // ── API ──
  async getMe(): Promise<Me> {
    await delay();
    const hist = this.history(ME, null);
    const active = this.games.find((g) => g.status === 'active' && (g.dealer === ME || this.isActive(g, ME)));
    return clone({
      user: u(ME), member_since: iso(Date.now() - 600 * DAY), games_played: hist.length,
      total_net_rub: hist.reduce((s, h) => s + h.net_rub, 0), active_game: active ? this.summary(active) : null,
    });
  }

  async listChats(): Promise<Chat[]> {
    await delay();
    // Как на бэкенде: по числу завершённых игр, при равенстве — по дате последней игры.
    return clone(CHATS.filter((c) => c.members.includes(ME)).map((c) => {
      const active = this.games.find((g) => g.chat_id === c.id && g.status === 'active');
      const last = this.games.filter((g) => g.chat_id === c.id).sort((a, b) => b.started_at - a.started_at)[0];
      const finished = this.games.filter((g) => g.chat_id === c.id && g.status === 'finished');
      const lastEnded = finished.reduce((m, g) => Math.max(m, g.ended_at ?? 0), 0);
      return {
        id: c.id, title: c.title, members_count: c.members.length,
        active_game: active ? { id: active.id, dealer: u(active.dealer) } : null, last_rate: last?.rate ?? null,
        games_count: finished.length, last_game_at: lastEnded ? iso(lastEnded) : null,
      };
    }).sort((a, b) => b.games_count - a.games_count || (b.last_game_at ?? '').localeCompare(a.last_game_at ?? '')));
  }

  async listChatMembers(chatId: number): Promise<ChatMember[]> {
    await delay();
    const active = this.games.find((g) => g.chat_id === chatId && g.status === 'active');
    return clone(this.chat(chatId).members.map((id) => ({ ...u(id), busy_in: this.busyIn(id, active?.id) })));
  }

  async listGames(params?: { chat_id?: number; status?: GameStatus }): Promise<GameSummary[]> {
    await delay();
    return clone(this.games
      .filter((g) => (params?.chat_id ? g.chat_id === params.chat_id : this.chat(g.chat_id).members.includes(ME)))
      .filter((g) => (params?.status ? g.status === params.status : true))
      .sort((a, b) => b.started_at - a.started_at)
      .map((g) => this.summary(g)));
  }

  async getGame(id: number): Promise<GameDetail> {
    await delay(120);
    return this.detail(this.game(id));
  }

  async createGame(req: CreateGameRequest): Promise<GameDetail> {
    await delay(400);
    const chat = this.chat(req.chat_id);
    if (this.games.some((g) => g.chat_id === chat.id && g.status === 'active')) fail(409, 'game_exists', 'В этом чате уже идёт игра');
    this.assertFree(ME, undefined, true);
    const r = req.rate;
    if ([r.buy_in_rub, r.buy_in_chips, r.rebuy_rub, r.rebuy_chips].some((x) => !Number.isInteger(x) || x <= 0) || !isProportional(r))
      fail(422, 'bad_rate', 'Курс rebuy не пропорционален buy-in');
    const number = this.games.filter((g) => g.chat_id === chat.id).length + 1;
    const g: MockGame = {
      id: nextId(), number, chat_id: chat.id, dealer: ME, rate: { ...r }, status: 'active', started_at: Date.now(), ended_at: null,
      bank_delta_rub: 0, ledger: [], pending: [], results: null, settlements: null,
    };
    if (req.dealer_buy_in) this.add(g, 'BUY_IN', ME, r.buy_in_rub, r.buy_in_chips);
    this.games.push(g);
    // через 15 с кто-нибудь просится в игру
    this.timers.push(window.setTimeout(() => {
      const who = chat.members.find((m) => m !== ME);
      if (who) this.incoming(g.id, 'JOIN', who);
    }, 15_000));
    return this.detail(g);
  }

  private request(gameId: number, type: PendingType, chips?: number): PendingAction {
    const g = this.game(gameId);
    if (g.status !== 'active') fail(409, 'not_active', 'Игра уже завершена');
    const active = this.isActive(g, ME);
    if (type === 'JOIN' && active) fail(409, 'already_in_game', 'Вы уже в игре');
    if (type === 'JOIN') this.assertFree(ME, g.id, true);
    if (type !== 'JOIN' && !active) fail(409, 'not_in_game', 'Сначала сделайте buy-in');
    if (g.pending.some((p) => p.status === 'pending' && p.target === ME)) fail(409, 'already_pending', 'Запрос уже отправлен');
    let rub: number; let ch: number;
    if (type === 'CASH_OUT') {
      ch = chips!;
      const r = rubForChips(g.rate, ch);
      if (!Number.isInteger(ch) || ch < 0 || r === null) fail(422, 'not_integer_rub', 'Фишки не дают целого числа рублей');
      rub = r!;
    } else if (type === 'JOIN') { rub = g.rate.buy_in_rub; ch = g.rate.buy_in_chips; }
    else { rub = g.rate.rebuy_rub; ch = g.rate.rebuy_chips; }
    // Дилер-игрок: действие применяется сразу, без подтверждения.
    if (g.dealer === ME) {
      this.add(g, type === 'JOIN' ? 'BUY_IN' : type, ME, rub, ch);
      const p = { id: nextId(), type, requester: ME, target: ME, rub, chips: ch, status: 'confirmed' as const, at: Date.now(), resolved_at: Date.now(), resolved_by: ME };
      g.pending.push(p);
      return this.pa(g, p);
    }
    const p = { id: nextId(), type, requester: ME, target: ME, rub, chips: ch, status: 'pending' as const, at: Date.now(), resolved_at: null, resolved_by: null };
    g.pending.push(p);
    // Чужой дилер решает запрос через ~6 с (в 15% случаев — отказ).
    this.timers.push(window.setTimeout(() => {
      const cur = g.pending.find((x) => x.id === p.id);
      if (!cur || cur.status !== 'pending' || g.status !== 'active') return;
      const ok = Math.random() > 0.15 && !(type === 'JOIN' && this.busyIn(ME, g.id));
      Object.assign(cur, { status: ok ? 'confirmed' : 'rejected', resolved_at: Date.now(), resolved_by: g.dealer });
      if (ok) this.add(g, type === 'JOIN' ? 'BUY_IN' : type, ME, rub, ch, g.dealer);
    }, 6000));
    return clone(this.pa(g, p));
  }

  async requestJoin(id: number) { await delay(); return this.request(id, 'JOIN'); }
  async requestRebuy(id: number) { await delay(); return this.request(id, 'REBUY'); }
  async requestCashout(id: number, chips: number) { await delay(); return this.request(id, 'CASH_OUT', chips); }

  async cancelRequest(gameId: number, pid: number) {
    await delay();
    const g = this.game(gameId);
    const p = g.pending.find((x) => x.id === pid && x.requester === ME && x.status === 'pending');
    if (p) Object.assign(p, { status: 'cancelled', resolved_at: Date.now(), resolved_by: ME });
    return this.detail(g);
  }

  private resolve(gameId: number, pid: number, ok: boolean) {
    const g = this.game(gameId);
    this.requireDealer(g);
    const p = g.pending.find((x) => x.id === pid);
    if (!p) fail(404, 'not_found', 'Запрос не найден');
    if (p!.status !== 'pending') return this.detail(g); // идемпотентно
    if (ok && p!.type === 'JOIN' && this.busyIn(p!.target, g.id)) {
      // Игрок успел сесть за другой стол — запрос отклоняется автоматически.
      Object.assign(p!, { status: 'rejected', resolved_at: Date.now(), resolved_by: ME });
      this.assertFree(p!.target, g.id, false);
    }
    Object.assign(p!, { status: ok ? 'confirmed' : 'rejected', resolved_at: Date.now(), resolved_by: ME });
    if (ok) this.add(g, p!.type === 'JOIN' ? 'BUY_IN' : p!.type, p!.target, p!.rub, p!.chips);
    return this.detail(g);
  }
  async approve(id: number, pid: number) { await delay(); return this.resolve(id, pid, true); }
  async reject(id: number, pid: number) { await delay(); return this.resolve(id, pid, false); }

  private dropPending(g: MockGame, target: number, types: PendingType[]) {
    for (const p of g.pending) if (p.status === 'pending' && p.target === target && types.includes(p.type))
      Object.assign(p, { status: 'cancelled', resolved_at: Date.now(), resolved_by: ME });
  }

  async dealerBuyIn(id: number, userId: number) {
    await delay();
    const g = this.game(id);
    this.requireDealer(g);
    if (this.isActive(g, userId)) fail(409, 'already_in_game', 'Игрок уже в игре');
    this.assertFree(userId, g.id, userId === ME);
    this.add(g, 'BUY_IN', userId, g.rate.buy_in_rub, g.rate.buy_in_chips);
    this.dropPending(g, userId, ['JOIN']);
    return this.detail(g);
  }
  async dealerRebuy(id: number, userId: number) {
    await delay();
    const g = this.game(id);
    this.requireDealer(g);
    if (!this.isActive(g, userId)) fail(409, 'not_in_game', 'Игрок не в игре');
    this.add(g, 'REBUY', userId, g.rate.rebuy_rub, g.rate.rebuy_chips);
    this.dropPending(g, userId, ['REBUY']);
    return this.detail(g);
  }
  async dealerCashout(id: number, userId: number, chips: number) {
    await delay();
    const g = this.game(id);
    this.requireDealer(g);
    if (!this.isActive(g, userId)) fail(409, 'not_in_game', 'Игрок не в игре');
    const rub = rubForChips(g.rate, chips);
    if (!Number.isInteger(chips) || chips < 0 || rub === null) fail(422, 'not_integer_rub', 'Фишки не дают целого числа рублей');
    this.add(g, 'CASH_OUT', userId, rub!, chips);
    this.dropPending(g, userId, ['REBUY', 'CASH_OUT']);
    return this.detail(g);
  }
  async undo(id: number, n = 1) {
    await delay();
    const g = this.game(id);
    this.requireDealer(g);
    const live = this.live(g);
    for (const e of live.slice(-n)) e.void = true;
    return this.detail(g);
  }
  async endGame(id: number, force = false) {
    await delay(500);
    const g = this.game(id);
    this.requireDealer(g);
    const ps = this.participants(g);
    if (!ps.length) { g.status = 'cancelled'; g.ended_at = Date.now(); return this.detail(g); }
    const act = ps.filter((p) => p.is_active);
    if (act.length) fail(409, 'active_players', 'Не все сделали cash-out', { user_ids: act.map((p) => p.user.id) });
    const bank = this.bank(g);
    if (bank !== 0 && !force) fail(409, 'bank_mismatch', `Расхождение: банк = ${bank}₽`, { bank_rub: bank });
    let nets = new Map(ps.map((p) => [p.user.id, p.net_rub]));
    if (bank !== 0) nets = adjustForDelta(nets);
    g.results = ps.map((p) => ({
      user: p.user, buy_in_count: p.buy_in_count, rebuy_count: p.rebuy_count, total_in_rub: p.total_in_rub,
      total_out_rub: p.total_out_rub, total_out_chips: p.total_out_chips, net_rub: p.net_rub,
    }));
    g.settlements = computeSettlements(nets).map((t) => st(g.id, t.from, t.to, t.amount, false, false));
    g.bank_delta_rub = bank;
    g.status = 'finished';
    g.ended_at = Date.now();
    for (const p of g.pending) if (p.status === 'pending') p.status = 'cancelled';
    return this.detail(g);
  }
  async cancelGame(id: number) {
    await delay();
    const g = this.game(id);
    this.requireDealer(g);
    g.status = 'cancelled';
    g.ended_at = Date.now();
    for (const p of g.pending) if (p.status === 'pending') p.status = 'cancelled';
    return this.detail(g);
  }
  async transferDealer(id: number, userId: number) {
    await delay();
    const g = this.game(id);
    this.requireDealer(g);
    if (!this.isActive(g, userId)) fail(409, 'not_in_game', 'Передать можно только активному игроку');
    this.assertFree(userId, g.id, false);
    g.dealer = userId;
    return this.detail(g);
  }

  async settle(id: number) {
    await delay();
    const g = this.game(id);
    const mine = (g.settlements ?? []).filter((s) => s.from.id === ME || s.to.id === ME);
    if (!mine.length) fail(409, 'not_in_settlements', 'Вас нет в переводах');
    const now = iso(Date.now());
    let changed = false;
    for (const s of mine) {
      if (s.from.id === ME && !s.is_paid) { s.is_paid = true; s.paid_at = now; changed = true; }
      if (s.to.id === ME && !s.is_received) { s.is_received = true; s.received_at = now; changed = true; }
    }
    if (!changed) fail(409, 'already_marked', 'Уже отмечено');
    return this.detail(g);
  }

  // ── статистика ──
  private history(userId: number, chatId: number | null): HistoryItem[] {
    return this.games
      .filter((g) => g.status === 'finished' && (chatId === null || g.chat_id === chatId))
      .flatMap((g) => {
        const r = g.results?.find((x) => x.user.id === userId);
        return r ? [{
          game_id: g.id, game_number: g.number, chat_id: g.chat_id, chat_title: this.chat(g.chat_id).title, ended_at: iso(g.ended_at!),
          buy_in_count: r.buy_in_count, rebuy_count: r.rebuy_count, total_in_rub: r.total_in_rub, total_out_rub: r.total_out_rub, net_rub: r.net_rub,
        }] : [];
      })
      .sort((a, b) => b.ended_at.localeCompare(a.ended_at));
  }

  private board(chatId: number, period: LeaderboardPeriod): LeaderboardEntry[] {
    const from = period === 'all' ? 0 : Date.now() - (period === 'year' ? 365 : 30) * DAY;
    const agg = new Map<number, { n: number; net: number; wins: number }>();
    for (const g of this.games) {
      if (g.status !== 'finished' || g.chat_id !== chatId || (g.ended_at ?? 0) < from) continue;
      for (const r of g.results ?? []) {
        const a = agg.get(r.user.id) ?? { n: 0, net: 0, wins: 0 };
        a.n++; a.net += r.net_rub; if (r.net_rub > 0) a.wins++;
        agg.set(r.user.id, a);
      }
    }
    return [...agg].sort((a, b) => b[1].net - a[1].net)
      .map(([id, a], i) => ({ place: i + 1, user: u(id), games_played: a.n, total_net_rub: a.net, win_rate: a.n ? a.wins / a.n : 0 }));
  }

  private stats(userId: number, chatId: number | null): PlayerStats {
    const history = this.history(userId, chatId);
    const n = history.length;
    const total = history.reduce((s, h) => s + h.net_rub, 0);
    const lbChat = chatId ?? CHATS[0].id;
    const board = this.board(lbChat, 'all');
    const me = board.find((e) => e.user.id === userId);
    let debts: OpenDebt[] | null = null;
    if (userId === ME) {
      debts = this.games.flatMap((g) => (g.settlements ?? [])
        .filter((s) => (s.from.id === ME && !s.is_paid) || (s.to.id === ME && !s.is_received))
        .map((s) => ({ ...s, chat_title: this.chat(g.chat_id).title, game_number: g.number, game_ended_at: iso(g.ended_at!), direction: s.from.id === ME ? 'out' as const : 'in' as const })));
    }
    return clone({
      user: u(userId), member_since: iso(Date.now() - 600 * DAY), chat_id: chatId, games_played: n, total_net_rub: total,
      win_rate: n ? history.filter((h) => h.net_rub > 0).length / n : 0, avg_net_rub: n ? Math.round(total / n) : 0,
      rebuy_total: history.reduce((s, h) => s + h.rebuy_count, 0),
      leaderboard_place: me?.place ?? null, leaderboard_total: board.length, leaderboard_chat_id: lbChat,
      history, open_debts: debts,
    });
  }

  async getMyStats(chatId?: number | null) { await delay(); return this.stats(ME, chatId ?? null); }
  async getUserStats(userId: number, chatId?: number | null) { await delay(); return this.stats(userId, chatId ?? null); }
  async getLeaderboard(chatId: number, period: LeaderboardPeriod = 'all'): Promise<Leaderboard> {
    await delay();
    return clone({ chat_id: chatId, chat_title: this.chat(chatId).title, period, entries: this.board(chatId, period) });
  }
}

import type {
  ChatMember,
  ApiErrorBody,
  Chat,
  CreateGameRequest,
  GameDetail,
  GameStatus,
  GameSummary,
  Leaderboard,
  LeaderboardPeriod,
  Me,
  PendingAction,
  PlayerStats,
} from './types';

/** Коды ошибок, на которые UI реагирует отдельно. */
export type ApiErrorCode =
  | 'bank_mismatch' // /end: банк ≠ 0 → предложить force (details.bank_rub)
  | 'active_players' // /end: есть active-участники (details.user_ids)
  | 'not_integer_rub' // /cashout: chips*rate не целое
  | 'already_pending'
  | 'already_in_game'
  | 'busy_in_other_game' // игрок уже в другой активной игре (details: BusyRef)
  | 'not_in_settlements'
  | 'already_marked'
  | 'game_exists' // POST /games: в чате уже есть active
  | 'bad_rate'
  | 'forbidden'
  | 'not_found'
  | 'unauthorized'
  | 'network'
  | string;

export class ApiError extends Error {
  readonly status: number;
  readonly code: ApiErrorCode;
  readonly details?: Record<string, unknown>;
  constructor(status: number, body: ApiErrorBody) {
    super(body.message);
    this.status = status;
    this.code = body.code;
    this.details = body.details;
  }
}

/**
 * Контракт Mini App API. Каждому методу соответствует один REST-эндпоинт (см. http.ts).
 * Мутации игры возвращают свежий GameDetail, чтобы UI не делал лишний GET.
 */
export interface BankirApi {
  /** GET /api/me */
  getMe(): Promise<Me>;
  /** GET /api/chats — группы, где есть бот и пользователь */
  listChats(): Promise<Chat[]>;
  /** GET /api/chats/:id/members — известные боту участники чата (для выбора игрока) */
  listChatMembers(chatId: number): Promise<ChatMember[]>;
  /** GET /api/games?chat_id=&status= */
  listGames(params?: { chat_id?: number; status?: GameStatus }): Promise<GameSummary[]>;
  /** GET /api/games/:id */
  getGame(id: number): Promise<GameDetail>;
  /** POST /api/games */
  createGame(req: CreateGameRequest): Promise<GameDetail>;

  // ── игрок (через подтверждение дилера) ──
  /** POST /api/games/:id/join */
  requestJoin(gameId: number): Promise<PendingAction>;
  /** POST /api/games/:id/rebuy (без user_id — свой запрос) */
  requestRebuy(gameId: number): Promise<PendingAction>;
  /** POST /api/games/:id/cashout { chips } (без user_id — запрос дилеру) */
  requestCashout(gameId: number, chips: number): Promise<PendingAction>;
  /** POST /api/games/:id/pending/:pid/cancel — отозвать свой запрос */
  cancelRequest(gameId: number, pendingId: number): Promise<GameDetail>;

  // ── дилер ──
  /** POST /api/games/:id/approve { pending_id } */
  approve(gameId: number, pendingId: number): Promise<GameDetail>;
  /** POST /api/games/:id/reject { pending_id } */
  reject(gameId: number, pendingId: number): Promise<GameDetail>;
  /** POST /api/games/:id/buyin { user_id } — /dealer_join */
  dealerBuyIn(gameId: number, userId: number): Promise<GameDetail>;
  /** POST /api/games/:id/rebuy { user_id } — /dealer_rebuy */
  dealerRebuy(gameId: number, userId: number): Promise<GameDetail>;
  /** POST /api/games/:id/cashout { user_id, chips } */
  dealerCashout(gameId: number, userId: number, chips: number): Promise<GameDetail>;
  /** POST /api/games/:id/undo { n } */
  undo(gameId: number, n?: number): Promise<GameDetail>;
  /** POST /api/games/:id/end { force } — 409 bank_mismatch / active_players */
  endGame(gameId: number, force?: boolean): Promise<GameDetail>;
  /** POST /api/games/:id/cancel */
  cancelGame(gameId: number): Promise<GameDetail>;
  /** POST /api/games/:id/transfer_dealer { user_id } */
  transferDealer(gameId: number, userId: number): Promise<GameDetail>;

  // ── итоги ──
  /** POST /api/games/:id/settle — кнопка «Я оплатил / получил» */
  settle(gameId: number): Promise<GameDetail>;

  // ── статистика ──
  /** GET /api/stats/me?chat_id= */
  getMyStats(chatId?: number | null): Promise<PlayerStats>;
  /** GET /api/stats/users/:id?chat_id= */
  getUserStats(userId: number, chatId?: number | null): Promise<PlayerStats>;
  /** GET /api/chats/:id/leaderboard?period= */
  getLeaderboard(chatId: number, period?: LeaderboardPeriod): Promise<Leaderboard>;
}

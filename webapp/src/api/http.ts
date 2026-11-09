import { ApiError, type BankirApi } from './client';
import type {
  ChatMember,
  Chat,
  CreateGameRequest,
  GameDetail,
  GameSummary,
  Leaderboard,
  Me,
  PendingAction,
  PlayerStats,
} from './types';

type Query = Record<string, string | number | null | undefined>;

/** Реальный клиент: fetch + `Authorization: tma <initData>` (валидируется на бэкенде по bot token). */
export class HttpApi implements BankirApi {
  constructor(
    private readonly baseUrl: string,
    private readonly getInitData: () => string,
  ) {}

  private async req<T>(method: 'GET' | 'POST', path: string, body?: unknown, query?: Query): Promise<T> {
    let url = this.baseUrl.replace(/\/$/, '') + path;
    if (query) {
      const qs = new URLSearchParams();
      for (const [k, v] of Object.entries(query)) if (v !== undefined && v !== null) qs.set(k, String(v));
      const s = qs.toString();
      if (s) url += '?' + s;
    }
    let res: Response;
    try {
      res = await fetch(url, {
        method,
        headers: {
          Authorization: `tma ${this.getInitData()}`,
          ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
        },
        body: body !== undefined ? JSON.stringify(body) : undefined,
      });
    } catch {
      throw new ApiError(0, { code: 'network', message: 'Нет связи с сервером' });
    }
    const text = await res.text();
    const data = text ? JSON.parse(text) : null;
    if (!res.ok) {
      const err = data?.error ?? { code: String(res.status), message: res.statusText || 'Ошибка сервера' };
      throw new ApiError(res.status, err);
    }
    return data as T;
  }

  getMe() { return this.req<Me>('GET', '/api/me'); }
  listChats() { return this.req<Chat[]>('GET', '/api/chats'); }
  listChatMembers(chatId: number) { return this.req<ChatMember[]>('GET', `/api/chats/${chatId}/members`); }
  listGames(params?: { chat_id?: number; status?: string }) {
    return this.req<GameSummary[]>('GET', '/api/games', undefined, params);
  }
  getGame(id: number) { return this.req<GameDetail>('GET', `/api/games/${id}`); }
  createGame(r: CreateGameRequest) { return this.req<GameDetail>('POST', '/api/games', r); }

  requestJoin(id: number) { return this.req<PendingAction>('POST', `/api/games/${id}/join`, {}); }
  requestRebuy(id: number) { return this.req<PendingAction>('POST', `/api/games/${id}/rebuy`, {}); }
  requestCashout(id: number, chips: number) {
    return this.req<PendingAction>('POST', `/api/games/${id}/cashout`, { chips });
  }
  cancelRequest(id: number, pid: number) {
    return this.req<GameDetail>('POST', `/api/games/${id}/pending/${pid}/cancel`, {});
  }

  approve(id: number, pending_id: number) { return this.req<GameDetail>('POST', `/api/games/${id}/approve`, { pending_id }); }
  reject(id: number, pending_id: number) { return this.req<GameDetail>('POST', `/api/games/${id}/reject`, { pending_id }); }
  dealerBuyIn(id: number, user_id: number) { return this.req<GameDetail>('POST', `/api/games/${id}/buyin`, { user_id }); }
  dealerRebuy(id: number, user_id: number) { return this.req<GameDetail>('POST', `/api/games/${id}/rebuy`, { user_id }); }
  dealerCashout(id: number, user_id: number, chips: number) {
    return this.req<GameDetail>('POST', `/api/games/${id}/cashout`, { user_id, chips });
  }
  undo(id: number, n = 1) { return this.req<GameDetail>('POST', `/api/games/${id}/undo`, { n }); }
  endGame(id: number, force = false) { return this.req<GameDetail>('POST', `/api/games/${id}/end`, { force }); }
  cancelGame(id: number) { return this.req<GameDetail>('POST', `/api/games/${id}/cancel`, {}); }
  transferDealer(id: number, user_id: number) {
    return this.req<GameDetail>('POST', `/api/games/${id}/transfer_dealer`, { user_id });
  }
  settle(id: number) { return this.req<GameDetail>('POST', `/api/games/${id}/settle`, {}); }

  getMyStats(chatId?: number | null) { return this.req<PlayerStats>('GET', '/api/stats/me', undefined, { chat_id: chatId }); }
  getUserStats(userId: number, chatId?: number | null) {
    return this.req<PlayerStats>('GET', `/api/stats/users/${userId}`, undefined, { chat_id: chatId });
  }
  getLeaderboard(chatId: number, period = 'all') {
    return this.req<Leaderboard>('GET', `/api/chats/${chatId}/leaderboard`, undefined, { period });
  }
}

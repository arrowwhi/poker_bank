// DTO для Mini App API. Все суммы — целые рубли / целые фишки.
// Имена полей — snake_case, как будет отдавать Go-бэкенд (json-теги).
// Даты — ISO-8601 строки.

export type GameStatus = 'active' | 'finished' | 'cancelled';
export type LedgerType = 'BUY_IN' | 'REBUY' | 'CASH_OUT';
/** CASH_OUT — запрос игрока на выход (дизайн 04); в ТЗ §5.7 пока только JOIN/REBUY. */
export type PendingType = 'JOIN' | 'REBUY' | 'CASH_OUT';
export type PendingStatus = 'pending' | 'confirmed' | 'rejected' | 'expired' | 'cancelled';
/** Роль текущего пользователя относительно игры. */
export type GameRole = 'dealer' | 'player' | 'out' | 'outsider';
export type LeaderboardPeriod = 'all' | 'year' | 'month';

export interface User {
  id: number; // telegram_user_id
  username?: string | null;
  display_name: string;
  photo_url?: string | null;
}

/** Ссылка на другую активную игру, где пользователь уже занят. */
export interface BusyRef {
  game_id: number;
  chat_id: number;
  chat_title: string;
}

/** Участник чата для picker'а дилера. busy_in — занят в другой активной игре (buy-in невозможен). */
export interface ChatMember extends User {
  busy_in?: BusyRef | null;
}

export interface Rate {
  buy_in_rub: number;
  buy_in_chips: number;
  rebuy_rub: number;
  rebuy_chips: number;
}

export interface Chat {
  id: number; // chat_id (отрицательный у групп)
  title: string;
  members_count: number;
  /** Активная игра в чате, если есть. */
  active_game?: { id: number; dealer: User } | null;
  /** Параметры последней игры — для пресета «Как в прошлый раз». */
  last_rate?: Rate | null;
}

export interface GameSummary {
  id: number;
  /** Порядковый номер игры в чате («Игра #13»). id — глобальный BIGSERIAL. */
  number: number;
  chat_id: number;
  chat_title: string;
  status: GameStatus;
  dealer: User;
  rate: Rate;
  started_at: string;
  ended_at?: string | null;
  players_total: number;
  players_active: number;
  bank_rub: number;
  my_role: GameRole;
}

export interface Participant {
  user: User;
  is_active: boolean;
  buy_in_count: number;
  rebuy_count: number;
  total_in_rub: number;
  total_out_rub: number;
  total_out_chips: number;
  /** total_out_rub − total_in_rub (для активных — без учёта фишек на столе). */
  net_rub: number;
  joined_at: string;
}

export interface PendingAction {
  id: number;
  game_id: number;
  type: PendingType;
  requester: User;
  target: User;
  amount_rub: number;
  amount_chips: number;
  status: PendingStatus;
  created_at: string;
  resolved_at?: string | null;
  resolved_by?: User | null;
}

export interface LedgerEntry {
  id: number;
  type: LedgerType;
  player: User;
  amount_rub: number;
  amount_chips: number;
  created_by: User;
  created_at: string;
  is_void: boolean;
}

export interface GameResult {
  user: User;
  buy_in_count: number;
  rebuy_count: number;
  total_in_rub: number;
  total_out_rub: number;
  total_out_chips: number;
  net_rub: number;
}

export interface Settlement {
  id: number;
  game_id: number;
  from: User;
  to: User;
  amount_rub: number;
  is_paid: boolean;
  paid_at?: string | null;
  is_received: boolean;
  received_at?: string | null;
}

export interface GameDetail extends GameSummary {
  participants: Participant[];
  /** Все pending-запросы игры (видны всем; подтверждать может только дилер). */
  pending: PendingAction[];
  /** Последние запросы текущего пользователя в любом статусе — для тостов «подтверждено/отклонено». */
  my_requests: PendingAction[];
  ledger: LedgerEntry[];
  bank_delta_rub: number;
  /** Только для finished. */
  results?: GameResult[] | null;
  settlements?: Settlement[] | null;
  /** Правило «одна активная игра на игрока»: если я уже занят в ДРУГОЙ активной игре — где именно. */
  my_busy_in?: BusyRef | null;
}

export interface Me {
  user: User;
  member_since: string;
  games_played: number;
  total_net_rub: number;
  /** Единственная активная игра пользователя (дилер или active-игрок). Если есть — приложение сразу открывает её. */
  active_game?: GameSummary | null;
}

export interface CreateGameRequest {
  chat_id: number;
  rate: Rate;
  /** Сразу записать BUY_IN дилеру. */
  dealer_buy_in: boolean;
}

export interface HistoryItem {
  game_id: number;
  game_number: number;
  chat_id: number;
  chat_title: string;
  ended_at: string;
  buy_in_count: number;
  rebuy_count: number;
  total_in_rub: number;
  total_out_rub: number;
  net_rub: number;
}

export interface OpenDebt extends Settlement {
  chat_title: string;
  game_number: number;
  game_ended_at: string;
  /** Направление относительно текущего пользователя. */
  direction: 'out' | 'in';
}

export interface PlayerStats {
  user: User;
  member_since: string;
  /** null — по всем чатам. */
  chat_id: number | null;
  games_played: number;
  total_net_rub: number;
  /** 0..1 — доля игр с net > 0. */
  win_rate: number;
  avg_net_rub: number;
  rebuy_total: number;
  leaderboard_place?: number | null;
  leaderboard_total?: number | null;
  /** Чат, по которому посчитано место (при chat_id=null — основной чат пользователя). */
  leaderboard_chat_id?: number | null;
  /** Все сыгранные игры, от новых к старым. Серии/рекорды/график считаются на клиенте. */
  history: HistoryItem[];
  /** Только для своего профиля. */
  open_debts?: OpenDebt[] | null;
}

export interface LeaderboardEntry {
  place: number;
  user: User;
  games_played: number;
  total_net_rub: number;
  win_rate: number;
}

export interface Leaderboard {
  chat_id: number;
  chat_title: string;
  period: LeaderboardPeriod;
  entries: LeaderboardEntry[];
}

/** Тело ошибки бэкенда: { "error": { "code": "...", "message": "...", "details": {...} } } */
export interface ApiErrorBody {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

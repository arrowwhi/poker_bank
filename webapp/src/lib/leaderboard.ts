import type { LeaderboardEntry } from '../api/types';

/** В чём показывать рейтинг: рубли, фишки или процент от внесённого. */
export type LeaderboardUnit = 'rub' | 'chips' | 'pct';

const UNITS: LeaderboardUnit[] = ['rub', 'chips', 'pct'];
const KEY = 'bankir.leaderboardUnit';

/** Последний выбранный режим (запоминается так же, как последний чат). */
export function lastUnit(): LeaderboardUnit {
  try {
    const v = localStorage.getItem(KEY) as LeaderboardUnit | null;
    return v && UNITS.includes(v) ? v : 'rub';
  } catch {
    return 'rub';
  }
}

export function rememberUnit(unit: LeaderboardUnit): void {
  try { localStorage.setItem(KEY, unit); } catch { /* приватный режим и т. п. */ }
}

/** «% от входа»: Σ результат / Σ вход × 100. */
export function pctOfBuyIn(e: Pick<LeaderboardEntry, 'total_net_rub' | 'total_in_rub'>): number {
  return e.total_in_rub > 0 ? (e.total_net_rub / e.total_in_rub) * 100 : 0;
}

/** Значение записи в выбранном режиме. */
export function unitValue(e: LeaderboardEntry, unit: LeaderboardUnit): number {
  return unit === 'chips' ? e.total_net_chips : unit === 'pct' ? pctOfBuyIn(e) : e.total_net_rub;
}

/**
 * Рейтинг в выбранном режиме: сортировка по убыванию значения и места заново с единицы.
 * При равенстве: ₽ → у кого больше игр; фишки → у кого больше рублей; % → у кого больше вход.
 * Дальше сохраняется порядок сервера (по рублям).
 */
export function rankBy(entries: LeaderboardEntry[], unit: LeaderboardUnit): LeaderboardEntry[] {
  const tie = (e: LeaderboardEntry) =>
    unit === 'chips' ? e.total_net_rub : unit === 'pct' ? e.total_in_rub : e.games_played;
  return [...entries]
    .sort((a, b) => unitValue(b, unit) - unitValue(a, unit) || tie(b) - tie(a))
    .map((e, i) => ({ ...e, place: i + 1 }));
}

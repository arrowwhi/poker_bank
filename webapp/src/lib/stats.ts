import type { HistoryItem } from '../api/types';

export interface Streaks { best: number; current: number }

/** Серия плюсовых игр (net > 0). history — от новых к старым. */
export function plusStreaks(history: HistoryItem[]): Streaks {
  const chrono = [...history].sort((a, b) => a.ended_at.localeCompare(b.ended_at));
  let best = 0;
  let run = 0;
  for (const h of chrono) {
    run = h.net_rub > 0 ? run + 1 : 0;
    best = Math.max(best, run);
  }
  return { best, current: run };
}

export function bestWorst(history: HistoryItem[]): { best?: HistoryItem; worst?: HistoryItem } {
  if (!history.length) return {};
  let best = history[0];
  let worst = history[0];
  for (const h of history) {
    if (h.net_rub > best.net_rub) best = h;
    if (h.net_rub < worst.net_rub) worst = h;
  }
  return { best, worst };
}

export type ChartPeriod = 'month' | 'year' | 'all';

export interface SeriesPoint { t: number; v: number; label: string }

/** Результат нарастающим итогом. Для периода считаем от начала периода (с нулевой точкой). */
export function cumulativeSeries(history: HistoryItem[], period: ChartPeriod, now = new Date()): SeriesPoint[] {
  const chrono = [...history].sort((a, b) => a.ended_at.localeCompare(b.ended_at));
  const from =
    period === 'all' ? -Infinity :
    period === 'year' ? new Date(now.getFullYear() - 1, now.getMonth(), now.getDate()).getTime() :
    new Date(now.getFullYear(), now.getMonth() - 1, now.getDate()).getTime();
  const pts: SeriesPoint[] = [];
  let acc = 0;
  for (const h of chrono) {
    const t = new Date(h.ended_at).getTime();
    if (t < from) continue;
    if (!pts.length) pts.push({ t: t - 1, v: 0, label: '' });
    acc += h.net_rub;
    pts.push({ t, v: acc, label: h.ended_at });
  }
  return pts;
}

import type { Rate } from '../api/types';
import { num, plural } from './format';

export function gcd(a: number, b: number): number {
  a = Math.abs(a); b = Math.abs(b);
  while (b) [a, b] = [b, a % b];
  return a;
}

/** ТЗ §5.2: buy_in_rub * rebuy_chips == rebuy_rub * buy_in_chips */
export function isProportional(r: Rate): boolean {
  return r.buy_in_rub * r.rebuy_chips === r.rebuy_rub * r.buy_in_chips;
}

/** Рубли за N фишек или null, если получается нецелое число (ТЗ §8.3 /cashout). */
export function rubForChips(r: Rate, chips: number): number | null {
  const x = chips * r.buy_in_rub;
  return x % r.buy_in_chips === 0 ? x / r.buy_in_chips : null;
}

/** Минимальный шаг фишек, дающий целые рубли. */
export function chipStep(r: Rate): number {
  return r.buy_in_chips / gcd(r.buy_in_rub, r.buy_in_chips);
}

/** Ближайшие допустимые значения фишек (снизу/сверху). */
export function nearestValidChips(r: Rate, chips: number): [number, number] {
  const s = chipStep(r);
  const lo = Math.floor(chips / s) * s;
  return [lo, lo + s];
}

/** «1₽ = 1 фишка», «1₽ = 2 фишки», «5₽ = 1 фишка», «2₽ = 3 фишки» */
export function rateLabel(r: Rate): string {
  const g = gcd(r.buy_in_rub, r.buy_in_chips);
  const a = r.buy_in_rub / g;
  const b = r.buy_in_chips / g;
  return `${num(a)}₽ = ${num(b)} ${plural(b, 'фишка', 'фишки', 'фишек')}`;
}

/** Рублей за одну фишку, для подсказки «0,83₽ за фишку». */
export function perChip(rub: number, chips: number): string {
  if (!chips) return '—';
  const v = rub / chips;
  return (Number.isInteger(v) ? String(v) : v.toFixed(2).replace(/0+$/, '')).replace('.', ',') + '₽';
}

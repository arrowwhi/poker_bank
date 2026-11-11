import type { User } from '../api/types';

const NBSP = ' ';
const MINUS = '−';

/** 9000 → «9 000» */
export function num(n: number): string {
  return Math.abs(n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, NBSP);
}

/** 9000 → «9 000₽», -500 → «−500₽» */
export function rub(n: number): string {
  return (n < 0 ? MINUS : '') + num(n) + '₽';
}

/** Со знаком: +1 300₽ / −2 000₽ / 0₽ */
export function signedRub(n: number): string {
  if (n === 0) return '0₽';
  return (n > 0 ? '+' : MINUS) + num(n) + '₽';
}

/** Фишки со знаком, без единицы (значок фишки рисует экран): +1 980 / −160 / 0 */
export function signedChips(n: number): string {
  if (n === 0) return '0';
  return (n > 0 ? '+' : MINUS) + num(n);
}

/** Проценты со знаком: целые без дробной части, иначе один знак через запятую — +127% / −33,3% / 0% */
export function signedPct(p: number): string {
  const tenths = Math.round(Math.abs(p) * 10);
  if (tenths === 0) return '0%';
  const frac = tenths % 10;
  return (p > 0 ? '+' : MINUS) + num(Math.floor(tenths / 10)) + (frac ? ',' + frac : '') + '%';
}

export function plural(n: number, one: string, few: string, many: string): string {
  const a = Math.abs(n) % 100;
  const b = a % 10;
  if (a > 10 && a < 20) return many;
  if (b > 1 && b < 5) return few;
  if (b === 1) return one;
  return many;
}

export function chips(n: number): string {
  return `${num(n)}${NBSP}${plural(n, 'фишка', 'фишки', 'фишек')}`;
}

export function netClass(n: number): string {
  return n > 0 ? 'pos' : n < 0 ? 'neg' : '';
}

export function handle(u: User): string {
  return u.username ? '@' + u.username : u.display_name;
}

export function initials(u: User): string {
  const parts = u.display_name.trim().split(/\s+/).filter(Boolean);
  const s = parts.length >= 2 ? parts[0][0] + parts[1][0] : (parts[0] ?? u.username ?? '?').slice(0, 2);
  return s.toUpperCase();
}

const MONTHS = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'];
const MONTHS_SHORT = ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'];
const MONTHS_NOM = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'];

export function date(iso: string): string {
  const d = new Date(iso);
  return `${d.getDate()} ${MONTHS[d.getMonth()]}`;
}
export function dateShort(iso: string): string {
  const d = new Date(iso);
  return `${d.getDate()} ${MONTHS_SHORT[d.getMonth()]}`;
}
export function monthYear(iso: string): string {
  const d = new Date(iso);
  return `${MONTHS_NOM[d.getMonth()]} ${d.getFullYear()}`;
}
export function time(iso: string): string {
  const d = new Date(iso);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

export function duration(fromIso: string, to: Date = new Date()): string {
  const min = Math.max(0, Math.round((to.getTime() - new Date(fromIso).getTime()) / 60000));
  const h = Math.floor(min / 60);
  const m = min % 60;
  return h > 0 ? `${h}${NBSP}ч ${m}${NBSP}мин` : `${m}${NBSP}мин`;
}

export function ago(iso: string, now: Date = new Date()): string {
  const s = Math.round((now.getTime() - new Date(iso).getTime()) / 1000);
  if (s < 45) return 'только что';
  const m = Math.round(s / 60);
  if (m < 60) return `${m}${NBSP}мин назад`;
  const h = Math.round(m / 60);
  return `${h}${NBSP}ч назад`;
}

const LEDGER_LABEL = { BUY_IN: 'Buy-in', REBUY: 'Rebuy', CASH_OUT: 'Cash-out' } as const;
export function ledgerLabel(t: keyof typeof LEDGER_LABEL): string {
  return LEDGER_LABEL[t];
}

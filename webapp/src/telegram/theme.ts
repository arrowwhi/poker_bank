import { useSyncExternalStore } from 'react';
import { inTelegram, tg, type ThemeParams } from './telegram';

/** Выбор пользователя: «как в Telegram» либо принудительно светлая/тёмная. */
export type ThemePref = 'system' | 'light' | 'dark';
export type Scheme = 'light' | 'dark';

/** Ключ выбора в localStorage и CloudStorage. Его же читает inline-скрипт в index.html (до первого рендера). */
const KEY = 'bankir.theme';
const isPref = (v: unknown): v is ThemePref => v === 'system' || v === 'light' || v === 'dark';

function readLocal(): ThemePref {
  try {
    const v = localStorage.getItem(KEY);
    return isPref(v) ? v : 'system';
  } catch {
    return 'system';
  }
}
function writeLocal(p: ThemePref) {
  try { localStorage.setItem(KEY, p); } catch { /* приватный режим и т. п. */ }
}

let pref: ThemePref = readLocal();
let scheme: Scheme = 'light';
let version = 0;
/** Пользователь менял тему в этой сессии — запоздавший ответ CloudStorage её уже не перекрывает. */
let touched = false;
const subs = new Set<() => void>();
const subscribe = (f: () => void) => { subs.add(f); return () => { subs.delete(f); }; };

// themeParams Telegram → токены «Банкир». Отсутствующие параметры берутся из tokens.css
// (палитры «Банкир · Light» / «Банкир · Dark» из Figma).
const MAP: [keyof ThemeParams, string][] = [
  ['bg_color', '--bg-primary'],
  ['secondary_bg_color', '--bg-secondary'],
  ['section_bg_color', '--bg-surface'],
  ['text_color', '--text-primary'],
  ['subtitle_text_color', '--text-secondary'],
  ['hint_color', '--text-hint'],
  ['button_color', '--accent-primary'],
  ['button_text_color', '--text-on-accent'],
  ['section_separator_color', '--border-separator'],
  ['destructive_text_color', '--status-danger'],
];

/** «Системная» схема: тема Telegram, вне его — ?theme=dark|light (dev) либо prefers-color-scheme. */
function systemScheme(): Scheme {
  if (inTelegram) return tg.colorScheme;
  const qs = new URLSearchParams(location.search).get('theme');
  if (qs === 'dark' || qs === 'light') return qs;
  return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function cssVar(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function applyTheme() {
  const root = document.documentElement;
  scheme = pref === 'system' ? systemScheme() : pref;
  root.dataset.theme = scheme;

  // themeParams описывают текущую тему Telegram, поэтому годятся, только когда итоговая схема с ней совпадает.
  // Иначе (например, «Светлая» при тёмном Telegram) работает собственная палитра из tokens.css.
  const native = inTelegram && scheme === tg.colorScheme;
  const tp: ThemeParams = native ? tg.themeParams ?? {} : {};
  for (const [k, v] of MAP) {
    const val = tp[k];
    if (val) root.style.setProperty(v, val);
    else root.style.removeProperty(v);
  }
  if (tp.button_color) root.style.setProperty('--accent-soft', `color-mix(in srgb, ${tp.button_color} 14%, var(--bg-surface))`);
  else root.style.removeProperty('--accent-soft');

  if (inTelegram) {
    try {
      const page = cssVar('--bg-secondary');
      if (native) {
        if (tg.isVersionAtLeast('6.1')) {
          tg.setHeaderColor?.('bg_color');
          tg.setBackgroundColor?.(page);
        }
        if (tg.isVersionAtLeast('7.10')) tg.setBottomBarColor?.('bg_color');
      } else {
        const bar = cssVar('--bg-primary');
        if (tg.isVersionAtLeast('6.9')) tg.setHeaderColor?.(bar); // произвольный цвет шапки — с 6.9
        if (tg.isVersionAtLeast('6.1')) tg.setBackgroundColor?.(page);
        if (tg.isVersionAtLeast('7.10')) tg.setBottomBarColor?.(bar);
      }
    } catch { /* старый клиент */ }
  }

  version++;
  subs.forEach((f) => f());
}

const cloud = () => (inTelegram && tg.isVersionAtLeast('6.9') ? tg.CloudStorage : undefined);

/** Выбор, сделанный на другом устройстве: CloudStorage главнее локального значения. */
function syncFromCloud() {
  try {
    cloud()?.getItem(KEY, (err, value) => {
      if (err || touched || !isPref(value) || value === pref) return;
      pref = value;
      writeLocal(value);
      applyTheme();
    });
  } catch { /* noop */ }
}

export function setThemePref(next: ThemePref) {
  touched = true;
  pref = next;
  writeLocal(next);
  try { cloud()?.setItem(KEY, next); } catch { /* noop */ }
  applyTheme();
}

/** Текущий выбор и итоговая схема; компонент перерисовывается при смене темы. */
export function useTheme(): { pref: ThemePref; scheme: Scheme } {
  useSyncExternalStore(subscribe, () => version);
  return { pref, scheme };
}

/** Счётчик применений темы — для эффектов, которые читают CSS-токены (цвет нативной MainButton). */
export function useThemeVersion(): number {
  return useSyncExternalStore(subscribe, () => version);
}

function applyInsets() {
  const s = document.documentElement.style;
  const a = tg.safeAreaInset ?? { top: 0, bottom: 0, left: 0, right: 0 };
  const c = tg.contentSafeAreaInset ?? { top: 0, bottom: 0, left: 0, right: 0 };
  // В fullscreen-режиме (Bot API 8.0+) контент нужно отодвинуть от системных и телеграмных контролов.
  s.setProperty('--safe-top', `${a.top + c.top}px`);
  s.setProperty('--safe-bottom', `${a.bottom + c.bottom}px`);
  s.setProperty('--safe-left', `${a.left + c.left}px`);
  s.setProperty('--safe-right', `${a.right + c.right}px`);
  s.setProperty('--vh-stable', `${tg.viewportStableHeight || innerHeight}px`);
}

/** Инициализация Mini App: ready/expand, тема, insets, подписки на изменения. */
export function initTelegram() {
  applyTheme();
  applyInsets();
  tg.ready();
  tg.expand();
  try { if (tg.isVersionAtLeast('7.7')) tg.disableVerticalSwipes?.(); } catch { /* noop */ }
  syncFromCloud();
  tg.onEvent('themeChanged', applyTheme);
  tg.onEvent('viewportChanged', applyInsets);
  tg.onEvent('safeAreaChanged', applyInsets);
  tg.onEvent('contentSafeAreaChanged', applyInsets);
  if (!inTelegram) {
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyTheme);
    addEventListener('resize', applyInsets);
  }
}

/** Быстрое переключение темы в браузере (dev) кнопкой «···» в шапке-заглушке. */
export function toggleDevTheme() {
  setThemePref(scheme === 'dark' ? 'light' : 'dark');
}

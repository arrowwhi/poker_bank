// Тонкая типизированная обёртка над window.Telegram.WebApp (официальный telegram-web-app.js).
// Вне Telegram (обычный браузер) подставляется мок, чтобы приложение работало в dev.

export interface ThemeParams {
  bg_color?: string;
  secondary_bg_color?: string;
  section_bg_color?: string;
  text_color?: string;
  hint_color?: string;
  subtitle_text_color?: string;
  link_color?: string;
  button_color?: string;
  button_text_color?: string;
  accent_text_color?: string;
  destructive_text_color?: string;
  section_separator_color?: string;
  header_bg_color?: string;
  bottom_bar_bg_color?: string;
}

interface Inset { top: number; bottom: number; left: number; right: number }

interface BottomButton {
  text: string;
  isVisible: boolean;
  isActive: boolean;
  setText(t: string): BottomButton;
  setParams(p: { text?: string; color?: string; text_color?: string; is_active?: boolean; is_visible?: boolean; has_shine_effect?: boolean }): BottomButton;
  show(): BottomButton;
  hide(): BottomButton;
  enable(): BottomButton;
  disable(): BottomButton;
  showProgress(leaveActive?: boolean): BottomButton;
  hideProgress(): BottomButton;
  onClick(cb: () => void): BottomButton;
  offClick(cb: () => void): BottomButton;
}

export interface WebApp {
  initData: string;
  initDataUnsafe: {
    user?: { id: number; first_name: string; last_name?: string; username?: string; photo_url?: string };
    start_param?: string;
  };
  version: string;
  platform: string;
  colorScheme: 'light' | 'dark';
  themeParams: ThemeParams;
  isExpanded: boolean;
  /** Bot API 8.0+: приложение развёрнуто на весь экран, шапки Telegram нет. */
  isFullscreen?: boolean;
  viewportHeight: number;
  viewportStableHeight: number;
  safeAreaInset?: Inset;
  contentSafeAreaInset?: Inset;
  ready(): void;
  expand(): void;
  requestFullscreen?(): void;
  exitFullscreen?(): void;
  close(): void;
  isVersionAtLeast(v: string): boolean;
  setHeaderColor?(c: string): void;
  setBackgroundColor?(c: string): void;
  setBottomBarColor?(c: string): void;
  enableClosingConfirmation?(): void;
  disableVerticalSwipes?(): void;
  onEvent(e: string, cb: () => void): void;
  offEvent(e: string, cb: () => void): void;
  BackButton: { isVisible: boolean; show(): void; hide(): void; onClick(cb: () => void): void; offClick(cb: () => void): void };
  MainButton: BottomButton;
  HapticFeedback: {
    impactOccurred(s: 'light' | 'medium' | 'heavy' | 'rigid' | 'soft'): void;
    notificationOccurred(t: 'error' | 'success' | 'warning'): void;
    selectionChanged(): void;
  };
  openTelegramLink?(url: string): void;
  /** Bot API 6.9+: хранилище «ключ → строка», общее для устройств пользователя. */
  CloudStorage?: {
    setItem(key: string, value: string, cb?: (err: string | null, ok?: boolean) => void): void;
    getItem(key: string, cb: (err: string | null, value?: string) => void): void;
  };
}

declare global {
  interface Window { Telegram?: { WebApp?: WebApp } }
}

function createMock(): WebApp {
  const qs = new URLSearchParams(location.search);
  const dark = qs.get('theme') ? qs.get('theme') === 'dark' : matchMedia('(prefers-color-scheme: dark)').matches;
  const noop = () => {};
  // ?fullscreen=1 эмулирует fullscreen-режим телефона: статус-бар 47px + зона кнопок Telegram 46px.
  const fs = qs.get('fullscreen') === '1';
  const btn = (): BottomButton => {
    const b: BottomButton = {
      text: '', isVisible: false, isActive: true,
      setText: (t) => { b.text = t; return b; },
      setParams: (p) => { if (p.text !== undefined) b.text = p.text; return b; },
      show: () => { b.isVisible = true; return b; },
      hide: () => { b.isVisible = false; return b; },
      enable: () => { b.isActive = true; return b; },
      disable: () => { b.isActive = false; return b; },
      showProgress: () => b, hideProgress: () => b, onClick: () => b, offClick: () => b,
    };
    return b;
  };
  return {
    initData: '',
    initDataUnsafe: {
      user: { id: 1, first_name: 'Иван', last_name: 'Захаров', username: 'ivan' },
      // ?startapp=-1001001 эмулирует deep-link t.me/<bot>/<app>?startapp=<chat_id>
      start_param: qs.get('startapp') ?? qs.get('tgWebAppStartParam') ?? undefined,
    },
    version: '0.0-mock',
    platform: 'unknown',
    colorScheme: dark ? 'dark' : 'light',
    themeParams: {}, // пусто → используются собственные токены «Банкир · Light/Dark»
    isExpanded: true,
    viewportHeight: innerHeight,
    viewportStableHeight: innerHeight,
    isFullscreen: fs,
    safeAreaInset: { top: fs ? 47 : 0, bottom: 0, left: 0, right: 0 },
    contentSafeAreaInset: { top: fs ? 46 : 0, bottom: 0, left: 0, right: 0 },
    ready: noop, expand: noop, close: noop,
    isVersionAtLeast: () => false,
    onEvent: noop, offEvent: noop,
    BackButton: { isVisible: false, show: noop, hide: noop, onClick: noop, offClick: noop },
    MainButton: btn(),
    HapticFeedback: { impactOccurred: noop, notificationOccurred: noop, selectionChanged: noop },
  };
}

const real = window.Telegram?.WebApp;
/** true — запущено внутри клиента Telegram (есть initData). */
export const inTelegram = !!real && !!real.initData;
export const tg: WebApp = inTelegram ? real! : createMock();

/** chat_id из deep-link `startapp` (ТЗ: start_param = chat_id). */
export function startChatId(): number | null {
  const p = tg.initDataUnsafe.start_param;
  if (!p) return null;
  const n = Number(p);
  return Number.isFinite(n) && n !== 0 ? n : null;
}

/** id игры из deep-link `startapp=g<id>`: кнопка «Итоги в приложении» под планом переводов. */
export function startGameId(): number | null {
  const m = /^g(\d+)$/.exec(tg.initDataUnsafe.start_param ?? '');
  return m ? Number(m[1]) : null;
}

export const haptic = {
  success: () => tg.HapticFeedback.notificationOccurred('success'),
  warning: () => tg.HapticFeedback.notificationOccurred('warning'),
  error: () => tg.HapticFeedback.notificationOccurred('error'),
  tap: () => tg.HapticFeedback.impactOccurred('light'),
  select: () => tg.HapticFeedback.selectionChanged(),
};

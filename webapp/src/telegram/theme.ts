import { inTelegram, tg, type ThemeParams } from './telegram';

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

function applyTheme() {
  const root = document.documentElement;
  const qs = new URLSearchParams(location.search).get('theme');
  const scheme = inTelegram ? tg.colorScheme : qs ?? (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  root.dataset.theme = scheme;
  const tp = tg.themeParams ?? {};
  for (const [k, v] of MAP) {
    const val = tp[k];
    if (val) root.style.setProperty(v, val);
    else root.style.removeProperty(v);
  }
  if (tp.button_color) root.style.setProperty('--accent-soft', `color-mix(in srgb, ${tp.button_color} 14%, var(--bg-surface))`);
  else root.style.removeProperty('--accent-soft');

  if (inTelegram) {
    try {
      if (tg.isVersionAtLeast('6.1')) {
        tg.setHeaderColor?.('bg_color');
        tg.setBackgroundColor?.(tp.secondary_bg_color ?? (scheme === 'dark' ? '#0e1621' : '#f1f2f6'));
      }
      if (tg.isVersionAtLeast('7.10')) tg.setBottomBarColor?.('bg_color');
    } catch { /* старый клиент */ }
  }
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
  tg.onEvent('themeChanged', applyTheme);
  tg.onEvent('viewportChanged', applyInsets);
  tg.onEvent('safeAreaChanged', applyInsets);
  tg.onEvent('contentSafeAreaChanged', applyInsets);
  if (!inTelegram) {
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyTheme);
    addEventListener('resize', applyInsets);
  }
}

/** Переключение темы в браузере (dev): ?theme=dark|light либо кнопкой. */
export function toggleDevTheme() {
  const root = document.documentElement;
  root.dataset.theme = root.dataset.theme === 'dark' ? 'light' : 'dark';
}

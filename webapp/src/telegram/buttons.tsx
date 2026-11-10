// MainButton / BackButton через стек регистраций: верхний (последний смонтированный) владелец
// управляет кнопкой. В Telegram — нативные кнопки, в браузере — HTML-заглушки той же формы.
import { useEffect, useId, useLayoutEffect, useRef, useSyncExternalStore } from 'react';
import { inTelegram, tg } from './telegram';
import { toggleDevTheme, useFullscreen, useThemeVersion } from './theme';

function createStack<T>() {
  // prio: базовые владельцы (роутер) = 0, экраны/шторки = 1. Верх — максимальный prio, среди равных — последний.
  let entries: { id: string; value: T; prio: number }[] = [];
  const subs = new Set<() => void>();
  const emit = () => subs.forEach((f) => f());
  return {
    set(id: string, value: T, prio = 1) {
      const i = entries.findIndex((e) => e.id === id);
      if (i >= 0) entries = entries.map((e) => (e.id === id ? { id, value, prio } : e));
      else entries = [...entries, { id, value, prio }];
      emit();
    },
    remove(id: string) { entries = entries.filter((e) => e.id !== id); emit(); },
    top: (): T | undefined => {
      let best: { value: T; prio: number } | undefined;
      for (const e of entries) if (!best || e.prio >= best.prio) best = e;
      return best?.value;
    },
    subscribe: (f: () => void) => { subs.add(f); return () => { subs.delete(f); }; },
  };
}

export type MainButtonTone = 'accent' | 'success' | 'danger' | 'neutral';
export interface MainButtonConfig {
  text: string;
  onClick: () => void;
  disabled?: boolean;
  progress?: boolean;
  tone?: MainButtonTone;
}

const mainStack = createStack<MainButtonConfig>();
const backStack = createStack<() => void>();

/** Показывает MainButton, пока компонент смонтирован. null — не показывать (и не перекрывать нижележащие). */
export function useMainButton(cfg: MainButtonConfig | null) {
  const id = useId();
  const ref = useRef(cfg);
  ref.current = cfg;
  const key = cfg ? `${cfg.text}|${cfg.disabled}|${cfg.progress}|${cfg.tone}` : '';
  useLayoutEffect(() => {
    if (!ref.current) { mainStack.remove(id); return; }
    mainStack.set(id, { ...ref.current, onClick: () => ref.current?.onClick() });
  }, [id, key]);
  useEffect(() => () => mainStack.remove(id), [id]);
}

/** Регистрирует обработчик «Назад» (нативная BackButton). null — кнопка не нужна. */
export function useBackButton(handler: (() => void) | null, prio = 1) {
  const id = useId();
  const ref = useRef(handler);
  ref.current = handler;
  const on = !!handler;
  useLayoutEffect(() => {
    if (!on) { backStack.remove(id); return; }
    backStack.set(id, () => ref.current?.(), prio);
  }, [id, on, prio]);
  useEffect(() => () => backStack.remove(id), [id]);
}

const useTop = <T,>(s: { top: () => T | undefined; subscribe: (f: () => void) => () => void }) =>
  useSyncExternalStore(s.subscribe, s.top);

function cssVar(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}
const toneVar: Record<MainButtonTone, [string, string]> = {
  accent: ['--accent-primary', '--text-on-accent'],
  success: ['--status-success', '--text-on-accent'],
  danger: ['--status-danger', '--text-on-accent'],
  neutral: ['--bg-elevated', '--text-hint'],
};

/** Нативная MainButton Telegram. */
function NativeMain() {
  const cfg = useTop(mainStack);
  const theme = useThemeVersion(); // цвета кнопки берутся из токенов — перекрашиваем при смене темы
  useEffect(() => {
    const mb = tg.MainButton;
    if (!cfg) { mb.hide(); return; }
    const [bg, fg] = toneVar[cfg.disabled ? 'neutral' : cfg.tone ?? 'accent'];
    const color = cssVar(bg);
    const text_color = cssVar(fg);
    mb.setParams({ text: cfg.text, is_active: !cfg.disabled, is_visible: true, ...(color.startsWith('#') ? { color, text_color } : {}) });
    if (cfg.progress) mb.showProgress(false); else mb.hideProgress();
    const h = () => { if (!cfg.disabled && !cfg.progress) cfg.onClick(); };
    mb.onClick(h);
    return () => { mb.offClick(h); };
  }, [cfg, theme]);
  return null;
}

function NativeBack() {
  const h = useTop(backStack);
  useEffect(() => {
    const bb = tg.BackButton;
    if (!h) { bb.hide(); return; }
    bb.show();
    bb.onClick(h);
    return () => bb.offClick(h);
  }, [h]);
  return null;
}

/** Браузерная заглушка MainButton (вне Telegram). */
function FallbackMain() {
  const cfg = useTop(mainStack);
  if (!cfg) return null;
  return (
    <div className="mainbar">
      <button
        className={`btn btn-lg btn-${cfg.disabled ? 'neutral' : cfg.tone ?? 'accent'}`}
        disabled={cfg.disabled || cfg.progress}
        onClick={cfg.onClick}
      >
        {cfg.progress ? <span className="spinner" /> : cfg.text}
      </button>
    </div>
  );
}

/**
 * Шапка экрана. В Telegram с его собственной шапкой не нужна. В fullscreen — полоса под статус-баром
 * с заголовком между плавающими кнопками Telegram (`overlay` — без фона и заголовка, для экрана со столом).
 * В браузере — заглушка шапки Telegram с «‹ Назад».
 */
export function FallbackHeader({ title, subtitle, onMenu, overlay }: { title: string; subtitle?: string; onMenu?: () => void; overlay?: boolean }) {
  const back = useTop(backStack);
  const fullscreen = useFullscreen();
  const onBack = back ?? (() => tg.close());
  const menu = onMenu ?? toggleDevTheme;
  if (fullscreen) {
    if (inTelegram && overlay) return null;
    return (
      <div className={`fs-bar ${overlay ? 'overlay' : ''}`}>
        <div className="fs-bar-row">
          {/* В Telegram эти кнопки рисует сам клиент — в браузере их заменяют заглушки. */}
          {!inTelegram && <button className="fs-pill" onClick={onBack}>{back ? '‹ Назад' : '✕ Закрыть'}</button>}
          {!overlay && <div className="fs-bar-title">{title}</div>}
          {!inTelegram && <button className="fs-pill right" onClick={menu} title="Сменить тему (dev)" aria-label="Сменить тему">···</button>}
        </div>
      </div>
    );
  }
  if (inTelegram) return null;
  return (
    <div className="tg-header">
      <button className="tg-header-side" onClick={onBack}>{back ? '‹ Назад' : 'Закрыть'}</button>
      <div className="tg-header-title">
        <div>{title}</div>
        {subtitle && <small>{subtitle}</small>}
      </div>
      <button className="tg-header-side right" onClick={menu} title="Сменить тему (dev)" aria-label="Сменить тему">···</button>
    </div>
  );
}

/** Есть ли сейчас видимая MainButton — для нижнего отступа контента. */
export function useHasMainButton() {
  return !!useTop(mainStack);
}

export function TelegramButtons() {
  return inTelegram ? (<><NativeMain /><NativeBack /></>) : <FallbackMain />;
}

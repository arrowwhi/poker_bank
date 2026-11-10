import { useEffect, useRef, useState, type ReactNode } from 'react';
import { haptic, inTelegram } from '../telegram/telegram';
import { setThemePref, useTheme, type ThemePref } from '../telegram/theme';

const svg = (children: ReactNode) => (
  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {children}
  </svg>
);

const OPTIONS: { value: ThemePref; label: string; icon: ReactNode }[] = [
  {
    value: 'system',
    label: inTelegram ? 'Как в Telegram' : 'Как в системе',
    icon: svg(<><circle cx="12" cy="12" r="8.5" /><path d="M12 3.5a8.5 8.5 0 0 1 0 17z" fill="currentColor" stroke="none" /></>),
  },
  {
    value: 'light',
    label: 'Светлая',
    icon: svg(<><circle cx="12" cy="12" r="4" /><path d="M12 2.5V5M12 19v2.5M2.5 12H5M19 12h2.5M5.3 5.3l1.8 1.8M16.9 16.9l1.8 1.8M5.3 18.7l1.8-1.8M16.9 7.1l1.8-1.8" /></>),
  },
  {
    value: 'dark',
    label: 'Тёмная',
    icon: svg(<path d="M20 14.2A8.5 8.5 0 0 1 9.8 4a8.5 8.5 0 1 0 10.2 10.2z" />),
  },
];

/** Сколько раскрытая кнопка остаётся на экране после выбора, прежде чем свернуться. */
const COLLAPSE_MS = 1600;

/**
 * Компактная кнопка темы: в покое — один значок текущего режима; по нажатию раскрывается
 * в три (как в Telegram / светлая / тёмная) и сама сворачивается после выбора или касания мимо.
 */
export function ThemeButton() {
  const { pref } = useTheme();
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const timer = useRef<number>();

  useEffect(() => {
    if (!open) return;
    const outside = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', outside);
    return () => document.removeEventListener('pointerdown', outside);
  }, [open]);
  useEffect(() => () => clearTimeout(timer.current), []);

  const pick = (value: ThemePref) => {
    clearTimeout(timer.current);
    if (!open) { haptic.tap(); setOpen(true); return; }
    if (value === pref) { setOpen(false); return; }
    haptic.select();
    setThemePref(value);
    timer.current = window.setTimeout(() => setOpen(false), COLLAPSE_MS);
  };

  const current = OPTIONS.find((o) => o.value === pref)!;
  return (
    <div ref={root} className={`theme-switch ${open ? 'open' : ''}`}>
      <div className="theme-pill" role={open ? 'radiogroup' : undefined} aria-label={open ? 'Тема оформления' : undefined}>
        {OPTIONS.map((o) => {
          const on = o.value === pref;
          const hidden = !open && !on;
          return (
            <button key={o.value} type="button" className={on ? 'on' : ''}
              {...(open ? { role: 'radio', 'aria-checked': on, 'aria-label': o.label } : { 'aria-label': `Тема оформления: ${o.label}`, 'aria-expanded': false })}
              aria-hidden={hidden || undefined} tabIndex={hidden ? -1 : 0} title={open ? o.label : 'Тема оформления'}
              onClick={() => pick(o.value)}>
              {o.icon}
            </button>
          );
        })}
      </div>
      <span className="theme-label" aria-hidden={!open}>{current.label}</span>
    </div>
  );
}

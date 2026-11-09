import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import type { User } from '../api';
import { initials } from '../lib/format';
import { FallbackHeader, useBackButton } from '../telegram/buttons';

export function Screen({ title, subtitle, children }: { title: string; subtitle?: string; children: ReactNode }) {
  useEffect(() => { document.title = title; }, [title]);
  return (
    <>
      <FallbackHeader title={title} subtitle={subtitle} />
      <div className="screen stack">{children}</div>
    </>
  );
}

export function Avatar({ user, size = 40 }: { user: User; size?: number }) {
  return (
    <div className="avatar" style={{ width: size, height: size, fontSize: size * 0.36 }}>
      {user.photo_url ? <img src={user.photo_url} alt="" /> : initials(user)}
    </div>
  );
}

export type BadgeTone = 'success' | 'neutral' | 'dealer' | 'warning' | 'accent' | 'danger';
export function Badge({ tone, children }: { tone: BadgeTone; children: ReactNode }) {
  return <span className={`badge badge-${tone}`}>{children}</span>;
}

export function SectionHeader({ title, right }: { title: string; right?: ReactNode }) {
  return (
    <div className="section-h">
      <span>{title}</span>
      {right !== undefined && <span>{right}</span>}
    </div>
  );
}

export function ListRow(props: {
  left?: ReactNode; title: ReactNode; sub?: ReactNode; right?: ReactNode; chevron?: boolean; onClick?: () => void; className?: string;
}) {
  const cls = `list-row ${props.className ?? ''}`;
  const body = (
    <>
      {props.left}
      <div className="grow">
        <div className="title ellipsis">{props.title}</div>
        {props.sub && <div className="sub">{props.sub}</div>}
      </div>
      {props.right !== undefined && <div className="right">{props.right}</div>}
      {props.chevron && <span className="chev">›</span>}
    </>
  );
  return props.onClick ? (
    <button className={cls} onClick={props.onClick}>{body}</button>
  ) : (
    <div className={cls}>{body}</div>
  );
}

export function StatTile({ label, value, className }: { label: string; value: ReactNode; className?: string }) {
  return (
    <div className="stat">
      <div className="label">{label}</div>
      <div className={`value tabular ${className ?? ''}`}>{value}</div>
    </div>
  );
}

export function Segmented<T extends string>({ value, options, onChange }: {
  value: T; options: { value: T; label: ReactNode }[]; onChange: (v: T) => void;
}) {
  return (
    <div className="segmented" role="tablist">
      {options.map((o) => (
        <button key={o.value} role="tab" aria-selected={o.value === value} className={o.value === value ? 'on' : ''} onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Chips<T extends string | number>({ value, options, onChange }: {
  value: T | null; options: { value: T; label: string }[]; onChange: (v: T) => void;
}) {
  return (
    <div className="chips">
      {options.map((o) => (
        <button key={String(o.value)} className={`chip ${o.value === value ? 'on' : ''}`} onClick={() => onChange(o.value)}>{o.label}</button>
      ))}
    </div>
  );
}

export function NumberField({ label, value, onChange, suffix, error, autoFocus }: {
  label: string; value: string; onChange: (v: string) => void; suffix?: string; error?: string | boolean; autoFocus?: boolean;
}) {
  return (
    <div className="field">
      <label>{label}</label>
      <div className={`input ${error ? 'error' : ''}`}>
        <input
          inputMode="numeric" pattern="[0-9]*" value={value} autoFocus={autoFocus}
          onChange={(e) => onChange(e.target.value.replace(/\D/g, '').slice(0, 9))}
        />
        {suffix && <span className="suffix">{suffix}</span>}
      </div>
      {typeof error === 'string' && error && <div className="err">{error}</div>}
    </div>
  );
}

export function SearchField({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder: string }) {
  return (
    <div className="input search">
      <span className="hint" style={{ marginRight: 8 }}>🔍</span>
      <input value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />
    </div>
  );
}

export function Switch({ on, onChange }: { on: boolean; onChange: (v: boolean) => void }) {
  return <button role="switch" aria-checked={on} className={`switch ${on ? 'on' : ''}`} onClick={() => onChange(!on)} />;
}

export function Steps({ total, current }: { total: number; current: number }) {
  return <div className="steps">{Array.from({ length: total }, (_, i) => <i key={i} className={i < current ? 'on' : ''} />)}</div>;
}

export function Skeleton({ h = 80 }: { h?: number }) {
  return <div className="skeleton" style={{ height: h }} />;
}

export function Loading() {
  return (
    <>
    <FallbackHeader title="" />
    <div className="screen stack">
      <Skeleton h={120} /><Skeleton h={70} /><Skeleton h={220} />
    </div>
    </>
  );
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <div className="empty">
      <div className="big">⚠️</div>
      <h3>Не удалось загрузить</h3>
      <p>{error instanceof Error ? error.message : String(error)}</p>
      {onRetry && <button className="btn btn-soft btn-sm" onClick={onRetry}>Повторить</button>}
    </div>
  );
}

/** Нижняя шторка. Пока открыта — перехватывает BackButton. */
export function Sheet({ title, subtitle, onClose, children }: { title: string; subtitle?: ReactNode; onClose: () => void; children: ReactNode }) {
  useBackButton(onClose);
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <div className="sheet" role="dialog" aria-label={title}>
        <div className="sheet-handle" />
        <div className="sheet-head">
          <div>
            <h3>{title}</h3>
            {subtitle && <div className="muted" style={{ fontSize: 13, marginTop: 2 }}>{subtitle}</div>}
          </div>
          <button className="sheet-close" onClick={onClose} aria-label="Закрыть">✕</button>
        </div>
        {children}
      </div>
    </>
  );
}

export interface ConfirmOptions {
  icon?: ReactNode;
  title: string;
  text?: ReactNode;
  ok: string;
  tone?: 'accent' | 'success' | 'danger';
}

/** Модальное подтверждение: `await confirm({...})` → true/false. */
const ConfirmCtx = createContext<(o: ConfirmOptions) => Promise<boolean>>(async () => false);
export const useConfirm = () => useContext(ConfirmCtx);

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<(ConfirmOptions & { resolve: (v: boolean) => void }) | null>(null);
  const confirm = useCallback((o: ConfirmOptions) => new Promise<boolean>((resolve) => setState({ ...o, resolve })), []);
  const close = (v: boolean) => { state?.resolve(v); setState(null); };
  return (
    <ConfirmCtx.Provider value={confirm}>
      {children}
      {state && <ConfirmDialog o={state} onClose={close} />}
    </ConfirmCtx.Provider>
  );
}

function ConfirmDialog({ o, onClose }: { o: ConfirmOptions; onClose: (v: boolean) => void }) {
  useBackButton(() => onClose(false));
  return (
    <>
      <div className="scrim dialog-scrim" onClick={() => onClose(false)} />
      <div className="dialog" role="alertdialog">
        {o.icon && <div style={{ fontSize: 36 }}>{o.icon}</div>}
        <h3>{o.title}</h3>
        {o.text && <p>{o.text}</p>}
        <div className="grid2">
          <button className="btn btn-neutral" style={{ color: 'var(--text-primary)' }} onClick={() => onClose(false)}>Отмена</button>
          <button className={`btn btn-${o.tone ?? 'accent'}`} onClick={() => onClose(true)}>{o.ok}</button>
        </div>
      </div>
    </>
  );
}

// ── тосты ──
export type ToastTone = 'success' | 'danger' | 'warning' | 'info';
const ToastCtx = createContext<(text: ReactNode, tone?: ToastTone) => void>(() => {});
export const useToast = () => useContext(ToastCtx);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<{ id: number; text: ReactNode; tone: ToastTone }[]>([]);
  const n = useRef(0);
  const show = useCallback((text: ReactNode, tone: ToastTone = 'info') => {
    const id = ++n.current;
    setItems((x) => [...x.slice(-2), { id, text, tone }]);
    setTimeout(() => setItems((x) => x.filter((t) => t.id !== id)), 3500);
  }, []);
  return (
    <ToastCtx.Provider value={show}>
      {children}
      <div className="toasts" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast toast-${t.tone}`} onClick={() => setItems((x) => x.filter((i) => i.id !== t.id))}>{t.text}</div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}

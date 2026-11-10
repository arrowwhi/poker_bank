import type { Chat, GameSummary, Participant, PendingAction, Settlement, User } from '../api';
import { ago, chips, dateShort, handle, netClass, plural, rub, signedRub, time } from '../lib/format';
import type { SeriesPoint } from '../lib/stats';
import { Avatar, Badge, ListRow, Select, type SelectOption } from './ui';

export const ME_SUFFIX = ' (вы)';

export function who(u: User, meId: number) {
  return handle(u) + (u.id === meId ? ME_SUFFIX : '');
}

export function PlayerRow({ p, meId, dealerId, onClick }: { p: Participant; meId: number; dealerId: number; onClick?: () => void }) {
  const isDealer = p.user.id === dealerId;
  const parts: string[] = [];
  if (!p.is_active) {
    parts.push(`вышел: ${chips(p.total_out_chips)}`, `внёс ${rub(p.total_in_rub)}`);
  } else {
    if (isDealer) parts.push('Дилер');
    parts.push(`${p.buy_in_count} buy-in`);
    if (p.rebuy_count) parts.push(`${p.rebuy_count} rebuy`);
    parts.push(`внёс ${rub(p.total_in_rub)}`);
  }
  return (
    <ListRow
      onClick={onClick}
      left={<Avatar user={p.user} />}
      title={who(p.user, meId)}
      sub={parts.join(' · ')}
      right={
        <>
          <span className={`amount tabular ${netClass(p.net_rub)}`}>{signedRub(p.net_rub)}</span>
          {isDealer && p.is_active ? <Badge tone="dealer">🎩 Дилер</Badge> : p.is_active ? <Badge tone="success">В игре</Badge> : <Badge tone="neutral">Вышел</Badge>}
        </>
      }
    />
  );
}

const PA_LABEL: Record<PendingAction['type'], string> = { JOIN: 'Buy-in', REBUY: 'Rebuy', CASH_OUT: 'Cash-out' };

export function paTitle(p: PendingAction) {
  return `${handle(p.requester)} просит ${PA_LABEL[p.type]}`;
}
export function paSub(p: PendingAction) {
  const amount = p.type === 'CASH_OUT' ? `${chips(p.amount_chips)} → ${rub(p.amount_rub)}` : `${rub(p.amount_rub)} → ${chips(p.amount_chips)}`;
  return `${amount} · ${ago(p.created_at)}`;
}

export function RequestCard({ p, busy, onApprove, onReject }: { p: PendingAction; busy?: boolean; onApprove: () => void; onReject: () => void }) {
  return (
    <div className="card stack" style={{ gap: 12 }}>
      <div className="row">
        <Avatar user={p.requester} />
        <div className="grow">
          <div style={{ fontWeight: 600 }}>{paTitle(p)}</div>
          <div className="muted" style={{ fontSize: 13 }}>{paSub(p)}</div>
        </div>
      </div>
      <div className="grid2">
        <button className="btn btn-soft-danger" disabled={busy} onClick={onReject}>✕ Отклонить</button>
        <button className="btn btn-success" disabled={busy} onClick={onApprove}>✓ Подтвердить</button>
      </div>
    </div>
  );
}

export function GameCard({ g, meId, onClick }: { g: GameSummary; meId: number; onClick: () => void }) {
  const badge =
    g.my_role === 'dealer' ? <Badge tone="dealer">🎩 Дилер</Badge> :
    g.my_role === 'player' ? <Badge tone="success">Вы в игре</Badge> :
    g.my_role === 'out' ? <Badge tone="neutral">Вышли</Badge> :
    <Badge tone="accent">Войти</Badge>;
  return (
    <button className="card stack" style={{ textAlign: 'left', gap: 10 }} onClick={onClick}>
      <div className="row">
        <Avatar user={{ id: g.chat_id, display_name: g.chat_title }} />
        <div className="grow">
          <div style={{ fontWeight: 600 }} className="ellipsis">{g.chat_title}</div>
          <div className="muted" style={{ fontSize: 13 }}>Игра #{g.number} · идёт с {time(g.started_at)}</div>
        </div>
        {badge}
      </div>
      <div className="dealer-c" style={{ fontWeight: 500 }}>🎩 {g.dealer.id === meId ? 'Дилер — вы' : `Дилер ${handle(g.dealer)}`}</div>
      <div style={{ borderTop: '0.5px solid var(--border-separator)', paddingTop: 10 }} className="kv">
        <div><div className="k">Курс</div><div className="v">{rub(g.rate.buy_in_rub)} / {g.rate.buy_in_chips}</div></div>
        <div><div className="k">Игроков</div><div className="v">{g.players_total} · в игре {g.players_active}</div></div>
        <div><div className="k">Банк</div><div className="v">{rub(g.bank_rub)}</div></div>
      </div>
    </button>
  );
}

/** Строка перевода: 🟢 у отправителя — «оплатил», у получателя — «получил». */
export function SettlementRow({ s, meId }: { s: Settlement; meId: number }) {
  const name = (u: User, marked: boolean) => (
    <span className={`ellipsis ${marked ? 'marked' : ''}`}>{marked ? '🟢' : ''}{who(u, meId)}</span>
  );
  return (
    <div className="settle-row">
      {name(s.from, s.is_paid)}
      <span className="arrow">→</span>
      {name(s.to, s.is_received)}
      <span className="amt tabular">{rub(s.amount_rub)}</span>
    </div>
  );
}

/** Простой линейный график нарастающего итога (inline SVG). */
export function NetChart({ points }: { points: SeriesPoint[] }) {
  if (points.length < 2) return <div className="hint center" style={{ padding: 40 }}>Мало игр за период</div>;
  const W = 320, H = 150, P = 6;
  const ts = points.map((p) => p.t);
  const vs = points.map((p) => p.v);
  const t0 = Math.min(...ts), t1 = Math.max(...ts);
  const v0 = Math.min(0, ...vs), v1 = Math.max(0, ...vs);
  const x = (t: number) => P + ((t - t0) / (t1 - t0 || 1)) * (W - 2 * P);
  const y = (v: number) => P + (1 - (v - v0) / (v1 - v0 || 1)) * (H - 2 * P);
  const d = points.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`).join(' ');
  const last = vs[vs.length - 1];
  const color = last >= 0 ? 'var(--status-success)' : 'var(--status-danger)';
  const area = `${d} L${x(t1).toFixed(1)},${y(0).toFixed(1)} L${x(t0).toFixed(1)},${y(0).toFixed(1)} Z`;
  const fmt = (t: number) => new Date(t).toLocaleDateString('ru-RU', { month: 'short', year: '2-digit' }).replace(' г.', '');
  return (
    <div>
      <svg className="chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={`Итог ${signedRub(last)}`}>
        <line x1={P} x2={W - P} y1={y(0)} y2={y(0)} stroke="var(--border-separator)" strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />
        <path d={area} fill={color} opacity={0.12} />
        <path d={d} fill="none" stroke={color} strokeWidth={2.5} strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
      </svg>
      <div className="row hint" style={{ justifyContent: 'space-between', fontSize: 11, marginTop: 4 }}>
        <span>{fmt(t0)}</span><span>{fmt(t0 + (t1 - t0) / 2)}</span><span>{fmt(t1)}</span>
      </div>
    </div>
  );
}

const gamesLabel = (n: number) => `${n} ${plural(n, 'игра', 'игры', 'игр')}`;

/**
 * Выбор чата выпадающим списком: «Все чаты» (value = null) — первым пунктом, дальше чаты
 * в порядке API — от самых популярных вниз. С `all={false}` пункта «Все чаты» нет:
 * так выбирают чат там, где данные есть только по одному чату (рейтинг).
 */
export function ChatSelect({ chats, value, onChange, all = true }: {
  chats: Chat[]; value: number | null; onChange: (chatId: number | null) => void; all?: boolean;
}) {
  const total = chats.reduce((n, c) => n + c.games_count, 0);
  const options: SelectOption<number | 'all'>[] = [
    ...(!all ? [] : [{
      value: 'all' as const, title: 'Все чаты',
      sub: `${chats.length} ${plural(chats.length, 'чат', 'чата', 'чатов')} · ${gamesLabel(total)}`,
      left: (
        <span className="select-icon">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
            <rect width="6.5" height="6.5" rx="2" /><rect x="9.5" width="6.5" height="6.5" rx="2" />
            <rect y="9.5" width="6.5" height="6.5" rx="2" /><rect x="9.5" y="9.5" width="6.5" height="6.5" rx="2" />
          </svg>
        </span>
      ),
    }]),
    ...chats.map((c) => ({
      value: c.id as number | 'all', title: c.title,
      sub: c.games_count ? `${gamesLabel(c.games_count)}${c.last_game_at ? ` · последняя ${dateShort(c.last_game_at)}` : ''}` : 'ещё не играли',
      left: <Avatar user={{ id: c.id, display_name: c.title }} size={36} />,
    })),
  ];
  return <Select label="Чат" value={value ?? 'all'} options={options} onChange={(v) => onChange(v === 'all' ? null : v)} />;
}

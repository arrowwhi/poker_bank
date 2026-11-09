import { useMemo, useState } from 'react';
import { api, type ChatMember, type GameDetail, type Participant, type User } from '../api';
import { who } from '../components/domain';
import { Avatar, Badge, ListRow, NumberField, SearchField, Sheet, Skeleton, useConfirm } from '../components/ui';
import { chips, handle, netClass, num, rub, signedRub, time, ledgerLabel } from '../lib/format';
import { useAsync } from '../lib/hooks';
import { nearestValidChips, perChip, rubForChips } from '../lib/rate';
import { useMainButton } from '../telegram/buttons';
import { haptic } from '../telegram/telegram';

export type Act = (fn: () => Promise<GameDetail | unknown>, ok?: string) => Promise<boolean>;

// ── Cash-out: ввод фишек с валидацией chips*rate ∈ ℤ ──
export function CashoutSheet({ game, p, meId, mode, act, onClose }: {
  game: GameDetail; p: Participant; meId: number; mode: 'dealer' | 'request'; act: Act; onClose: () => void;
}) {
  const [v, setV] = useState('');
  const n = v === '' ? null : Number(v);
  const rubOut = n === null ? null : rubForChips(game.rate, n);
  const invalid = n !== null && rubOut === null;
  const net = rubOut !== null ? rubOut - p.total_in_rub : null;
  const [lo, hi] = n !== null ? nearestValidChips(game.rate, n) : [0, 0];
  const self = p.user.id === meId;
  const confirm = useConfirm();

  const submit = async () => {
    if (n === null || rubOut === null) return;
    if (mode === 'dealer') {
      const ok = await confirm({ icon: '💸', title: `Cash-out ${who(p.user, meId)}?`, text: `${chips(n)} = ${rub(rubOut)}. Игрок выйдет из игры.`, ok: 'Подтвердить', tone: 'success' });
      if (!ok) return;
      if (await act(() => api.dealerCashout(game.id, p.user.id, n), `${handle(p.user)} вышел: ${chips(n)} = ${rub(rubOut)}`)) onClose();
    } else if (await act(() => api.requestCashout(game.id, n), self && game.my_role === 'dealer' ? `Вы вышли: ${rub(rubOut)}` : 'Запрос на cash-out отправлен дилеру')) onClose();
  };

  useMainButton({
    text: mode === 'dealer' ? (rubOut !== null ? `Cash-out · ${rub(rubOut)}` : 'Cash-out') : game.my_role === 'dealer' ? 'Выйти из игры' : 'Отправить дилеру на подтверждение',
    disabled: rubOut === null,
    tone: mode === 'dealer' ? 'success' : 'accent',
    onClick: submit,
  });

  return (
    <Sheet title={mode === 'dealer' ? `Cash-out ${who(p.user, meId)}` : 'Выход из игры'}
      subtitle={mode === 'dealer' ? 'Посчитайте фишки игрока' : 'Посчитайте фишки и введите их количество. Дилер подтвердит выход.'}
      onClose={onClose}>
      <div className="stack">
        <NumberField label="Фишек на руках" value={v} onChange={setV} suffix="фишек" autoFocus
          error={invalid ? `${num(n!)} × ${perChip(game.rate.buy_in_rub, game.rate.buy_in_chips)} — нецелое число рублей. Ближайшие: ${num(lo)} или ${num(hi)}` : false} />
        <div className="card" style={{ background: 'var(--bg-elevated)' }}>
          <div className="row" style={{ justifyContent: 'space-between' }}><span className="muted">{self ? 'Вы внесли' : 'Внёс'}</span><b className="tabular">{rub(p.total_in_rub)}</b></div>
          <div className="row" style={{ justifyContent: 'space-between', marginTop: 6 }}><span className="muted">{self ? 'Вынесете' : 'Вынесет'}</span><b className="tabular">{rubOut !== null ? rub(rubOut) : '—'}</b></div>
          <div className="row" style={{ justifyContent: 'space-between', marginTop: 6 }}><span className="muted">Результат</span><b className={`tabular ${net !== null ? netClass(net) : ''}`}>{net !== null ? signedRub(net) : '—'}</b></div>
        </div>
        <div className="hint" style={{ fontSize: 13 }}>Курс игры: {perChip(game.rate.buy_in_rub, game.rate.buy_in_chips)} за фишку. Рубли — только целые.</div>
      </div>
    </Sheet>
  );
}

// ── Действия дилера с конкретным игроком ──
export function PlayerActionSheet({ game, p, meId, act, onClose, onCashout }: {
  game: GameDetail; p: Participant; meId: number; act: Act; onClose: () => void; onCashout: () => void;
}) {
  const confirm = useConfirm();
  const last = [...game.ledger].reverse().find((e) => !e.is_void);
  const lastIsHis = last && last.player.id === p.user.id;
  const r = game.rate;

  const rebuy = async () => {
    if (!(await confirm({ icon: '🔁', title: `Rebuy для ${handle(p.user)}?`, text: `${rub(r.rebuy_rub)} → ${chips(r.rebuy_chips)}`, ok: 'Подтвердить', tone: 'success' }))) return;
    if (await act(() => api.dealerRebuy(game.id, p.user.id), `Rebuy ${handle(p.user)} записан`)) onClose();
  };
  const buyin = async () => {
    if (!(await confirm({ icon: '🃏', title: `Buy-in для ${handle(p.user)}?`, text: `${rub(r.buy_in_rub)} → ${chips(r.buy_in_chips)}`, ok: 'Подтвердить', tone: 'success' }))) return;
    if (await act(() => api.dealerBuyIn(game.id, p.user.id), `${handle(p.user)} снова в игре`)) onClose();
  };
  const undo = async () => {
    if (!last) return;
    if (!(await confirm({ icon: '↩️', title: 'Отменить последнее?', text: `${ledgerLabel(last.type)} ${handle(last.player)} · ${rub(last.amount_rub)}, ${time(last.created_at)}`, ok: 'Отменить', tone: 'danger' }))) return;
    if (await act(() => api.undo(game.id, 1), 'Последнее действие отменено')) onClose();
  };
  const transfer = async () => {
    if (!(await confirm({ icon: '🎩', title: `Передать дилерство ${handle(p.user)}?`, text: 'Вы останетесь игроком, но потеряете права дилера.', ok: 'Передать' }))) return;
    if (await act(() => api.transferDealer(game.id, p.user.id), `Дилер теперь ${handle(p.user)}`)) onClose();
  };

  return (
    <Sheet title={who(p.user, meId)}
      subtitle={<>{p.buy_in_count} buy-in · {p.rebuy_count} rebuy · внёс {rub(p.total_in_rub)} · <span className={netClass(p.net_rub)}>{signedRub(p.net_rub)}</span></>}
      onClose={onClose}>
      <div className="card flush" style={{ background: 'var(--bg-secondary)', ['--sep-left' as string]: '64px' }}>
        {p.is_active ? (
          <>
            <ListRow left={<div className="icon-box">🔁</div>} title={`Rebuy · ${rub(r.rebuy_rub)} → ${num(r.rebuy_chips)} фишек`} sub="Без подтверждения" chevron onClick={rebuy} />
            <ListRow left={<div className="icon-box">💸</div>} title="Cash-out…" sub="Ввести фишки и вывести игрока" chevron onClick={onCashout} />
            {p.user.id !== game.dealer.id && (
              <ListRow left={<div className="icon-box">🎩</div>} title="Передать дилерство" sub="Только активному игроку" chevron onClick={transfer} />
            )}
          </>
        ) : (
          <ListRow left={<div className="icon-box">🃏</div>} title={`Вернуть в игру · Buy-in ${rub(r.buy_in_rub)}`} sub={`Вышел: ${chips(p.total_out_chips)}`} chevron onClick={buyin} />
        )}
        {lastIsHis && last && (
          <ListRow left={<div className="icon-box" style={{ background: 'var(--status-danger-soft)', color: 'var(--status-danger)' }}>↩️</div>}
            title={<span className="neg">Отменить последнее действие</span>} sub={`Undo: ${ledgerLabel(last.type)} ${handle(last.player)}, ${time(last.created_at)}`} onClick={undo} />
        )}
      </div>
    </Sheet>
  );
}

// ── Выбор игрока для buy-in / rebuy / cash-out ──
export type PickMode = 'buyin' | 'rebuy' | 'cashout';

export function PlayerPickerSheet({ game, mode, meId, act, onClose, onCashout }: {
  game: GameDetail; mode: PickMode; meId: number; act: Act; onClose: () => void; onCashout: (p: Participant) => void;
}) {
  const [q, setQ] = useState('');
  const [sel, setSel] = useState<User | null>(null);
  const confirm = useConfirm();
  const members = useAsync(() => (mode === 'buyin' ? api.listChatMembers(game.chat_id) : Promise.resolve([] as ChatMember[])), [mode, game.chat_id]);
  const activeIds = new Set(game.participants.filter((p) => p.is_active).map((p) => p.user.id));
  const out = new Map(game.participants.filter((p) => !p.is_active).map((p) => [p.user.id, p]));

  const candidates: { user: User; disabled?: boolean; busy?: boolean; note: string }[] = useMemo(() => {
    if (mode === 'buyin') {
      return (members.data ?? []).map((u) => ({
        user: u,
        disabled: activeIds.has(u.id) || !!u.busy_in,
        busy: !activeIds.has(u.id) && !!u.busy_in,
        note: activeIds.has(u.id) ? 'уже в игре' : u.busy_in ? `в игре в «${u.busy_in.chat_title}»` : out.has(u.id) ? `вышел · ${chips(out.get(u.id)!.total_out_chips)}` : game.pending.some((p) => p.target.id === u.id && p.type === 'JOIN') ? 'просит buy-in' : u.display_name,
      }));
    }
    return game.participants.filter((p) => p.is_active).map((p) => ({
      user: p.user, note: `${p.buy_in_count} buy-in · ${p.rebuy_count} rebuy · внёс ${rub(p.total_in_rub)}`,
    }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, members.data, game]);

  const s = q.trim().toLowerCase().replace(/^@/, '');
  const list = candidates.filter((c) => !s || c.user.display_name.toLowerCase().includes(s) || (c.user.username ?? '').toLowerCase().includes(s));
  const r = game.rate;
  const [title, verb] = mode === 'buyin' ? ['Buy-in игрока', 'Buy-in'] : mode === 'rebuy' ? ['Rebuy игроку', 'Rebuy'] : ['Cash-out игрока', 'Cash-out'];

  const go = async () => {
    if (!sel) return;
    if (mode === 'cashout') {
      const p = game.participants.find((x) => x.user.id === sel.id);
      if (p) onCashout(p);
      return;
    }
    const [rr, cc] = mode === 'buyin' ? [r.buy_in_rub, r.buy_in_chips] : [r.rebuy_rub, r.rebuy_chips];
    if (!(await confirm({ icon: mode === 'buyin' ? '🃏' : '🔁', title: `${verb} для ${handle(sel)}?`, text: `${rub(rr)} → ${chips(cc)}. Запишется сразу, без подтверждения.`, ok: 'Подтвердить', tone: 'success' }))) return;
    const ok = await act(
      () => (mode === 'buyin' ? api.dealerBuyIn(game.id, sel.id) : api.dealerRebuy(game.id, sel.id)),
      `${verb} ${handle(sel)}: ${rub(rr)} → ${chips(cc)}`,
    );
    if (ok) onClose();
  };

  useMainButton({ text: sel ? `${verb} для ${handle(sel)}` : 'Выберите игрока', disabled: !sel, onClick: go });

  return (
    <Sheet title={title}
      subtitle={mode === 'buyin' ? `${rub(r.buy_in_rub)} → ${chips(r.buy_in_chips)}` : mode === 'rebuy' ? `${rub(r.rebuy_rub)} → ${chips(r.rebuy_chips)} · только тем, кто в игре` : 'Только тем, кто в игре'}
      onClose={onClose}>
      <div className="stack">
        <SearchField value={q} onChange={setQ} placeholder="Поиск по имени или @нику" />
        {mode === 'buyin' && !members.data ? <Skeleton h={180} /> : (
          <div className="card flush" style={{ background: 'var(--bg-secondary)', ['--sep-left' as string]: '64px' }}>
            {list.map((c) => (
              <ListRow key={c.user.id}
                onClick={c.disabled ? undefined : () => { haptic.select(); setSel(c.user); }}
                left={<Avatar user={c.user} />}
                title={<span style={{ opacity: c.disabled ? 0.5 : 1 }}>{who(c.user, meId)}</span>}
                sub={c.note}
                right={sel?.id === c.user.id ? <span style={{ color: 'var(--accent-primary)', fontSize: 20 }}>✓</span> : c.busy ? <Badge tone="warning">Занят</Badge> : c.disabled ? <Badge tone="success">В игре</Badge> : undefined}
              />
            ))}
            {!list.length && <div className="hint center" style={{ padding: 20 }}>Никого не нашли</div>}
          </div>
        )}
        {mode === 'buyin' && (
          <div className="hint" style={{ fontSize: 13 }}>Нет нужного игрока? Бот знает только тех, кто писал в группе. Попросите его отправить <b>/join</b>.</div>
        )}
      </div>
    </Sheet>
  );
}

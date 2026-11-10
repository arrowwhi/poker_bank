import { useCallback, useEffect, useRef, useState } from 'react';
import { api, ApiError, type GameDetail, type Participant, type PendingAction } from '../api';
import { PlayerRow, RequestCard, paSub, paTitle, who } from '../components/domain';
import { Avatar, ErrorState, ListRow, Loading, Screen, SectionHeader, Segmented, StatTile, useConfirm, useToast } from '../components/ui';
import { chips, duration, handle, netClass, rub, signedRub, time, ledgerLabel } from '../lib/format';
import { useAsync } from '../lib/hooks';
import { rateLabel } from '../lib/rate';
import { useNav } from '../router';
import { useSession } from '../session';
import { useMainButton } from '../telegram/buttons';
import { haptic, tg } from '../telegram/telegram';
import { CashoutSheet, PlayerActionSheet, PlayerPickerSheet, type Act, type PickMode } from './GameSheets';

const POLL_MS = 4000;

export function GameScreen({ id }: { id: number }) {
  const nav = useNav();
  const toast = useToast();
  const { me } = useSession();
  const meId = me.user.id;
  const g = useAsync(() => api.getGame(id), [id], POLL_MS);
  const game = g.data;
  const [tab, setTab] = useState<'dealer' | 'player'>('dealer');
  const [busy, setBusy] = useState(false);

  /** Обёртка мутаций: прогресс, haptic, тост, обновление данных. */
  const act: Act = useCallback(async (fn, ok) => {
    setBusy(true);
    try {
      const res = await fn();
      if (res && typeof res === 'object' && 'participants' in res) g.set(res as GameDetail);
      else await g.reload();
      haptic.success();
      if (ok) toast(ok, 'success');
      return true;
    } catch (e) {
      haptic.error();
      toast(e instanceof Error ? e.message : 'Ошибка', 'danger');
      await g.reload();
      return false;
    } finally {
      setBusy(false);
    }
  }, [g, toast]);

  // Тосты «подтверждено / отклонено» для собственных запросов.
  const seenMine = useRef<Map<number, string>>();
  useEffect(() => {
    if (!game) return;
    const prev = seenMine.current;
    const next = new Map(game.my_requests.map((p) => [p.id, p.status]));
    if (prev) {
      for (const p of game.my_requests) {
        if (prev.get(p.id) !== 'pending') continue;
        const label = p.type === 'JOIN' ? 'Buy-in' : p.type === 'REBUY' ? 'Rebuy' : 'Cash-out';
        if (p.status === 'confirmed') { haptic.success(); toast(`✅ ${label} подтверждён дилером`, 'success'); }
        if (p.status === 'rejected') { haptic.error(); toast(`❌ Запрос отклонён`, 'danger'); }
        if (p.status === 'expired') { haptic.warning(); toast('⏰ Запрос истёк — отправьте снова', 'warning'); }
      }
    }
    seenMine.current = next;
  }, [game, toast]);

  // In-app уведомление дилеру о новых запросах.
  const seenPending = useRef<Set<number>>();
  const [incoming, setIncoming] = useState<PendingAction | null>(null);
  useEffect(() => {
    if (!game || game.my_role !== 'dealer') return;
    const ids = new Set(game.pending.map((p) => p.id));
    if (seenPending.current) {
      const fresh = game.pending.filter((p) => !seenPending.current!.has(p.id) && p.requester.id !== meId);
      if (fresh.length) { setIncoming(fresh[fresh.length - 1]); tg.HapticFeedback.impactOccurred('medium'); }
    }
    seenPending.current = ids;
  }, [game, meId]);
  useEffect(() => {
    if (!incoming) return;
    if (game && !game.pending.some((p) => p.id === incoming.id)) { setIncoming(null); return; }
    const t = setTimeout(() => setIncoming(null), 8000);
    return () => clearTimeout(t);
  }, [incoming, game]);

  // Завершённая игра → экран итогов.
  useEffect(() => {
    if (game?.status === 'finished') nav.replace({ name: 'results', id: game.id });
  }, [game?.status, game?.id, nav]);

  if (g.error && !game) return <><ErrorState error={g.error} onRetry={g.reload} /></>;
  if (!game || game.status === 'finished') return <Loading />;

  if (game.status === 'cancelled') {
    return (
      <Screen title={`Игра #${game.number}`} subtitle={game.chat_title}>
        <div className="empty"><div className="big">🚫</div><h3>Игра отменена</h3><p>Итоги и переводы не создавались. Лог событий сохранён.</p></div>
      </Screen>
    );
  }

  const isDealer = game.my_role === 'dealer';
  const pendingCount = game.pending.length;

  return (
    <Screen title={`Игра #${game.number}`} subtitle={game.chat_title}>
      {isDealer && (
        <Segmented value={tab} onChange={(v) => { haptic.select(); setTab(v); }} options={[
          { value: 'player', label: '🃏 Игрок' },
          { value: 'dealer', label: <>🎩 Дилер {pendingCount > 0 && <span className="count-dot">{pendingCount}</span>}</> },
        ]} />
      )}
      {isDealer && tab === 'dealer'
        ? <DealerView game={game} meId={meId} act={act} busy={busy} />
        : <PlayerView game={game} meId={meId} act={act} busy={busy} />}

      {incoming && isDealer && (
        <div className="notify card stack" style={{ gap: 10 }}>
          <div className="row">
            <Avatar user={incoming.requester} />
            <div className="grow">
              <div style={{ fontWeight: 600 }}>🃏 {paTitle(incoming)}</div>
              <div className="muted" style={{ fontSize: 13 }}>{paSub(incoming)}</div>
            </div>
          </div>
          <div className="grid2">
            <button className="btn btn-soft-danger btn-sm" style={{ width: '100%' }} disabled={busy}
              onClick={() => { setIncoming(null); haptic.warning(); void act(() => api.reject(game.id, incoming.id), 'Запрос отклонён'); }}>✕ Отклонить</button>
            <button className="btn btn-success btn-sm" style={{ width: '100%' }} disabled={busy}
              onClick={() => { setIncoming(null); void act(() => api.approve(game.id, incoming.id), `✅ ${paTitle(incoming).replace('просит', '—')} подтверждён`); }}>✓ Подтвердить</button>
          </div>
        </div>
      )}
    </Screen>
  );
}

function GameHeader({ game, meId }: { game: GameDetail; meId: number }) {
  const r = game.rate;
  return (
    <div className="card stack" style={{ gap: 12 }}>
      <div className="row">
        <Avatar user={{ id: game.chat_id, display_name: game.chat_title }} />
        <div className="grow">
          <div style={{ fontWeight: 600, fontSize: 17 }} className="ellipsis">{game.chat_title}</div>
          <div className="muted" style={{ fontSize: 13 }}>Игра #{game.number} · идёт {duration(game.started_at)}</div>
        </div>
      </div>
      <div className="kv">
        <div><div className="k">Дилер</div><div className="v dealer-c ellipsis">{game.dealer.id === meId ? 'вы' : handle(game.dealer)}</div></div>
        <div><div className="k">Курс</div><div className="v">{rateLabel(r)}</div></div>
        <div><div className="k">Банк</div><div className="v">{rub(game.bank_rub)}</div></div>
      </div>
      <div className="hint" style={{ fontSize: 13 }}>Buy-in {rub(r.buy_in_rub)} → {chips(r.buy_in_chips)} · Rebuy {rub(r.rebuy_rub)} → {chips(r.rebuy_chips)}</div>
    </div>
  );
}

function Participants({ game, meId, onClick, hint }: { game: GameDetail; meId: number; onClick?: (p: Participant) => void; hint?: string }) {
  const ps = [...game.participants].sort((a, b) =>
    Number(b.user.id === meId) - Number(a.user.id === meId) || Number(b.is_active) - Number(a.is_active));
  const active = ps.filter((p) => p.is_active).length;
  return (
    <>
      <SectionHeader title="Участники" right={`${ps.length} · ${hint ?? `в игре ${active}`}`} />
      {ps.length ? (
        <div className="card flush" style={{ ['--sep-left' as string]: '68px' }}>
          {ps.map((p) => <PlayerRow key={p.user.id} p={p} meId={meId} dealerId={game.dealer.id} onClick={onClick ? () => onClick(p) : undefined} />)}
        </div>
      ) : <div className="hint center" style={{ padding: 16 }}>Пока никто не сделал buy-in</div>}
    </>
  );
}

// ───────────────────────── Игрок ─────────────────────────

function PlayerView({ game, meId, act, busy }: { game: GameDetail; meId: number; act: Act; busy: boolean }) {
  const me = game.participants.find((p) => p.user.id === meId);
  const myPending = game.pending.find((p) => p.target.id === meId) ?? game.my_requests.find((p) => p.status === 'pending');
  const [cashout, setCashout] = useState(false);
  const confirm = useConfirm();
  const r = game.rate;
  const isDealer = game.my_role === 'dealer';

  const state: 'waiting' | 'none' | 'active' | 'out' = myPending ? 'waiting' : !me ? 'none' : me.is_active ? 'active' : 'out';
  // Правило «одна активная игра на игрока»: войти нельзя, пока я в другой игре.
  const elsewhere = (state === 'none' || state === 'out') ? game.my_busy_in : null;
  const nav = useNav();

  const join = () => act(() => api.requestJoin(game.id), isDealer ? 'Buy-in записан' : 'Запрос на buy-in отправлен дилеру');
  const rebuy = async () => {
    if (!(await confirm({ icon: '🔁', title: 'Rebuy?', text: `${rub(r.rebuy_rub)} → ${chips(r.rebuy_chips)}${isDealer ? '' : '. Дилер подтвердит запрос.'}`, ok: isDealer ? 'Записать' : 'Отправить' }))) return;
    await act(() => api.requestRebuy(game.id), isDealer ? 'Rebuy записан' : 'Запрос на rebuy отправлен дилеру');
  };

  useMainButton(
    elsewhere ? { text: 'Перейти к своей игре', onClick: () => nav.replace({ name: 'game', id: elsewhere.game_id }) } :
    state === 'waiting' ? { text: '⏳ Ждём подтверждения дилера', disabled: true, onClick: () => {} } :
    state === 'none' ? { text: `Buy-in · ${rub(r.buy_in_rub)} → ${chips(r.buy_in_chips)}`, progress: busy, onClick: join } :
    state === 'active' ? { text: `Rebuy · ${rub(r.rebuy_rub)} → ${chips(r.rebuy_chips)}`, progress: busy, onClick: rebuy } :
    { text: `Вернуться · Buy-in ${rub(r.buy_in_rub)}`, progress: busy, onClick: join },
  );

  return (
    <>
      <GameHeader game={game} meId={meId} />

      {state === 'waiting' && myPending && (
        <div className="banner banner-warning row">
          <div className="grow">
            <b>⏳ Ждём подтверждения дилера</b>
            {myPending.type === 'JOIN' ? 'Buy-in' : myPending.type === 'REBUY' ? 'Rebuy' : 'Cash-out'} · {myPending.type === 'CASH_OUT' ? `${chips(myPending.amount_chips)} → ${rub(myPending.amount_rub)}` : `${rub(myPending.amount_rub)} → ${chips(myPending.amount_chips)}`} · {handle(game.dealer)} получил уведомление
          </div>
          <button className="btn btn-plain" style={{ width: 'auto' }} disabled={busy}
            onClick={() => act(() => api.cancelRequest(game.id, myPending.id), 'Запрос отозван')}>Отозвать</button>
        </div>
      )}

      {elsewhere && (
        <div className="banner banner-warning">
          <b>Вы уже в другой игре</b>
          Сейчас вы в игре в «{elsewhere.chat_title}». Одновременно можно быть только в одной: выйдите оттуда через cash-out (или завершите её, если вы дилер), и тогда сможете сесть за этот стол.
        </div>
      )}

      {state === 'none' && !elsewhere && (
        <div className="banner banner-accent">
          <b>Вы ещё не в игре</b>
          Нажмите «Buy-in» — {isDealer ? 'запись появится сразу.' : `${handle(game.dealer)} подтвердит запрос, и фишки ваши.`}
        </div>
      )}

      {me && (state === 'active' || (state === 'waiting' && me.is_active)) && (
        <>
          <div className="grid2">
            <StatTile label="Внесено" value={rub(me.total_in_rub)} />
            <StatTile label="Buy-in + Rebuy" value={`${me.buy_in_count} + ${me.rebuy_count}`} />
          </div>
          <button className="btn btn-soft btn-lg" disabled={state === 'waiting'} onClick={() => setCashout(true)}>Выйти из игры (cash-out)</button>
        </>
      )}

      {me && state === 'out' && (
        <div className={`banner ${me.net_rub >= 0 ? 'banner-success' : 'banner-danger'}`}>
          <span className="muted" style={{ fontSize: 13 }}>Вы вышли из игры</span>
          <div className={`tabular ${netClass(me.net_rub)}`} style={{ fontSize: 30, fontWeight: 800, margin: '2px 0' }}>{signedRub(me.net_rub)}</div>
          Внесено {rub(me.total_in_rub)} · вынесено {rub(me.total_out_rub)} ({chips(me.total_out_chips)})
          <div className="hint" style={{ fontSize: 13, marginTop: 6 }}>Итог станет окончательным после завершения игры. Можно вернуться — нужен новый buy-in.</div>
        </div>
      )}

      <Participants game={game} meId={meId} />

      {cashout && me && <CashoutSheet game={game} p={me} meId={meId} mode="request" act={act} onClose={() => setCashout(false)} />}
    </>
  );
}

// ───────────────────────── Дилер ─────────────────────────

function DealerView({ game, meId, act, busy }: { game: GameDetail; meId: number; act: Act; busy: boolean }) {
  const nav = useNav();
  const confirm = useConfirm();
  const [picker, setPicker] = useState<PickMode | null>(null);
  const [actionFor, setActionFor] = useState<Participant | null>(null);
  const [cashoutFor, setCashoutFor] = useState<Participant | null>(null);

  const active = game.participants.filter((p) => p.is_active);
  const outN = game.participants.length - active.length;
  const allOut = game.participants.length > 0 && active.length === 0;
  const ready = allOut && game.bank_rub === 0;
  const mismatch = allOut && game.bank_rub !== 0;
  const last = [...game.ledger].reverse().find((e) => !e.is_void);

  const finish = async (force: boolean) => {
    try {
      const d = await api.endGame(game.id, force);
      haptic.success();
      if (d.status === 'cancelled') { nav.pop(); return; }
      nav.replace({ name: 'results', id: game.id });
    } catch (e) {
      if (e instanceof ApiError && e.code === 'bank_mismatch') {
        haptic.warning();
        const delta = Number(e.details?.bank_rub ?? game.bank_rub);
        if (await confirm(forceDialog(delta))) void finish(true);
      } else {
        haptic.error();
        await act(() => Promise.reject(e));
      }
    }
  };
  const end = async () => {
    if (mismatch) {
      if (await confirm(forceDialog(game.bank_rub))) void finish(true);
      return;
    }
    if (await confirm({ icon: '🏁', title: 'Завершить игру?', text: 'Бот посчитает переводы и опубликует план в группе. Отменить будет нельзя.', ok: 'Завершить', tone: 'success' })) void finish(false);
  };
  const cancelGame = async () => {
    if (!(await confirm({ icon: '🚫', title: 'Отменить игру?', text: 'Итоги и переводы не будут созданы. Лог событий сохранится.', ok: 'Отменить игру', tone: 'danger' }))) return;
    if (await act(() => api.cancelGame(game.id), 'Игра отменена')) nav.pop();
  };
  const undo = async () => {
    if (!last) return;
    if (!(await confirm({ icon: '↩️', title: 'Отменить последнее действие?', text: `${ledgerLabel(last.type)} ${handle(last.player)} · ${rub(last.amount_rub)}, ${time(last.created_at)}`, ok: 'Отменить', tone: 'danger' }))) return;
    await act(() => api.undo(game.id, 1), 'Последнее действие отменено');
  };

  // Завершение игры живёт только в MainButton: в контенте его нет, чтобы на экране было одно главное действие.
  const noSheet = !picker && !actionFor && !cashoutFor;
  useMainButton(!noSheet ? null :
    ready ? { text: '🏁 Завершить игру', tone: 'success', onClick: end } :
    mismatch ? { text: `Завершить с расхождением ${signedRub(game.bank_rub)}`, tone: 'danger', onClick: end } :
    { text: '＋ Buy-in игрока', onClick: () => setPicker('buyin') });

  return (
    <>
      {game.pending.length > 0 && (
        <>
          <SectionHeader title="Запросы" right={game.pending.length} />
          {game.pending.map((p) => (
            <RequestCard key={p.id} p={p} busy={busy}
              onApprove={() => act(() => api.approve(game.id, p.id), `✅ Подтверждено: ${paTitle(p).replace(' просит', ' —')}`)}
              onReject={() => { haptic.warning(); void act(() => api.reject(game.id, p.id), 'Запрос отклонён'); }} />
          ))}
        </>
      )}

      {ready && <div className="banner banner-success"><b>✅ Все вышли, банк сошёлся</b>Бот посчитает переводы и опубликует план в группе.</div>}
      {mismatch && (
        <div className="banner banner-warning">
          <b>⚠️ Расхождение банка: {signedRub(game.bank_rub)}</b>
          {game.bank_rub > 0
            ? `Выдано фишек больше, чем вернули. Проверьте cash-out’ы. Если завершить так — долг проигравших пропорционально уменьшится на ${rub(Math.abs(game.bank_rub))}.`
            : `Вернули фишек больше, чем выдано. Проверьте cash-out’ы. Если завершить так — выигрыш победителей пропорционально уменьшится на ${rub(Math.abs(game.bank_rub))}.`}
          <div><button className="btn btn-plain" style={{ padding: '6px 0', width: 'auto' }} onClick={undo}>↩️ Отменить последнее действие</button></div>
        </div>
      )}

      <div className="grid2">
        <StatTile label="Банк" value={rub(game.bank_rub)} className={mismatch ? 'neg' : ''} />
        <StatTile label="В игре / вышли" value={`${active.length} / ${outN}`} />
      </div>

      <SectionHeader title="Действия дилера" />
      <div className="card flush" style={{ ['--sep-left' as string]: '64px' }}>
        <ListRow left={<div className="icon-box">＋</div>} title="Buy-in игрока" sub="Добавить нового участника" chevron onClick={() => setPicker('buyin')} />
        <ListRow left={<div className="icon-box">🔁</div>} title="Rebuy игроку" sub="Только тем, кто в игре" chevron onClick={() => active.length && setPicker('rebuy')} />
        <ListRow left={<div className="icon-box">💸</div>} title="Cash-out игрока" sub="Вывести фишки и закрыть игрока" chevron onClick={() => active.length && setPicker('cashout')} />
        <ListRow left={<div className="icon-box">↩️</div>} title="Отменить последнее"
          sub={last ? `Undo: ${ledgerLabel(last.type)} ${handle(last.player)}, ${time(last.created_at)}` : 'Нет действий'} chevron onClick={undo} />
        <ListRow left={<div className="icon-box icon-box-danger">✕</div>} title={<span className="neg">Отменить игру</span>} sub="Без итогов и переводов" chevron onClick={cancelGame} />
      </div>

      <Participants game={game} meId={meId} hint="нажмите для действий" onClick={(p) => { haptic.tap(); setActionFor(p); }} />

      {active.length > 0 && (
        <div className="card flush">
          <ListRow left={<div className="icon-box icon-box-neutral">🏁</div>} title="Завершение игры"
            sub={`Осталось сделать cash-out: ${active.map((p) => who(p.user, meId)).join(', ')}`} chevron onClick={() => setPicker('cashout')} />
        </div>
      )}

      {picker && (
        <PlayerPickerSheet game={game} mode={picker} meId={meId} act={act} onClose={() => setPicker(null)}
          onCashout={(p) => { setPicker(null); setCashoutFor(p); }} />
      )}
      {actionFor && (
        <PlayerActionSheet game={game} p={game.participants.find((x) => x.user.id === actionFor.user.id) ?? actionFor} meId={meId} act={act}
          onClose={() => setActionFor(null)} onCashout={() => { setCashoutFor(actionFor); setActionFor(null); }} />
      )}
      {cashoutFor && <CashoutSheet game={game} p={cashoutFor} meId={meId} mode="dealer" act={act} onClose={() => setCashoutFor(null)} />}
    </>
  );
}

function forceDialog(delta: number) {
  return {
    icon: '⚠️',
    title: `Расхождение ${signedRub(delta)}`,
    text: delta > 0
      ? `Долг проигравших будет пропорционально уменьшен на ${rub(Math.abs(delta))}. Никто не заплатит больше расчётного.`
      : `Выигрыш победителей будет пропорционально уменьшен на ${rub(Math.abs(delta))}. Никто не получит больше расчётного.`,
    ok: 'Завершить',
    tone: 'danger' as const,
  };
}


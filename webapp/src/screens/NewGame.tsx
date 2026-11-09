import { useEffect, useState } from 'react';
import { api, type Chat, type GameDetail, type Rate } from '../api';
import { Avatar, Chips, ErrorState, ListRow, NumberField, Screen, SectionHeader, Skeleton, Steps, Switch, useToast } from '../components/ui';
import { gameLink } from '../lib/config';
import { chips, handle, rub } from '../lib/format';
import { useAsync } from '../lib/hooks';
import { isProportional, perChip, rateLabel } from '../lib/rate';
import { useNav } from '../router';
import { useSession } from '../session';
import { useBackButton, useMainButton } from '../telegram/buttons';
import { haptic } from '../telegram/telegram';

type Step = 'chat' | 'params' | 'confirm' | 'done';
const STEP_N: Record<Step, number> = { chat: 1, params: 2, confirm: 3, done: 3 };

const PRESETS: { label: string; rate: Rate }[] = [
  { label: '1000 / 1000', rate: { buy_in_rub: 1000, buy_in_chips: 1000, rebuy_rub: 1000, rebuy_chips: 1000 } },
  { label: '500 / 500', rate: { buy_in_rub: 500, buy_in_chips: 500, rebuy_rub: 500, rebuy_chips: 500 } },
  { label: '2000 / 2000', rate: { buy_in_rub: 2000, buy_in_chips: 2000, rebuy_rub: 2000, rebuy_chips: 2000 } },
];

export function NewGame({ chatId }: { chatId?: number }) {
  const nav = useNav();
  const toast = useToast();
  const { me, reloadMe } = useSession();
  const chats = useAsync(() => api.listChats(), []);
  const [step, setStep] = useState<Step>('chat');
  const [chat, setChat] = useState<Chat | null>(null);
  const [f, setF] = useState({ bir: '1000', bic: '1000', rr: '1000', rc: '1000' });
  const [same, setSame] = useState(true);
  const [preset, setPreset] = useState<string | null>('1000 / 1000');
  const [selfBuyIn, setSelfBuyIn] = useState(true);
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState<GameDetail | null>(null);

  // Предвыбор чата из deep-link / параметра.
  useEffect(() => {
    if (!chats.data || chat) return;
    const c = chats.data.find((x) => x.id === chatId && !x.active_game);
    if (c) setChat(c);
  }, [chats.data, chatId, chat]);

  const rate: Rate = {
    buy_in_rub: Number(f.bir) || 0, buy_in_chips: Number(f.bic) || 0,
    rebuy_rub: Number(same ? f.bir : f.rr) || 0, rebuy_chips: Number(same ? f.bic : f.rc) || 0,
  };
  const positive = Object.values(rate).every((v) => v > 0);
  const proportional = positive && isProportional(rate);
  const suggestChips = rate.buy_in_rub ? (rate.rebuy_rub * rate.buy_in_chips) / rate.buy_in_rub : 0;

  const applyRate = (r: Rate, label: string) => {
    haptic.select();
    setPreset(label);
    setF({ bir: String(r.buy_in_rub), bic: String(r.buy_in_chips), rr: String(r.rebuy_rub), rc: String(r.rebuy_chips) });
    setSame(r.buy_in_rub === r.rebuy_rub && r.buy_in_chips === r.rebuy_chips);
  };
  const edit = (k: keyof typeof f) => (v: string) => { setPreset(null); setF((x) => ({ ...x, [k]: v })); };

  const create = async () => {
    if (!chat) return;
    setBusy(true);
    try {
      const g = await api.createGame({ chat_id: chat.id, rate, dealer_buy_in: selfBuyIn });
      haptic.success();
      setCreated(g);
      setStep('done');
      void reloadMe();
    } catch (e) {
      haptic.error();
      toast(e instanceof Error ? e.message : 'Ошибка', 'danger');
    } finally {
      setBusy(false);
    }
  };

  // «Назад» внутри мастера — на предыдущий шаг.
  useBackButton(step === 'params' ? () => setStep('chat') : step === 'confirm' ? () => setStep('params') : null);

  // Правило «одна активная игра на игрока»: пока я в другой игре, создать новую нельзя.
  const mine = step !== 'done' ? me.active_game : null;

  useMainButton(
    mine ? { text: 'Перейти к своей игре', onClick: () => nav.replace({ name: 'game', id: mine.id }) } :
    step === 'chat' ? { text: 'Далее', disabled: !chat, onClick: () => setStep('params') } :
    step === 'params' ? { text: 'Далее', disabled: !proportional, onClick: () => setStep('confirm') } :
    step === 'confirm' ? { text: '🃏 Начать игру', tone: 'success', progress: busy, onClick: create } :
    { text: 'Открыть экран дилера', onClick: () => created && nav.replace({ name: 'game', id: created.id }) },
  );

  if (chats.error) return <ErrorState error={chats.error} onRetry={chats.reload} />;

  if (mine) {
    return (
      <Screen title="Новая игра">
        <div className="banner banner-warning">
          <b>Вы уже в игре</b>
          {mine.my_role === 'dealer' ? 'Вы ведёте' : 'Вы играете в'} игру #{mine.number} в «{mine.chat_title}». Одновременно можно быть только в одной игре — завершите её или сделайте cash-out, затем создайте новую.
        </div>
      </Screen>
    );
  }

  if (step === 'done' && created) {
    const link = gameLink(created.chat_id);
    return (
      <Screen title="Банкир" subtitle={created.chat_title}>
        <div className="empty">
          <div style={{ width: 72, height: 72, borderRadius: 36, background: 'var(--status-success-soft)', color: 'var(--status-success)', display: 'grid', placeItems: 'center', fontSize: 36, margin: '0 auto' }}>✓</div>
          <h3 style={{ fontSize: 22 }}>Игра #{created.number} начата</h3>
          <p>Вы — дилер. Сообщение с параметрами опубликовано в «{created.chat_title}». Игроки жмут «Войти» или пишут <b>/join</b> — запросы придут сюда.</p>
          {link && (
            <button className="btn btn-plain" onClick={() => { void navigator.clipboard?.writeText(link); haptic.success(); toast('Ссылка скопирована', 'success'); }}>
              Скопировать ссылку на игру
            </button>
          )}
        </div>
      </Screen>
    );
  }

  return (
    <Screen title="Новая игра" subtitle={`Шаг ${STEP_N[step]} из 3`}>
      <Steps total={3} current={STEP_N[step]} />

      {step === 'chat' && (
        <>
          <h2 style={{ margin: '6px 0 0' }}>За каким столом играем?</h2>
          <div className="muted">Игра будет привязана к группе — туда бот пришлёт сводку и план переводов.</div>
          <SectionHeader title="Группы с ботом" />
          {!chats.data ? <Skeleton h={180} /> : (
            <div className="card flush" style={{ ['--sep-left' as string]: '68px' }}>
              {chats.data.map((c) => {
                const busyChat = !!c.active_game;
                const on = chat?.id === c.id;
                return (
                  <ListRow key={c.id} className={on ? 'option-row on' : 'option-row'}
                    onClick={() => { if (busyChat) { nav.push({ name: 'game', id: c.active_game!.id }); return; } haptic.select(); setChat(c); }}
                    left={<Avatar user={{ id: c.id, display_name: c.title }} />}
                    title={c.title}
                    sub={busyChat ? <span style={{ color: 'var(--status-warning)' }}>Идёт игра · дилер {c.active_game!.id && handle(c.active_game!.dealer)}</span> : `${c.members_count} участников · стол свободен`}
                    right={busyChat ? <span className="chev">›</span> : <span style={{ fontSize: 20, color: on ? 'var(--accent-primary)' : 'var(--text-hint)' }}>{on ? '◉' : '○'}</span>} />
                );
              })}
            </div>
          )}
          <div className="hint" style={{ fontSize: 13 }}>Нет нужной группы? Добавьте бота в группу и напишите там <b>/start</b>. В одной группе может идти только одна игра.</div>
        </>
      )}

      {step === 'params' && (
        <>
          <h2 style={{ margin: '6px 0 0' }}>Ставки и фишки</h2>
          <Chips<string> value={preset} onChange={(l) => {
            const p = PRESETS.find((x) => x.label === l);
            if (p) applyRate(p.rate, l); else if (chat?.last_rate) applyRate(chat.last_rate, l);
          }} options={[...PRESETS.map((p) => ({ value: p.label, label: p.label })), ...(chat?.last_rate ? [{ value: 'last', label: 'Как в прошлый раз' }] : [])]} />
          <div className="grid2">
            <NumberField label="Buy-in, ₽" value={f.bir} onChange={edit('bir')} suffix="₽" error={!rate.buy_in_rub} />
            <NumberField label="Фишек за buy-in" value={f.bic} onChange={edit('bic')} suffix="ф." error={!rate.buy_in_chips} />
          </div>
          <div className="card row">
            <div className="grow">
              <div style={{ fontWeight: 600 }}>Rebuy = buy-in</div>
              <div className="muted" style={{ fontSize: 13 }}>{same ? 'Те же сумма и фишки' : 'Задайте rebuy отдельно'}</div>
            </div>
            <Switch on={same} onChange={(v) => { haptic.select(); setSame(v); setPreset(null); if (!v) setF((x) => ({ ...x, rr: x.bir, rc: x.bic })); }} />
          </div>
          {!same && (
            <div className="grid2">
              <NumberField label="Rebuy, ₽" value={f.rr} onChange={edit('rr')} suffix="₽" error={positive && !proportional} />
              <NumberField label="Фишек за rebuy" value={f.rc} onChange={edit('rc')} suffix="ф." error={positive && !proportional} />
            </div>
          )}
          {positive && !proportional && (
            <div className="banner banner-danger">
              <b>Курс не пропорционален</b>
              Buy-in: {perChip(rate.buy_in_rub, rate.buy_in_chips)} за фишку, rebuy: {perChip(rate.rebuy_rub, rate.rebuy_chips)} за фишку.
              {Number.isInteger(suggestChips) && suggestChips > 0 && <> Например, {rub(rate.rebuy_rub)} → {chips(suggestChips)}.</>}
            </div>
          )}
          {proportional && <div className="hint" style={{ fontSize: 13 }}>Курс игры: {rateLabel(rate)}. Рубли и фишки — только целые.</div>}
        </>
      )}

      {step === 'confirm' && chat && (
        <>
          <h2 style={{ margin: '6px 0 0' }}>Всё верно?</h2>
          <div className="card flush">
            <ListRow title="Стол" right={<b>{chat.title}</b>} />
            <ListRow title="Дилер" right={<b className="dealer-c">🎩 {handle(me.user)} (вы)</b>} />
            <ListRow title="Buy-in" right={<b>{rub(rate.buy_in_rub)} → {chips(rate.buy_in_chips)}</b>} />
            <ListRow title="Rebuy" right={<b>{rub(rate.rebuy_rub)} → {chips(rate.rebuy_chips)}</b>} />
            <ListRow title="Курс" right={<b>{rateLabel(rate)}</b>} />
          </div>
          <div className="card row">
            <div className="grow">
              <div style={{ fontWeight: 600 }}>Сразу сделать мой buy-in</div>
              <div className="muted" style={{ fontSize: 13 }}>Дилер тоже играет</div>
            </div>
            <Switch on={selfBuyIn} onChange={(v) => { haptic.select(); setSelfBuyIn(v); }} />
          </div>
          <div className="hint" style={{ fontSize: 13 }}>Бот опубликует сводку в группе «{chat.title}». Курс нельзя изменить до конца игры.</div>
        </>
      )}
    </Screen>
  );
}

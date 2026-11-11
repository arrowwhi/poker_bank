import { useMemo, useState } from 'react';
import { api, type OpenDebt } from '../api';
import { ChatSelect, NetChart } from '../components/domain';
import { Avatar, Chips, ErrorState, ListRow, Loading, Screen, SectionHeader, Skeleton, StatTile, useToast } from '../components/ui';
import { date, dateShort, handle, monthYear, netClass, rub, signedRub } from '../lib/format';
import { useAsync } from '../lib/hooks';
import { rememberChat } from '../lib/lastChat';
import { bestWorst, cumulativeSeries, plusStreaks, type ChartPeriod } from '../lib/stats';
import { useNav } from '../router';
import { useSession } from '../session';
import { haptic } from '../telegram/telegram';

export function Profile({ userId, chatId: initialChat }: { userId?: number; chatId?: number | null }) {
  const { me } = useSession();
  const nav = useNav();
  const toast = useToast();
  const own = !userId || userId === me.user.id;
  const [chatId, setChatId] = useState<number | null>(initialChat ?? null);
  const [period, setPeriod] = useState<ChartPeriod>('all');
  const [shown, setShown] = useState(5);
  const chats = useAsync(() => api.listChats(), []);
  const st = useAsync(() => (own ? api.getMyStats(chatId) : api.getUserStats(userId!, chatId)), [userId, chatId]);
  const s = st.data;

  const series = useMemo(() => (s ? cumulativeSeries(s.history, period) : []), [s, period]);
  const streak = useMemo(() => (s ? plusStreaks(s.history) : { best: 0, current: 0 }), [s]);
  const bw = useMemo(() => (s ? bestWorst(s.history) : {}), [s]);

  // Отметка уходит одним запросом: повторный тап по той же кнопке, пока она в полёте, игнорируется.
  const [marking, setMarking] = useState<number | null>(null);
  const markDebt = async (d: OpenDebt) => {
    if (marking !== null) return;
    setMarking(d.id);
    try {
      await api.settle(d.game_id);
      haptic.success();
      toast(d.direction === 'out' ? '🟢 Отмечено: оплачено' : '🟢 Отмечено: получено', 'success');
      await st.reload();
    } catch (e) {
      haptic.error();
      toast(e instanceof Error ? e.message : 'Ошибка', 'danger');
    } finally {
      setMarking(null);
    }
  };

  if (st.error && !s) return <ErrorState error={st.error} onRetry={st.reload} />;
  if (!s) return <Loading />;

  const chatTitle = chats.data?.find((c) => c.id === chatId)?.title;
  const avgRebuy = s.games_played ? (s.rebuy_total / s.games_played).toFixed(1).replace('.', ',') : '0';

  return (
    <Screen title={own ? 'Мой профиль' : 'Профиль игрока'} subtitle={chatTitle ?? 'Все чаты'}>
      <div className="card stack">
        <div className="row">
          <Avatar user={s.user} size={64} />
          <div className="grow">
            <div style={{ fontWeight: 700, fontSize: 20 }}>{s.user.display_name}</div>
            <div className="muted">{handle(s.user)} · играет с {monthYear(s.member_since)}</div>
          </div>
        </div>
        {chats.loading ? <Skeleton h={55} /> : (
          <ChatSelect chats={chats.data ?? []} value={chatId} onChange={(id) => { haptic.select(); if (id !== null) rememberChat(id); setChatId(id); }} />
        )}
      </div>

      <div className="grid2">
        <StatTile label="Итог за всё время" value={signedRub(s.total_net_rub)} className={netClass(s.total_net_rub)} />
        <StatTile label="Игр сыграно" value={s.games_played} />
        <StatTile label="Win-rate" value={`${Math.round(s.win_rate * 100)}%`} />
        <StatTile label="Средний за игру" value={signedRub(s.avg_net_rub)} className={netClass(s.avg_net_rub)} />
      </div>

      <div className="card stack" style={{ gap: 10 }}>
        <div style={{ fontWeight: 600 }}>Результат нарастающим итогом</div>
        <Chips<ChartPeriod> value={period} onChange={(v) => { haptic.select(); setPeriod(v); }}
          options={[{ value: 'month', label: 'Месяц' }, { value: 'year', label: 'Год' }, { value: 'all', label: 'Всё время' }]} />
        <NetChart points={series} />
      </div>

      <SectionHeader title="Рекорды" />
      <div className="card flush" style={{ ['--sep-left' as string]: '52px' }}>
        {bw.best && <ListRow left={<span style={{ fontSize: 22 }}>🏆</span>} title="Лучшая игра" sub={`${date(bw.best.ended_at)} · игра #${bw.best.game_number}`}
          right={<span className={`amount ${netClass(bw.best.net_rub)}`}>{signedRub(bw.best.net_rub)}</span>} />}
        {bw.worst && <ListRow left={<span style={{ fontSize: 22 }}>💀</span>} title="Худшая игра" sub={`${date(bw.worst.ended_at)} · игра #${bw.worst.game_number}`}
          right={<span className={`amount ${netClass(bw.worst.net_rub)}`}>{signedRub(bw.worst.net_rub)}</span>} />}
        <ListRow left={<span style={{ fontSize: 22 }}>🔥</span>} title="Серия плюсовых игр" sub={`лучшая — ${streak.best} подряд · сейчас ${streak.current}`}
          right={<span className="amount">{streak.best}</span>} />
        <ListRow left={<span style={{ fontSize: 22 }}>🔁</span>} title="Rebuy" sub={`в среднем ${avgRebuy} за игру · всего ${s.rebuy_total}`} right={<span className="amount">{avgRebuy}</span>} />
      </div>

      {s.leaderboard_place && s.leaderboard_chat_id && (
        <div className="card flush">
          <ListRow left={<span style={{ fontSize: 22 }}>{s.leaderboard_place <= 3 ? ['🥇', '🥈', '🥉'][s.leaderboard_place - 1] : '🎖️'}</span>}
            title="Место в рейтинге чата" sub="по итоговому результату"
            right={<span style={{ color: 'var(--accent-primary)', fontWeight: 600 }}>#{s.leaderboard_place} из {s.leaderboard_total} ›</span>}
            onClick={() => nav.push({ name: 'leaderboard', chatId: s.leaderboard_chat_id! })} />
        </div>
      )}

      {own && !!s.open_debts?.length && (
        <>
          <SectionHeader title="Незакрытые расчёты" right={s.open_debts.length} />
          <div className="card flush">
            {s.open_debts.map((d) => (
              <ListRow key={d.id}
                title={d.direction === 'out' ? `Вы должны ${handle(d.to)} ${rub(d.amount_rub)}` : `${handle(d.from)} должен вам ${rub(d.amount_rub)}`}
                sub={`Игра #${d.game_number} · ${date(d.game_ended_at)}${d.direction === 'in' && d.is_paid ? ' · отметил «оплатил»' : ''}`}
                right={<button className={`btn btn-sm ${d.direction === 'out' ? 'btn-accent' : 'btn-soft'}`} disabled={marking !== null} onClick={() => markDebt(d)}>
                  {d.direction === 'out' ? 'Я оплатил' : 'Я получил'}
                </button>} />
            ))}
          </div>
        </>
      )}

      <SectionHeader title="История игр" right={s.history.length} />
      {s.history.length ? (
        <div className="card flush">
          {s.history.slice(0, shown).map((h) => (
            <ListRow key={h.game_id} onClick={() => nav.push({ name: 'results', id: h.game_id })}
              title={`${dateShort(h.ended_at)} · игра #${h.game_number}${chatId ? '' : ` · ${h.chat_title}`}`}
              sub={`${h.buy_in_count} + ${h.rebuy_count} · ${rub(h.total_in_rub)} → ${rub(h.total_out_rub)}`}
              right={<span className={`amount tabular ${netClass(h.net_rub)}`}>{signedRub(h.net_rub)}</span>} />
          ))}
        </div>
      ) : <div className="hint center">Пока нет завершённых игр</div>}
      {shown < s.history.length && <button className="btn btn-plain" onClick={() => setShown((n) => n + 10)}>Показать ещё</button>}
    </Screen>
  );
}

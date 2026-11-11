import { useMemo, useState } from 'react';
import { api, type LeaderboardEntry, type LeaderboardPeriod } from '../api';
import { ChatSelect, who } from '../components/domain';
import { Avatar, Chips, ErrorState, ListRow, Screen, Segmented, Skeleton } from '../components/ui';
import { handle, netClass, plural, rub, signedChips, signedPct, signedRub } from '../lib/format';
import { useAsync } from '../lib/hooks';
import { rememberChat } from '../lib/lastChat';
import { lastUnit, pctOfBuyIn, rankBy, rememberUnit, type LeaderboardUnit } from '../lib/leaderboard';
import { useNav } from '../router';
import { useSession } from '../session';
import { haptic } from '../telegram/telegram';

const FOOTNOTE: Record<LeaderboardUnit, string | null> = {
  rub: null,
  chips: 'Σ (фишки на выходе − фишки на входе) по всем играм',
  pct: '% = Σ результат / Σ вход (buy-in + rebuy) за период',
};

function ChipIcon() {
  return (
    <svg className="chip-ico" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" role="img" aria-label="фишек">
      <circle cx="8" cy="8" r="6.7" />
      <circle cx="8" cy="8" r="2.6" />
      <path d="M8 1.3v2.4M8 12.3v2.4M1.3 8h2.4M12.3 8h2.4" />
    </svg>
  );
}

/** Результат игрока в выбранном режиме: +990₽ / +1 980 ◎ / +57,5%. */
function Value({ entry: x, unit, className = '' }: { entry: LeaderboardEntry; unit: LeaderboardUnit; className?: string }) {
  if (unit === 'chips') {
    return <span className={`tabular ${netClass(x.total_net_chips)} ${className}`}>{signedChips(x.total_net_chips)}<ChipIcon /></span>;
  }
  if (unit === 'pct') {
    const p = pctOfBuyIn(x);
    // Цвет — по округлённому значению, чтобы «0%» не оказался красным или зелёным.
    return <span className={`tabular ${netClass(Math.round(p * 10))} ${className}`}>{signedPct(p)}</span>;
  }
  return <span className={`tabular ${netClass(x.total_net_rub)} ${className}`}>{signedRub(x.total_net_rub)}</span>;
}

export function LeaderboardScreen({ chatId: initialChat }: { chatId: number }) {
  const { me } = useSession();
  const nav = useNav();
  const [chatId, setChatId] = useState(initialChat);
  const [period, setPeriod] = useState<LeaderboardPeriod>('all');
  const [unit, setUnit] = useState<LeaderboardUnit>(lastUnit);
  const chats = useAsync(() => api.listChats(), []);
  const lb = useAsync(() => api.getLeaderboard(chatId, period), [chatId, period]);
  // Чата, открытого по ссылке, в списке пользователя может и не быть — тогда выбора нет.
  const pickable = !!chats.data?.some((c) => c.id === chatId);
  const pickChat = (id: number | null) => {
    if (id === null) return;
    haptic.select();
    rememberChat(id);
    setChatId(id);
  };
  const open = (id: number) => nav.push({ name: 'profile', userId: id === me.user.id ? undefined : id, chatId });

  const pickUnit = (v: LeaderboardUnit) => {
    haptic.select();
    rememberUnit(v);
    setUnit(v);
  };

  // Сервер отдаёт все три меры сразу — режим переключается без запроса, места считаются здесь.
  const e = useMemo(() => rankBy(lb.data?.entries ?? [], unit), [lb.data, unit]);
  const podium = [e[1], e[0], e[2]];
  const medals = ['🥈', '🥇', '🥉'];

  return (
    <Screen title="Рейтинг" subtitle={lb.data ? `${lb.data.chat_title} · ${e.length} ${plural(e.length, 'игрок', 'игрока', 'игроков')}` : undefined}>
      {chats.loading ? <div className="card"><Skeleton h={55} /></div>
        : pickable && <div className="card"><ChatSelect all={false} chats={chats.data!} value={chatId} onChange={pickChat} /></div>}
      <Chips<LeaderboardPeriod> value={period} onChange={(v) => { haptic.select(); setPeriod(v); }}
        options={[{ value: 'all', label: 'За всё время' }, { value: 'year', label: 'За год' }, { value: 'month', label: 'За месяц' }]} />
      <Segmented<LeaderboardUnit> value={unit} onChange={pickUnit}
        options={[{ value: 'rub', label: '₽ Рубли' }, { value: 'chips', label: 'Фишки' }, { value: 'pct', label: '% от входа' }]} />
      {lb.error ? <ErrorState error={lb.error} onRetry={lb.reload} /> : !lb.data ? <Skeleton h={300} /> : !e.length ? (
        <div className="empty"><div className="big">🏆</div><h3>Нет завершённых игр</h3><p>За выбранный период игр не было.</p></div>
      ) : (
        <>
          {e.length >= 3 && (
            <div className="podium">
              {podium.map((x, i) => (
                <button key={x.user.id} className="col stack" style={{ alignItems: 'center', gap: 4, paddingTop: i === 1 ? 18 : 12 }} onClick={() => open(x.user.id)}>
                  <div className="medal">{medals[i]}</div>
                  <Avatar user={x.user} size={i === 1 ? 52 : 44} />
                  <div style={{ fontWeight: 600, fontSize: 13 }} className="ellipsis">{handle(x.user)}</div>
                  <div style={{ fontWeight: 700 }}><Value entry={x} unit={unit} /></div>
                </button>
              ))}
            </div>
          )}
          <div className="card flush" style={{ ['--sep-left' as string]: '92px' }}>
            {e.map((x) => (
                <ListRow key={x.user.id} className={x.user.id === me.user.id ? 'me-row' : ''} onClick={() => open(x.user.id)}
                  left={<><span className="hint tabular" style={{ width: 20, textAlign: 'right' }}>{x.place}</span><Avatar user={x.user} /></>}
                  title={x.user.id === me.user.id ? `Вы · ${handle(x.user)}` : who(x.user, me.user.id)}
                  sub={`${x.games_played} ${plural(x.games_played, 'игра', 'игры', 'игр')} · ${unit === 'pct' ? `вход ${rub(x.total_in_rub)}` : `win-rate ${Math.round(x.win_rate * 100)}%`}`}
                  right={<Value entry={x} unit={unit} className="amount" />} />
            ))}
          </div>
          {FOOTNOTE[unit] && <div className="footnote">{FOOTNOTE[unit]}</div>}
        </>
      )}
    </Screen>
  );
}

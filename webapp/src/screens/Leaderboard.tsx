import { useState } from 'react';
import { api, type LeaderboardPeriod } from '../api';
import { who } from '../components/domain';
import { Avatar, Chips, ErrorState, ListRow, Screen, Skeleton } from '../components/ui';
import { handle, netClass, plural, signedRub } from '../lib/format';
import { useAsync } from '../lib/hooks';
import { useNav } from '../router';
import { useSession } from '../session';
import { haptic } from '../telegram/telegram';

export function LeaderboardScreen({ chatId }: { chatId: number }) {
  const { me } = useSession();
  const nav = useNav();
  const [period, setPeriod] = useState<LeaderboardPeriod>('all');
  const lb = useAsync(() => api.getLeaderboard(chatId, period), [chatId, period]);
  const open = (id: number) => nav.push({ name: 'profile', userId: id === me.user.id ? undefined : id, chatId });

  const e = lb.data?.entries ?? [];
  const podium = [e[1], e[0], e[2]];
  const medals = ['🥈', '🥇', '🥉'];

  return (
    <Screen title="Рейтинг" subtitle={lb.data ? `${lb.data.chat_title} · ${e.length} ${plural(e.length, 'игрок', 'игрока', 'игроков')}` : undefined}>
      <Chips<LeaderboardPeriod> value={period} onChange={(v) => { haptic.select(); setPeriod(v); }}
        options={[{ value: 'all', label: 'За всё время' }, { value: 'year', label: 'За год' }, { value: 'month', label: 'За месяц' }]} />
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
                  <div className={`tabular ${netClass(x.total_net_rub)}`} style={{ fontWeight: 700 }}>{signedRub(x.total_net_rub)}</div>
                </button>
              ))}
            </div>
          )}
          <div className="card flush" style={{ ['--sep-left' as string]: '92px' }}>
            {e.map((x) => (
                <ListRow key={x.user.id} className={x.user.id === me.user.id ? 'me-row' : ''} onClick={() => open(x.user.id)}
                  left={<><span className="hint tabular" style={{ width: 20, textAlign: 'right' }}>{x.place}</span><Avatar user={x.user} /></>}
                  title={x.user.id === me.user.id ? `Вы · ${handle(x.user)}` : who(x.user, me.user.id)}
                  sub={`${x.games_played} ${plural(x.games_played, 'игра', 'игры', 'игр')} · win-rate ${Math.round(x.win_rate * 100)}%`}
                  right={<span className={`amount tabular ${netClass(x.total_net_rub)}`}>{signedRub(x.total_net_rub)}</span>} />
            ))}
          </div>
        </>
      )}
    </Screen>
  );
}

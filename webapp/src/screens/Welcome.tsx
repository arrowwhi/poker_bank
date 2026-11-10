import { useEffect } from 'react';
import { api } from '../api';
import { useAsync } from '../lib/hooks';
import { Avatar, Badge, ListRow, Screen } from '../components/ui';
import { handle, plural, rub, signedRub, time } from '../lib/format';
import { useNav } from '../router';
import { useSession } from '../session';
import { useMainButton } from '../telegram/buttons';
import { startChatId } from '../telegram/telegram';

export function Welcome() {
  const { me, reloadMe } = useSession();
  const nav = useNav();
  const active = me.active_game;
  useEffect(() => { void reloadMe(); }, [reloadMe]);

  useMainButton(
    active
      ? { text: `Вернуться в игру #${active.number}`, onClick: () => nav.push({ name: 'game', id: active.id }) }
      : { text: 'Выбрать игру', onClick: () => nav.push({ name: 'picker' }) },
  );

  const chats = useAsync(() => api.listChats(), []);
  const mainChat = active?.chat_id ?? startChatId() ?? chats.data?.[0]?.id ?? null;
  const first = me.user.display_name.split(' ')[0];

  return (
    <Screen title="Банкир" subtitle="мини-приложение">
      <div className="hero">
        <div className="suits">♠️♥️♣️♦️</div>
        <h1>Банкир</h1>
        <p>Банк домашней игры в покер:<br />buy-in, rebuy и расчёт переводов</p>
      </div>

      {active ? (
        <button className="card stack" style={{ textAlign: 'left', gap: 10, border: '1.5px solid var(--status-success)' }}
          onClick={() => nav.push({ name: 'game', id: active.id })}>
          <div className="row" style={{ justifyContent: 'space-between' }}>
            <span style={{ fontWeight: 600 }}><span className="pos">●</span> Вы в игре · {active.chat_title}</span>
            {active.my_role === 'dealer' && <Badge tone="dealer">🎩 Дилер</Badge>}
          </div>
          <div className="kv">
            <div><div className="k">Игра</div><div className="v">#{active.number}</div></div>
            <div><div className="k">Банк</div><div className="v">{rub(active.bank_rub)}</div></div>
            <div><div className="k">Игроков</div><div className="v">{active.players_active} из {active.players_total}</div></div>
          </div>
          <div className="dealer-c" style={{ fontSize: 13 }}>🎩 {active.dealer.id === me.user.id ? 'Вы дилер' : `Дилер ${handle(active.dealer)}`} · идёт с {time(active.started_at)}</div>
        </button>
      ) : (
        <div className="card row">
          <Avatar user={me.user} size={56} />
          <div className="grow">
            <div style={{ fontWeight: 700, fontSize: 19 }}>Привет, {first}!</div>
            <div className="muted">
              {handle(me.user)} · {me.games_played} {plural(me.games_played, 'игра', 'игры', 'игр')} · <span className={me.total_net_rub >= 0 ? 'pos' : 'neg'}>{signedRub(me.total_net_rub)}</span>
            </div>
          </div>
        </div>
      )}

      <div className="card flush" style={{ ['--sep-left' as string]: '64px' }}>
        {active ? (
          <ListRow left={<div className="icon-box">🃏</div>} title="Другие игры" sub="Активные игры в ваших группах" chevron onClick={() => nav.push({ name: 'picker' })} />
        ) : (
          <ListRow left={<div className="icon-box">🃏</div>} title="Выбрать игру" sub="Активные игры в ваших группах" chevron onClick={() => nav.push({ name: 'picker' })} />
        )}
        <ListRow left={<div className="icon-box">＋</div>} title="Новая игра" sub="Начать игру за столом в группе" chevron onClick={() => nav.push({ name: 'newgame' })} />
        <ListRow left={<div className="icon-box">📊</div>} title="Мой профиль" sub="Статистика за все игры" chevron onClick={() => nav.push({ name: 'profile' })} />
        {mainChat !== null && (
          <ListRow left={<div className="icon-box">🏆</div>} title="Рейтинг чата" sub="Кто в плюсе за всё время" chevron
            onClick={() => nav.push({ name: 'leaderboard', chatId: mainChat })} />
        )}
      </div>
    </Screen>
  );
}

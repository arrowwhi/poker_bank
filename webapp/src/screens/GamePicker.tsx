import { useMemo, useState } from 'react';
import { api } from '../api';
import { GameCard } from '../components/domain';
import { ErrorState, SearchField, SectionHeader, Screen, Skeleton } from '../components/ui';
import { handle, plural } from '../lib/format';
import { useAsync } from '../lib/hooks';
import { useNav } from '../router';
import { useSession } from '../session';
import { useMainButton } from '../telegram/buttons';
import { tg } from '../telegram/telegram';
import { addToGroupLink } from '../lib/config';

export function GamePicker() {
  const nav = useNav();
  const { me } = useSession();
  const games = useAsync(() => api.listGames({ status: 'active' }), [], 10_000);
  const [q, setQ] = useState('');
  useMainButton({ text: '＋ Новая игра', onClick: () => nav.push({ name: 'newgame' }) });

  const list = useMemo(() => {
    const s = q.trim().toLowerCase();
    const all = games.data ?? [];
    return s ? all.filter((g) => g.chat_title.toLowerCase().includes(s) || handle(g.dealer).toLowerCase().includes(s)) : all;
  }, [games.data, q]);

  const n = games.data?.length ?? 0;
  return (
    <Screen title="Выбор игры" subtitle={games.data ? `${n} ${plural(n, 'активная игра', 'активные игры', 'активных игр')}` : undefined}>
      {games.error ? <ErrorState error={games.error} onRetry={games.reload} /> : !games.data ? (
        <><Skeleton h={40} /><Skeleton h={160} /><Skeleton h={160} /></>
      ) : n === 0 ? (
        <div className="empty">
          <div className="big">🂠</div>
          <h3>Активных игр нет</h3>
          <p>Начните игру сами или попросите дилера запустить <b>/newgame</b> в группе. Когда дилер начнёт игру, она появится здесь.</p>
          {addToGroupLink() && (
            <button className="btn btn-plain" onClick={() => (tg.openTelegramLink ? tg.openTelegramLink(addToGroupLink()!) : window.open(addToGroupLink()!))}>Добавить бота в группу</button>
          )}
        </div>
      ) : (
        <>
          <SearchField value={q} onChange={setQ} placeholder="Поиск по группе или дилеру" />
          <SectionHeader title="Активные игры" right={list.length} />
          {list.map((g) => <GameCard key={g.id} g={g} meId={me.user.id} onClick={() => nav.push({ name: 'game', id: g.id })} />)}
          {!list.length && <div className="hint center">Ничего не найдено</div>}
        </>
      )}
    </Screen>
  );
}

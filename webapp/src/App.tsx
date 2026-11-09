import { useCallback, useEffect, useState } from 'react';
import { api, type Me } from './api';
import { ConfirmProvider, ErrorState, Loading, ToastProvider } from './components/ui';
import { RouterProvider, useNav, type Route } from './router';
import { SessionCtx } from './session';
import { TelegramButtons, useHasMainButton } from './telegram/buttons';
import { inTelegram, startChatId } from './telegram/telegram';
import { Welcome } from './screens/Welcome';
import { GamePicker } from './screens/GamePicker';
import { GameScreen } from './screens/Game';
import { Results } from './screens/Results';
import { Profile } from './screens/Profile';
import { LeaderboardScreen } from './screens/Leaderboard';
import { NewGame } from './screens/NewGame';

/** Начальный стек: deep-link startapp=<chat_id> открывает активную игру этого чата (или создание новой). */
async function initialStack(): Promise<Route[]> {
  const chatId = startChatId();
  const home: Route = { name: 'welcome' };
  if (!chatId) return [home];
  try {
    const games = await api.listGames({ chat_id: chatId, status: 'active' });
    if (games[0]) return [home, { name: 'game', id: games[0].id }];
    return [home, { name: 'newgame', chatId }];
  } catch {
    return [home];
  }
}

function CurrentScreen() {
  const { route } = useNav();
  switch (route.name) {
    case 'welcome': return <Welcome />;
    case 'picker': return <GamePicker />;
    case 'game': return <GameScreen key={route.id} id={route.id} />;
    case 'results': return <Results key={route.id} id={route.id} />;
    case 'profile': return <Profile key={`${route.userId}`} userId={route.userId} chatId={route.chatId} />;
    case 'leaderboard': return <LeaderboardScreen chatId={route.chatId} />;
    case 'newgame': return <NewGame chatId={route.chatId} />;
  }
}

function Shell() {
  const hasMain = useHasMainButton();
  return (
    <div className={`app ${hasMain && !inTelegram ? 'has-mainbar' : ''}`}>
      <CurrentScreen />
      <TelegramButtons />
    </div>
  );
}

export function App() {
  const [me, setMe] = useState<Me>();
  const [stack, setStack] = useState<Route[]>();
  const [error, setError] = useState<unknown>();

  const reloadMe = useCallback(async () => { setMe(await api.getMe()); }, []);
  const boot = useCallback(async () => {
    setError(undefined);
    try {
      const [m, s] = await Promise.all([api.getMe(), initialStack()]);
      setMe(m);
      // Одна активная игра на игрока: без deep-link сразу открываем её, минуя приветствие.
      setStack(s.length === 1 && m.active_game ? [...s, { name: 'game', id: m.active_game.id }] : s);
    } catch (e) {
      setError(e);
    }
  }, []);
  useEffect(() => { void boot(); }, [boot]);

  if (error) return <ErrorState error={error} onRetry={boot} />;
  if (!me || !stack) return <Loading />;
  return (
    <SessionCtx.Provider value={{ me, reloadMe }}>
      <ToastProvider>
        <ConfirmProvider>
          <RouterProvider initial={stack}>
            <Shell />
          </RouterProvider>
        </ConfirmProvider>
      </ToastProvider>
    </SessionCtx.Provider>
  );
}

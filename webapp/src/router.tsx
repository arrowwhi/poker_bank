// Минимальный стековый роутер: push/pop/replace. «Назад» = pop (нативная BackButton).
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import { useBackButton } from './telegram/buttons';

export type Route =
  | { name: 'welcome' }
  | { name: 'picker' }
  | { name: 'game'; id: number }
  | { name: 'results'; id: number }
  | { name: 'profile'; userId?: number; chatId?: number | null }
  | { name: 'leaderboard'; chatId: number }
  | { name: 'newgame'; chatId?: number };

interface Nav {
  route: Route;
  depth: number;
  push(r: Route): void;
  replace(r: Route): void;
  pop(): void;
  reset(stack: Route[]): void;
}

const Ctx = createContext<Nav | null>(null);
export function useNav(): Nav {
  const n = useContext(Ctx);
  if (!n) throw new Error('no router');
  return n;
}

export function RouterProvider({ initial, children }: { initial: Route[]; children: ReactNode }) {
  const [stack, setStack] = useState<Route[]>(initial);
  const push = useCallback((r: Route) => { setStack((s) => [...s, r]); scrollTo(0, 0); }, []);
  const replace = useCallback((r: Route) => { setStack((s) => [...s.slice(0, -1), r]); scrollTo(0, 0); }, []);
  const pop = useCallback(() => setStack((s) => (s.length > 1 ? s.slice(0, -1) : s)), []);
  const reset = useCallback((s: Route[]) => setStack(s), []);
  const nav = useMemo(() => ({ route: stack[stack.length - 1], depth: stack.length, push, replace, pop, reset }), [stack, push, replace, pop, reset]);
  useBackButton(stack.length > 1 ? pop : null, 0);
  return <Ctx.Provider value={nav}>{children}</Ctx.Provider>;
}

import { createContext, useContext } from 'react';
import type { Me } from './api';

export interface Session { me: Me; reloadMe: () => Promise<void> }
export const SessionCtx = createContext<Session | null>(null);
export function useSession(): Session {
  const s = useContext(SessionCtx);
  if (!s) throw new Error('no session');
  return s;
}

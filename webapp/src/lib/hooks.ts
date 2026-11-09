import { useCallback, useEffect, useRef, useState } from 'react';

export interface Async<T> {
  data: T | undefined;
  error: unknown;
  loading: boolean;
  reload: () => Promise<void>;
  set: (v: T) => void;
}

/** Загрузка данных + опциональный опрос (poll, мс) — пока вкладка видима. */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[], poll?: number): Async<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const fnRef = useRef(fn);
  fnRef.current = fn;
  const gen = useRef(0);

  const reload = useCallback(async () => {
    const g = ++gen.current;
    try {
      const v = await fnRef.current();
      if (g === gen.current) { setData(v); setError(undefined); }
    } catch (e) {
      if (g === gen.current) setError(e);
    } finally {
      if (g === gen.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    setLoading(true);
    setData(undefined);
    void reload();
    if (!poll) return;
    const t = setInterval(() => { if (document.visibilityState === 'visible') void reload(); }, poll);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  const set = useCallback((v: T) => { ++gen.current; setData(v); }, []);
  return { data, error, loading, reload, set };
}

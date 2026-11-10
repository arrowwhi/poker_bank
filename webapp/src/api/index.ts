import type { BankirApi } from './client';
import { HttpApi } from './http';
import { MockApi } from './mock';
import { tg } from '../telegram/telegram';

export * from './types';
export { ApiError } from './client';
export type { BankirApi } from './client';

/** База API. Пусто — тот же origin: в проде Mini App отдаёт Go-сервер вместе с /api. */
const baseUrl = ((import.meta.env.VITE_API_URL as string | undefined) ?? '').trim();
const forceMock = import.meta.env.VITE_USE_MOCK === '1';

/** Мок — только по явному флагу или в dev без адреса бэкенда. Прод-сборка всегда ходит в /api. */
export const isMockApi = forceMock || (import.meta.env.DEV && !baseUrl);

/** Единственный экземпляр API. */
export const api: BankirApi = isMockApi ? new MockApi() : new HttpApi(baseUrl, () => tg.initData);

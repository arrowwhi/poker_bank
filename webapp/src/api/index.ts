import type { BankirApi } from './client';
import { HttpApi } from './http';
import { MockApi } from './mock';
import { tg } from '../telegram/telegram';

export * from './types';
export { ApiError } from './client';
export type { BankirApi } from './client';

const baseUrl = (import.meta.env.VITE_API_URL as string | undefined)?.trim();
const forceMock = import.meta.env.VITE_USE_MOCK === '1';

export const isMockApi = !baseUrl || forceMock;

/** Единственный экземпляр API: мок, если VITE_API_URL не задан. */
export const api: BankirApi = isMockApi ? new MockApi() : new HttpApi(baseUrl!, () => tg.initData);

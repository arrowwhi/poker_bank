const env = import.meta.env;

/** Username бота без @ (для ссылок t.me). */
export const BOT_USERNAME = (env.VITE_BOT_USERNAME as string | undefined)?.trim() || '';
/** Short name Mini App из BotFather (/newapp) — для deep-link t.me/<bot>/<app>?startapp=<chat_id>. */
export const APP_SHORT_NAME = (env.VITE_APP_SHORT_NAME as string | undefined)?.trim() || '';

export function gameLink(chatId: number): string | null {
  if (!BOT_USERNAME || !APP_SHORT_NAME) return null;
  return `https://t.me/${BOT_USERNAME}/${APP_SHORT_NAME}?startapp=${chatId}`;
}

export function addToGroupLink(): string | null {
  return BOT_USERNAME ? `https://t.me/${BOT_USERNAME}?startgroup=bankir` : null;
}

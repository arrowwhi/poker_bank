// Последний чат, который пользователь сам выбрал в приложении (рейтинг, профиль).
// Ссылка t.me/<bot>/<app> без startapp не сообщает, из какого чата её открыли,
// поэтому без этого выбора приложению пришлось бы угадывать чат.

const KEY = 'bankir.chat';

export function lastChatId(): number | null {
  try {
    const n = Number(localStorage.getItem(KEY));
    return Number.isFinite(n) && n !== 0 ? n : null;
  } catch {
    return null;
  }
}

export function rememberChat(chatId: number): void {
  try { localStorage.setItem(KEY, String(chatId)); } catch { /* приватный режим и т. п. */ }
}

/**
 * Чат по умолчанию из списка пользователя: последний выбранный, если он ещё в списке,
 * иначе первый (API отдаёт чаты от самых популярных).
 */
export function defaultChat<C extends { id: number }>(chats: C[] | undefined): C | null {
  if (!chats?.length) return null;
  const last = lastChatId();
  return chats.find((c) => c.id === last) ?? chats[0];
}

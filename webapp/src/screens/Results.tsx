import { useState } from 'react';
import { api, ApiError } from '../api';
import { SettlementRow, who } from '../components/domain';
import { Avatar, Badge, ErrorState, ListRow, Loading, Screen, SectionHeader, useToast } from '../components/ui';
import { date, handle, netClass, plural, rub, signedRub, time } from '../lib/format';
import { useAsync } from '../lib/hooks';
import { useNav } from '../router';
import { useSession } from '../session';
import { useMainButton } from '../telegram/buttons';
import { haptic } from '../telegram/telegram';

export function Results({ id }: { id: number }) {
  const { me } = useSession();
  const meId = me.user.id;
  const nav = useNav();
  const toast = useToast();
  const g = useAsync(() => api.getGame(id), [id], 8000);
  const [busy, setBusy] = useState(false);
  const game = g.data;

  const settlements = game?.settlements ?? [];
  const closed = settlements.filter((s) => s.is_paid && s.is_received).length;
  const allClosed = settlements.length > 0 && closed === settlements.length;
  const mine = settlements.filter((s) => s.from.id === meId || s.to.id === meId);
  const myDone = mine.every((s) => (s.from.id === meId ? s.is_paid : s.is_received));

  const settle = async () => {
    setBusy(true);
    try {
      g.set(await api.settle(id));
      haptic.success();
      toast('🟢 Отмечено: вы оплатили / получили', 'success');
    } catch (e) {
      haptic.error();
      toast(e instanceof ApiError ? e.message : 'Ошибка', e instanceof ApiError && e.code === 'already_marked' ? 'info' : 'danger');
    } finally {
      setBusy(false);
    }
  };

  useMainButton(!game || allClosed || !settlements.length ? null :
    !mine.length ? { text: 'Вас нет в переводах', disabled: true, onClick: () => {} } :
    myDone ? { text: '🟢 Вы отметились', disabled: true, onClick: () => {} } :
    { text: '✅ Я оплатил / получил', progress: busy, onClick: settle });

  if (g.error && !game) return <ErrorState error={g.error} onRetry={g.reload} />;
  if (!game) return <Loading />;

  const results = [...(game.results ?? [])].sort((a, b) => b.net_rub - a.net_rub);
  const top = results[0]?.net_rub > 0 ? results[0].user.id : null;
  const bank = results.reduce((s, r) => s + r.total_in_rub, 0);

  return (
    <Screen title={`Игра #${game.number}`} subtitle={game.chat_title}>
      <div className="felt-card">
        <h2>🏁 Игра #{game.number} завершена</h2>
        <p>
          {date(game.started_at)}, {time(game.started_at)}–{game.ended_at ? time(game.ended_at) : ''} · {results.length} {plural(results.length, 'игрок', 'игрока', 'игроков')} · банк {rub(bank)} · дилер {handle(game.dealer)}
        </p>
      </div>

      {game.bank_delta_rub !== 0 && (
        <div className="banner banner-warning">
          <b>⚠️ Завершено с расхождением {signedRub(game.bank_delta_rub)}</b>
          {game.bank_delta_rub > 0 ? 'Долги проигравших пропорционально уменьшены.' : 'Выигрыши победителей пропорционально уменьшены.'}
        </div>
      )}

      {allClosed && <div className="banner banner-success"><b>✅ Все расчёты закрыты</b>Все переводы оплачены и получены.</div>}

      <SectionHeader title="Переводы" right={settlements.length ? `закрыто ${closed} из ${settlements.length}` : ''} />
      {settlements.length ? (
        <div className="card flush">
          {settlements.map((s) => <SettlementRow key={s.id} s={s} meId={meId} />)}
        </div>
      ) : <div className="hint center">Переводы не нужны — все при своих</div>}
      {!allClosed && settlements.length > 0 && (
        <div className="hint" style={{ fontSize: 13, padding: '0 4px' }}>🟢 у отправителя — «оплатил», у получателя — «получил». Нажмите кнопку, когда перевели или получили деньги.</div>
      )}

      <SectionHeader title="Результаты" right={`${results.length} ${plural(results.length, 'игрок', 'игрока', 'игроков')}`} />
      <div className="card flush" style={{ ['--sep-left' as string]: '68px' }}>
        {results.map((r) => (
          <ListRow key={r.user.id} left={<Avatar user={r.user} />} title={who(r.user, meId)}
            onClick={() => nav.push({ name: 'profile', userId: r.user.id === meId ? undefined : r.user.id, chatId: game.chat_id })}
            sub={`внёс ${rub(r.total_in_rub)} · вынес ${rub(r.total_out_rub)}`}
            right={<>
              <span className={`amount tabular ${netClass(r.net_rub)}`}>{signedRub(r.net_rub)}</span>
              {r.user.id === top ? <Badge tone="dealer">🏆 Топ</Badge> : r.user.id === meId ? <Badge tone="accent">Вы</Badge> : null}
            </>} />
        ))}
      </div>
    </Screen>
  );
}

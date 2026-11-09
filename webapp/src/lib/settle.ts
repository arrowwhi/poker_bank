// Алгоритм расчёта переводов (ТЗ §9). На проде считает бэкенд — здесь для мока.
export interface Transfer { from: number; to: number; amount: number }

export function computeSettlements(nets: Map<number, number>): Transfer[] {
  const out: Transfer[] = [];
  let debtors = [...nets].filter(([, v]) => v < 0).map(([id, v]) => ({ id, v: -v }));
  let creditors = [...nets].filter(([, v]) => v > 0).map(([id, v]) => ({ id, v }));

  // Шаг 1 — точные совпадения
  for (const d of debtors) {
    const c = creditors.find((c) => c.v === d.v && c.v > 0);
    if (c && d.v > 0) {
      out.push({ from: d.id, to: c.id, amount: d.v });
      c.v = 0;
      d.v = 0;
    }
  }
  // Шаг 2 — жадный
  const sort = () => {
    debtors = debtors.filter((d) => d.v > 0).sort((a, b) => b.v - a.v);
    creditors = creditors.filter((c) => c.v > 0).sort((a, b) => b.v - a.v);
  };
  sort();
  while (debtors.length && creditors.length) {
    const d = debtors[0];
    const c = creditors[0];
    const amount = Math.min(d.v, c.v);
    out.push({ from: d.id, to: c.id, amount });
    d.v -= amount;
    c.v -= amount;
    sort();
  }
  return out;
}

/**
 * /endgame_force: бо́льшая сторона (долги или выигрыши) пропорционально уменьшается до меньшей.
 * Остаток от округления добавляется к крупнейшему по модулю.
 */
export function adjustForDelta(nets: Map<number, number>): Map<number, number> {
  const res = new Map(nets);
  const X = [...nets.values()].filter((v) => v < 0).reduce((s, v) => s - v, 0);
  const Y = [...nets.values()].filter((v) => v > 0).reduce((s, v) => s + v, 0);
  if (X === Y || X === 0 || Y === 0) return res;
  const shrinkDebtors = X > Y;
  const [big, target] = shrinkDebtors ? [X, Y] : [Y, X];
  const side = [...nets].filter(([, v]) => (shrinkDebtors ? v < 0 : v > 0));
  let assigned = 0;
  for (const [id, v] of side) {
    const nv = Math.floor((Math.abs(v) * target) / big);
    assigned += nv;
    res.set(id, shrinkDebtors ? -nv : nv);
  }
  const top = side.sort((a, b) => Math.abs(b[1]) - Math.abs(a[1]))[0];
  if (top) {
    const cur = Math.abs(res.get(top[0])!) + (target - assigned);
    res.set(top[0], shrinkDebtors ? -cur : cur);
  }
  return res;
}

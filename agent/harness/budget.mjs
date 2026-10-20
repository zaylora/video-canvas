// 预算闸门：开跑前把所有用例的 budget × repeat 求和，超过 --max-credits 就拒绝，防止一次误操作烧掉积分。
// 后端会按每个 run 的 budget_credits 强制执行，这里是第一道（整批）防线。

export const DEFAULT_MAX_CREDITS = 200;

/** cases: [{id, budget}]。返回 {rows, total, maxCredits, repeat, ok}；budget 缺失或不合法直接抛错。 */
export function planBudget(cases, { repeat = 1, maxCredits = DEFAULT_MAX_CREDITS } = {}) {
  const rows = cases.map((c) => {
    if (typeof c.budget !== "number" || !Number.isFinite(c.budget) || c.budget < 0) {
      throw new Error(`用例 ${c.id} 的 budget 缺失或不合法（需要 ≥ 0 的数字；不花钱的用例写 0）`);
    }
    return { id: c.id, budget: c.budget, repeat, subtotal: c.budget * repeat };
  });
  const total = rows.reduce((sum, r) => sum + r.subtotal, 0);
  return { rows, total, maxCredits, repeat, ok: total <= maxCredits };
}

/** 放行时一行预算检查；拒绝时列出每个用例的明细、合计和上限。 */
export function formatBudgetPlan(plan) {
  if (plan.ok) return `预算检查 ${plan.total} / ${plan.maxCredits} 积分`;
  const width = Math.max(...plan.rows.map((r) => r.id.length));
  const lines = plan.rows.map((r) => `  ${r.id.padEnd(width)}  ${r.budget} × ${r.repeat} = ${r.subtotal} 积分`);
  return [`预算闸门：合计 ${plan.total} 积分，超过上限 ${plan.maxCredits}，不开跑`, ...lines, "把 --max-credits 调大，或减少 --repeat / 用例。"].join("\n");
}

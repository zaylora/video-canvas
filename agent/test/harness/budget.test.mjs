import test from "node:test";
import assert from "node:assert/strict";
import { planBudget, formatBudgetPlan, DEFAULT_MAX_CREDITS } from "../../harness/budget.mjs";

const cases = [
  { id: "a", budget: 0 },
  { id: "b", budget: 40 },
  { id: "c", budget: 25 },
];

test("默认上限 200 积分", () => {
  assert.equal(DEFAULT_MAX_CREDITS, 200);
});

test("所有用例 budget × repeat 求和，不超过上限就放行", () => {
  const p = planBudget(cases, { repeat: 3, maxCredits: 200 });
  assert.equal(p.total, (0 + 40 + 25) * 3);
  assert.equal(p.ok, true);
  assert.deepEqual(p.rows.map((r) => [r.id, r.budget, r.repeat, r.subtotal]), [["a", 0, 3, 0], ["b", 40, 3, 120], ["c", 25, 3, 75]]);
});

test("刚好等于上限放行，超过一点就拒绝", () => {
  assert.equal(planBudget(cases, { repeat: 1, maxCredits: 65 }).ok, true);
  assert.equal(planBudget(cases, { repeat: 1, maxCredits: 64 }).ok, false);
});

test("上限 0 时只放行全是 0 预算的用例", () => {
  assert.equal(planBudget([{ id: "a", budget: 0 }], { repeat: 5, maxCredits: 0 }).ok, true);
  assert.equal(planBudget(cases, { repeat: 1, maxCredits: 0 }).ok, false);
});

test("拒绝时的文字列出每个用例的明细和总数", () => {
  const text = formatBudgetPlan(planBudget(cases, { repeat: 2, maxCredits: 100 }));
  assert.match(text, /预算闸门/);
  assert.match(text, /b\s.*40 × 2 = 80/);
  assert.match(text, /c\s.*25 × 2 = 50/);
  assert.match(text, /130/);
  assert.match(text, /100/);
  assert.match(text, /不开跑/);
});

test("放行时的文字是一行预算检查", () => {
  const text = formatBudgetPlan(planBudget(cases, { repeat: 1, maxCredits: 200 }));
  assert.match(text, /预算检查 65 \/ 200 积分/);
});

test("每个用例必须写 budget：缺失、负数、非数字都报错", () => {
  assert.throws(() => planBudget([{ id: "x" }], { repeat: 1, maxCredits: 10 }), /x.*budget/);
  assert.throws(() => planBudget([{ id: "x", budget: -1 }], { repeat: 1, maxCredits: 10 }), /budget/);
  assert.throws(() => planBudget([{ id: "x", budget: "5" }], { repeat: 1, maxCredits: 10 }), /budget/);
});

import assert from "node:assert/strict";
import { test } from "node:test";
import { statusEvent, isActive } from "../src/engine/status.ts";
import { Err } from "../src/engine/errors.ts";

test("run.status 事件：需要提示用户的状态带标题和提示，别的状态只有 status；附加字段原样带上", () => {
  assert.deepEqual(statusEvent("running"), { status: "running" });
  assert.deepEqual(statusEvent("budget_exhausted"), { status: "budget_exhausted", title: "本轮额度用完了", hint: "追加额度后可以继续" });
  assert.deepEqual(statusEvent("failed", { error: "上游调用失败" }), { status: "failed", title: "运行失败", hint: "可以换个说法重试", error: "上游调用失败" });
  assert.deepEqual(statusEvent("succeeded", { error: "" }), { status: "succeeded", error: "" });
});

test("活跃状态占用画布：排队、运行中、等审批、等回答", () => {
  for (const s of ["queued", "running", "waiting_approval", "waiting_input"]) assert.ok(isActive(s), s);
  for (const s of ["succeeded", "failed", "canceled", "budget_exhausted", "step_limit", "timeout", "interrupted", "expired"]) assert.ok(!isActive(s), s);
});

test("业务错误的码和状态与 Go 的 errcode 一致", () => {
  assert.deepEqual([Err.runActive().code, Err.runActive().status], [60001, 409]);
  assert.deepEqual([Err.overBudget().code, Err.overBudget().status], [60008, 402]);
  assert.deepEqual([Err.sessionMissing().code, Err.sessionMissing().status], [60011, 404]);
  assert.equal(Err.invalidParams("消息不能为空").message, "消息不能为空");
});

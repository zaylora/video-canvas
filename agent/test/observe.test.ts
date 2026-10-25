import assert from "node:assert/strict";
import { test } from "node:test";
import { AgentApiService } from "../src/api/service.ts";
import { AgentError } from "../src/engine/errors.ts";
import { codec, withEngine, type Env } from "./engine-helpers.ts";
import { withHttp } from "./http-helpers.ts";
import type { FakeGoOptions } from "./fake-go.ts";

async function withApi(go: FakeGoOptions, fn: (api: AgentApiService, e: Env) => Promise<void>) {
  await withEngine(go, async (e) => fn(new AgentApiService({ store: e.store, engine: e.engine, platform: e.platform, codec, now: () => Date.now() }), e));
}

const askTurn = { tools: [{ id: "c1", name: "ask_user", args: { question: "几张？" } }] };

test("观测事实：运行、本轮事件、审批、评价、会话是否被删、更早两轮的上下文——ID 是十进制字符串，时间是 ISO", () =>
  withApi({ turns: [{ text: "第一轮的回答" }, { text: "第二轮的回答" }, askTurn] }, async (api, e) => {
    const r1 = await e.start({ message: "第一轮" });
    await e.engine.idle();
    const r2 = await e.start({ message: "第二轮" });
    await e.engine.idle();
    const r3 = await e.start({ message: "第三轮" });
    await e.engine.idle(); // 停在 waiting_input：有一张提问审批
    const f = await api.observeFacts(r3.id);

    assert.equal(f.run.id, r3.id);
    assert.deepEqual([f.run.user_id, f.run.canvas_id, f.run.session_id, f.run.status, f.run.mode], ["1", "7", e.sessionId, "waiting_input", "all"]);
    assert.match(f.run.created_at, /^\d{4}-\d\d-\d\dT/);
    assert.equal(f.run.ended_at, null);
    assert.equal(f.run.harness_key, e.store.getRunById(r3.id).harnessKey);
    assert.equal(f.session_deleted, false);

    assert.deepEqual(f.events.map((x) => x.type).filter((t) => t !== "run.usage"), ["message.user", "run.status", "run.status", "message.done", "tool.start", "approval.created", "run.status", "tool.end"]);
    assert.ok(f.events.every((x) => x.run_id === r3.id), "只有本轮的事件");
    assert.ok(f.events.every((x, i) => i === 0 || x.seq > f.events[i - 1].seq), "按序号升序");
    assert.deepEqual((f.events[0].data as { text: string }).text, "第三轮");

    assert.equal(f.approvals.length, 1);
    assert.deepEqual([f.approvals[0].kind, f.approvals[0].status, f.approvals[0].tool_call_id], ["ask", "pending", "c1"]);
    assert.match(f.approvals[0].id, /^\d+$/);
    assert.equal(f.feedback, null);

    // 上下文：更早的两轮（第一轮和第二轮），只有用户消息和最终回复
    assert.deepEqual(f.context.map((x) => [x.run_id, x.type]), [[r1.id, "message.user"], [r1.id, "message.done"], [r2.id, "message.user"], [r2.id, "message.done"]]);
    const f2 = await api.observeFacts(r2.id);
    assert.deepEqual(f2.context.map((x) => x.run_id), [r1.id, r1.id], "第二轮只有第一轮在前面");
    assert.ok(f2.run.ended_at && /^\d{4}-/.test(f2.run.ended_at), "结束的运行有结束时间");
  }));

test("观测事实：评价、会话删除如实反映；运行不存在是 60012", () =>
  withApi({ turns: [{ text: "好" }] }, async (api, e) => {
    const r = await e.start();
    await e.engine.idle();
    await api.setFeedback("1", r.id, { rating: -1, reasons: ["slow"], comment: "不行" });
    const f = await api.observeFacts(r.id);
    assert.deepEqual(f.feedback && { rating: f.feedback.rating, reasons: f.feedback.reasons, comment: f.feedback.comment }, { rating: -1, reasons: ["slow"], comment: "不行" });
    await api.deleteSession("1", e.sessionId);
    assert.equal((await api.observeFacts(r.id)).session_deleted, true);
    await assert.rejects(api.observeFacts("999"), (err: unknown) => err instanceof AgentError && err.code === 60012 && err.status === 404);
  }));

test("观测列表：按更新时间返回变过的运行；评价、取消评价、撤销都会让运行重新出现（Go 据此重新汇总）", () =>
  withApi({ turns: [{ text: "好" }], undo: () => ({ reverted: 1, skipped: [], revision: 5 }) }, async (api, e) => {
    const r = await e.start();
    await e.engine.idle();
    const seen = (await api.observeRuns(0, 100)).find((x) => x.id === r.id)!;
    assert.deepEqual([seen.status, typeof seen.updated_at], ["succeeded", "number"]);

    const bumped = async (what: string, act: () => Promise<unknown>) => {
      const before = (await api.observeRuns(0, 100)).find((x) => x.id === r.id)!.updated_at;
      e.advanceClock(5);
      await new Promise((res) => setTimeout(res, 3));
      await act();
      const after = (await api.observeRuns(before, 100)).find((x) => x.id === r.id);
      assert.ok(after && after.updated_at > before, `${what}之后应重新出现在列表里`);
    };
    await bumped("评价", () => api.setFeedback("1", r.id, { rating: 1, reasons: [], comment: "" }));
    await bumped("取消评价", () => api.clearFeedback("1", r.id));
    await bumped("撤销", () => api.undo("1", r.id));
    await assert.rejects(api.observeRuns(0, 0), (err: unknown) => err instanceof AgentError && err.code === 10001);
  }));

test("观测接口只认服务令牌（不要用户头）；路径和查询参数不对是 10001；不暴露在用户的接口上", () =>
  withHttp({ turns: [{ text: "好" }] }, async (h, e) => {
    const r = await e.start();
    await e.engine.idle();
    const noUser = { "x-agent-user": undefined };
    const list = await h.call("GET", "/observe/runs?updated_after=0&limit=10", undefined, noUser);
    assert.equal(list.status, 200);
    assert.equal(list.body.code, 0);
    assert.ok(list.body.data.some((x: { id: string }) => x.id === r.id));
    const facts = await h.call("GET", `/observe/runs/${r.id}`, undefined, noUser);
    assert.equal(facts.status, 200);
    assert.equal(facts.body.data.run.id, r.id);

    assert.equal((await h.call("GET", `/observe/runs/${r.id}`, undefined, { "x-agent-service-token": "x".repeat(40), ...noUser })).status, 401, "令牌不对");
    assert.equal((await h.call("GET", `/observe/runs/${r.id}`, undefined, { "x-agent-service-token": undefined, ...noUser })).status, 401, "没有令牌");
    assert.equal((await h.call("GET", "/observe/runs/abc", undefined, noUser)).body.code, 10001);
    assert.equal((await h.call("GET", "/observe/runs/0", undefined, noUser)).body.code, 10001);
    assert.equal((await h.call("GET", "/observe/runs/999999999999", undefined, noUser)).body.code, 60012);
    assert.equal((await h.call("GET", "/observe/runs?updated_after=x", undefined, noUser)).body.code, 10001);
    assert.equal((await h.call("GET", "/observe/runs?limit=9999", undefined, noUser)).body.code, 10001);
  }));

import assert from "node:assert/strict";
import { test } from "node:test";
import { codec } from "./engine-helpers.ts";
import { withHttp } from "./http-helpers.ts";
import type { ToolDecl } from "../src/platform-client.ts";

const enc = (id: string) => codec.encode(BigInt(id));
const canvasCheck = (body: { canvas_id: string; mode?: string }) => {
  if (body.mode === "drama" && body.canvas_id !== "8") return { error: { status: 400, code: 10001, msg: "只有短剧画布可以使用短剧助手" } };
};

test("短剧模式：普通画布不能创建，会话不落库；短剧画布可创建并运行", () =>
  withHttp({ turns: [{ text: "先读短剧" }], checkCanvas: canvasCheck }, async (h, e) => {
    const denied = await h.call("POST", `/canvas/${enc("7")}/agent/sessions`, { mode: "drama" });
    assert.deepEqual([denied.status, denied.body.code], [400, 10001]);
    assert.equal(e.store.countSessions("1", "7"), 1);
    const created = await h.call("POST", `/canvas/${enc("8")}/agent/sessions`, { mode: "drama" });
    assert.equal(created.status, 200);
    assert.equal(created.body.data.mode, "drama");
    const started = await h.call("POST", `/agent/sessions/${created.body.data.id}/runs`, { message: "读短剧" });
    assert.equal(started.status, 202);
    assert.equal(started.body.data.mode, "drama");
    await e.engine.idle();
    const checked = e.go.seen.filter((r) => r.path.endsWith("/canvases/check") && r.body.canvas_id === "8");
    assert.ok(checked.every((r) => r.body.mode === "drama"));
  }));

test("运行切换到 drama 仍检查画布，拒绝时不创建运行也不更改会话模式", () =>
  withHttp({ turns: [], checkCanvas: canvasCheck }, async (h, e) => {
    const started = await h.call("POST", `/agent/sessions/${enc(e.sessionId)}/runs`, { message: "改短剧", mode: "drama" });
    assert.deepEqual([started.status, started.body.code], [400, 10001]);
    assert.equal(e.store.activeRun("7"), undefined);
    assert.equal(e.store.getSession("1", e.sessionId).mode, "all");
  }));

for (const name of ["drama_update_outline", "drama_update_episode", "drama_update_segment", "drama_run_job"]) {
  for (const decision of ["approve", "reject"] as const) {
    test(`${name}：${decision} 沿现有审批暂停和恢复，批准前不执行写入`, () => {
      const payload = { before: "修改前", after: "修改后", quote_credits: 7 };
      const tool: ToolDecl = { name, label: "修改短剧", verb: "修改", executor: "server", scriptable: false, description: "短剧", parameters: { type: "object", properties: {} } };
      return withHttp({
        turns: [{ tools: [{ id: "drama_call", name, args: {} }] }, { text: "完成" }],
        setup: { tools: [tool] },
        propose: { [name]: { payload, quote_credits: 7, content: "等待审批", summary: "修改短剧", terminate: true } },
      }, async (h, e) => {
        const run = await e.start({ mode: "drama" });
        await e.engine.idle();
        assert.equal(e.run(run.id).status, "waiting_approval");
        const [approval] = e.store.listApprovals(run.id);
        assert.equal(approval.kind, name);
        assert.deepEqual(approval.payload, payload);
        assert.equal(approval.quoteCredits, 7);
        assert.equal(e.go.seen.some((r) => r.path.includes("/approvals/execute")), false);
        assert.equal(e.go.seen.some((r) => r.path.includes(`/tools/${name}`)), false);
        const decided = await h.call("POST", `/agent/approvals/${enc(approval.id)}/decision`, { decision });
        assert.equal(decided.status, 200);
        assert.equal(decided.body.data.status, decision === "approve" ? "executed" : "rejected");
        await e.engine.idle();
        assert.equal(e.run(run.id).status, "succeeded");
        assert.equal(e.store.listApprovals(run.id).length, 1);
      });
    });
  }
}

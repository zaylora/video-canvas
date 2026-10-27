import assert from "node:assert/strict";
import { test } from "node:test";
import { AgentError } from "../src/engine/errors.ts";
import { decodeRunContext } from "../src/platform-client.ts";
import { codec, withEngine, type Env } from "./engine-helpers.ts";

const GEN = { generate: { payload: { items: [{ node_id: "n1", price: 5 }], quote_credits: 5 }, quote_credits: 5, summary: "生成 1 个，预估 5 积分", content: "已提交给用户批准，等待决定。" } };

/** 模型申请生成 → 等审批 */
async function waitingApproval(e: Env, over: Partial<Parameters<Env["start"]>[0]> = {}) {
  const run = await e.start(over);
  await e.engine.idle();
  const [approval] = e.store.listApprovals(run.id);
  return { run, approval };
}

const code = (c: number) => (err: unknown) => err instanceof AgentError && err.code === c;

test("批准生成：解析 → 登记已花积分 → 执行 → 结果交给模型 → 运行接着往下走直到成功", () =>
  withEngine({
    turns: [{ tools: [{ id: "call_g", name: "generate_media", args: { items: [{ nodeId: "n1" }] } }] }, { text: "已开始生成" }],
    propose: GEN,
  }, async (e) => {
    const { run, approval } = await waitingApproval(e, { budgetCredits: 20 });
    const view = await e.engine.decide("1", approval.id, { decision: "approve", add_budget: 10 });
    await e.engine.idle();

    // 平台调用：解析带审批内容和决定；执行带审批 id（十进制字符串）、调用 id 和解析出的结果
    const resolve = e.go.seen.find((s) => s.path.endsWith("/approvals/resolve"))!;
    assert.deepEqual(resolve.body, { kind: "generate", payload: { items: [{ node_id: "n1", price: 5 }], quote_credits: 5 }, decision: { decision: "approve", add_budget: 10 } });
    const rc = decodeRunContext(String(resolve.headers["x-agent-context"]));
    assert.deepEqual([rc.run_id, rc.budget_credits, rc.spent_extra ?? 0], [run.id, 20, 0], "解析时本轮预算和已花的积分");
    const exec = e.go.seen.find((s) => s.path.endsWith("/approvals/execute"))!;
    assert.deepEqual([exec.body.kind, exec.body.approval_id, exec.body.tool_call_id, exec.body.outcome.status], ["generate", approval.id, "call_g", "approved"]);
    assert.equal(decodeRunContext(String(exec.headers["x-agent-context"])).spent_extra, 5, "执行时已批准的预估已经算进已花积分");

    // 审批记录
    const done = e.store.getApproval("1", approval.id);
    assert.deepEqual([done.status, done.quoteCredits, done.decision, done.result], ["executed", 5, { decision: "approve" }, { ok: true }]);
    assert.ok(done.decidedAt);
    assert.deepEqual([view.status, view.id, view.run_id], ["executed", codec.encode(BigInt(approval.id)), codec.encode(BigInt(run.id))], "返回的是对外的视图（id 是编码串）");

    // 运行：预算追加生效，已花积分含批准的预估，最后成功
    const r = e.run(run.id);
    assert.equal(r.budgetCredits, 30);
    assert.equal(r.status, "succeeded");
    assert.equal(r.spentCredits, 5 + 2, "批准的 5 + 两次模型调用各 1 积分");

    // 事件顺序：approval.created → 决定后 run.status running → run.usage → approval.decided → 后面的回答
    const seq = e.types().slice(e.types().indexOf("approval.created"));
    assert.deepEqual(seq.filter((t) => ["approval.created", "run.status", "run.usage", "approval.decided"].includes(t)).slice(0, 5), ["approval.created", "run.status", "run.status", "run.usage", "approval.decided"]);
    assert.deepEqual(e.persisted("run.status").slice(-3).map((x) => x.status), ["waiting_approval", "running", "succeeded"]);
    const decided = e.persisted("approval.decided")[0];
    assert.deepEqual([decided.status, decided.id], ["executed", codec.encode(BigInt(approval.id))]);

    // 模型下一次请求里，那次工具调用的结果是执行的结果文字，不是占位
    const second = JSON.stringify(e.go.modelRequests[1].body.messages);
    assert.match(second, /用户批准了generate，已执行/);
    assert.doesNotMatch(second, /已提交给用户批准，等待决定/);
    assert.equal(e.go.modelRequests.length, 2);
  }));

test("Gemini 长签名审批：执行时 Go 收短 ID，续跑按原始 ID 找回工具结果", () =>
  withEngine({
    turns: [{ tools: [{ id: "gc_abcdefgh_" + Buffer.from("s".repeat(1424)).toString("base64url"), name: "generate_media", args: { items: [{ nodeId: "n1" }] } }] }, { text: "已开始生成" }],
    propose: GEN,
  }, async (e) => {
    const { run, approval } = await waitingApproval(e);
    const started = e.persisted("tool.start")[0];
    assert.ok(approval.toolCallId.length > 128 && started.id.length <= 64);
    await e.engine.decide("1", approval.id, { decision: "approve" });
    await e.engine.idle();
    const exec = e.go.seen.find((s) => s.path.endsWith("/approvals/execute"))!;
    assert.equal(exec.body.tool_call_id, started.id);
    assert.equal(e.run(run.id).status, "succeeded");
    const second = JSON.stringify(e.go.modelRequests[1].body.messages);
    assert.ok(second.includes(approval.toolCallId), "Gemini 签名仍在模型请求中");
    assert.match(second, /用户批准了generate，已执行/);
    assert.doesNotMatch(second, /已提交给用户批准，等待决定/);
  }));

test("拒绝：附上理由转告模型，不登记积分，审批保持已拒绝，运行继续", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "generate_media" }] }, { text: "好的，不生成了" }], propose: GEN }, async (e) => {
    const { run, approval } = await waitingApproval(e, { budgetCredits: 20 });
    await e.engine.decide("1", approval.id, { decision: "reject", answer: "太贵", add_budget: 10 });
    await e.engine.idle();
    const a = e.store.getApproval("1", approval.id);
    assert.deepEqual([a.status, a.quoteCredits], ["rejected", 0]);
    assert.equal(e.run(run.id).status, "succeeded");
    assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /用户的说明：太贵/);
    assert.ok(!e.types().includes("run.usage") || e.persisted("run.usage").every((u) => u.spent_credits <= 2), "没有登记被拒绝的预估");
    assert.equal(e.run(run.id).spentCredits, 2, "只有两次模型调用的积分");
    assert.equal(e.run(run.id).budgetCredits, 20, "拒绝时追加的预算不生效");
  }));

test("回答提问：运行从 waiting_input 回到运行，回答转给模型", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "ask_user", args: { question: "要几张？" } }] }, { text: "收到，3 张" }] }, async (e) => {
    const { run, approval } = await waitingApproval(e);
    assert.equal(e.run(run.id).status, "waiting_input");
    await e.engine.decide("1", approval.id, { decision: "approve", answer: "3 张" });
    await e.engine.idle();
    assert.equal(e.store.getApproval("1", approval.id).status, "approved", "提问不执行，状态保持已批准");
    assert.equal(e.run(run.id).status, "succeeded");
    assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /用户回答：3 张/);
  }));

test("部分批准：状态原样沿用平台解析出的 partially_approved", () =>
  withEngine({
    turns: [{ tools: [{ id: "c1", name: "generate_media" }] }, { text: "好" }], propose: GEN,
    resolve: (b) => ({ status: "partially_approved", decision: { decision: "approve", items: [{ node_id: "n1" }] }, quote_credits: 3, items: [] }),
  }, async (e) => {
    const { run, approval } = await waitingApproval(e);
    await e.engine.decide("1", approval.id, { decision: "approve", items: [{ index: 0, approve: true, count: 1 }] });
    await e.engine.idle();
    const a = e.store.getApproval("1", approval.id);
    assert.deepEqual([a.status, a.quoteCredits], ["executed", 3], "按批准的内容重新算出的积分");
    assert.equal(JSON.parse(JSON.stringify(e.go.seen.find((s) => s.path.endsWith("/execute"))!.body.outcome.status)), "partially_approved");
    assert.equal(e.run(run.id).spentCredits, 3 + 2);
  }));

test("执行失败（积分不足、模型下线……）：审批记为 failed，原因交给模型，不让这次请求失败", () =>
  withEngine({
    turns: [{ tools: [{ id: "c1", name: "generate_media" }] }, { text: "那我换个方式" }], propose: GEN,
    execute: () => ({ status: "failed", result: { error: "积分不足" }, outcome_text: "用户批准了生成，但任务提交失败：积分不足。没有扣积分。" }),
  }, async (e) => {
    const { run, approval } = await waitingApproval(e);
    await e.engine.decide("1", approval.id, { decision: "approve" });
    await e.engine.idle();
    const a = e.store.getApproval("1", approval.id);
    assert.deepEqual([a.status, a.result], ["failed", { error: "积分不足" }]);
    assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /任务提交失败：积分不足/);
    assert.equal(e.run(run.id).status, "succeeded");
  }));

test("执行接口连不上：重试几次仍不行就如实告诉模型（不知道有没有执行），审批记为 failed，运行继续", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "canvas_delete" }] }, { text: "我先看看画布" }], propose: { delete: { payload: { node_ids: ["n1"] }, content: "已提交确认。" } }, execute: () => "down" }, async (e) => {
    const { run, approval } = await waitingApproval(e);
    await e.engine.decide("1", approval.id, { decision: "approve" });
    await e.engine.idle();
    assert.ok(e.go.seen.filter((s) => s.path.endsWith("/approvals/execute")).length >= 2, "重试过（执行是幂等的）");
    const a = e.store.getApproval("1", approval.id);
    assert.equal(a.status, "failed");
    assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /可能已经生效也可能没有/);
    assert.equal(e.run(run.id).status, "succeeded");
  }));

test("决定不合法（超预算、一项都没批准）：业务错误原样返回，审批仍待处理，运行仍在等", () =>
  withEngine({
    turns: [{ tools: [{ id: "c1", name: "generate_media" }] }], propose: GEN,
    resolve: (b) => (b.decision.add_budget === 1 ? { error: { status: 402, code: 60008, msg: "超出本轮积分预算" } } : { error: { status: 400, code: 10001, msg: "至少批准一项" } }),
  }, async (e) => {
    const { run, approval } = await waitingApproval(e);
    await assert.rejects(e.engine.decide("1", approval.id, { decision: "approve", add_budget: 1 }), (err: AgentError) => err.code === 60008 && err.status === 402 && /预算/.test(err.message));
    await assert.rejects(e.engine.decide("1", approval.id, { decision: "approve" }), (err: AgentError) => err.code === 10001 && err.status === 400);
    assert.equal(e.store.getApproval("1", approval.id).status, "pending");
    assert.equal(e.run(run.id).status, "waiting_approval");
    assert.ok(!e.go.seen.some((s) => s.path.endsWith("/approvals/execute")), "没有执行任何东西");
  }));

test("不能决定的情况：审批不存在 / 别人的 → 60013；已处理（重复点击）、已过期、运行已停止 → 60004", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "generate_media" }] }], propose: GEN }, async (e) => {
    const { run, approval } = await waitingApproval(e);
    await assert.rejects(e.engine.decide("1", "999", { decision: "approve" }), code(60013));
    await assert.rejects(e.engine.decide("2", approval.id, { decision: "approve" }), code(60013));
    // 运行已被停止
    await e.engine.cancel("1", run.id);
    await assert.rejects(e.engine.decide("1", approval.id, { decision: "approve" }), code(60004));
  }));

test("已过期的审批不能再决定（24 小时）", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "generate_media" }] }], propose: GEN }, async (e) => {
    const { approval } = await waitingApproval(e);
    e.advanceClock(24 * 3600 * 1000 + 1);
    await assert.rejects(e.engine.decide("1", approval.id, { decision: "approve" }), code(60004));
    assert.equal(e.store.getApproval("1", approval.id).status, "pending", "过期只是不能再决定，记录不动");
  }));

test("重复点击 / 两个标签页同时点：只有一次生效，另一次得到 60004，不会执行两次", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "generate_media" }] }, { text: "好" }], propose: GEN }, async (e) => {
    const { approval } = await waitingApproval(e);
    const results = await Promise.allSettled([
      e.engine.decide("1", approval.id, { decision: "approve" }),
      e.engine.decide("1", approval.id, { decision: "approve" }),
    ]);
    assert.deepEqual(results.map((r) => r.status).sort(), ["fulfilled", "rejected"]);
    const rejected = results.find((r) => r.status === "rejected") as PromiseRejectedResult;
    assert.equal((rejected.reason as AgentError).code, 60004);
    await e.engine.idle();
    assert.equal(e.go.seen.filter((s) => s.path.endsWith("/approvals/execute")).length, 1, "只执行一次");
  }));

// ---- 继续 ----

const BUDGET_OUT = { status: 402, message: "超出本轮积分预算", errorCode: 60008 };

test("额度用完后继续：必须追加预算；追加后运行重新排队、运行、完成，模型收到「请继续」", () =>
  withEngine({ turns: [{ error: BUDGET_OUT }, { text: "接着做完了" }] }, async (e) => {
    const run = await e.start({ budgetCredits: 5 });
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "budget_exhausted");
    await assert.rejects(e.engine.resume("1", run.id), (err: AgentError) => err.code === 10001 && /追加预算/.test(err.message));
    await assert.rejects(e.engine.resume("1", run.id, 0), (err: AgentError) => err.code === 10001);

    const queued = await e.engine.resume("1", run.id, 20);
    assert.equal(queued.status, "queued", "返回的是排队状态，和现在一致");
    await e.engine.idle();
    const r = e.run(run.id);
    assert.deepEqual([r.status, r.budgetCredits, r.errorMessage, r.endedAt !== null], ["succeeded", 25, "", true]);
    assert.deepEqual(e.statuses().slice(-4), ["budget_exhausted", "queued", "running", "succeeded"]);
    assert.deepEqual(e.persisted("run.status").find((x) => x.status === "queued" && x.resumed)?.resumed, true, "排队事件带 resumed");
    assert.match(JSON.stringify(e.go.modelRequests.at(-1)!.body.messages), /请继续刚才的任务/);
    assert.doesNotMatch(JSON.stringify(e.go.modelRequests.at(-1)!.body.messages), /被中断/, "额度用完不是中断，不带中断的提醒");
    const rc = decodeRunContext(String(e.go.modelRequests.at(-1)!.headers["x-agent-context"]));
    assert.equal(rc.budget_credits, 25, "继续的这一段用的是新预算");
  }));

test("步数用尽后继续：步数上限放宽一轮，之前的步数累计，运行能接着做完", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { tools: [{ id: "c2", name: "canvas_get_state" }] }, { tools: [{ id: "c3", name: "canvas_get_state" }] }, { text: "做完了" }] }, async (e) => {
    const run = await e.start({ maxSteps: 1 });
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "step_limit");
    await e.engine.resume("1", run.id);
    await e.engine.idle();
    const r = e.run(run.id);
    assert.deepEqual([r.status, r.maxSteps], ["succeeded", 41], "上限 +40");
    assert.equal(r.steps, 2, "继续前执行的 1 步 + 继续后的 1 步（被拦下的那次不算）");
  }));

test("被中断后继续：模型收到的提醒说明最后一步可能已经执行", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { tools: [{ id: "c2", name: "canvas_get_state" }] }, { text: "确认过了" }] }, async (e) => {
    const run = await e.start({ maxSteps: 1 });
    await e.engine.idle();
    // 服务重启把运行标成了中断（3c 的启动恢复就是这么做的）
    e.store.updateRunIf(run.id, ["step_limit"], { status: "interrupted" });
    await e.engine.resume("1", run.id);
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "succeeded");
    assert.match(JSON.stringify(e.go.modelRequests.at(-1)!.body.messages), /可能已经执行/);
  }));

test("继续的限制：只有中断、额度用完、步数用尽能继续；别人的运行 60012；画布上已有别的运行 60001；准备失败时运行标失败", async () => {
  await withEngine({ turns: [{ text: "好" }, { error: BUDGET_OUT }] }, async (e) => {
    const done = await e.start();
    await e.engine.idle();
    await assert.rejects(e.engine.resume("1", done.id), (err: AgentError) => err.code === 60003, "已成功的不能继续");
    const next = await e.start();
    await e.engine.idle();
    assert.equal(e.run(next.id).status, "budget_exhausted");
    await assert.rejects(e.engine.resume("2", next.id, 5), (err: AgentError) => err.code === 60012);
    // 画布被别的运行占了
    e.store.updateRunIf(next.id, ["budget_exhausted"], { status: "interrupted" });
    const other = e.store.createRun({ sessionId: e.sessionId, canvasId: "7", userId: "1", status: "running", mode: "all" });
    await assert.rejects(e.engine.resume("1", next.id), (err: AgentError) => err.code === 60001);
    assert.equal(e.run(next.id).status, "interrupted", "没有占到画布，状态不变");
    void other;
  });
  await withEngine({ turns: [{ error: BUDGET_OUT }] }, async (e) => {
    const run = await e.start();
    await e.engine.idle();
    e.go.setPrepareError({ status: 400, code: 60002, msg: "Agent 模型不可用" });
    await assert.rejects(e.engine.resume("1", run.id, 5), (err: AgentError) => err.code === 60005);
    assert.equal(e.run(run.id).status, "failed");
    assert.equal(e.store.activeRun("7"), undefined, "画布立刻空出来");
  });
});

test("插话落在收尾的瞬间：运行等 Pi 真正空闲才标完成，插话得到回应；收尾之后再插话被拒绝", () =>
  withEngine({ turns: [{ text: "第一版的回答，要流一会儿", charDelayMs: 20 }, { text: "改好了" }] }, async (e) => {
    const run = await e.start();
    for (let i = 0; i < 100 && e.go.modelRequests.length === 0; i++) await new Promise((x) => setTimeout(x, 10));
    await new Promise((x) => setTimeout(x, 60));
    await e.engine.interject("1", run.id, "改成竖屏"); // 模型正在输出：回答写完后作为下一轮的输入
    await e.engine.idle();
    assert.equal(e.go.modelRequests.length, 2, "插话得到了回应");
    assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /改成竖屏/);
    assert.equal(e.run(run.id).status, "succeeded");
    assert.equal(e.persisted("message.done").length, 2);
    await assert.rejects(e.engine.interject("1", run.id, "晚了"), (err: AgentError) => err.code === 60003);
  }));

import assert from "node:assert/strict";
import { test } from "node:test";
import { withEngine, type Env } from "./engine-helpers.ts";

/**
 * 后台等待生成：批准生成后本轮照常结束；这批任务全部结束后，系统在同一个会话里自动唤醒 Agent，把结果交给它。
 * 设计见 docs/design/Agent等待生成结果/Agent等待生成结果.md。
 */

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));
async function until(cond: () => boolean | Promise<boolean>, what: string, ms = 4000) {
  const t0 = Date.now();
  while (!(await cond())) {
    if (Date.now() - t0 > ms) assert.fail(`等待超时：${what}`);
    await sleep(15);
  }
}

const items = [{ node_id: "n1", label: "镜头 1", price: 5 }, { node_id: "n2", label: "镜头 2", price: 5 }];
const propose = (wait: boolean | undefined) => ({
  generate: { payload: { items, quote_credits: 10, ...(wait !== undefined && { wait }) }, quote_credits: 10, summary: "生成 2 个，预估 10 积分", content: "已提交给用户批准，等待决定。" },
});
const execute = () => ({
  status: "executed",
  result: { tasks: [{ node_id: "n1", task_id: "101", credits: 5 }, { node_id: "n2", task_id: "102", credits: 5 }] },
  outcome_text: "用户批准了生成，任务已提交。",
});
const GEN_TURN = { tools: [{ id: "call_g", name: "generate_media", args: { items: [{ nodeId: "n1" }, { nodeId: "n2" }] } }] };

/** 模型申请生成 → 用户批准 → 本轮结束（后台等待中） */
async function approved(e: Env) {
  const run = await e.start();
  await e.engine.idle();
  const [approval] = e.store.listApprovals(run.id);
  await e.engine.decide("1", approval.id, { decision: "approve" });
  await e.engine.idle();
  return { run, approval };
}
/** 唤醒发生，并且被唤醒的那一轮运行已经结束（唤醒事件先于运行登记进引擎，只靠 idle() 会抢在它前面） */
async function wakeRunDone(e: Env, what: string) {
  await until(() => wakes(e).length >= 1, what);
  const id = wakes(e)[0].runId;
  await until(() => !["queued", "running"].includes(e.run(id).status), "被唤醒的那一轮结束");
  await e.engine.idle();
}
const watchesOf = (e: Env) => e.engine.watches("1", e.sessionId);
const wakes = (e: Env) => e.store.listEvents(e.sessionId, 0, 1000).filter((x) => x.type === "tasks.settled");

test("批准生成（wait=true）：本轮照常结束，监听登记在会话里，画布释放；前端收到 watch.updated", () =>
  withEngine({ turns: [GEN_TURN, { text: "已提交，结果出来后我继续" }], propose: propose(true), execute }, async (e) => {
    const { run, approval } = await approved(e);
    assert.equal(e.run(run.id).status, "succeeded", "本轮正常结束，不占着画布");
    assert.equal(e.store.activeRun("7"), undefined);

    const [b] = await watchesOf(e);
    assert.deepEqual([b.batch_id, b.state, b.total, b.settled], [approval.id, "waiting", 2, 0]);
    assert.deepEqual(b.tasks.map((t) => [t.task_id, t.node_id, t.label, t.status]), [["101", "n1", "镜头 1", "pending"], ["102", "n2", "镜头 2", "pending"]]);
    assert.equal(await e.engine.liveTaskCount(), 1, "每批一个 Pi 后台任务在睡");

    const evs = e.persisted("watch.updated");
    assert.equal(evs.length, 1);
    assert.deepEqual([evs[0].batch_id, evs[0].state, evs[0].total, evs[0].settled], [approval.id, "waiting", 2, 0]);
    // 给模型的工具结果仍是执行结果；提示词（Go）负责告诉它系统会自动继续
    assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /任务已提交/);
  }, { piClock: true }));

test("没有 wait 或 wait=false（用户说提交就行）：不登记监听，和以前一样", () => Promise.all([undefined, false].map((wait) =>
  withEngine({ turns: [GEN_TURN, { text: "已提交" }], propose: propose(wait), execute }, async (e) => {
    await approved(e);
    assert.deepEqual(await watchesOf(e), []);
    assert.equal(e.persisted("watch.updated").length, 0);
    assert.equal(await e.engine.liveTaskCount(), 0);
  }, { piClock: true }))).then(() => undefined));

test("提交失败（没有创建任务）的批准：没有东西可等，不登记", () =>
  withEngine({
    turns: [GEN_TURN, { text: "提交失败了" }], propose: propose(true),
    execute: () => ({ status: "failed", result: { error: "积分不足" }, outcome_text: "任务提交失败：积分不足。" }),
  }, async (e) => {
    await approved(e);
    assert.deepEqual(await watchesOf(e), []);
  }, { piClock: true }));

test("门铃：一个个任务结束只记账，全部结束才唤醒；唤醒是同一会话里新开的一轮，结果交给模型", () =>
  withEngine({ turns: [GEN_TURN, { text: "已提交" }, { text: "两张图都合格，可以做视频了" }], propose: propose(true), execute }, async (e) => {
    const { run } = await approved(e);
    assert.equal(await e.engine.taskSettled(run.id, "101", "succeeded"), true);
    const [b] = await watchesOf(e);
    assert.deepEqual([b.state, b.settled], ["waiting", 1]);
    assert.equal(e.go.modelRequests.length, 2, "还有任务没结束，不唤醒");
    assert.equal(e.store.activeRun("7"), undefined);

    assert.equal(await e.engine.taskSettled(run.id, "102", "succeeded"), true);
    await wakeRunDone(e, "全部结束后唤醒");

    // 向 Go 取了这批任务的结果
    const q = e.go.seen.find((s) => s.path.endsWith("/tasks/status"))!;
    assert.deepEqual(q.body, { user_id: "1", task_ids: ["101", "102"] });
    // 新开了一轮运行（和第一轮不同），成功结束
    const wake = wakes(e)[0];
    assert.notEqual(wake.runId, run.id);
    assert.equal(e.run(wake.runId).status, "succeeded");
    assert.deepEqual([(wake.payload as any).steer, (wake.payload as any).batch_ids.length], [false, 1]);
    assert.deepEqual((wake.payload as any).tasks.map((t: any) => [t.task_id, t.label, t.task_status]), [["101", "镜头 1", "succeeded"], ["102", "镜头 2", "succeeded"]]);
    // 模型收到的是系统通知，写明结果和不是用户说的话
    const text = JSON.stringify(e.go.modelRequests[2].body.messages);
    assert.match(text, /你之前申请的生成已全部结束/);
    assert.match(text, /镜头 1/);
    assert.match(text, /系统通知/);
    assert.match(text, /不代表用户同意或批准了任何事/);
    // 批已交付：不再出现在进行中的列表里，前端收到最终状态
    assert.deepEqual(await watchesOf(e), []);
    assert.equal(e.persisted("watch.updated").at(-1)!.state, "delivered");
    assert.equal(await e.engine.liveTaskCount(), 0, "交付后监听任务收尾（或被收掉）");
  }, { piClock: true }));

test("失败、超时也算结束：结果里写明状态，Agent 据此决定重做", () =>
  withEngine({
    turns: [GEN_TURN, { text: "已提交" }, { text: "镜头 2 失败了，我改提示词" }], propose: propose(true), execute,
    taskStates: (ids) => ids.map((id) => id === "101"
      ? { task_id: id, node_id: "n1", kind: "image", status: "succeeded", outputs: [{ media_type: "image", asset_id: "42" }] }
      : { task_id: id, node_id: "n2", kind: "image", status: "failed", error: "触发内容安全风控" }),
  }, async (e) => {
    const { run } = await approved(e);
    await e.engine.taskSettled(run.id, "101", "succeeded");
    await e.engine.taskSettled(run.id, "102", "failed");
    await wakeRunDone(e, "唤醒");
    const text = JSON.stringify(e.go.modelRequests[2].body.messages);
    assert.match(text, /镜头 2/);
    assert.match(text, /失败/);
    assert.match(text, /触发内容安全风控/);
    assert.deepEqual((wakes(e)[0].payload as any).tasks.map((t: any) => t.task_status), ["succeeded", "failed"]);
  }, { piClock: true }));

test("重复的门铃（River 重试、乱序）不会重复唤醒；批交付后再来的门铃回 false（Go 据此结束重试）", () =>
  withEngine({ turns: [GEN_TURN, { text: "已提交" }, { text: "验收完" }], propose: propose(true), execute }, async (e) => {
    const { run } = await approved(e);
    assert.equal(await e.engine.taskSettled(run.id, "101", "succeeded"), true);
    assert.equal(await e.engine.taskSettled(run.id, "101", "succeeded"), true, "同一个任务重复通知：照常接受，没有副作用");
    assert.equal((await watchesOf(e))[0].settled, 1);
    await e.engine.taskSettled(run.id, "102", "succeeded");
    await e.engine.taskSettled(run.id, "102", "succeeded");
    await wakeRunDone(e, "唤醒");
    assert.equal(wakes(e).length, 1, "只唤醒一次");
    assert.equal(e.go.modelRequests.length, 3);
    assert.equal(await e.engine.taskSettled(run.id, "101", "succeeded"), false);
  }, { piClock: true }));

test("不认识的门铃（运行不存在、任务不在任何批里）：回 false，不抛错", () =>
  withEngine({ turns: [GEN_TURN, { text: "已提交" }], propose: propose(true), execute }, async (e) => {
    const { run } = await approved(e);
    assert.equal(await e.engine.taskSettled("999999", "101", "succeeded"), false);
    assert.equal(await e.engine.taskSettled(run.id, "555", "succeeded"), false);
    assert.equal((await watchesOf(e))[0].settled, 0);
  }, { piClock: true }));

test("不用等了：批取消、监听任务收掉；之后任务结束只回 false，不唤醒；生成不受影响", () =>
  withEngine({ turns: [GEN_TURN, { text: "已提交" }], propose: propose(true), execute }, async (e) => {
    const { run } = await approved(e);
    assert.equal(await e.engine.liveTaskCount(), 1);
    const canceled = await e.engine.cancelWatches("1", e.sessionId);
    assert.deepEqual(canceled.map((b) => b.state), ["canceled"]);
    assert.deepEqual(await watchesOf(e), [], "进行中的列表里没有了");
    assert.equal(e.persisted("watch.updated").at(-1)!.state, "canceled");
    await until(async () => (await e.engine.liveTaskCount()) === 0, "监听任务被收掉");

    assert.equal(await e.engine.taskSettled(run.id, "101", "succeeded"), false);
    assert.equal(await e.engine.taskSettled(run.id, "102", "succeeded"), false);
    await sleep(100);
    assert.equal(wakes(e).length, 0);
    assert.equal(e.go.modelRequests.length, 2, "没有再调用模型");
    assert.deepEqual(await e.engine.cancelWatches("1", e.sessionId), [], "再点一次什么都没有");
  }, { piClock: true }));

test("别人的会话：不能看也不能取消（按用户隔离）", () =>
  withEngine({ turns: [GEN_TURN, { text: "已提交" }], propose: propose(true), execute }, async (e) => {
    await approved(e);
    await assert.rejects(e.engine.watches("2", e.sessionId), /会话不存在/);
    await assert.rejects(e.engine.cancelWatches("2", e.sessionId), /会话不存在/);
    assert.equal((await watchesOf(e))[0].state, "waiting");
  }, { piClock: true }));

test("画布被另一个会话的运行占着：整批结束也先不唤醒，那个运行一结束就唤醒", () =>
  withEngine({
    turns: [GEN_TURN, { text: "已提交" }, { text: "另一个会话在写分镜", hang: true }, { text: "验收完成" }], propose: propose(true), execute,
  }, async (e) => {
    const { run } = await approved(e);
    const other = e.store.createSession({ userId: "1", canvasId: "7", title: "角色设定", mode: "all", modelKey: "" });
    const busy = await e.engine.startRun({ userId: "1", sessionId: other.id, message: "写分镜", modelKey: "claude" });
    await until(() => e.go.modelRequests.length === 3, "另一个会话的运行在进行中");

    await e.engine.taskSettled(run.id, "101", "succeeded");
    await e.engine.taskSettled(run.id, "102", "succeeded");
    await sleep(150);
    assert.equal(wakes(e).length, 0, "画布被占着：不唤醒");
    assert.equal((await watchesOf(e))[0].state, "deliverable", "整批已结束，排队中");
    assert.equal(e.persisted("watch.updated").at(-1)!.state, "deliverable");

    await e.engine.cancel("1", busy.id);
    await wakeRunDone(e, "占用解除后唤醒");
    assert.equal(e.go.modelRequests.length, 4);
    assert.equal(e.run(wakes(e)[0].runId).status, "succeeded");
    assert.deepEqual(await watchesOf(e), []);
  }, { piClock: true }));

test("本会话正在回答时整批结束：结果插进当前这一轮，不另开一轮", () =>
  withEngine({
    turns: [GEN_TURN, { text: "已提交" }, { tools: [{ id: "c_get", name: "canvas_get_state" }] }, { text: "顺手改好了，也看了生成结果" }],
    propose: propose(true), execute,
    tools: { canvas_get_state: async () => { await sleep(400); return { content: "ok" }; } },
  }, async (e) => {
    const { run } = await approved(e);
    // 用户接着聊：第二个运行在调一个慢工具
    const chat = await e.engine.startRun({ userId: "1", sessionId: e.sessionId, message: "把镜头 3 改成黄昏", modelKey: "claude" });
    await until(() => e.go.paths().some((p) => p.includes("/tools/canvas_get_state")), "工具开始执行");

    await e.engine.taskSettled(run.id, "101", "succeeded");
    await e.engine.taskSettled(run.id, "102", "succeeded");
    await until(() => wakes(e).length === 1, "结果插进当前轮");
    assert.equal(wakes(e)[0].runId, chat.id, "属于正在进行的这一轮，没有新开运行");
    assert.equal((wakes(e)[0].payload as any).steer, true);
    await e.engine.idle();

    assert.equal(e.run(chat.id).status, "succeeded");
    const last = JSON.stringify(e.go.modelRequests.at(-1)!.body.messages);
    assert.match(last, /你之前申请的生成已全部结束/, "下一次请求里带着结果");
    assert.deepEqual(await watchesOf(e), []);
  }, { piClock: true }));

test("本会话在等审批时整批结束：排队，审批处理完、运行结束后才唤醒", () =>
  withEngine({
    turns: [GEN_TURN, { text: "已提交" }, { tools: [{ id: "c_ask", name: "ask_user", args: { question: "要横屏还是竖屏？" } }] }, { text: "好的，竖屏" }, { text: "验收完成" }],
    propose: { ...propose(true), ask: { payload: { question: "要横屏还是竖屏？" }, quote_credits: 0, summary: "提问", content: "已提交，等待回答。", terminate: true } }, execute,
  }, async (e) => {
    const { run } = await approved(e);
    const chat = await e.engine.startRun({ userId: "1", sessionId: e.sessionId, message: "再做一组", modelKey: "claude" });
    await e.engine.idle();
    assert.equal(e.run(chat.id).status, "waiting_input");

    await e.engine.taskSettled(run.id, "101", "succeeded");
    await e.engine.taskSettled(run.id, "102", "succeeded");
    await sleep(150);
    assert.equal(wakes(e).length, 0, "在等用户回答：不打断");
    assert.equal((await watchesOf(e))[0].state, "deliverable");

    const [ask] = e.store.listApprovals(chat.id);
    await e.engine.decide("1", ask.id, { decision: "approve", answer: "竖屏" });
    await wakeRunDone(e, "那一轮结束后唤醒");
    assert.equal(e.run(wakes(e)[0].runId).status, "succeeded");
  }, { piClock: true }));

test("门铃丢了：Pi 监听任务睡过宽限期后醒来向 Go 查，补记并唤醒", () =>
  withEngine({
    turns: [GEN_TURN, { text: "已提交" }, { text: "验收完成" }], propose: propose(true), execute,
  }, async (e) => {
    await approved(e);
    e.advanceClock(60_000);
    await sleep(100);
    assert.equal(e.go.seen.filter((s) => s.path.endsWith("/tasks/status")).length, 0, "宽限期（2 分钟）内不查");

    e.advanceClock(61_000);
    await wakeRunDone(e, "醒来查到都结束了，唤醒");
    assert.deepEqual(e.go.seen.find((s) => s.path.endsWith("/tasks/status"))!.body, { user_id: "1", task_ids: ["101", "102"] });
    assert.equal(e.run(wakes(e)[0].runId).status, "succeeded");
    assert.deepEqual(await watchesOf(e), []);
  }, { piClock: true }));

test("兜底查询时有任务还在跑：继续睡 60 秒再查，不唤醒", () => {
  let round = 0;
  return withEngine({
    turns: [GEN_TURN, { text: "已提交" }, { text: "验收完成" }], propose: propose(true), execute,
    taskStates: (ids) => { round++; return ids.map((id) => ({ task_id: id, node_id: "n" + id, kind: "image", status: round === 1 && id === "102" ? "running" : "succeeded", progress: 50 })); },
  }, async (e) => {
    await approved(e);
    e.advanceClock(121_000);
    await until(() => round === 1, "第一次兜底查询");
    await sleep(100);
    assert.equal(wakes(e).length, 0, "102 还在跑");
    assert.equal((await watchesOf(e))[0].settled, 1, "已结束的 101 记上了");

    e.advanceClock(61_000);
    await wakeRunDone(e, "再查一次后全部结束，唤醒");
  }, { piClock: true });
});

test("Go 暂时不可用：整批结束但取不到结果，不丢唤醒，下一次兜底再试", () => {
  let down = true;
  return withEngine({
    turns: [GEN_TURN, { text: "已提交" }, { text: "验收完成" }], propose: propose(true), execute,
    taskStates: (ids) => (down ? "down" : ids.map((id) => ({ task_id: id, node_id: "n" + id, kind: "image", status: "succeeded" }))),
  }, async (e) => {
    const { run } = await approved(e);
    await e.engine.taskSettled(run.id, "101", "succeeded");
    await e.engine.taskSettled(run.id, "102", "succeeded");
    await sleep(200);
    assert.equal(wakes(e).length, 0, "取不到结果就先不唤醒");
    assert.equal((await watchesOf(e))[0].state, "deliverable");
    assert.equal(e.store.activeRun("7"), undefined, "也没有留下半截的运行");

    down = false;
    e.advanceClock(121_000);
    await wakeRunDone(e, "Go 恢复后兜底把它送出去");
    assert.equal(e.go.modelRequests.length, 3);
  }, { piClock: true });
});

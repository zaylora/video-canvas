import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { openSession, runCase, HarnessError } from "../../harness/runner.mjs";
import { BASE, createFakeBackend, createFakeClock, fakeTraceBundle } from "./fake-backend.mjs";
import { goodStoryboard, group, node, canvas, approval } from "./helpers.mjs";

// 端到端：假后端按脚本吐事件，runner 走完 登录 → 建画布 → 建会话 → 开跑 → 补拉事件并应答 → 终态 → 读画布 → 拉轨迹 → 断言 → 写文件 → 删画布。

const ENV = { HARNESS_BASE_URL: BASE, HARNESS_EMAIL: "harness@example.com", HARNESS_PASSWORD: "pw-harness-secret", HARNESS_ADMIN_EMAIL: "admin@example.com", HARNESS_ADMIN_PASSWORD: "pw-admin-secret" };
const ENV_NO_ADMIN = { HARNESS_BASE_URL: BASE, HARNESS_EMAIL: ENV.HARNESS_EMAIL, HARNESS_PASSWORD: ENV.HARNESS_PASSWORD };
const SECRETS = ["pw-harness-secret", "pw-admin-secret", "tok-harness-secret", "tok-admin-secret"];

const askBatch = (id, question, payloadExtra = {}) => [{ type: "approval.created", data: approval(id, "ask", { question, kind: "choice", options: ["16:9", "9:16"], ...payloadExtra }) }, { type: "run.status", data: { status: "waiting_input" } }];
const runningBatch = [{ type: "run.status", data: { status: "running" } }, { type: "tool.start", data: { id: "t1", name: "canvas_get_state" } }, { type: "tool.end", data: { id: "t1", name: "canvas_get_state", is_error: false, summary: "1 个节点" } }];
const finishBatch = (extra = []) => [
  { type: "tool.end", data: { id: "t2", name: "canvas_apply_ops", is_error: false, summary: "+10 节点" } },
  { type: "run.usage", data: { spent_credits: 0, budget_credits: 0 } },
  ...extra,
  { type: "run.status", data: { status: "succeeded" } },
];

const baseCase = (over = {}) => ({
  id: "storyboard-9x16-3shots",
  title: "三镜头",
  mode: "storyboard",
  message: "把剧本拆成 3 个镜头，竖屏 9:16",
  selection: ["n_script"],
  budget: 0,
  timeout_sec: 600,
  wait_tasks: false,
  answers: [{ match: { kind: "ask", contains: "画幅" }, answer: { answer: "9:16" } }],
  expect: [{ type: "run_status", in: ["succeeded"] }, { type: "no_overlap" }, { type: "members_inside_group" }, { type: "count", of: "group", min: 3, max: 4 }],
  ...over,
});

const FIXTURE = canvas([node("n_script", "script", 0, 0, { prompt: "剧本" })]);

async function setup(backendOpts, { env = ENV, caseOver = {}, keep = false, caseFile = "cases/x.case.json" } = {}) {
  const backend = createFakeBackend(backendOpts);
  const clock = createFakeClock();
  const logs = [];
  const log = (cls, text) => logs.push(`${cls}|${text}`);
  const outRoot = fs.mkdtempSync(path.join(os.tmpdir(), "hn-run-"));
  const session = await openSession({ env, fetch: backend.fetch, log, clock });
  const result = await runCase({ def: baseCase(caseOver), file: caseFile, fixture: FIXTURE, session, outRoot, keep, index: 1, total: 1, clock, log, pollMs: 1000, taskPollMs: 5000 });
  return { backend, clock, logs, outRoot, result, session };
}

const readDir = (dir) => Object.fromEntries(fs.readdirSync(dir).map((f) => [f, fs.readFileSync(path.join(dir, f), "utf8")]));

function happyBackend(over = {}) {
  return { batches: [runningBatch, askBatch("a1", "画幅用哪个？16:9 / 9:16"), finishBatch()], finalCanvas: goodStoryboard(), trace: fakeTraceBundle(), ...over };
}

test("通过：完整流程、请求形状、应答、输出文件、删画布", async () => {
  const { backend, result, logs } = await setup(happyBackend());
  assert.equal(result.passed, true, JSON.stringify(result.results.filter((r) => !r.ok)));
  assert.equal(result.status, "succeeded");

  // 登录用 username 字段（后端 LoginUserReq），用户和管理员各登录一次
  const logins = backend.callsTo("POST", /^\/auth\/login$/);
  assert.equal(logins.length, 2);
  assert.deepEqual(Object.keys(logins[0].body).sort(), ["password", "username"]);

  // 建画布：标题带 [harness] 前缀，内容是 fixture
  const created = backend.state.canvasCreated;
  assert.match(created.title, /^\[harness\] storyboard-9x16-3shots /);
  assert.deepEqual(created.payload_json, FIXTURE);

  // 建会话和开跑
  const sess = backend.callsTo("POST", /\/agent\/sessions$/)[0];
  assert.equal(sess.body.mode, "storyboard");
  const run = backend.callsTo("POST", /\/runs$/)[0];
  assert.equal(run.headers["x-agent-harness"], "storyboard-9x16-3shots");
  assert.deepEqual(run.body, { message: "把剧本拆成 3 个镜头，竖屏 9:16", mode: "storyboard", selection: ["n_script"], budget_credits: 0 });
  assert.equal(run.headers.authorization, "Bearer tok-harness-secret");

  // 按剧本应答
  assert.deepEqual(backend.state.decisions, [{ id: "a1", body: { decision: "approve", answer: "9:16" } }]);
  assert.ok(logs.some((l) => l.includes("按剧本应答") && l.includes("9:16")));

  // 事件按 after 补拉，序号单调递增
  const afters = backend.callsTo("GET", /\/events$/).map((c) => Number(c.query.after));
  assert.equal(afters[0], 0);
  for (let i = 1; i < afters.length; i++) assert.ok(afters[i] >= afters[i - 1]);
  assert.ok(afters.at(-1) > 0);

  // 管理员取轨迹，用管理员令牌
  const tr = backend.callsTo("GET", /\/admin\/agent\/runs\/r1\/trace$/);
  assert.equal(tr.length, 1);
  assert.equal(tr[0].query.format, "json");
  assert.equal(tr[0].headers.authorization, "Bearer tok-admin-secret");

  // 默认删掉测试画布
  assert.equal(backend.state.canvasDeleted, true);
  assert.equal(result.canvasDeleted, true);
});

test("通过：输出目录名、文件清单、report.md 保留后端内容并在顶部插入断言段", async () => {
  const { result, outRoot } = await setup(happyBackend());
  assert.equal(path.dirname(result.dir), outRoot);
  assert.match(path.basename(result.dir), /^\d{8}-\d{6}-storyboard-9x16-3shots$/);
  const files = readDir(result.dir);
  for (const f of ["report.md", "trace.jsonl", "events.jsonl", "mutations.jsonl", "canvas.before.json", "canvas.after.json", "meta.json", "assertions.json", "canvas.final.json"]) assert.ok(f in files, `缺 ${f}`);

  assert.match(files["report.md"], /^# Agent 运行报告 · run r1\n> 终态 succeeded/);
  assert.match(files["report.md"], /## 断言结论\n\n通过（4\/4 条断言通过）/);
  assert.match(files["report.md"], /（后端生成）/, "后端生成的画布概要保持原样");
  assert.ok(files["report.md"].indexOf("## 断言结论") < files["report.md"].indexOf("## 时间线"));
  assert.match(files["trace.jsonl"], /"response":\{"text":"原文"\}/, "有管理员凭据时 trace.jsonl 是后端的带原文版本");

  const meta = JSON.parse(files["meta.json"]);
  assert.equal(meta.case_id, "storyboard-9x16-3shots");
  assert.equal(meta.base_url, BASE);
  assert.equal(meta.prompt_version, 10, "后端 meta 的字段保留");
  assert.equal(meta.case_file, "cases/x.case.json");

  const asserts = JSON.parse(files["assertions.json"]);
  assert.equal(asserts.case_id, "storyboard-9x16-3shots");
  assert.equal(asserts.passed, true);
  assert.equal(asserts.results.length, 4);
  assert.deepEqual(asserts.expect, baseCase().expect);
  assert.ok(files["events.jsonl"].trim().split("\n").every((l) => JSON.parse(l).seq > 0));
});

test("repeat 的第几次进目录名后缀", async () => {
  const backend = createFakeBackend(happyBackend());
  const clock = createFakeClock();
  const session = await openSession({ env: ENV, fetch: backend.fetch, log: () => {}, clock });
  const outRoot = fs.mkdtempSync(path.join(os.tmpdir(), "hn-run-"));
  const r = await runCase({ def: baseCase(), file: "x", fixture: FIXTURE, session, outRoot, keep: false, index: 2, total: 3, clock, log: () => {}, pollMs: 1000, taskPollMs: 5000 });
  assert.match(path.basename(r.dir), /-storyboard-9x16-3shots-2$/);
});

test("断言失败：组压组 + 竖图伸出组框，报告里有证据，用例判失败", async () => {
  const bad = canvas([
    group("g1", 696, 0, 2064, 444),
    group("g2", 1016, 0, 2064, 444),
    node("g1n2", "image", 48, 72, { parent: "g1", aspect: "9:16", prompt: "x" }),
  ]);
  const { result } = await setup(happyBackend({ finalCanvas: bad }));
  assert.equal(result.passed, false);
  const failedTypes = result.results.filter((r) => !r.ok).map((r) => r.type).sort();
  assert.deepEqual(failedTypes, ["count", "members_inside_group", "no_overlap"]);
  const report = fs.readFileSync(path.join(result.dir, "report.md"), "utf8");
  assert.match(report, /失败（1\/4 条断言通过）/);
  assert.match(report, /g1\(696,0,2064×444\)/);
  assert.match(report, /g1n2\(324×576\) 底边超出组框 204px/);
});

test("未预写的提问：取消运行、判失败、报告里给建议补的 answers 条目", async () => {
  const { backend, result, logs } = await setup(happyBackend({ batches: [runningBatch, askBatch("a9", "背景音乐要吗？"), finishBatch()] }));
  assert.equal(result.passed, false);
  assert.equal(backend.state.cancelCount, 1);
  assert.equal(backend.state.decisions.length, 0, "没有预写就不能乱答");
  const f = result.results.find((r) => r.type === "harness.unscripted_question");
  assert.equal(f.ok, false);
  assert.equal(f.got, "未预写的提问：背景音乐要吗？");
  assert.deepEqual(f.suggestion.match, { kind: "ask", contains: "背景音乐要吗" });
  const report = fs.readFileSync(path.join(result.dir, "report.md"), "utf8");
  assert.match(report, /未预写的提问：背景音乐要吗？/);
  assert.match(report, /"contains": "背景音乐要吗"/);
  assert.ok(logs.some((l) => l.startsWith("err|") && l.includes("没有预写的提问")));
  assert.equal(result.status, "canceled");
});

test("超时：取消运行、判失败，已有轨迹照常拉取", async () => {
  const { backend, result } = await setup({ batches: [runningBatch], finalCanvas: goodStoryboard(), trace: fakeTraceBundle() }, { caseOver: { timeout_sec: 30 } });
  assert.equal(result.passed, false);
  assert.equal(backend.state.cancelCount, 1);
  const f = result.results.find((r) => r.type === "harness.timeout");
  assert.equal(f.ok, false);
  assert.match(f.got, /30 秒/);
  assert.equal(backend.callsTo("GET", /\/trace$/).length, 1, "超时后仍然拉轨迹");
  assert.equal(backend.state.canvasDeleted, true);
});

test("运行以 failed 结束：run_status 断言失败，证据带后端的错误文案", async () => {
  const batches = [runningBatch, [{ type: "run.status", data: { status: "failed", error: "渠道余额不足" } }]];
  const { result } = await setup({ batches, finalCanvas: goodStoryboard(), trace: fakeTraceBundle() });
  const r = result.results.find((x) => x.type === "run_status");
  assert.equal(r.ok, false);
  assert.match(r.got, /failed/);
  assert.match(r.evidence.join("\n"), /渠道余额不足/);
  assert.equal(result.passed, false);
});

test("缺管理员凭据：run 仍执行，不碰 admin 接口，用事件拼简版轨迹，报告标「无轨迹」", async () => {
  const { backend, result, logs } = await setup(happyBackend(), { env: ENV_NO_ADMIN });
  assert.equal(result.passed, true);
  assert.equal(backend.callsTo("GET", /^\/admin\//).length, 0);
  assert.equal(backend.callsTo("POST", /^\/auth\/login$/).length, 1);
  const files = readDir(result.dir);
  assert.match(files["report.md"], /无轨迹（缺管理员凭据）/);
  assert.match(files["report.md"], /## 时间线/);
  assert.match(files["report.md"], /## 断言结论/);
  assert.match(files["trace.jsonl"].split("\n")[0], /"kind":"system"/);
  assert.doesNotMatch(files["trace.jsonl"], /"response"/);
  assert.equal(JSON.parse(files["meta.json"]).trace_complete, false);
  assert.ok("canvas.before.json" in files && "canvas.after.json" in files && "events.jsonl" in files);
  assert.ok(logs.some((l) => l.startsWith("warn|") && l.includes("HARNESS_ADMIN")));
});

test("管理员登录失败：同样降级为无轨迹，不让整批失败", async () => {
  const env = { ...ENV, HARNESS_ADMIN_PASSWORD: "wrong-password-xyz" };
  const { result } = await setup(happyBackend(), { env });
  assert.equal(result.passed, true);
  const report = fs.readFileSync(path.join(result.dir, "report.md"), "utf8");
  assert.match(report, /无轨迹（管理员登录失败）/);
  assert.doesNotMatch(report, /wrong-password-xyz/);
});

test("取轨迹被后端拒绝（轨迹已过期）：降级为无轨迹并写明原因", async () => {
  const { result } = await setup(happyBackend({ trace: undefined, traceError: { status: 410, code: 60021, msg: "轨迹已过期" } }));
  assert.equal(result.passed, true);
  const report = fs.readFileSync(path.join(result.dir, "report.md"), "utf8");
  assert.match(report, /无轨迹（轨迹已过期）/);
});

test("画布删除失败：报告里提示手动清理并给出画布 id，不影响用例结论", async () => {
  const { result, logs } = await setup(happyBackend({ deleteFails: true }));
  assert.equal(result.passed, true);
  assert.equal(result.canvasDeleted, false);
  const report = fs.readFileSync(path.join(result.dir, "report.md"), "utf8");
  assert.match(report, /## 运行备注/);
  assert.match(report, /测试画布删除失败，请手动清理：canvas=c1/);
  assert.ok(logs.some((l) => l.startsWith("warn|") && l.includes("canvas=c1")));
});

test("--keep 保留画布：不发删除请求", async () => {
  const { backend, result } = await setup(happyBackend(), { keep: true });
  assert.equal(backend.callsTo("DELETE", /^\/canvas\//).length, 0);
  assert.equal(result.canvasDeleted, false);
  assert.equal(result.canvasId, "c1");
});

test("generate 和 delete 审批按剧本应答；批准生成可带逐项决定", async () => {
  const gen = [{ type: "approval.created", data: approval("g1", "generate", { reason: "出 2 张关键帧", items: [{ node_id: "n1" }] }, { quote_credits: 24 }) }];
  const del = [{ type: "approval.created", data: approval("d1", "delete", { reason: "清理", node_ids: ["n2"], labels: ["旧"] }) }];
  const { backend, result } = await setup(
    happyBackend({ batches: [runningBatch, gen, del, finishBatch()] }),
    { caseOver: { budget: 40, answers: [{ match: { kind: "generate" }, answer: { decision: "approve", items: [{ index: 0, approve: true, count: 2 }] } }, { match: { kind: "delete" }, answer: { decision: "reject" } }] } },
  );
  assert.equal(result.passed, true);
  assert.deepEqual(backend.state.decisions, [
    { id: "g1", body: { decision: "approve", items: [{ index: 0, approve: true, count: 2 }] } },
    { id: "d1", body: { decision: "reject" } },
  ]);
  assert.equal(backend.callsTo("POST", /\/runs$/)[0].body.budget_credits, 40);
});

test("选模型的提问：$first_model 解析成提问里的第一个模型 key", async () => {
  const ask = [{ type: "approval.created", data: approval("m1", "ask", { question: "图片用哪个模型？", kind: "model", model_kind: "image", models: [{ key: "img-a", name: "A" }] }) }];
  const { backend, result } = await setup(happyBackend({ batches: [runningBatch, ask, finishBatch()] }), { caseOver: { answers: [{ match: { kind: "ask", ask_kind: "model" }, answer: { answer: "$first_model" } }] } });
  assert.equal(result.passed, true);
  assert.deepEqual(backend.state.decisions[0].body, { decision: "approve", answer: "img-a" });
});

test("应答请求被后端拒绝：取消运行并判失败（不是卡死）", async () => {
  const { backend, result } = await setup(happyBackend({ decisionError: "审批已处理或已过期" }));
  assert.equal(result.passed, false);
  assert.equal(backend.state.cancelCount, 1);
  const f = result.results.find((r) => r.type === "harness.run_error");
  assert.match(f.got, /审批已处理或已过期/);
});

test("事件接口偶发报错：重试后继续；连续失败才放弃", async () => {
  const { result } = await setup(happyBackend({ eventsError: 2 }));
  assert.equal(result.passed, true);
  const bad = await setup(happyBackend({ eventsError: 99 }));
  assert.equal(bad.result.passed, false);
  assert.ok(bad.result.results.some((r) => r.type === "harness.run_error"));
  assert.equal(bad.backend.state.canvasDeleted, true, "出错也要清理画布");
});

test("开跑被拒绝（同一画布已有运行）：抛出带原因的错误，画布照样清理", async () => {
  const backend = createFakeBackend(happyBackend({ startRunError: "这张画布上已有运行" }));
  const clock = createFakeClock();
  const session = await openSession({ env: ENV, fetch: backend.fetch, log: () => {}, clock });
  const outRoot = fs.mkdtempSync(path.join(os.tmpdir(), "hn-run-"));
  const result = await runCase({ def: baseCase(), file: "x", fixture: FIXTURE, session, outRoot, keep: false, index: 1, total: 1, clock, log: () => {}, pollMs: 1000, taskPollMs: 5000 });
  assert.equal(result.passed, false);
  assert.match(result.results.find((r) => r.type === "harness.run_error").got, /这张画布上已有运行/);
  assert.equal(backend.state.canvasDeleted, true);
});

test("wait_tasks：终态后轮询画布里的任务，直到全部结束，再把产物算进 nodes_have_output", async () => {
  const withTask = canvas([...goodStoryboard().nodes.map((n) => (n.id === "g1i" ? { ...n, data: { ...n.data, taskId: "tk1", status: "running" } } : n))], goodStoryboard().edges);
  const { backend, result, clock } = await setup(
    {
      batches: [runningBatch, finishBatch()],
      finalCanvas: withTask,
      trace: fakeTraceBundle(),
      tasks: { tk1: { node_id: "g1i", steps: [{ status: "running", outputs: [] }, { status: "running", outputs: [] }, { status: "succeeded", outputs: [{ url: "u" }] }] } },
    },
    { caseOver: { wait_tasks: true, expect: [{ type: "run_status", in: ["succeeded"] }, { type: "nodes_have_output", kinds: ["image"] }] } },
  );
  assert.equal(backend.callsTo("GET", /^\/generation-tasks\/tk1$/).length, 3);
  // 画布里只有 g1i 绑了任务，其余两个图片节点没有产物 → 断言应指出它们，而 g1i 被任务产物算上
  const r = result.results.find((x) => x.type === "nodes_have_output");
  assert.equal(r.ok, false);
  assert.doesNotMatch(r.evidence.join("\n"), /g1i/);
  assert.match(r.evidence.join("\n"), /g2i/);
  assert.ok(fs.existsSync(path.join(result.dir, "tasks.json")));
  assert.ok(clock.now() > Date.UTC(2026, 9, 20, 8, 0, 9), "按 5 秒间隔等待");
});

test("wait_tasks：任务一直不结束，到 timeout_sec 判失败", async () => {
  const withTask = canvas([node("img", "image", 0, 0, { prompt: "x", data: { taskId: "tk1", status: "running" } })]);
  const { result } = await setup(
    { batches: [runningBatch, finishBatch()], finalCanvas: withTask, trace: fakeTraceBundle(), tasks: { tk1: { node_id: "img", steps: [{ status: "running", outputs: [] }] } } },
    { caseOver: { wait_tasks: true, timeout_sec: 60, expect: [{ type: "run_status", in: ["succeeded"] }] } },
  );
  assert.equal(result.passed, false);
  assert.match(result.results.find((r) => r.type === "harness.timeout").got, /生成任务/);
});

test("画布内容是字符串形式的 JSON 也能读", async () => {
  const { result } = await setup(happyBackend({ finalCanvasAsString: true }));
  assert.equal(result.passed, true);
});

test("密钥不进任何输出文件，也不进日志", async () => {
  const cases = [
    await setup(happyBackend()),
    await setup(happyBackend({ trace: undefined, traceError: { msg: "轨迹已过期" } })),
    await setup(happyBackend(), { env: ENV_NO_ADMIN }),
    await setup(happyBackend({ batches: [runningBatch, askBatch("a9", "背景音乐要吗？")] })),
  ];
  for (const { result, logs } of cases) {
    const blob = Object.values(readDir(result.dir)).join("\n") + "\n" + logs.join("\n");
    for (const s of SECRETS) assert.ok(!blob.includes(s), `输出里出现了密钥 ${s}`);
  }
});

test("openSession：用户登录失败直接抛 HarnessError，信息里没有密码", async () => {
  const backend = createFakeBackend({});
  const env = { ...ENV, HARNESS_PASSWORD: "totally-wrong-pass" };
  await assert.rejects(
    () => openSession({ env, fetch: backend.fetch, log: () => {}, clock: createFakeClock() }),
    (e) => e instanceof HarnessError && /登录失败/.test(e.message) && !e.message.includes("totally-wrong-pass"),
  );
});

test("openSession：缺必需环境变量，一次列出全部缺的变量名", async () => {
  await assert.rejects(
    () => openSession({ env: { HARNESS_BASE_URL: BASE }, fetch: async () => { throw new Error("不该联网"); }, log: () => {}, clock: createFakeClock() }),
    (e) => e instanceof HarnessError && /HARNESS_EMAIL/.test(e.message) && /HARNESS_PASSWORD/.test(e.message),
  );
});

test("openSession：网络不通给出能看懂的提示", async () => {
  const backend = createFakeBackend({ networkDown: true });
  await assert.rejects(() => openSession({ env: ENV, fetch: backend.fetch, log: () => {}, clock: createFakeClock() }), /无法连接/);
});

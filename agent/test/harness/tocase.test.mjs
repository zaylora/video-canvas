import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { main } from "../../harness/cli.mjs";
import { answersFromEvents } from "../../harness/tocase.mjs";
import { loadCases, loadFixture, validateCase } from "../../harness/cases.mjs";
import { runAssertions } from "../../harness/assertions.mjs";
import { deriveRun } from "../../harness/events.mjs";
import { ev, approval, canvas, group, node } from "./helpers.mjs";
import { tmpDir, writeBundle, onlineRunEvents, smallCanvas, toolEvents } from "./bundle-fixtures.mjs";

const RUN_ID = "aaaabbbbccccdddd1111222233334444";

function io(over = {}) {
  const out = [];
  const err = [];
  return {
    env: {},
    fetch: async () => {
      throw new Error("to-case 不该联网");
    },
    stdout: { write: (s) => out.push(s) },
    stderr: { write: (s) => err.push(s) },
    color: false,
    ...over,
    out,
    err,
    text: () => out.join("") + err.join(""),
  };
}

const read = (f) => JSON.parse(fs.readFileSync(f, "utf8"));

/** 造一个带完整线上运行的轨迹包，并给 to-case 一个临时的 harness 根目录（不碰仓库里的 cases-imported）。 */
function setup(opts = {}) {
  const bundle = writeBundle(tmpDir(), { events: onlineRunEvents(), before: smallCanvas(), after: smallCanvas(), ...opts });
  const root = tmpDir("hn-root-");
  return { bundle, root, caseFile: path.join(root, "cases-imported", `imported-aaaabbbb.case.json`), fixtureFile: path.join(root, "fixtures", "imported", "imported-aaaabbbb.canvas.json") };
}

// ---------- 应答还原 ----------

const find = (answers, pred) => answers.find(pred);

test("answersFromEvents：选项题 → ask + ask_kind=choice + 提问开头作 contains，回答取用户实际的", () => {
  const { answers } = answersFromEvents(onlineRunEvents());
  const a = find(answers, (x) => x.match.ask_kind === "choice");
  assert.deepEqual(a, { match: { kind: "ask", ask_kind: "choice", contains: "画幅用哪个" }, answer: { answer: "9:16" } });
});

test("answersFromEvents：选模型题 → 按 ask_kind=model 和 model_kind 匹配，回答是用户实际选的模型 key", () => {
  const { answers } = answersFromEvents(onlineRunEvents());
  const a = find(answers, (x) => x.match.ask_kind === "model");
  assert.deepEqual(a, { match: { kind: "ask", ask_kind: "model", model_kind: "image" }, answer: { answer: "img-a" } });
});

test("answersFromEvents：生成审批部分批准 → items 逐条还原（拒绝的条目 approve:false，减张数的带 count）", () => {
  const { answers } = answersFromEvents(onlineRunEvents());
  const a = find(answers, (x) => x.match.kind === "generate");
  assert.deepEqual(a, { match: { kind: "generate" }, answer: { decision: "approve", items: [{ index: 0, approve: true, count: 1 }, { index: 1, approve: false }, { index: 2, approve: true }] } });
});

test("answersFromEvents：生成审批整体批准 → 只写 decision: approve，不写 items", () => {
  const payload = { reason: "出图", items: [{ node_id: "n1", model_key: "m", price: 2, count: 2 }] };
  const events = [ev(1, "message.user", { text: "x" }), ev(2, "approval.created", approval("g1", "generate", payload)), ev(3, "approval.decided", approval("g1", "generate", payload, { status: "executed", quote_credits: 4, decision: { decision: "approve", items: [{ node_id: "n1", model_key: "m", price: 2, count: 2 }] } }))];
  assert.deepEqual(answersFromEvents(events).answers, [{ match: { kind: "generate" }, answer: { decision: "approve" } }]);
});

test("answersFromEvents：删除审批拒绝带理由；部分批准按 node_ids 还原 items", () => {
  const { answers } = answersFromEvents(onlineRunEvents());
  const del = answers.filter((x) => x.match.kind === "delete");
  assert.deepEqual(del[0], { match: { kind: "delete" }, answer: { decision: "reject", answer: "别删" } });
  assert.deepEqual(del[1], { match: { kind: "delete" }, answer: { decision: "approve", items: [{ index: 0, approve: true }, { index: 1, approve: false }, { index: 2, approve: true }] } });
});

test("answersFromEvents：按审批出现的顺序输出；没有决定的审批不生成应答并记入 notes", () => {
  const events = [ev(1, "message.user", { text: "x" }), ev(2, "approval.created", approval("a1", "ask", { question: "问一下？", kind: "choice", options: ["a"] })), ev(3, "run.status", { status: "interrupted" })];
  const r = answersFromEvents(events);
  assert.deepEqual(r.answers, []);
  assert.match(r.notes.join("\n"), /没有等到用户的决定/);
});

test("answersFromEvents：同一个 match 出现多次且回答不同时，后面的会被前面盖住，要在 notes 里提醒；完全相同的合并", () => {
  const same = onlineRunEvents().filter((e) => !["ap3", "ap4"].includes(e.data?.id)); // 去掉生成和第一条删除，只剩一条删除
  assert.equal(answersFromEvents(same).notes.filter((n) => /盖住|覆盖/.test(n)).length, 0);
  const r = answersFromEvents(onlineRunEvents());
  assert.match(r.notes.join("\n"), /delete.*(盖住|覆盖)/);
  const dup = [ev(1, "message.user", { text: "x" })];
  for (const id of ["d1", "d2"]) {
    const p = { reason: "r", node_ids: ["n1"], labels: ["a"] };
    dup.push(ev(dup.length + 1, "approval.created", approval(id, "delete", p)), ev(dup.length + 2, "approval.decided", approval(id, "delete", p, { status: "rejected", decision: { decision: "reject" } })));
  }
  const r2 = answersFromEvents(dup);
  assert.equal(r2.answers.length, 1, "相同的应答合并成一条");
  assert.equal(r2.notes.filter((n) => /盖住|覆盖/.test(n)).length, 0);
});

test("budget：已批准生成的报价总和，拒绝的不算，没有则 0", () => {
  assert.equal(answersFromEvents(onlineRunEvents()).budget, 8);
  const rejected = (() => {
    const p = { reason: "r", items: [{ node_id: "n1", price: 3, count: 1 }] };
    return [ev(1, "message.user", { text: "x" }), ev(2, "approval.created", approval("g1", "generate", p, { quote_credits: 3 })), ev(3, "approval.decided", approval("g1", "generate", p, { status: "rejected", decision: { decision: "reject" } }))];
  })();
  assert.equal(answersFromEvents(rejected).budget, 0);
  assert.equal(answersFromEvents([ev(1, "message.user", { text: "x" })]).budget, 0);
});

test("budget：决定事件里没有 quote_credits 时用批准条目的 价格×张数 补", () => {
  const p = { reason: "r", items: [{ node_id: "n1", price: 3, count: 2 }, { node_id: "n2", price: 5, count: 1 }] };
  const events = [ev(1, "message.user", { text: "x" }), ev(2, "approval.created", approval("g1", "generate", p)), ev(3, "approval.decided", approval("g1", "generate", p, { status: "executed", quote_credits: 0, decision: { decision: "approve", items: [{ node_id: "n1", price: 3, count: 2 }, { node_id: "n2", price: 5, count: 1 }] } }))];
  assert.equal(answersFromEvents(events).budget, 11);
});

// ---------- 命令 ----------

test("to-case：写出用例和 fixture，用例能被 cases.mjs 载入并通过校验，fixture 能被找到", async () => {
  const s = setup();
  const c = io({ harnessRoot: s.root });
  assert.equal(await main(["to-case", s.bundle], c), 0, c.text());
  assert.deepEqual(loadCases(path.join(s.root, "cases-imported")).map((x) => x.def.id), ["imported-aaaabbbb"]);
});

test("to-case：用例内容——id、message、mode、selection、answers、budget、expect、source", async () => {
  const s = setup();
  const c = io({ harnessRoot: s.root });
  assert.equal(await main(["to-case", s.bundle], c), 0, c.text());
  assert.ok(fs.existsSync(s.caseFile), `应写到 ${s.caseFile}`);
  assert.ok(fs.existsSync(s.fixtureFile));
  const def = read(s.caseFile);
  assert.equal(def.id, "imported-aaaabbbb");
  assert.equal(def.message, "把剧本拆成 3 个镜头，竖屏");
  assert.equal(def.mode, "storyboard");
  assert.deepEqual(def.selection, ["n_script"]);
  assert.equal(def.budget, 8);
  assert.equal(def.fixture, "fixtures/imported/imported-aaaabbbb.canvas.json");
  assert.deepEqual(def.source, { run_id: RUN_ID, exported_at: "2026-10-20T09:00:00Z", prompt_version: 9, status: "succeeded" });
  assert.equal(def.answers.length, 5);
  assert.deepEqual(def.expect.slice(0, 3), [{ type: "run_status", in: ["succeeded"] }, { type: "no_overlap" }, { type: "members_inside_group" }]);
  assert.deepEqual(def.expect[3], { type: "max_tool_errors", max: 2 });
  assert.deepEqual(validateCase(def), []);
  assert.deepEqual(read(s.fixtureFile), smallCanvas(), "fixture 就是 canvas.before.json");
});

test("to-case：写出的用例能被 loadCases / loadFixture 载入", async () => {
  const s = setup();
  await main(["to-case", s.bundle], io({ harnessRoot: s.root }));
  const list = loadCases(path.join(s.root, "cases-imported"));
  assert.equal(list.length, 1);
  assert.equal(list[0].def.id, "imported-aaaabbbb");
  assert.deepEqual(loadFixture(list[0].def, list[0].file), smallCanvas());
  // 写出的断言能在 before 画布上执行（语法没问题）
  const ctx = { canvas: smallCanvas(), run: deriveRun([], null), approvals: [], taskOutputs: {} };
  assert.equal(runAssertions(list[0].def.expect, ctx).length, 4);
});

test("max_tool_errors：取原运行报错数，至少 2", async () => {
  const mk = async (toolErrs) => {
    const events = [ev(1, "message.user", { text: "x", mode: "all", selection: [] })];
    let seq = 2;
    for (let i = 0; i < toolErrs; i++, seq += 2) events.push(...toolEvents(seq, `t${i}`, "canvas_apply_ops", { isError: true }));
    events.push(ev(seq, "run.status", { status: "succeeded" }));
    const s = setup({ events });
    await main(["to-case", s.bundle], io({ harnessRoot: s.root }));
    return read(s.caseFile).expect.find((e) => e.type === "max_tool_errors").max;
  };
  assert.equal(await mk(0), 2);
  assert.equal(await mk(2), 2);
  assert.equal(await mk(5), 5);
});

test("--id 和 --out：自定义用例 id 与输出文件；fixture 引用写成相对用例文件的路径", async () => {
  const s = setup();
  const outFile = path.join(tmpDir(), "my.case.json");
  const c = io({ harnessRoot: s.root });
  assert.equal(await main(["to-case", s.bundle, "--id", "my-case", "--out", outFile], c), 0, c.text());
  const def = read(outFile);
  assert.equal(def.id, "my-case");
  assert.ok(fs.existsSync(path.join(s.root, "fixtures", "imported", "my-case.canvas.json")));
  assert.ok(!fs.existsSync(path.join(s.root, "cases-imported")), "指定 --out 就不再写默认目录");
  const [loaded] = loadCases(outFile);
  assert.deepEqual(loadFixture(loaded.def, outFile), smallCanvas(), "fixture 引用要能从用例文件位置解析到");
});

test("id 不合法（大写、空格）：退出 2，提示规则，不写任何文件", async () => {
  const s = setup();
  const c = io({ harnessRoot: s.root });
  assert.equal(await main(["to-case", s.bundle, "--id", "Bad Id"], c), 2);
  assert.match(c.err.join(""), /id/);
  assert.ok(!fs.existsSync(path.join(s.root, "cases-imported")));
  assert.ok(!fs.existsSync(path.join(s.root, "fixtures")));
});

test("目录里没有 run id 时，id 退而用目录名的前 8 位", async () => {
  const bundle = writeBundle(path.join(tmpDir(), "abcdef0123456789"), { meta: { run_id: "" }, events: [ev(1, "message.user", { text: "x" }, null)], before: smallCanvas() });
  const root = tmpDir("hn-root-");
  assert.equal(await main(["to-case", bundle], io({ harnessRoot: root })), 0);
  assert.ok(fs.existsSync(path.join(root, "cases-imported", "imported-abcdef01.case.json")));
});

test("输出：提醒线上用户内容要脱敏、列出文件、应答条数、budget；批准了生成时提醒会花积分", async () => {
  const s = setup();
  const c = io({ harnessRoot: s.root });
  await main(["to-case", s.bundle], c);
  const t = c.text();
  assert.match(t, /线上用户内容.*脱敏/);
  assert.ok(t.includes("imported-aaaabbbb.case.json"));
  assert.ok(t.includes("imported-aaaabbbb.canvas.json"));
  assert.match(t, /5 条应答/);
  assert.match(t, /budget 8/);
  assert.match(t, /批准了生成.*积分/);
  assert.match(t, /\.gitignore/);
});

test("初始画布本身就违反默认断言（重叠）时给出警告，不算失败", async () => {
  const overlapping = canvas([node("a", "image", 0, 0), node("b", "image", 10, 10)]);
  const s = setup({ before: overlapping });
  const c = io({ harnessRoot: s.root });
  assert.equal(await main(["to-case", s.bundle], c), 0);
  assert.match(c.text(), /初始画布.*no_overlap/);
});

test("没有决定的审批：输出里说明哪几条没生成应答，用例仍写出", async () => {
  const events = [ev(1, "message.user", { text: "x", mode: "all" }), ev(2, "approval.created", approval("a1", "ask", { question: "问？", kind: "choice", options: ["a"] })), ev(3, "run.status", { status: "interrupted" })];
  const s = setup({ events });
  const c = io({ harnessRoot: s.root });
  assert.equal(await main(["to-case", s.bundle], c), 0);
  assert.match(c.text(), /没有等到用户的决定/);
  assert.deepEqual(read(s.caseFile).answers, []);
});

test("重复执行会覆盖同 id 的用例，不报错", async () => {
  const s = setup();
  assert.equal(await main(["to-case", s.bundle], io({ harnessRoot: s.root })), 0);
  assert.equal(await main(["to-case", s.bundle], io({ harnessRoot: s.root })), 0);
});

// ---------- 错误 ----------

test("错误：缺参数、目录不存在、缺 canvas.before.json、缺 events.jsonl、没有 message.user、坏 JSON，退出 2 加明确提示，没有堆栈", async () => {
  const root = tmpDir("hn-root-");
  const cases = [
    [["to-case"], /需要指定轨迹包目录/],
    [["to-case", path.join(tmpDir(), "nope")], /不存在/],
    [["to-case", writeBundle(tmpDir(), { events: onlineRunEvents(), before: null })], /canvas\.before\.json/],
    [["to-case", writeBundle(tmpDir(), { events: null, before: smallCanvas() })], /events\.jsonl/],
    [["to-case", writeBundle(tmpDir(), { events: [ev(1, "run.status", { status: "running" })], before: smallCanvas() })], /message\.user/],
    [["to-case", writeBundle(tmpDir(), { events: [ev(1, "message.user", { text: "   " })], before: smallCanvas() })], /message\.user.*text|text.*message\.user/],
    [["to-case", writeBundle(tmpDir(), { events: onlineRunEvents(), before: smallCanvas(), extra: { "canvas.before.json": "{坏的" } })], /canvas\.before\.json.*JSON/],
    [["to-case", writeBundle(tmpDir(), { events: onlineRunEvents(), before: smallCanvas(), extra: { "events.jsonl": "{\"a\":1}\n坏\n" } })], /events\.jsonl.*第 2 行/],
    [["to-case", writeBundle(tmpDir(), { events: onlineRunEvents(), before: smallCanvas(), extra: { "canvas.before.json": "[1,2]" } })], /canvas\.before\.json.*nodes/],
  ];
  for (const [argv, re] of cases) {
    const c = io({ harnessRoot: root });
    assert.equal(await main(argv, c), 2, argv.slice(0, 2).join(" "));
    assert.match(c.err.join(""), re, argv.slice(0, 2).join(" "));
    assert.doesNotMatch(c.text(), /\n\s+at .*\(/, "不该打印堆栈");
  }
  assert.ok(!fs.existsSync(path.join(root, "cases-imported")), "出错时不留半截文件");
});

// ---------- 仓库配置 ----------

test("根 .gitignore 忽略 cases-imported/ 与 fixtures/imported/（线上用户内容不入库）", () => {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
  const lines = fs.readFileSync(path.join(root, ".gitignore"), "utf8").split(/\r?\n/).map((l) => l.trim());
  assert.ok(lines.includes("/agent/harness/cases-imported/"), "缺 cases-imported");
  assert.ok(lines.includes("/agent/harness/fixtures/imported/"), "缺 fixtures/imported");
  assert.ok(lines.includes("/.harness/"), "原有的 /.harness/ 不能丢");
});

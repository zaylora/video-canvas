import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { main } from "../../harness/cli.mjs";
import { ev } from "./helpers.mjs";
import { tmpDir, writeBundle, toolEvents, chatSpan, toolSpan, assertionsOf, result } from "./bundle-fixtures.mjs";

function io(over = {}) {
  const out = [];
  const err = [];
  return {
    env: {},
    fetch: async () => {
      throw new Error("diff 不该联网");
    },
    stdout: { write: (s) => out.push(s) },
    stderr: { write: (s) => err.push(s) },
    color: false,
    ...over,
    out,
    err,
    stdoutText: () => out.join(""),
    text: () => out.join("") + err.join(""),
  };
}

const runEvents = ({ status = "succeeded", tools = [["canvas_apply_ops"]], credits = 0 } = {}) => {
  const rows = [ev(1, "message.user", { text: "拆分镜", mode: "storyboard", selection: [] }), ev(2, "run.status", { status: "running" })];
  let seq = 3;
  tools.forEach(([name, flag], i) => {
    rows.push(...toolEvents(seq, `t${i}`, name, { isError: flag === "err" }));
    seq += 2;
  });
  if (credits) rows.push(ev(seq++, "run.usage", { spent_credits: credits, budget_credits: 100 }));
  rows.push(ev(seq, "run.status", { status }));
  return rows;
};

const spansOf = (tools, chats = [[1000, 200]]) => [...chats.map(([i, o], k) => chatSpan(k + 1, i, o)), ...tools.map(([name], k) => toolSpan(chats.length + k + 1, name))];

test("退化：A 通过、B 没过的断言标「退化」，退出 1，Markdown 也写到 stdout", async () => {
  const a = writeBundle(tmpDir(), { assertions: assertionsOf([result("run_status", true), result("no_overlap", true), result("max_steps", false, "步数 50")]), events: runEvents(), spans: spansOf([["canvas_apply_ops"]]) });
  const b = writeBundle(tmpDir(), { meta: { prompt_version: 10 }, assertions: assertionsOf([result("run_status", true), result("no_overlap", false, "组 g1 和 g2 相交"), result("max_steps", true, "步数 3")]), events: runEvents(), spans: spansOf([["canvas_apply_ops"]]) });
  const c = io();
  assert.equal(await main(["diff", a, b], c), 1);
  const md = c.stdoutText();
  assert.match(md, /^# Harness 对比/);
  assert.match(md, /结论：有退化/);
  assert.match(md, /\| no_overlap \|[^\n]*\| 退化 \|/);
  assert.match(md, /\| max_steps \|[^\n]*\| 改进 \|/);
  assert.match(md, /\| run_status \|[^\n]*\| 持平 \|/);
  assert.match(md, /组 g1 和 g2 相交/, "退化断言要带 B 的实际值");
});

test("只有改进：退出 0，结论写无退化", async () => {
  const a = writeBundle(tmpDir(), { assertions: assertionsOf([result("no_overlap", false)]), events: runEvents(), spans: spansOf([]) });
  const b = writeBundle(tmpDir(), { assertions: assertionsOf([result("no_overlap", true)]), events: runEvents(), spans: spansOf([]) });
  const c = io();
  assert.equal(await main(["diff", a, b], c), 0);
  assert.match(c.stdoutText(), /结论：无退化/);
  assert.match(c.stdoutText(), /\| no_overlap \|[^\n]*\| 改进 \|/);
});

test("B 新增的失败断言（比如未预写的提问、超时）也算退化；只有一边有的断言单独标注", async () => {
  const a = writeBundle(tmpDir(), { assertions: assertionsOf([result("run_status", true)]), events: runEvents(), spans: spansOf([]) });
  const b = writeBundle(tmpDir(), { assertions: assertionsOf([result("run_status", true), result("harness.unscripted_question", false, "未预写的提问：画幅")]), events: runEvents(), spans: spansOf([]) });
  const c = io();
  assert.equal(await main(["diff", a, b], c), 1);
  assert.match(c.stdoutText(), /harness\.unscripted_question[^\n]*仅 B/);
});

test("概要：prompt_version、模型、终态、步数、工具报错、积分、token、耗时都有 A、B 和变化量", async () => {
  const a = writeBundle(tmpDir(), {
    meta: { prompt_version: 9, agent_model_key: "model-a", started_at: "2026-10-20T08:00:00Z", ended_at: "2026-10-20T08:00:41Z" },
    events: runEvents({ status: "succeeded", tools: [["canvas_apply_ops"], ["canvas_arrange", "err"], ["canvas_apply_ops", "err"], ["canvas_arrange"]], credits: 4 }),
    spans: spansOf([["canvas_apply_ops"]], [[1000, 200], [2000, 300]]),
  });
  const b = writeBundle(tmpDir(), {
    meta: { prompt_version: 10, agent_model_key: "model-b", started_at: "2026-10-20T08:00:00Z", ended_at: "2026-10-20T08:01:10Z", status: "step_limit" },
    events: runEvents({ status: "step_limit", tools: [["canvas_apply_ops"]], credits: 1 }),
    spans: spansOf([["canvas_apply_ops"]], [[500, 100]]),
  });
  const c = io();
  assert.equal(await main(["diff", a, b], c), 0, "没有断言就没有退化");
  const md = c.stdoutText();
  assert.match(md, /\| prompt_version \| 9 \| 10 \|/);
  assert.match(md, /\| 模型 \| model-a \| model-b \|/);
  assert.match(md, /\| 终态 \| succeeded \| step_limit \|/);
  assert.match(md, /\| 步数 \| 4 \| 1 \| -3 \|/);
  assert.match(md, /\| 工具报错数 \| 2 \| 0 \| -2 \|/);
  assert.match(md, /\| 积分 \| 4 \| 1 \| -3 \|/);
  assert.match(md, /\| token 入 \| 3000 \| 500 \| -2500 \|/);
  assert.match(md, /\| token 出 \| 500 \| 100 \| -400 \|/);
  assert.match(md, /\| 耗时 \| 41s \| 1m10s \| \+29s \|/);
});

test("步数不数嵌套的脚本调用（口径与 events.mjs 一致）", async () => {
  const events = [ev(1, "message.user", { text: "x" }), ...toolEvents(2, "outer", "run_script"), ...toolEvents(4, "inner", "canvas_apply_ops", { parent: "outer" }), ev(6, "run.status", { status: "succeeded" })];
  const a = writeBundle(tmpDir(), { events, spans: spansOf([]) });
  const b = writeBundle(tmpDir(), { events, spans: spansOf([]) });
  const c = io();
  await main(["diff", a, b], c);
  assert.match(c.stdoutText(), /\| 步数 \| 1 \| 1 \| 0 \|/);
});

test("没有 assertions.json（线上轨迹包）：跳过断言一节并注明，不算退化", async () => {
  const a = writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([]) });
  const b = writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([]) });
  const c = io();
  assert.equal(await main(["diff", a, b], c), 0);
  assert.match(c.stdoutText(), /## 断言对比\n[\s\S]*A、B 都没有 assertions\.json/);
  const d = writeBundle(tmpDir(), { assertions: assertionsOf([result("run_status", true)]), events: runEvents(), spans: spansOf([]) });
  const c2 = io();
  assert.equal(await main(["diff", a, d], c2), 0);
  assert.match(c2.stdoutText(), /A 没有 assertions\.json/);
});

test("工具调用：A、B 各调用了哪些工具几次，顺序不同时指出分叉位置", async () => {
  const a = writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([["canvas_read"], ["canvas_apply_ops"], ["canvas_apply_ops"], ["canvas_arrange"]]) });
  const b = writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([["canvas_read"], ["canvas_arrange"], ["ask_user"]]) });
  const c = io();
  await main(["diff", a, b], c);
  const md = c.stdoutText();
  assert.match(md, /\| canvas_apply_ops \| 2 \| 0 \| -2 \|/);
  assert.match(md, /\| canvas_arrange \| 1 \| 1 \| 0 \|/);
  assert.match(md, /\| ask_user \| 0 \| 1 \| \+1 \|/);
  assert.match(md, /顺序：不同，第 2 步起分叉（A：canvas_apply_ops，B：canvas_arrange）/);
  const c2 = io();
  await main(["diff", a, a], c2);
  assert.match(c2.stdoutText(), /顺序：相同/);
});

test("轨迹缺失时（没有 trace.jsonl）用事件里的工具名，token 写「-」", async () => {
  const a = writeBundle(tmpDir(), { events: runEvents({ tools: [["canvas_read"]] }), spans: null });
  const b = writeBundle(tmpDir(), { events: runEvents({ tools: [["canvas_read"]] }), spans: null });
  const c = io();
  assert.equal(await main(["diff", a, b], c), 0);
  assert.match(c.stdoutText(), /\| canvas_read \| 1 \| 1 \| 0 \|/);
  assert.match(c.stdoutText(), /\| token 入 \| - \| - \|/);
});

test("harness 运行目录优先用 events.pulled.jsonl（与 report 命令同一口径）", async () => {
  const pulled = runEvents({ tools: [["a"], ["b"]] }).map((e) => JSON.stringify(e)).join("\n") + "\n";
  const a = writeBundle(tmpDir(), { events: runEvents({ tools: [["a"]] }), spans: spansOf([]), extra: { "events.pulled.jsonl": pulled } });
  const b = writeBundle(tmpDir(), { events: runEvents({ tools: [["a"]] }), spans: spansOf([]) });
  const c = io();
  await main(["diff", a, b], c);
  assert.match(c.stdoutText(), /\| 步数 \| 2 \| 1 \| -1 \|/);
});

test("--out 把 Markdown 写进文件，stdout 仍然有", async () => {
  const a = writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([]) });
  const b = writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([]) });
  const out = path.join(tmpDir(), "diff.md");
  const c = io();
  assert.equal(await main(["diff", a, b, "--out", out], c), 0);
  assert.equal(fs.readFileSync(out, "utf8"), c.stdoutText());
  assert.match(c.err.join(""), /已写入/);
});

// ---------- 整批目录 ----------

const summaryOf = (cases) => ({ batch_id: "20261020-080000", base_url: "http://x", repeat: 1, max_credits: 200, total_budget: 0, all_passed: cases.every((c) => c.passed === c.total), cases });
const caseRow = (id, passed, total, dir, extra = {}) => ({ id, title: "", budget: 0, passed, total, pass_rate: passed / total, avg_steps: 5, avg_credits: 2, avg_duration_ms: 10000, runs: [{ dir, passed: passed === total, status: "succeeded", steps: 5, credits: 2, duration_ms: 10000 }], ...extra });

function makeBatch(rows) {
  const root = tmpDir("hn-batch-");
  const entries = rows.map((r) => {
    const dir = path.join(root, `20261020-080000-${r.id}`);
    writeBundle(dir, { meta: { harness_case: r.id }, assertions: assertionsOf(r.results ?? [result("run_status", r.passed)], { case_id: r.id }), events: runEvents(), spans: spansOf([["canvas_read"]]) });
    return caseRow(r.id, r.passed ? 1 : 0, 1, r.absDir === false ? "gone" : dir);
  });
  fs.writeFileSync(path.join(root, "summary.json"), JSON.stringify(summaryOf(entries), null, 2));
  return root;
}

test("整批目录：按用例 id 配对，给总览（通过数变化），有退化退出 1", async () => {
  const a = makeBatch([{ id: "case-1", passed: true }, { id: "case-2", passed: true }, { id: "case-3", passed: false }]);
  const b = makeBatch([{ id: "case-1", passed: true }, { id: "case-2", passed: false }, { id: "case-3", passed: true }]);
  const c = io();
  assert.equal(await main(["diff", a, b], c), 1);
  const md = c.stdoutText();
  assert.match(md, /^# Harness 批次对比/);
  assert.match(md, /通过用例数：A 2\/3 → B 2\/3/);
  assert.match(md, /\| case-1 \| 1\/1 \| 1\/1 \| 持平 \|/);
  assert.match(md, /\| case-2 \| 1\/1 \| 0\/1 \| 退化 \|/);
  assert.match(md, /\| case-3 \| 0\/1 \| 1\/1 \| 改进 \|/);
  assert.match(md, /## 用例 case-2[\s\S]*### 断言对比[\s\S]*退化/);
});

test("整批目录：只有一边有的用例单独列出；全部持平退出 0", async () => {
  const a = makeBatch([{ id: "case-1", passed: true }, { id: "only-a", passed: true }]);
  const b = makeBatch([{ id: "case-1", passed: true }, { id: "only-b", passed: true }]);
  const c = io();
  assert.equal(await main(["diff", a, b], c), 0);
  assert.match(c.stdoutText(), /\| only-a \|[^\n]*仅 A 有/);
  assert.match(c.stdoutText(), /\| only-b \|[^\n]*仅 B 有/);
});

test("整批目录：repeat 的通过率下降算退化（2/3 → 1/3）", async () => {
  const mk = (passed) => {
    const root = tmpDir("hn-batch-");
    const dirs = [1, 2, 3].map((n) => writeBundle(path.join(root, `r${n}`), { events: runEvents(), spans: spansOf([]), assertions: assertionsOf([result("run_status", true)]) }));
    fs.writeFileSync(path.join(root, "summary.json"), JSON.stringify(summaryOf([{ ...caseRow("flaky", passed, 3, dirs[0]), runs: dirs.map((d) => ({ dir: d, passed: true })) }])));
    return root;
  };
  const c = io();
  assert.equal(await main(["diff", mk(2), mk(1)], c), 1);
  assert.match(c.stdoutText(), /\| flaky \| 2\/3 \| 1\/3 \| 退化 \|/);
});

test("整批目录：summary 里的绝对路径失效（目录被挪动）时，回退到批次目录下同名子目录", async () => {
  const a = makeBatch([{ id: "case-1", passed: true, absDir: false }]);
  const sub = fs.readdirSync(a).find((n) => n !== "summary.json");
  const s = JSON.parse(fs.readFileSync(path.join(a, "summary.json"), "utf8"));
  s.cases[0].runs[0].dir = path.join("Z:\\gone", sub);
  fs.writeFileSync(path.join(a, "summary.json"), JSON.stringify(s));
  const b = makeBatch([{ id: "case-1", passed: true }]);
  const c = io();
  assert.equal(await main(["diff", a, b], c), 0);
  assert.match(c.stdoutText(), /## 用例 case-1[\s\S]*### 概要/);
});

test("一边是整批目录、一边是单次运行：用法错误，退出 2", async () => {
  const batch = makeBatch([{ id: "case-1", passed: true }]);
  const run = writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([]) });
  const c = io();
  assert.equal(await main(["diff", batch, run], c), 2);
  assert.match(c.text(), /整批目录/);
});

// ---------- 错误 ----------

test("错误：缺参数、目录不存在、缺关键文件、坏 JSON，都是退出 2 加明确提示，没有堆栈", async () => {
  const good = writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([]) });
  const cases = [
    [["diff"], /需要两个目录/],
    [["diff", good], /需要两个目录/],
    [["diff", good, path.join(tmpDir(), "nope")], /不存在/],
    [["diff", good, writeBundle(tmpDir(), { meta: null, events: runEvents(), spans: spansOf([]) })], /meta\.json/],
    [["diff", good, writeBundle(tmpDir(), { events: null, spans: null })], /events\.jsonl.*trace\.jsonl|trace\.jsonl.*events\.jsonl/],
    [["diff", good, tmpDir()], /meta\.json/],
    [["diff", good, writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([]), extra: { "meta.json": "{坏的" } })], /meta\.json.*JSON/],
    [["diff", good, writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([]), extra: { "events.jsonl": "{\"seq\":1}\n不是json\n" } })], /events\.jsonl.*第 2 行/],
    [["diff", good, writeBundle(tmpDir(), { events: runEvents(), spans: spansOf([]), extra: { "assertions.json": "[]" } })], /assertions\.json/],
  ];
  for (const [argv, re] of cases) {
    const c = io();
    assert.equal(await main(argv, c), 2, argv.join(" "));
    assert.match(c.err.join(""), re, argv.join(" "));
    assert.doesNotMatch(c.text(), /\n\s+at .*\(/, "不该打印堆栈");
    assert.equal(c.stdoutText(), "", "出错时不输出半截报告");
  }
});

test("--help 不再标二期未实现，写明 diff 与 to-case 的用法", async () => {
  const c = io();
  assert.equal(await main(["--help"], c), 0);
  const t = c.stdoutText();
  assert.doesNotMatch(t, /二期，未实现/);
  assert.match(t, /diff <目录A> <目录B>/);
  assert.match(t, /to-case <轨迹包目录>/);
  assert.match(t, /--id/);
});

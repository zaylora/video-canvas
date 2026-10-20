import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { main } from "../../harness/cli.mjs";
import { BASE, createFakeBackend, createFakeClock, fakeTraceBundle } from "./fake-backend.mjs";
import { goodStoryboard, canvas, group, node, approval } from "./helpers.mjs";

const ENV = { HARNESS_BASE_URL: BASE, HARNESS_EMAIL: "harness@example.com", HARNESS_PASSWORD: "pw-harness-secret", HARNESS_ADMIN_EMAIL: "admin@example.com", HARNESS_ADMIN_PASSWORD: "pw-admin-secret" };

function io(over = {}) {
  const out = [];
  const err = [];
  return {
    env: ENV,
    fetch: async () => {
      throw new Error("不该联网");
    },
    stdout: { write: (s) => out.push(s) },
    stderr: { write: (s) => err.push(s) },
    clock: createFakeClock(),
    color: false,
    pollMs: 1000,
    taskPollMs: 5000,
    ...over,
    out,
    err,
    text: () => out.join("") + err.join(""),
  };
}

const tmp = () => fs.mkdtempSync(path.join(os.tmpdir(), "hn-cli-"));

const goodCase = (over = {}) => ({
  id: "demo",
  message: "拆分镜",
  budget: 0,
  answers: [{ match: { kind: "ask" }, answer: { answer: "9:16" } }],
  expect: [{ type: "run_status", in: ["succeeded"] }, { type: "no_overlap" }, { type: "count", of: "group", min: 3, max: 3 }],
  ...over,
});

function writeCase(def, dir = tmp()) {
  fs.writeFileSync(path.join(dir, `${def.id}.case.json`), JSON.stringify(def));
  return dir;
}

const running = [{ type: "run.status", data: { status: "running" } }];
const ask = [{ type: "approval.created", data: approval("a1", "ask", { question: "画幅用哪个？", kind: "choice", options: ["16:9", "9:16"] }) }];
const finish = [{ type: "tool.end", data: { id: "t", name: "canvas_apply_ops", is_error: false, summary: "ok" } }, { type: "run.status", data: { status: "succeeded" } }];

test("--help：列出命令，diff / to-case 标二期未实现，列出环境变量", async () => {
  const c = io();
  const code = await main(["--help"], c);
  assert.equal(code, 0);
  const t = c.text();
  for (const w of ["run", "report", "diff", "to-case", "--repeat", "--max-credits", "--keep", "--out", "HARNESS_BASE_URL", "HARNESS_ADMIN_EMAIL"]) assert.ok(t.includes(w), `帮助里缺 ${w}`);
  assert.match(t, /diff.*二期，未实现/);
  assert.match(t, /to-case.*二期，未实现/);
});

test("没有命令 / 未知命令：打印帮助并退出 2", async () => {
  const a = io();
  assert.equal(await main([], a), 2);
  assert.match(a.text(), /用法/);
  const b = io();
  assert.equal(await main(["dance"], b), 2);
  assert.match(b.text(), /未知命令/);
});

test("diff / to-case 提示二期未实现，退出 2", async () => {
  for (const cmd of ["diff", "to-case"]) {
    const c = io();
    assert.equal(await main([cmd, "x"], c), 2);
    assert.match(c.text(), /二期，未实现/);
  }
});

test("run 参数错误：--repeat 不是正整数、--max-credits 不是数字、没给路径", async () => {
  const dir = writeCase(goodCase());
  assert.equal(await main(["run", dir, "--repeat", "0"], io()), 2);
  assert.equal(await main(["run", dir, "--repeat", "abc"], io()), 2);
  assert.equal(await main(["run", dir, "--max-credits", "-5"], io()), 2);
  const c = io();
  assert.equal(await main(["run"], c), 2);
  assert.match(c.text(), /用例/);
});

test("预算闸门：超过 --max-credits 就拒绝，打印每个用例的明细，不联网", async () => {
  const dir = tmp();
  writeCase(goodCase({ id: "cheap", budget: 5 }), dir);
  writeCase(goodCase({ id: "dear", budget: 40 }), dir);
  const c = io();
  const code = await main(["run", dir, "--max-credits", "50", "--repeat", "2"], c);
  assert.equal(code, 2);
  const t = c.text();
  assert.match(t, /预算闸门/);
  assert.match(t, /cheap\s+5 × 2 = 10/);
  assert.match(t, /dear\s+40 × 2 = 80/);
  assert.match(t, /90/);
  assert.match(t, /不开跑/);
});

test("默认上限 200：cases/ 与 cases-paid/ 的示例都放行（缺凭据时才停在环境变量这一步）", async () => {
  const c = io({ env: {} });
  const code = await main(["run", path.resolve(import.meta.dirname, "../../harness/cases")], c);
  assert.equal(code, 2);
  assert.match(c.text(), /预算检查 \d+ \/ 200 积分/);
  assert.match(c.text(), /HARNESS_BASE_URL/);
});

test("默认目录 cases/ 里的示例用例都不花钱：--max-credits 0 也放行（缺凭据时才停在环境变量这一步）", async () => {
  const c = io({ env: {} });
  const code = await main(["run", path.resolve(import.meta.dirname, "../../harness/cases"), "--max-credits", "0"], c);
  assert.equal(code, 2);
  assert.match(c.text(), /预算检查 0 \/ 0 积分/);
  assert.match(c.text(), /HARNESS_BASE_URL/);
});

test("--max-credits 0 + 花钱的示例（cases-paid/）：被闸门拦下并列出明细", async () => {
  const c = io();
  const code = await main(["run", path.resolve(import.meta.dirname, "../../harness/cases-paid"), "--max-credits", "0"], c);
  assert.equal(code, 2);
  assert.match(c.text(), /预算闸门/);
  assert.match(c.text(), /keyframe-generate-2\s+40 × 1 = 40/);
});

test("缺环境变量：一次列出所有缺的变量名，不打印任何值", async () => {
  const dir = writeCase(goodCase());
  const c = io({ env: { HARNESS_BASE_URL: BASE, HARNESS_PASSWORD: "pw-secret-value" } });
  assert.equal(await main(["run", dir], c), 2);
  const t = c.text();
  assert.match(t, /HARNESS_EMAIL/);
  assert.doesNotMatch(t, /pw-secret-value/);
});

test("用例文件有问题：把所有问题说完再退出 2", async () => {
  const dir = writeCase({ id: "bad", message: "x", expect: [{ type: "no_overlap" }] });
  const c = io();
  assert.equal(await main(["run", dir], c), 2);
  assert.match(c.text(), /bad\.case\.json.*budget/);
});

test("run 全部通过：退出 0，终端有进度，写 summary.md / summary.json 和每个用例的目录", async () => {
  const dir = writeCase(goodCase());
  const out = path.join(tmp(), "runs");
  const backend = createFakeBackend({ batches: [running, ask, finish], finalCanvas: goodStoryboard(), trace: fakeTraceBundle() });
  const c = io({ fetch: backend.fetch });
  const code = await main(["run", dir, "--out", out], c);
  assert.equal(code, 0, c.text());
  const t = c.text();
  assert.match(t, /✓ 预算检查 0 \/ 200 积分/);
  assert.match(t, /按剧本应答/);
  assert.match(t, /✓ 断言 3\/3/);
  assert.match(t, /通过 1\/1/);

  const dirs = fs.readdirSync(out).filter((f) => fs.statSync(path.join(out, f)).isDirectory());
  assert.equal(dirs.length, 1);
  assert.match(dirs[0], /^\d{8}-\d{6}-demo$/);

  const summary = JSON.parse(fs.readFileSync(path.join(out, "summary.json"), "utf8"));
  assert.equal(summary.cases.length, 1);
  assert.equal(summary.cases[0].id, "demo");
  assert.equal(summary.cases[0].passed, 1);
  assert.equal(summary.cases[0].total, 1);
  assert.equal(summary.cases[0].pass_rate, 1);
  assert.equal(typeof summary.cases[0].avg_steps, "number");
  assert.equal(summary.all_passed, true);
  const md = fs.readFileSync(path.join(out, "summary.md"), "utf8");
  assert.match(md, /\| 用例 \| 是否通过 \| 通过率 \| 步数 \| 积分 \| 耗时 \|/);
  assert.match(md, /\| demo \| 通过 \| 1\/1 \| /);
});

test("run 有失败：退出 1，summary 里写失败原因", async () => {
  const dir = writeCase(goodCase());
  const out = path.join(tmp(), "runs");
  const bad = canvas([group("g1", 0, 0, 500, 400), group("g2", 100, 0, 500, 400), node("a", "script", 0, 900)]);
  const backend = createFakeBackend({ batches: [running, ask, finish], finalCanvas: bad, trace: fakeTraceBundle() });
  const c = io({ fetch: backend.fetch });
  assert.equal(await main(["run", dir, "--out", out], c), 1);
  assert.match(c.text(), /✗ 断言 1\/3/);
  const md = fs.readFileSync(path.join(out, "summary.md"), "utf8");
  assert.match(md, /\| demo \| 失败 \| 0\/1 \|/);
  assert.match(md, /## 失败详情/);
  assert.match(md, /no_overlap/);
});

test("--repeat N：每次一个目录，报告里给通过率", async () => {
  const dir = writeCase(goodCase());
  const out = path.join(tmp(), "runs");
  const bad = canvas([group("g1", 0, 0, 500, 400), group("g2", 100, 0, 500, 400), node("a", "script", 0, 900)]);
  const backend = createFakeBackend({ batches: [running, ask, finish], finalCanvas: [goodStoryboard(), bad], trace: fakeTraceBundle() });
  const c = io({ fetch: backend.fetch });
  const code = await main(["run", dir, "--out", out, "--repeat", "2"], c);
  assert.equal(code, 1);
  const dirs = fs.readdirSync(out).filter((f) => fs.statSync(path.join(out, f)).isDirectory()).sort();
  assert.equal(dirs.length, 2);
  assert.match(dirs[0], /-demo-1$/);
  assert.match(dirs[1], /-demo-2$/);
  const summary = JSON.parse(fs.readFileSync(path.join(out, "summary.json"), "utf8"));
  assert.equal(summary.cases[0].passed, 1);
  assert.equal(summary.cases[0].total, 2);
  assert.equal(summary.cases[0].pass_rate, 0.5);
  assert.match(fs.readFileSync(path.join(out, "summary.md"), "utf8"), /\| demo \| 失败 \| 1\/2 \|/);
  assert.match(c.text(), /通过率 1\/2/);
});

test("--keep 保留测试画布", async () => {
  const dir = writeCase(goodCase());
  const backend = createFakeBackend({ batches: [running, ask, finish], finalCanvas: goodStoryboard(), trace: fakeTraceBundle() });
  const c = io({ fetch: backend.fetch });
  assert.equal(await main(["run", dir, "--out", path.join(tmp(), "runs"), "--keep"], c), 0);
  assert.equal(backend.callsTo("DELETE", /canvas/).length, 0);
});

test("缺管理员凭据：run 仍然执行，终端提示「无轨迹」", async () => {
  const dir = writeCase(goodCase());
  const backend = createFakeBackend({ batches: [running, ask, finish], finalCanvas: goodStoryboard() });
  const env = { HARNESS_BASE_URL: BASE, HARNESS_EMAIL: ENV.HARNESS_EMAIL, HARNESS_PASSWORD: ENV.HARNESS_PASSWORD };
  const c = io({ fetch: backend.fetch, env });
  assert.equal(await main(["run", dir, "--out", path.join(tmp(), "runs")], c), 0);
  assert.match(c.text(), /HARNESS_ADMIN/);
  assert.match(c.text(), /无轨迹/);
});

test("登录失败：退出 2，错误里没有密码", async () => {
  const dir = writeCase(goodCase());
  const backend = createFakeBackend({});
  const c = io({ fetch: backend.fetch, env: { ...ENV, HARNESS_PASSWORD: "nope-nope-nope" } });
  assert.equal(await main(["run", dir], c), 2);
  assert.match(c.text(), /登录失败/);
  assert.doesNotMatch(c.text(), /nope-nope-nope/);
});

test("默认输出目录在仓库根的 .harness/runs（和 .gitignore 对得上），不是 agent/ 下", async () => {
  const { defaultOutDir } = await import("../../harness/cli.mjs");
  const repoRoot = path.resolve(import.meta.dirname, "../../..");
  assert.equal(path.resolve(defaultOutDir()), path.join(repoRoot, ".harness", "runs"));
  assert.match(fs.readFileSync(path.join(repoRoot, ".gitignore"), "utf8"), /^\/\.harness\/$/m);
});

test("report 命令：改了断言后根据目录里的文件重新执行，断言段整段替换，退出码跟结论走", async () => {
  const dir = writeCase(goodCase());
  const out = path.join(tmp(), "runs");
  const backend = createFakeBackend({ batches: [running, ask, finish], finalCanvas: goodStoryboard(), trace: fakeTraceBundle() });
  assert.equal(await main(["run", dir, "--out", out], io({ fetch: backend.fetch })), 0);
  const runDir = path.join(out, fs.readdirSync(out).find((f) => /-demo$/.test(f)));

  // 1. 不改：重跑仍然通过，断言段只有一份
  const c1 = io();
  assert.equal(await main(["report", runDir], c1), 0, c1.text());
  assert.equal((fs.readFileSync(path.join(runDir, "report.md"), "utf8").match(/## 断言结论/g) ?? []).length, 1);

  // 2. 换一份更严的用例（--case）：要 4 个组，现在只有 3 个 → 失败
  const strict = writeCase(goodCase({ expect: [{ type: "count", of: "group", min: 4 }] }));
  const c2 = io();
  assert.equal(await main(["report", runDir, "--case", path.join(strict, "demo.case.json")], c2), 1);
  const md = fs.readFileSync(path.join(runDir, "report.md"), "utf8");
  assert.equal((md.match(/## 断言结论/g) ?? []).length, 1);
  assert.match(md, /失败（0\/1 条断言通过）/);
  assert.match(md, /后端生成/, "后端生成的内容仍在");
  const a = JSON.parse(fs.readFileSync(path.join(runDir, "assertions.json"), "utf8"));
  assert.equal(a.passed, false);
  assert.deepEqual(a.expect, [{ type: "count", of: "group", min: 4 }]);
});

test("report 命令：目录不存在、没有记录断言也没指定 --case 时给出明确错误", async () => {
  const c = io();
  assert.equal(await main(["report", path.join(tmp(), "nope")], c), 2);
  assert.match(c.text(), /不存在/);
  const empty = tmp();
  const c2 = io();
  assert.equal(await main(["report", empty], c2), 2);
  assert.match(c2.text(), /assertions\.json|--case/);
});

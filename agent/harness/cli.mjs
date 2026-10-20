#!/usr/bin/env node
// Agent 调优 Harness 命令行入口。用法见 --help 和 README.md。
//
//   node harness/cli.mjs run <用例文件或目录> [--repeat N] [--max-credits N] [--keep] [--out DIR]
//   node harness/cli.mjs report <运行目录> [--case 用例文件]
//
// 退出码：0 全部通过；1 有用例失败；2 用法 / 配置错误、环境变量缺失、被预算闸门拒绝。

import fs from "node:fs";
import path from "node:path";
import { parseArgs } from "node:util";
import { fileURLToPath } from "node:url";
import { loadCases, loadFixture, CaseError } from "./cases.mjs";
import { planBudget, formatBudgetPlan, DEFAULT_MAX_CREDITS } from "./budget.mjs";
import { openSession, runCase, HarnessError, defaultClock, stamp } from "./runner.mjs";
import { refreshReport, formatDuration } from "./report.mjs";

const HARNESS_ROOT = path.dirname(fileURLToPath(import.meta.url));

/** 默认输出目录：仓库根的 .harness/runs（根 .gitignore 只忽略 /.harness/，所以不能跟着 cwd 落在 agent/ 下）。 */
export const defaultOutDir = () => path.resolve(HARNESS_ROOT, "..", "..", ".harness", "runs");

const HELP = `Agent 调优 Harness：用 JSON 用例走真实服务跑 Agent，做画布结构断言，出给 AI 编码助手读的报告。

用法：node harness/cli.mjs <命令> [选项]

命令：
  run <用例文件或目录> [--repeat N] [--max-credits N] [--keep] [--out DIR]
        跑用例。目录会递归找 *.case.json。
          --repeat N        每个用例跑 N 次，报告里给通过率（默认 1；每次都花钱）
          --max-credits N   整批预算上限，所有用例 budget × repeat 求和超过就拒绝开跑（默认 ${DEFAULT_MAX_CREDITS}）
          --keep            保留测试画布（默认跑完删除）
          --out DIR         输出根目录（默认仓库根的 .harness/runs）
  report <运行目录> [--case 用例文件]
        改了断言之后，根据运行目录里的轨迹重新执行断言并刷新 report.md 的断言部分。
        --case 指定用新的用例文件里的 expect，不指定就用目录里 assertions.json 记下的。
  diff <运行目录A> <运行目录B>      二期，未实现
  to-case <轨迹包目录>              二期，未实现

环境变量（只从环境变量读，不写进仓库或输出目录）：
  HARNESS_BASE_URL         后端地址，如 http://localhost:8080（必需）
  HARNESS_EMAIL            跑用例的专用测试账号的用户名或邮箱（必需，作为登录用户名）
  HARNESS_PASSWORD         该账号的密码（必需）
  HARNESS_ADMIN_EMAIL      同一环境的管理员账号，用来取轨迹包；缺少时 run 仍执行，报告标「无轨迹」
  HARNESS_ADMIN_PASSWORD   管理员密码
  HARNESS_USERNAME / HARNESS_ADMIN_USERNAME   可选：登录用户名与邮箱不同时单独指定

退出码：0 全部通过；1 有用例失败；2 用法 / 配置错误、环境变量缺失、被预算闸门拒绝。`;

// ---------- 终端输出 ----------

const COLORS = { ok: "32", err: "31", warn: "33", dim: "90", ask: "36", info: "" };

function makeLogger(io) {
  const paint = (cls, text) => (io.color && COLORS[cls] ? `\x1b[${COLORS[cls]}m${text}\x1b[0m` : text);
  const log = (cls, text) => io.stdout.write(paint(cls, text) + "\n");
  const errLog = (text) => io.stderr.write(paint("err", text) + "\n");
  return { log, errLog };
}

// ---------- run ----------

const summarizeCase = (def, runs) => {
  const passed = runs.filter((r) => r.passed).length;
  const avg = (f) => Math.round(runs.reduce((n, r) => n + f(r), 0) / runs.length);
  return {
    id: def.id,
    title: def.title ?? "",
    budget: def.budget,
    passed,
    total: runs.length,
    pass_rate: passed / runs.length,
    avg_steps: avg((r) => r.steps),
    avg_credits: avg((r) => r.credits),
    avg_duration_ms: avg((r) => r.durationMs),
    runs: runs.map((r) => ({ dir: r.dir, passed: r.passed, status: r.status, steps: r.steps, credits: r.credits, duration_ms: r.durationMs, run_id: r.runId, canvas_id: r.canvasId, canvas_deleted: r.canvasDeleted, no_trace: r.noTrace, fail_reasons: r.failReasons })),
  };
};

function renderSummaryMd(batch) {
  const rows = batch.cases.map((c) => `| ${c.id} | ${c.passed === c.total ? "通过" : "失败"} | ${c.passed}/${c.total} | ${c.avg_steps} | ${c.avg_credits} | ${formatDuration(c.avg_duration_ms)} |`);
  const lines = [
    "# Harness 批次摘要",
    `> 批次 ${batch.batch_id} · 后端 ${batch.base_url} · 预算 ${batch.total_budget} / ${batch.max_credits} 积分 · repeat ${batch.repeat} · ${batch.all_passed ? "全部通过" : "有失败"}`,
    "",
    "| 用例 | 是否通过 | 通过率 | 步数 | 积分 | 耗时 |",
    "| --- | --- | --- | --- | --- | --- |",
    ...rows,
    "",
  ];
  const failed = batch.cases.filter((c) => c.passed !== c.total);
  if (failed.length) {
    lines.push("## 失败详情", "");
    for (const c of failed) {
      lines.push(`### ${c.id}（通过率 ${c.passed}/${c.total}）`, "");
      c.runs.forEach((r, i) => {
        if (r.passed) return;
        lines.push(`- 第 ${i + 1} 次：${r.dir}`);
        for (const reason of r.fail_reasons) lines.push(`  - ${reason}`);
      });
      lines.push("");
    }
  }
  lines.push("每个用例的细节看对应目录里的 report.md（先看断言结论和失败断言，再看时间线）。", "");
  return lines.join("\n");
}

async function cmdRun(values, positionals, io, { log, errLog }) {
  const target = positionals[1];
  if (!target) {
    errLog("run 需要指定用例文件或目录：node harness/cli.mjs run <用例文件或目录>");
    return 2;
  }
  const repeat = Number(values.repeat ?? 1);
  if (!Number.isInteger(repeat) || repeat < 1) {
    errLog(`--repeat 必须是正整数，收到 ${values.repeat}`);
    return 2;
  }
  const maxCredits = Number(values["max-credits"] ?? DEFAULT_MAX_CREDITS);
  if (!Number.isFinite(maxCredits) || maxCredits < 0) {
    errLog(`--max-credits 必须是 ≥ 0 的数字，收到 ${values["max-credits"]}`);
    return 2;
  }

  let cases;
  try {
    cases = loadCases(target);
  } catch (e) {
    if (e instanceof CaseError) {
      errLog(e.message);
      return 2;
    }
    throw e;
  }
  // 预算闸门在联网之前：被拒绝时不需要任何凭据
  const plan = planBudget(cases.map((c) => c.def), { repeat, maxCredits });
  if (!plan.ok) {
    errLog("✗ " + formatBudgetPlan(plan));
    return 2;
  }
  log("ok", `✓ ${formatBudgetPlan(plan)}`);

  let fixtures;
  try {
    fixtures = cases.map((c) => loadFixture(c.def, c.file));
  } catch (e) {
    errLog(e.message);
    return 2;
  }

  let session;
  try {
    session = await openSession({ env: io.env, fetch: io.fetch, log, clock: io.clock });
  } catch (e) {
    if (e instanceof HarnessError) {
      errLog(`✗ ${e.message}`);
      return 2;
    }
    throw e;
  }

  const outRoot = path.resolve(io.cwd ?? process.cwd(), values.out ?? defaultOutDir());
  const batchId = stamp(io.clock.now());
  const summaries = [];
  for (let i = 0; i < cases.length; i++) {
    const { def, file } = cases[i];
    const runs = [];
    for (let n = 1; n <= repeat; n++) {
      log("info", `\n▶ ${def.id}${repeat > 1 ? `（第 ${n}/${repeat} 次）` : ""}  ${def.title ?? ""}`);
      try {
        runs.push(await runCase({ def, file, fixture: fixtures[i], session, outRoot, keep: Boolean(values.keep), index: n, total: repeat, clock: io.clock, log, pollMs: io.pollMs, taskPollMs: io.taskPollMs }));
      } catch (e) {
        errLog(`✗ 用例 ${def.id} 执行时出现意外错误：${e.stack ?? e.message}`);
        runs.push({ passed: false, steps: 0, credits: 0, durationMs: 0, dir: "", status: "unknown", runId: null, canvasId: null, canvasDeleted: false, noTrace: true, failReasons: [`意外错误：${e.message}`] });
      }
    }
    const s = summarizeCase(def, runs);
    summaries.push(s);
    if (repeat > 1) log(s.passed === s.total ? "ok" : "err", `  通过率 ${s.passed}/${s.total}`);
  }

  const batch = { batch_id: batchId, base_url: session.baseUrl, repeat, max_credits: maxCredits, total_budget: plan.total, all_passed: summaries.every((c) => c.passed === c.total), cases: summaries };
  fs.mkdirSync(outRoot, { recursive: true });
  fs.writeFileSync(path.join(outRoot, "summary.json"), JSON.stringify(batch, null, 2) + "\n");
  fs.writeFileSync(path.join(outRoot, "summary.md"), renderSummaryMd(batch));

  const passedCases = summaries.filter((c) => c.passed === c.total).length;
  log("info", "");
  for (const c of summaries) log(c.passed === c.total ? "ok" : "err", `${c.passed === c.total ? "✓" : "✗"} ${c.id}  通过率 ${c.passed}/${c.total}  步数 ${c.avg_steps}  积分 ${c.avg_credits}  耗时 ${formatDuration(c.avg_duration_ms)}`);
  log(batch.all_passed ? "ok" : "err", `${batch.all_passed ? "✓" : "✗"} 通过 ${passedCases}/${summaries.length} 个用例`);
  log("dim", `→ 摘要 ${path.join(outRoot, "summary.md")}`);
  return batch.all_passed ? 0 : 1;
}

// ---------- report ----------

function cmdReport(values, positionals, io, { log, errLog }) {
  const dir = positionals[1];
  if (!dir) {
    errLog("report 需要指定运行目录：node harness/cli.mjs report <运行目录>");
    return 2;
  }
  const full = path.resolve(io.cwd ?? process.cwd(), dir);
  if (!fs.existsSync(full) || !fs.statSync(full).isDirectory()) {
    errLog(`运行目录不存在：${full}`);
    return 2;
  }
  let expect;
  if (values.case) {
    try {
      const list = loadCases(path.resolve(io.cwd ?? process.cwd(), values.case));
      expect = list[0].def.expect;
    } catch (e) {
      errLog(e.message);
      return 2;
    }
  }
  let res;
  try {
    res = refreshReport(full, { expect });
  } catch (e) {
    errLog(`✗ ${e.message}`);
    return 2;
  }
  for (const r of res.results) log(r.ok ? "ok" : "err", `${r.ok ? "✓" : "✗"} ${r.type}：${r.got}`);
  log(res.ok ? "ok" : "err", `${res.ok ? "✓" : "✗"} 断言 ${res.passed}/${res.total}`);
  log("dim", `→ 已刷新 ${path.join(full, "report.md")} 和 assertions.json`);
  return res.ok ? 0 : 1;
}

// ---------- 入口 ----------

/** 命令行主函数。io 可注入（测试用）：env、fetch、stdout / stderr（有 write 方法）、clock、cwd、color、pollMs、taskPollMs。 */
export async function main(argv, io = {}) {
  io = { env: process.env, fetch: globalThis.fetch, stdout: process.stdout, stderr: process.stderr, clock: defaultClock, cwd: process.cwd(), color: Boolean(process.stdout.isTTY) && !process.env.NO_COLOR, pollMs: 1000, taskPollMs: 5000, ...io };
  const out = makeLogger(io);
  let parsed;
  try {
    parsed = parseArgs({
      args: argv,
      allowPositionals: true,
      options: { help: { type: "boolean", short: "h" }, repeat: { type: "string" }, "max-credits": { type: "string" }, keep: { type: "boolean" }, out: { type: "string" }, case: { type: "string" } },
    });
  } catch (e) {
    out.errLog(`参数错误：${e.message}\n\n${HELP}`);
    return 2;
  }
  const { values, positionals } = parsed;
  if (values.help) {
    io.stdout.write(HELP + "\n");
    return 0;
  }
  const cmd = positionals[0];
  if (!cmd) {
    io.stderr.write(HELP + "\n");
    return 2;
  }
  switch (cmd) {
    case "run":
      return cmdRun(values, positionals, io, out);
    case "report":
      return cmdReport(values, positionals, io, out);
    case "diff":
    case "to-case":
      out.errLog(`${cmd}：二期，未实现。一期请用 run / report；线上轨迹包解压到 .harness/imports/<runId>/ 后直接读 report.md（见 harness/README.md）。`);
      return 2;
    default:
      out.errLog(`未知命令：${cmd}\n\n${HELP}`);
      return 2;
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).then(
    (code) => {
      process.exitCode = code;
    },
    (e) => {
      console.error(e);
      process.exitCode = 2;
    },
  );
}

// diff 命令：对比两次运行（harness run 的输出目录，或后台下载解压的轨迹包目录），输出 Markdown。
// 指标口径与已有的保持一致：步数、工具报错、积分、终态来自事件（events.mjs 的 deriveRun，和 report / 断言同一套）；
// token 入/出取 trace.jsonl 里所有 chat span 的 usage 之和（与后端 report.md 的概要行一致）；耗时取 meta.json 的开始与结束时间。
// 两边都是整批目录（含 summary.json）时，按用例 id 配对，给总览，再逐个用例对比。
//
// 退化的定义：A 通过的断言 B 没过；B 新增的失败断言（如未预写的提问、超时）；整批对比时用例通过率下降。有退化退出码 1。

import fs from "node:fs";
import path from "node:path";
import { deriveRun } from "./events.mjs";
import { formatDuration } from "./report.mjs";
import { BundleError, requireDir, readJsonFile, readJsonlFile, findEventsFile, runIdFilter } from "./bundle.mjs";

const cell = (v) => String(v ?? "-").replace(/\s+/g, " ").replace(/\|/g, "\\|");
const isObject = (v) => v && typeof v === "object" && !Array.isArray(v);

// ---------- 读一次运行 ----------

/** 读单次运行目录。缺关键文件、JSON 坏了抛 BundleError。 */
export function loadRun(dir) {
  requireDir(dir, "目录");
  const metaFile = path.join(dir, "meta.json");
  const eventsFile = findEventsFile(dir);
  const traceFile = path.join(dir, "trace.jsonl");
  const hasTrace = fs.existsSync(traceFile);
  const missing = [];
  if (!fs.existsSync(metaFile)) missing.push("meta.json");
  if (!eventsFile && !hasTrace) missing.push("events.jsonl 或 trace.jsonl（至少要有一个）");
  if (missing.length) throw new BundleError(`${dir} 不是有效的运行目录，缺少：${missing.join("、")}`);

  const meta = readJsonFile(metaFile);
  if (!isObject(meta)) throw new BundleError(`${metaFile} 格式不对：应是 JSON 对象`);
  const events = eventsFile ? readJsonlFile(eventsFile) : [];
  const spans = hasTrace ? readJsonlFile(traceFile).filter((r) => r && r.kind !== "system") : null;

  let assertions = null;
  const assertionsFile = path.join(dir, "assertions.json");
  if (fs.existsSync(assertionsFile)) {
    assertions = readJsonFile(assertionsFile);
    if (!isObject(assertions) || !Array.isArray(assertions.results)) throw new BundleError(`${assertionsFile} 格式不对：应是含 results 数组的 JSON 对象`);
  }
  return { dir, meta, events, spans, assertions };
}

/** 一次运行的指标。取不到的写 null（输出时是「-」）。 */
export function metricsOf(run) {
  const { meta, events, spans } = run;
  const derived = deriveRun(events, runIdFilter(events, meta.run_id));
  const hasStatus = events.some((e) => e.type === "run.status");
  const toolNamesFromSpans = spans ? spans.filter((s) => s.kind === "execute_tool").map((s) => s.name) : [];
  const toolNamesFromEvents = events.filter((e) => e.type === "tool.start").map((e) => e.data?.name);
  const hasToolEvents = events.some((e) => e.type === "tool.end");

  let steps = derived.steps;
  let toolErrors = derived.tool_errors;
  if (!hasToolEvents && spans) {
    // 事件里没有工具记录（极少见）：退而用轨迹的 span，口径一样——不带 parent 的算步，status=error 的算报错
    const tools = spans.filter((s) => s.kind === "execute_tool");
    steps = tools.filter((s) => !s.parent_span_id).length;
    toolErrors = tools.filter((s) => s.status === "error" || s.is_error).length;
  }
  let tokensIn = null;
  let tokensOut = null;
  if (spans) {
    tokensIn = 0;
    tokensOut = 0;
    for (const s of spans) if (s.kind === "chat" && s.usage) [tokensIn, tokensOut] = [tokensIn + (s.usage.input || 0), tokensOut + (s.usage.output || 0)];
  }
  const started = Date.parse(meta.started_at);
  const ended = Date.parse(meta.ended_at);
  return {
    run_id: meta.run_id || "",
    prompt_version: meta.prompt_version ?? null,
    model: meta.agent_model_key || "",
    status: (hasStatus ? derived.status : meta.status) || "",
    steps,
    tool_errors: toolErrors,
    credits: derived.spent_credits,
    tokens_in: tokensIn,
    tokens_out: tokensOut,
    duration_ms: Number.isFinite(started) && Number.isFinite(ended) ? Math.max(0, ended - started) : null,
    tools: toolNamesFromSpans.length ? toolNamesFromSpans : toolNamesFromEvents,
  };
}

// ---------- 渲染一次对比 ----------

const sign = (n) => (n > 0 ? `+${n}` : String(n));
const delta = (a, b) => (a == null || b == null ? "-" : sign(b - a));
const deltaTime = (a, b) => {
  if (a == null || b == null) return "-";
  const d = b - a;
  return d === 0 ? "0s" : `${d > 0 ? "+" : "-"}${formatDuration(Math.abs(d))}`;
};
const same = (a, b) => (String(a ?? "") === String(b ?? "") ? "相同" : "变了");

/** 把连续重复的工具名折叠成「a×2」，太长就截断。 */
function compressSequence(names, limit = 30) {
  const out = [];
  for (const n of names) {
    const last = out[out.length - 1];
    if (last && last.name === n) last.n++;
    else out.push({ name: n, n: 1 });
  }
  const text = out.slice(0, limit).map((x) => (x.n > 1 ? `${x.name}×${x.n}` : x.name)).join(" → ");
  return out.length > limit ? `${text} → …（共 ${names.length} 次）` : text || "（无）";
}

/** 断言逐条对比。key 是「类型#第几次出现」，让同一类型出现多次的断言也能配对。 */
function compareAssertions(a, b) {
  const keyed = (list) => {
    const seen = new Map();
    return list.map((r) => {
      const n = (seen.get(r.type) ?? 0) + 1;
      seen.set(r.type, n);
      return { key: `${r.type}#${n}`, n, r };
    });
  };
  const ka = keyed(a);
  const kb = keyed(b);
  const mapB = new Map(kb.map((x) => [x.key, x]));
  const mapA = new Map(ka.map((x) => [x.key, x]));
  const multi = new Set([...ka, ...kb].filter((x) => x.n > 1).map((x) => x.r.type));
  const rows = [];
  for (const x of [...ka, ...kb.filter((y) => !mapA.has(y.key))]) {
    const ra = mapA.get(x.key)?.r;
    const rb = mapB.get(x.key)?.r;
    const name = multi.has(x.r.type) ? x.key : x.r.type;
    let verdict;
    let kind;
    if (ra && rb) {
      if (ra.ok && !rb.ok) [verdict, kind] = ["退化", "regress"];
      else if (!ra.ok && rb.ok) [verdict, kind] = ["改进", "improve"];
      else [verdict, kind] = [ra.ok ? "持平" : "同样失败", "same"];
    } else if (rb) [verdict, kind] = rb.ok ? ["仅 B（通过）", "same"] : ["仅 B（失败，计入退化）", "regress"];
    else [verdict, kind] = [ra.ok ? "仅 A（通过）" : "仅 A（失败）", "same"];
    rows.push({ name, ra, rb, verdict, kind });
  }
  return rows;
}

const okText = (r) => (r ? (r.ok ? "通过" : "失败") : "-");

/**
 * 渲染一次运行的对比（概要、断言、工具调用）。level 是章节标题的级别（单次对比用 2，整批里嵌套用 3）。
 * 返回 {lines, regress, improve, compared}：regress / improve 是断言退化、改进条数，compared 表示是否有断言可比。
 */
export function renderRunDiff(runA, runB, { level = 2 } = {}) {
  const ma = metricsOf(runA);
  const mb = metricsOf(runB);
  const h = (text) => `${"#".repeat(level)} ${text}`;
  const lines = [];

  lines.push(h("概要"), "", "| 项 | A | B | 变化 |", "| --- | --- | --- | --- |");
  lines.push(`| run id | ${cell(ma.run_id)} | ${cell(mb.run_id)} | ${same(ma.run_id, mb.run_id)} |`);
  lines.push(`| prompt_version | ${cell(ma.prompt_version)} | ${cell(mb.prompt_version)} | ${same(ma.prompt_version, mb.prompt_version)} |`);
  lines.push(`| 模型 | ${cell(ma.model)} | ${cell(mb.model)} | ${same(ma.model, mb.model)} |`);
  lines.push(`| 终态 | ${cell(ma.status)} | ${cell(mb.status)} | ${same(ma.status, mb.status)} |`);
  lines.push(`| 步数 | ${ma.steps} | ${mb.steps} | ${delta(ma.steps, mb.steps)} |`);
  lines.push(`| 工具报错数 | ${ma.tool_errors} | ${mb.tool_errors} | ${delta(ma.tool_errors, mb.tool_errors)} |`);
  lines.push(`| 积分 | ${ma.credits} | ${mb.credits} | ${delta(ma.credits, mb.credits)} |`);
  lines.push(`| token 入 | ${ma.tokens_in ?? "-"} | ${mb.tokens_in ?? "-"} | ${delta(ma.tokens_in, mb.tokens_in)} |`);
  lines.push(`| token 出 | ${ma.tokens_out ?? "-"} | ${mb.tokens_out ?? "-"} | ${delta(ma.tokens_out, mb.tokens_out)} |`);
  lines.push(`| 耗时 | ${ma.duration_ms == null ? "-" : formatDuration(ma.duration_ms)} | ${mb.duration_ms == null ? "-" : formatDuration(mb.duration_ms)} | ${deltaTime(ma.duration_ms, mb.duration_ms)} |`, "");

  // 断言
  lines.push(h("断言对比"), "");
  let regress = 0;
  let improve = 0;
  let compared = false;
  if (!runA.assertions || !runB.assertions) {
    const none = !runA.assertions && !runB.assertions ? "A、B 都没有" : !runA.assertions ? "A 没有" : "B 没有";
    lines.push(`${none} assertions.json（线上轨迹包不带断言；用例跑出来的目录才有），跳过断言对比。`, "");
  } else {
    compared = true;
    const rows = compareAssertions(runA.assertions.results, runB.assertions.results);
    lines.push("| 断言 | A | B | 结论 |", "| --- | --- | --- | --- |");
    for (const r of rows) lines.push(`| ${cell(r.name)} | ${okText(r.ra)} | ${okText(r.rb)} | ${r.verdict} |`);
    lines.push("");
    regress = rows.filter((r) => r.kind === "regress").length;
    improve = rows.filter((r) => r.kind === "improve").length;
    const bad = rows.filter((r) => r.kind === "regress");
    if (bad.length) {
      lines.push("**退化详情**", "");
      for (const r of bad) {
        lines.push(`- ${r.name}：A「${cell(r.ra?.got ?? "无此断言")}」→ B「${cell(r.rb.got)}」`);
        lines.push(`  - 期望：${cell(r.rb.expected)}`);
        for (const e of r.rb.evidence ?? []) lines.push(`  - 证据：${cell(e)}`);
      }
      lines.push("");
    }
  }

  // 工具调用
  lines.push(h("工具调用"), "");
  const names = [...new Set([...ma.tools, ...mb.tools])];
  const count = (list, n) => list.filter((x) => x === n).length;
  if (names.length === 0) lines.push("A、B 都没有记录到工具调用。", "");
  else {
    lines.push("| 工具 | A | B | 变化 |", "| --- | --- | --- | --- |");
    for (const n of names) lines.push(`| ${cell(n)} | ${count(ma.tools, n)} | ${count(mb.tools, n)} | ${delta(count(ma.tools, n), count(mb.tools, n))} |`);
    lines.push("");
    let at = 0;
    while (at < ma.tools.length && at < mb.tools.length && ma.tools[at] === mb.tools[at]) at++;
    const identical = at === ma.tools.length && at === mb.tools.length;
    lines.push(identical ? "顺序：相同" : `顺序：不同，第 ${at + 1} 步起分叉（A：${ma.tools[at] ?? "（已结束）"}，B：${mb.tools[at] ?? "（已结束）"}）`, "");
    lines.push(`- A 的序列：${compressSequence(ma.tools)}`, `- B 的序列：${compressSequence(mb.tools)}`, "");
  }
  return { lines, regress, improve, compared };
}

const conclusion = ({ regress, improve, compared }, unit = "条断言") =>
  regress > 0 ? `有退化（${regress} ${unit}退化，${improve} ${unit}改进）` : compared ? `无退化（${improve} ${unit}改进）` : "无退化（没有断言可比，只对比了运行指标）";

// ---------- 整批目录 ----------

function loadBatch(dir) {
  const file = path.join(dir, "summary.json");
  const s = readJsonFile(file);
  if (!isObject(s) || !Array.isArray(s.cases)) throw new BundleError(`${file} 格式不对：应是含 cases 数组的 JSON 对象`);
  return s;
}

/** summary.json 里记的 run 目录是写 summary 时的绝对路径；目录被挪动过就回退到批次目录下的同名子目录。 */
function resolveRunDir(batchDir, run) {
  if (!run?.dir) return null;
  const candidates = [run.dir, path.join(batchDir, path.basename(run.dir))];
  return candidates.find((p) => fs.existsSync(p) && fs.statSync(p).isDirectory()) ?? null;
}

const rate = (c) => (c.total > 0 ? c.passed / c.total : 0);
const passText = (c) => (c ? `${c.passed}/${c.total}` : "-");
const passedCases = (cases) => cases.filter((c) => c.total > 0 && c.passed === c.total).length;

function diffBatches(dirA, dirB) {
  const sa = loadBatch(dirA);
  const sb = loadBatch(dirB);
  const byIdB = new Map(sb.cases.map((c) => [c.id, c]));
  const byIdA = new Map(sa.cases.map((c) => [c.id, c]));
  const ids = [...sa.cases.map((c) => c.id), ...sb.cases.filter((c) => !byIdA.has(c.id)).map((c) => c.id)];

  let regressCases = 0;
  let improveCases = 0;
  let regressAssertions = 0;
  const overview = ["| 用例 | A 通过 | B 通过 | 变化 | 步数（A→B） | 积分（A→B） | 耗时（A→B） |", "| --- | --- | --- | --- | --- | --- | --- |"];
  const details = [];
  const arrow = (a, b) => `${a ?? "-"}→${b ?? "-"}`;
  const dur = (c) => (c ? formatDuration(c.avg_duration_ms ?? 0) : "-");

  for (const id of ids) {
    const ca = byIdA.get(id);
    const cb = byIdB.get(id);
    let verdict;
    if (!ca) verdict = "仅 B 有";
    else if (!cb) verdict = "仅 A 有";
    else if (rate(cb) < rate(ca)) [verdict, regressCases] = ["退化", regressCases + 1];
    else if (rate(cb) > rate(ca)) [verdict, improveCases] = ["改进", improveCases + 1];
    else verdict = "持平";
    overview.push(`| ${cell(id)} | ${passText(ca)} | ${passText(cb)} | ${verdict} | ${arrow(ca?.avg_steps, cb?.avg_steps)} | ${arrow(ca?.avg_credits, cb?.avg_credits)} | ${dur(ca)}→${dur(cb)} |`);
    if (!ca || !cb) continue;

    details.push(`## 用例 ${id}`, "");
    const n = Math.min(ca.runs?.length ?? 0, cb.runs?.length ?? 0);
    if (n === 0) details.push("没有记录到运行目录，跳过细节。", "");
    for (let i = 0; i < n; i++) {
      const [dA, dB] = [resolveRunDir(dirA, ca.runs[i]), resolveRunDir(dirB, cb.runs[i])];
      if (n > 1) details.push(`**第 ${i + 1} 次**`, "");
      if (!dA || !dB) {
        details.push(`运行目录读不到（${!dA ? "A" : "B"}：${(!dA ? ca : cb).runs[i]?.dir ?? "未记录"}），跳过细节。`, "");
        continue;
      }
      try {
        const r = renderRunDiff(loadRun(dA), loadRun(dB), { level: 3 });
        details.push(...r.lines);
        regressAssertions += r.regress;
      } catch (e) {
        if (!(e instanceof BundleError)) throw e;
        details.push(`读运行目录失败，跳过细节：${e.message}`, "");
      }
    }
  }

  const regress = regressCases + regressAssertions;
  const head = [
    "# Harness 批次对比",
    `> A：${dirA}（批次 ${sa.batch_id ?? "?"}）`,
    `> B：${dirB}（批次 ${sb.batch_id ?? "?"}）`,
    `> 结论：${regress > 0 ? `有退化（${regressCases} 个用例通过率下降，${regressAssertions} 条断言退化）` : `无退化（${improveCases} 个用例通过率上升）`}`,
    `> 通过用例数：A ${passedCases(sa.cases)}/${sa.cases.length} → B ${passedCases(sb.cases)}/${sb.cases.length}`,
    "",
    "## 总览",
    "",
    ...overview,
    "",
  ];
  return { markdown: [...head, ...details].join("\n"), regressed: regress > 0 };
}

// ---------- 入口 ----------

/**
 * 对比两个目录。返回 {markdown, regressed}；目录不存在、缺关键文件、两边类型不一致抛 BundleError。
 * 两边都是整批目录（含 summary.json）就按用例 id 配对，否则都当单次运行目录。
 */
export function diffDirs(dirA, dirB) {
  requireDir(dirA, "A 目录");
  requireDir(dirB, "B 目录");
  const batchA = fs.existsSync(path.join(dirA, "summary.json"));
  const batchB = fs.existsSync(path.join(dirB, "summary.json"));
  if (batchA !== batchB) throw new BundleError(`${batchA ? "A" : "B"} 是整批目录（含 summary.json），${batchA ? "B" : "A"} 是单次运行目录，没法对比；请两边都给整批目录，或都给单次运行目录。`);
  if (batchA) return diffBatches(dirA, dirB);

  const runA = loadRun(dirA);
  const runB = loadRun(dirB);
  const r = renderRunDiff(runA, runB, { level: 2 });
  const md = ["# Harness 对比", `> A：${dirA}`, `> B：${dirB}`, `> 结论：${conclusion(r)}`, "", ...r.lines].join("\n");
  return { markdown: md, regressed: r.regress > 0 };
}

// 报告：后端生成的 report.md 顶部插入「断言结论」「失败断言」；缺少轨迹包时，用事件自己拼简版 trace.jsonl 和报告。
// report.md 的固定结构见设计文档 8.4（后端生成的版本是第 0、3、4、5 节，这里只负责第 1、2 节和简版）。

import fs from "node:fs";
import path from "node:path";
import { nodeSize } from "./size.mjs";
import { approvalFromEvent, deriveRun } from "./events.mjs";
import { runAssertions } from "./assertions.mjs";

const START = "<!-- harness:assertions:start -->";
const END = "<!-- harness:assertions:end -->";

const clip = (s, n) => {
  const chars = [...String(s ?? "")];
  return chars.length > n ? chars.slice(0, n).join("") + "…" : chars.join("");
};
const cell = (s) => clip(String(s ?? "").replace(/\s+/g, " "), 120).replace(/\|/g, "\\|");

export function formatDuration(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m${String(s % 60).padStart(2, "0")}s`;
}

// ---------- 简版轨迹（缺管理员凭据时） ----------

const APPROVAL_STATUS = { approved: "ok", partially_approved: "ok", rejected: "denied", expired: "aborted", pending: "pending" };

/**
 * 用事件拼简版 trace.jsonl 的 span。格式对齐设计文档 8.4（第一行 system，之后一行一个 span，seq 升序），
 * 但没有 request / response / args / result 原文。返回 {spans, jsonl}。
 */
export function buildTraceFromEvents(events, { runId } = {}) {
  const spans = [];
  const open = new Map(); // 工具调用 id → span
  const approvals = new Map(); // 审批 id → span
  const ms = (a, b) => Math.max(0, Date.parse(b) - Date.parse(a)) || 0;
  const add = (s) => {
    const span = { seq: spans.length + 1, parent_span_id: null, status: "ok", summary: "", started_at: null, duration_ms: 0, error_code: "", ...s };
    spans.push(span);
    return span;
  };
  let chats = 0;
  for (const e of events) {
    if (runId && e.run_id != null && e.run_id !== runId) continue;
    const d = e.data ?? {};
    if (e.type === "tool.start") {
      open.set(d.id, add({ span_id: `t:${d.id}`, parent_span_id: d.parent_id ? `t:${d.parent_id}` : null, kind: "execute_tool", name: d.name, status: "pending", started_at: e.created_at, tool_call_id: d.id }));
    } else if (e.type === "tool.end") {
      const s = open.get(d.id) ?? add({ span_id: `t:${d.id}`, parent_span_id: d.parent_id ? `t:${d.parent_id}` : null, kind: "execute_tool", name: d.name, started_at: e.created_at, tool_call_id: d.id });
      s.status = d.is_error ? "error" : "ok";
      s.summary = clip(d.summary, 200);
      s.duration_ms = ms(s.started_at, e.created_at);
      s.is_error = Boolean(d.is_error);
    } else if (e.type === "message.done") {
      const calls = Array.isArray(d.tool_calls) ? d.tool_calls.map((c) => c.name) : [];
      add({ span_id: `c${++chats}`, kind: "chat", name: "chat", summary: clip(calls.length ? `→ ${calls.join(", ")}` : d.text, 120), started_at: e.created_at, credits: d.credits ?? 0 });
    } else if (e.type === "approval.created") {
      const a = approvalFromEvent(e);
      approvals.set(a.id, add({ span_id: `a:${a.id}`, kind: "approval", name: a.kind, status: "pending", summary: clip(a.text || a.kind, 120), started_at: e.created_at, approval_kind: a.kind, ask_kind: a.ask_kind, question: a.question, quote: a.quote }));
    } else if (e.type === "approval.decided") {
      const s = approvals.get(d.id);
      if (!s) continue;
      s.status = APPROVAL_STATUS[d.status] ?? "ok";
      s.decision = d.decision?.decision ?? "";
      s.result = d.decision ?? null;
      s.duration_ms = ms(s.started_at, e.created_at);
    }
  }
  const head = { seq: 0, kind: "system", prompt_version: null, tools_hash: null, system_prompt: null, tools: [], note: "无轨迹：这是 harness 用事件拼出的简版，没有请求和响应原文" };
  return { spans, jsonl: [head, ...spans].map((x) => JSON.stringify(x)).join("\n") + "\n" };
}

// ---------- 画布概要 ----------

/** 组 → 成员的树，每个节点写种类、尺寸、是否有提示词。 */
export function canvasOutline(canvas, sizeOpts) {
  const nodes = Array.isArray(canvas?.nodes) ? canvas.nodes : [];
  if (nodes.length === 0) return "（画布为空）";
  const ids = new Set(nodes.map((n) => n.id));
  const has = (n) => [n.data?.prompt, n.data?.params?.prompt].some((p) => typeof p === "string" && p.trim() !== "");
  const line = (n, indent) => {
    const { w, h } = nodeSize(n, sizeOpts);
    if (n.type === "group") return `${indent}- ${n.id}（组）${w}×${h}`;
    return `${indent}- ${n.id}（${n.data?.kind ?? "?"}）${w}×${h}，${has(n) ? "有提示词" : "无提示词"}`;
  };
  const out = [];
  for (const g of nodes.filter((n) => n.type === "group")) {
    out.push(line(g, ""));
    for (const m of nodes.filter((n) => n.parentId === g.id)) out.push(line(m, "  "));
  }
  for (const n of nodes.filter((n) => n.type !== "group" && (!n.parentId || !ids.has(n.parentId)))) out.push(line(n, ""));
  return out.join("\n");
}

// ---------- 简版报告 ----------

function suspiciousSignals({ run, spans, noTraceReason, unscriptedQuestions = [] }) {
  const out = [];
  const reps = new Map();
  for (const s of spans) {
    if (s.kind === "execute_tool" && s.status === "error") {
      const key = `${s.name}「${s.summary}」`;
      reps.set(key, (reps.get(key) ?? 0) + 1);
    }
  }
  for (const [key, n] of reps) if (n >= 3) out.push(`同一条工具报错重复 ${n} 次：${key}`);
  if (run.status === "step_limit") out.push("达到步数上限");
  if (run.status === "budget_exhausted") out.push("本轮积分预算用尽");
  for (const q of unscriptedQuestions) out.push(`未预写的提问：${q}`);
  if (noTraceReason) out.push(`无轨迹（${noTraceReason}）：没有请求和响应原文，摘要压缩是否触发也无从得知`);
  return out;
}

/** 缺管理员凭据（或取轨迹失败）时的简版报告，结构与后端生成的一致，但没有请求和响应原文。 */
export function buildFallbackReport({ runId, message, run, spans, canvas, noTraceReason, unscriptedQuestions, durationMs }) {
  const signals = suspiciousSignals({ run, spans, noTraceReason, unscriptedQuestions });
  const lines = [
    `# Agent 运行报告 · run ${runId}`,
    `> 终态 ${run.status} · prompt 未知 · 模型 未知 · ${run.steps} 步 · 积分 ${run.spent_credits}${durationMs === undefined ? "" : ` · 耗时 ${formatDuration(durationMs)}`}`,
    `> 用户原话：${clip(message, 200)}`,
    ...(noTraceReason ? [`> ⚠ 无轨迹（${noTraceReason}）：下面的时间线由事件拼出，没有请求和响应原文`] : []),
    "",
    "## 可疑信号",
    ...(signals.length ? signals.map((s) => `- ${s}`) : ["- 无"]),
    "",
    "## 时间线",
    "| # | span | 名称 | 耗时 | 状态 | 摘要 |",
    "| --- | --- | --- | --- | --- | --- |",
    ...spans.map((s) => `| ${s.seq} | ${s.kind} | ${cell(s.name)} | ${s.duration_ms}ms | ${s.status} | ${cell(s.summary)} |`),
    "",
    "## 最终画布概要",
    canvasOutline(canvas),
    "",
  ];
  return lines.join("\n");
}

// ---------- 断言段 ----------

export function summarizeResults(results) {
  const passed = results.filter((r) => r.ok).length;
  return { passed, total: results.length, ok: passed === results.length };
}

/** 渲染「断言结论」「失败断言」（以及有的话「运行备注」）三节，包在标记里，方便重复生成时整段替换。 */
export function renderAssertionBlock({ results, notes = [] }) {
  const sum = summarizeResults(results);
  const failed = results.filter((r) => !r.ok);
  const out = [START, "## 断言结论", "", `${sum.ok ? "通过" : "失败"}（${sum.passed}/${sum.total} 条断言通过）`, ""];
  for (const r of results) out.push(`- ${r.ok ? "✓" : "✗"} ${r.type}：${r.got}`);
  out.push("", "## 失败断言", "");
  if (failed.length === 0) out.push("无");
  for (const r of failed) {
    out.push(`### ${r.type}`, "", `- 期望：${r.expected}`, `- 实际：${r.got}`);
    if (r.evidence?.length) {
      out.push("- 证据：");
      for (const e of r.evidence) out.push(`  - ${e}`);
    }
    if (r.suggestion) out.push("", "建议在用例的 answers 里补一条（回答按需要改）：", "", "```json", JSON.stringify(r.suggestion, null, 2), "```");
    out.push("");
  }
  if (notes.length) {
    out.push("## 运行备注", "");
    for (const n of notes) out.push(`- ${n}`);
  }
  out.push(END);
  return out.join("\n");
}

const STRIP = new RegExp(`\\n?${START}[\\s\\S]*?${END}\\n`, "g");

/** 把断言段插到标题和紧跟标题的引用概要之后；已有的断言段先删掉，重复调用结果不叠加。 */
export function applyAssertionBlock(md, block) {
  const text = md.replace(STRIP, "");
  const lines = text.split("\n");
  if (!lines[0]?.startsWith("# ")) return block + "\n" + text;
  let i = 1;
  while (i < lines.length && lines[i].startsWith(">")) i++;
  const offset = lines.slice(0, i).join("\n").length + 1; // 标题和概要行（含最后的换行）之后
  if (offset > text.length) return text + "\n" + block + "\n";
  return text.slice(0, offset) + "\n" + block + "\n" + text.slice(offset);
}

// ---------- report 命令：根据目录里的轨迹重新执行断言 ----------

const readJson = (file, fallback) => {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch {
    return fallback;
  }
};

/**
 * 根据运行目录里的文件重新执行断言并刷新 report.md 的断言段。
 * expect 来自 opts.expect（--case 指定的用例），否则取 assertions.json 里记下的。
 * 运行时发现的问题（未预写的提问、超时等，type 以 harness. 开头）原样保留。
 */
export function refreshReport(dir, { expect } = {}) {
  const prev = readJson(path.join(dir, "assertions.json"), null);
  const list = expect ?? prev?.expect;
  if (!Array.isArray(list)) throw new Error(`${dir} 里没有 assertions.json，也没指定 --case，不知道要执行哪些断言`);
  const canvas = readJson(path.join(dir, "canvas.final.json"), null) ?? readJson(path.join(dir, "canvas.after.json"), null);
  if (!canvas) throw new Error(`${dir} 里没有 canvas.final.json / canvas.after.json`);
  const eventsFile = ["events.pulled.jsonl", "events.jsonl"].map((f) => path.join(dir, f)).find((f) => fs.existsSync(f));
  const events = eventsFile ? fs.readFileSync(eventsFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : [];
  const meta = readJson(path.join(dir, "meta.json"), {});
  const run = deriveRun(events, meta.run_id);
  const taskOutputs = readJson(path.join(dir, "tasks.json"), {});
  const approvals = run.approvals;
  const results = [...runAssertions(list, { canvas, run, approvals, taskOutputs: Object.fromEntries(Object.entries(taskOutputs).map(([k, v]) => [k, v.outputs ?? 0])) }), ...(prev?.results ?? []).filter((r) => r.type.startsWith("harness."))];
  const notes = prev?.notes ?? [];
  const sum = summarizeResults(results);
  const reportFile = path.join(dir, "report.md");
  const md = fs.existsSync(reportFile) ? fs.readFileSync(reportFile, "utf8") : `# Agent 运行报告 · run ${meta.run_id ?? "?"}\n`;
  fs.writeFileSync(reportFile, applyAssertionBlock(md, renderAssertionBlock({ results, notes })));
  fs.writeFileSync(path.join(dir, "assertions.json"), JSON.stringify({ ...(prev ?? {}), case_id: prev?.case_id ?? meta.case_id, passed: sum.ok, summary: { passed: sum.passed, total: sum.total }, notes, expect: list, results }, null, 2) + "\n");
  return { results, ...sum };
}

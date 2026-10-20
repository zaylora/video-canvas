// diff / to-case 测试共用的小夹具：在临时目录里造「轨迹包」或「harness 运行目录」。
// 全是自己造的小数据，不依赖 .harness/ 下任何真实目录。

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { ev, approval, canvas, group, node } from "./helpers.mjs";

export const tmpDir = (prefix = "hn-bundle-") => fs.mkdtempSync(path.join(os.tmpdir(), prefix));

const jsonl = (rows) => rows.map((r) => JSON.stringify(r)).join("\n") + (rows.length ? "\n" : "");

export const metaOf = (over = {}) => ({
  run_id: "aaaabbbbccccdddd1111222233334444",
  user_id: "u1",
  canvas_id: "c1",
  mode: "storyboard",
  status: "succeeded",
  prompt_version: 9,
  agent_model_key: "model-a",
  tools_hash: "abcd1234abcd1234",
  skill_pins: {},
  harness_case: "",
  started_at: "2026-10-20T08:00:00Z",
  ended_at: "2026-10-20T08:00:41Z",
  trace_complete: true,
  truncated: false,
  exported_at: "2026-10-20T09:00:00Z",
  exported_by: "admin",
  ...over,
});

/** trace.jsonl 的一个 chat span。 */
export const chatSpan = (seq, input, output, extra = {}) => ({ seq, span_id: `c${seq}`, parent_span_id: null, kind: "chat", name: "model-a", status: "ok", summary: "x", started_at: "2026-10-20T08:00:01Z", duration_ms: 1000, error_code: "", purpose: "chat", usage: { input, output, cached: 0 }, credits: 0, ...extra });

/** trace.jsonl 的一个工具 span。 */
export const toolSpan = (seq, name, extra = {}) => ({ seq, span_id: `t${seq}`, parent_span_id: null, kind: "execute_tool", name, status: "ok", summary: "ok", started_at: "2026-10-20T08:00:02Z", duration_ms: 100, error_code: "", tool_call_id: `tc${seq}`, is_error: false, ...extra });

const SYSTEM_ROW = { seq: 0, kind: "system", prompt_version: 9, tools_hash: "abcd1234abcd1234", system_prompt: "…", tools: [] };

/** 工具事件：一次工具调用的 start + end。 */
export function toolEvents(seq0, id, name, { isError = false, parent, runId = "r1" } = {}) {
  const base = parent ? { parent_id: parent } : {};
  return [ev(seq0, "tool.start", { id, name, ...base }, runId), ev(seq0 + 1, "tool.end", { id, name, is_error: isError, summary: isError ? "坏了" : "ok", ...base }, runId)];
}

/**
 * 写一个目录。opts：meta、events（数组）、spans（数组，不含 system 行）、assertions（对象）、before、after、extra（文件名→内容）；
 * 把某项设成 null 表示不写这个文件。
 */
export function writeBundle(dir, opts = {}) {
  fs.mkdirSync(dir, { recursive: true });
  const put = (name, content) => fs.writeFileSync(path.join(dir, name), content);
  const has = (k) => opts[k] !== null;
  if (has("meta")) put("meta.json", JSON.stringify(metaOf(opts.meta ?? {}), null, 2));
  if (has("events")) put("events.jsonl", jsonl(opts.events ?? [ev(1, "message.user", { text: "拆分镜", mode: "storyboard", selection: [] }), ev(2, "run.status", { status: "succeeded" })]));
  if (has("spans")) put("trace.jsonl", jsonl([SYSTEM_ROW, ...(opts.spans ?? [])]));
  if (opts.assertions) put("assertions.json", JSON.stringify(opts.assertions, null, 2));
  if (has("before")) put("canvas.before.json", JSON.stringify(opts.before ?? canvas([]), null, 2));
  if (opts.after) put("canvas.after.json", JSON.stringify(opts.after, null, 2));
  for (const [name, content] of Object.entries(opts.extra ?? {})) put(name, content);
  return dir;
}

/** assertions.json 里的一条结果。 */
export const result = (type, ok, got = ok ? "满足" : "不满足", extra = {}) => ({ type, ok, expected: `期望 ${type}`, got, evidence: ok ? [] : [`证据：${type}`], ...extra });

export const assertionsOf = (results, extra = {}) => ({ case_id: "demo", passed: results.every((r) => r.ok), summary: { passed: results.filter((r) => r.ok).length, total: results.length }, expect: results.map((r) => ({ type: r.type })), results, ...extra });

/**
 * 一个典型的线上运行（用来测 to-case）：
 *   画幅提问（选项题）→ 选模型提问 → 生成审批（3 条里批准 2 条，其中一条减张数）→ 删除审批（拒绝并附理由）
 *   → 第二次删除审批（整体批准）；两次工具报错。
 */
export function onlineRunEvents(runId = "aaaabbbbccccdddd1111222233334444") {
  const R = runId;
  const e = (seq, type, data) => ev(seq, type, data, R);
  return [
    e(1, "message.user", { text: "把剧本拆成 3 个镜头，竖屏", mode: "storyboard", selection: ["n_script"] }),
    e(2, "run.status", { status: "running" }),
    e(3, "approval.created", approval("ap1", "ask", { question: "画幅用哪个？16:9 还是 9:16", kind: "choice", options: ["16:9", "9:16"] })),
    e(4, "approval.decided", approval("ap1", "ask", { question: "画幅用哪个？16:9 还是 9:16", kind: "choice", options: ["16:9", "9:16"] }, { status: "approved", decision: { decision: "approve", answer: "9:16" } })),
    e(5, "approval.created", approval("ap2", "ask", { question: "图片用哪个模型？", kind: "model", model_kind: "image", models: [{ key: "img-a", name: "A" }] })),
    e(6, "approval.decided", approval("ap2", "ask", { question: "图片用哪个模型？", kind: "model", model_kind: "image", models: [{ key: "img-a", name: "A" }] }, { status: "approved", decision: { decision: "approve", answer: "img-a" } })),
    ...toolEvents(7, "t1", "canvas_apply_ops", { isError: true, runId: R }),
    e(9, "approval.created", approval("ap3", "generate", { reason: "出关键帧", items: [{ node_id: "n1", label: "镜头1", model_key: "img-a", model_name: "A", price: 3, count: 2 }, { node_id: "n2", label: "镜头2", model_key: "img-a", model_name: "A", price: 3, count: 1 }, { node_id: "n3", label: "镜头3", model_key: "img-a", model_name: "A", price: 5, count: 1 }] }, { quote_credits: 14 })),
    e(10, "approval.decided", approval("ap3", "generate", { reason: "出关键帧", items: [] }, { status: "executed", quote_credits: 8, decision: { decision: "approve", items: [{ node_id: "n1", label: "镜头1", model_key: "img-a", model_name: "A", price: 3, count: 1 }, { node_id: "n3", label: "镜头3", model_key: "img-a", model_name: "A", price: 5, count: 1 }] } })),
    e(11, "approval.created", approval("ap4", "delete", { reason: "清理旧节点", node_ids: ["n8", "n9"], labels: ["旧1", "旧2"] })),
    e(12, "approval.decided", approval("ap4", "delete", { reason: "清理旧节点", node_ids: ["n8", "n9"] }, { status: "rejected", decision: { decision: "reject", answer: "别删" } })),
    ...toolEvents(13, "t2", "canvas_apply_ops", { isError: true, runId: R }),
    e(15, "approval.created", approval("ap5", "delete", { reason: "再清理", node_ids: ["n8", "n9", "n10"], labels: ["a", "b", "c"] })),
    e(16, "approval.decided", approval("ap5", "delete", { reason: "再清理", node_ids: ["n8", "n9", "n10"] }, { status: "executed", decision: { decision: "approve", node_ids: ["n8", "n10"] } })),
    e(17, "run.status", { status: "succeeded" }),
  ];
}

/** 一个小画布：一个组里放一个文本节点，另有一个游离节点。 */
export const smallCanvas = () => canvas([group("g1", 0, 0, 800, 500), node("n_script", "script", 48, 72, { parent: "g1", prompt: "剧本" }), node("n_free", "image", 2000, 0, { prompt: "图" })]);

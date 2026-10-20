// to-case 命令：把线上导出的轨迹包转成可回归的用例。
//   初始画布 ← canvas.before.json（写到 fixtures/imported/<id>.canvas.json）
//   message / mode / selection ← events.jsonl 里第一条 message.user 的 data
//   answers ← events.jsonl 里的 approval.created / approval.decided：每个审批按种类和用户当时实际的决定生成一条应答
//   budget ← 运行里已批准生成的报价总和
//   expect ← 一组保守的默认断言
// 隐私：用例和 fixture 含用户的剧本与提示词，默认写进的两个目录已加入根 .gitignore。

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { BundleError, requireDir, readJsonFile, readJsonlFile, findEventsFile } from "./bundle.mjs";
import { approvalFromEvent, deriveRun } from "./events.mjs";
import { questionStem } from "./answers.mjs";
import { MODES, validateCase } from "./cases.mjs";
import { runAssertions } from "./assertions.mjs";

const HARNESS_ROOT = path.dirname(fileURLToPath(import.meta.url));
const isObject = (v) => v && typeof v === "object" && !Array.isArray(v);
const str = (v) => (typeof v === "string" ? v : "");

/** 不再批准 / 不再有结果的审批状态：没有真实的用户决定可还原。 */
const NO_DECISION_STATUSES = ["pending", "expired"];

// ---------- 应答还原 ----------

/** 审批条目的 index/approve/count 逐项决定：让回放时得到和用户当时一样的批准范围。 */
function generateAnswer(created, decision) {
  const asked = Array.isArray(created.payload.items) ? created.payload.items : [];
  const approved = Array.isArray(decision.items) ? decision.items : [];
  // 批准的条目是申请条目的子序列（后端按顺序过滤，数量只会减少），按节点和模型顺序配对
  let at = 0;
  const items = asked.map((it, index) => {
    const hit = approved[at];
    if (!hit || hit.node_id !== it.node_id || hit.model_key !== it.model_key) return { index, approve: false };
    at++;
    const n = Number(hit.count) || 0;
    const want = Number(it.count) || 0;
    return n > 0 && n < want ? { index, approve: true, count: n } : { index, approve: true };
  });
  const whole = items.every((x) => x.approve && x.count === undefined);
  return whole ? { decision: "approve" } : { decision: "approve", items };
}

function deleteAnswer(created, decision) {
  const asked = Array.isArray(created.payload.node_ids) ? created.payload.node_ids.map(String) : [];
  const kept = new Set(Array.isArray(decision.node_ids) ? decision.node_ids.map(String) : []);
  const items = asked.map((id, index) => ({ index, approve: kept.has(id) }));
  return items.every((x) => x.approve) ? { decision: "approve" } : { decision: "approve", items };
}

/** 审批的 match：和 answers.mjs 的字段一致。选项题用提问开头匹配；选模型题的提问是模板话，靠 model_kind 区分。 */
function matchOf(created) {
  if (created.kind !== "ask") return { kind: created.kind };
  if (created.ask_kind === "model") return { kind: "ask", ask_kind: "model", ...(created.model_kind ? { model_kind: created.model_kind } : {}) };
  return { kind: "ask", ...(created.ask_kind ? { ask_kind: created.ask_kind } : {}), contains: questionStem(created.question) };
}

/**
 * 从事件还原应答剧本和预算。返回 {answers, budget, approvals, notes}：
 * approvals 是出现过的审批数；notes 是要告诉用户的问题（没有决定的审批、被前面盖住的应答）。
 */
export function answersFromEvents(events) {
  const created = [];
  const decided = new Map();
  for (const e of events) {
    if (e.type === "approval.created") created.push(approvalFromEvent(e));
    else if (e.type === "approval.decided" && isObject(e.data) && str(e.data.id)) decided.set(str(e.data.id), e.data);
  }

  const answers = [];
  const notes = [];
  let budget = 0;
  const seen = new Map(); // match 的 JSON → 已有的 answer 的 JSON
  for (const c of created) {
    const d = decided.get(c.id);
    const decision = isObject(d?.decision) ? d.decision : {};
    const verdict = str(decision.decision) || (d?.status === "rejected" ? "reject" : "");
    const label = `${c.kind}${c.kind === "ask" ? `「${questionStem(c.question)}」` : ""}`;
    if (!d || NO_DECISION_STATUSES.includes(d.status) || !verdict) {
      notes.push(`审批 ${label} 没有等到用户的决定（运行被停止、中断或过期），没有为它生成应答；回放时若再遇到会判「未预写的提问」。`);
      continue;
    }

    let answer;
    if (verdict === "reject") {
      answer = { decision: "reject" };
      if (str(decision.answer).trim()) answer.answer = str(decision.answer);
    } else if (c.kind === "ask") {
      if (!str(decision.answer)) {
        notes.push(`审批 ${label} 用户批准了但没有回答内容，已跳过。`);
        continue;
      }
      answer = { answer: str(decision.answer) };
    } else if (c.kind === "delete") answer = deleteAnswer(c, decision);
    else {
      answer = generateAnswer(c, decision);
      budget += Number(d.quote_credits) > 0 ? Number(d.quote_credits) : (Array.isArray(decision.items) ? decision.items : []).reduce((n, it) => n + (Number(it.price) || 0) * (Number(it.count) || 0), 0);
    }

    const match = matchOf(c);
    const key = JSON.stringify(match);
    const same = seen.get(key);
    if (same !== undefined) {
      if (same === JSON.stringify(answer)) continue; // 完全相同的应答合并
      notes.push(`第 ${answers.length + 1} 条 ${label} 应答与前面某条的 match 相同、回答不同：answers 按顺序匹配、第一条命中的生效，所以这条会被前面那条盖住。需要的话手工给它们加 contains 区分。`);
    } else seen.set(key, JSON.stringify(answer));
    answers.push({ match, answer });
  }
  return { answers, budget, approvals: created.length, notes };
}

// ---------- 构造用例 ----------

const clip = (s, n) => {
  const chars = [...s.replace(/\s+/g, " ").trim()];
  return chars.length > n ? chars.slice(0, n).join("") + "…" : chars.join("");
};

/**
 * 读轨迹包，构造用例和 fixture（不写文件）。
 * 返回 {def, fixture, warnings, approvals, runId}；缺文件、JSON 坏了、没有 message.user、生成的用例不合法时抛 BundleError。
 */
export function buildCase(bundleDir, { id, fixtureRef } = {}) {  // fixtureRef: (用例 id) => fixture 在用例里的引用路径
  requireDir(bundleDir, "轨迹包目录");
  const eventsFile = findEventsFile(bundleDir);
  if (!eventsFile) throw new BundleError(`${bundleDir} 里没有 events.jsonl，没法还原用户原话和应答剧本`);
  const beforeFile = path.join(bundleDir, "canvas.before.json");
  if (!fs.existsSync(beforeFile)) throw new BundleError(`${bundleDir} 里没有 canvas.before.json（初始画布），没法转成可回放的用例`);

  const events = readJsonlFile(eventsFile);
  const before = readJsonFile(beforeFile);
  if (!isObject(before) || !Array.isArray(before.nodes)) throw new BundleError(`${beforeFile} 格式不对：应是含 nodes 数组的 JSON 对象（{nodes, edges}）`);
  const metaFile = path.join(bundleDir, "meta.json");
  const meta = fs.existsSync(metaFile) ? readJsonFile(metaFile) : {};
  if (!isObject(meta)) throw new BundleError(`${metaFile} 格式不对：应是 JSON 对象`);

  const first = events.find((e) => e.type === "message.user");
  if (!first) throw new BundleError(`${eventsFile} 里没有 message.user 事件（用户原话），没法还原用例`);
  const text = str(first.data?.text);
  if (!text.trim()) throw new BundleError(`${eventsFile} 里第一条 message.user 事件的 data.text 是空的，没法还原用户原话`);

  const warnings = [];
  const runId = str(meta.run_id) || str(events.find((e) => str(e.run_id))?.run_id) || path.basename(path.resolve(bundleDir));
  const short = runId.toLowerCase().replace(/[^a-z0-9]/g, "").slice(0, 8) || "run";
  const caseId = id ?? `imported-${short}`;

  let mode = str(first.data?.mode);
  if (!MODES.includes(mode)) {
    if (mode) warnings.push(`运行的 mode「${mode}」不是 ${MODES.join(" / ")} 之一，用例按 all 处理。`);
    mode = "all";
  }
  const { answers, budget, approvals, notes } = answersFromEvents(events);
  warnings.push(...notes);
  const toolErrors = deriveRun(events, undefined).tool_errors;

  const def = {
    id: caseId,
    title: `线上运行 ${short}：${clip(text, 24)}`,
    tags: ["imported"],
    source: { run_id: runId, exported_at: str(meta.exported_at) || null, prompt_version: Number.isFinite(meta.prompt_version) ? meta.prompt_version : null, status: str(meta.status) },
    fixture: fixtureRef ? fixtureRef(caseId) : `fixtures/imported/${caseId}.canvas.json`,
    mode,
    message: text,
    selection: Array.isArray(first.data?.selection) ? first.data.selection.filter((s) => typeof s === "string") : [],
    budget,
    answers,
    expect: [{ type: "run_status", in: ["succeeded"] }, { type: "no_overlap" }, { type: "members_inside_group" }, { type: "max_tool_errors", max: Math.max(toolErrors, 2) }],
  };
  const errs = validateCase(def);
  if (errs.length) throw new BundleError(`生成的用例不合法：\n${errs.map((e) => `  - ${e}`).join("\n")}`);

  // 默认断言看的是最终画布；初始画布本身就违反时，回放必然失败，提前说
  const ctx = { canvas: before, run: deriveRun([], undefined), approvals: [], taskOutputs: {} };
  for (const r of runAssertions([{ type: "no_overlap" }, { type: "members_inside_group" }], ctx)) {
    if (!r.ok) warnings.push(`初始画布本身就不满足 ${r.type}（${r.got}）：如果 Agent 不去动那部分，默认断言会一直失败，请先修 fixture 或去掉这条断言。`);
  }
  return { def, fixture: before, warnings, approvals, runId, toolErrors };
}

// ---------- 写文件 ----------

/**
 * 转换并写文件。opts：id、outFile（用例文件，默认 <harnessRoot>/cases-imported/<id>.case.json）、harnessRoot（测试用）。
 * 先全部构造、校验通过再写，出错不留半截文件。返回 {caseFile, fixtureFile, def, warnings, approvals, defaultOut}。
 */
export function toCase(bundleDir, { id, outFile, harnessRoot = HARNESS_ROOT } = {}) {
  const fixtureFileOf = (caseId) => path.join(harnessRoot, "fixtures", "imported", `${caseId}.canvas.json`);
  const caseFileOf = (caseId) => outFile ?? path.join(harnessRoot, "cases-imported", `${caseId}.case.json`);
  // 默认位置用 loadFixture 认的 harness 根相对路径；指定了 --out，则写成相对用例文件所在目录的路径，同样找得到
  const fixtureRef = (caseId) => (outFile ? path.relative(path.dirname(path.resolve(outFile)), fixtureFileOf(caseId)).split(path.sep).join("/") : `fixtures/imported/${caseId}.canvas.json`);
  const built = buildCase(bundleDir, { id, fixtureRef });
  const fixtureFile = fixtureFileOf(built.def.id);
  const caseFile = caseFileOf(built.def.id);

  fs.mkdirSync(path.dirname(fixtureFile), { recursive: true });
  fs.mkdirSync(path.dirname(caseFile), { recursive: true });
  fs.writeFileSync(fixtureFile, JSON.stringify(built.fixture, null, 2) + "\n");
  fs.writeFileSync(caseFile, JSON.stringify(built.def, null, 2) + "\n");
  return { caseFile, fixtureFile, def: built.def, warnings: built.warnings, approvals: built.approvals, defaultOut: !outFile, toolErrors: built.toolErrors };
}

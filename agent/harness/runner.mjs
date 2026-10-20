// 单个用例的执行流程，严格按设计文档 6.B.3「单个用例的执行流程」：
//   登录 → 建画布（fixture）→ 建会话 → 开跑（X-Agent-Harness）→ 每秒补拉事件并应答 → 终态后（wait_tasks 时等任务）
//   → 读最终画布 → 管理员取轨迹包写进输出目录 → 执行断言 → 写 assertions.json、补 report.md → 默认删除测试画布。
//
// HTTP 用注入的 fetch，时间用注入的 clock，所以测试可以用假后端和假时钟跑完整流程，不碰真实服务。
// 认证只从环境变量读，密钥不进任何输出文件或日志。

import fs from "node:fs";
import path from "node:path";
import { runAssertions } from "./assertions.mjs";
import { findAnswer, buildDecision, suggestAnswer } from "./answers.mjs";
import { deriveRun, isTerminalStatus, approvalFromEvent } from "./events.mjs";
import { buildTraceFromEvents, buildFallbackReport, renderAssertionBlock, applyAssertionBlock, summarizeResults, formatDuration } from "./report.mjs";

/** 配置或环境问题（缺环境变量、登录失败、连不上后端），整批都跑不了。 */
export class HarnessError extends Error {}

/** 后端返回了业务错误或非 2xx。 */
export class ApiError extends Error {
  constructor(message, { status = 0, code = 0 } = {}) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export const defaultClock = {
  now: () => Date.now(),
  sleep: (ms) => new Promise((r) => setTimeout(r, ms)),
};

const REQUIRED_ENV = ["HARNESS_BASE_URL", "HARNESS_EMAIL", "HARNESS_PASSWORD"];
const ACTIVE_TASK_STATUSES = ["pending", "queued", "running", "finalizing"]; // model.ActiveTaskStatuses
const MAX_EVENT_ERRORS = 3; // 事件接口连续失败几次就放弃
const EVENT_PAGE = 200; // 后端一页最多 200 条

/** 去掉末尾的斜杠和 /api/v1，也去掉可能写在地址里的用户名密码。 */
export function normalizeBaseUrl(raw) {
  const u = new URL(raw);
  u.username = "";
  u.password = "";
  return u.toString().replace(/\/+$/, "").replace(/\/api\/v1$/, "");
}

function createApi(baseUrl, fetchImpl) {
  async function request(method, pathname, { token, body, query, headers = {} } = {}) {
    const qs = query ? "?" + new URLSearchParams(query).toString() : "";
    const h = { accept: "application/json", ...headers };
    if (token) h.authorization = `Bearer ${token}`;
    if (body !== undefined) h["content-type"] = "application/json";
    let res;
    try {
      res = await fetchImpl(`${baseUrl}/api/v1${pathname}${qs}`, { method, headers: h, body: body === undefined ? undefined : JSON.stringify(body) });
    } catch (e) {
      throw new ApiError(`无法连接 ${baseUrl}：${e.cause?.code ?? e.message}`, { status: 0 });
    }
    const text = await res.text();
    let json;
    try {
      json = JSON.parse(text);
    } catch {
      /* 不是 JSON，下面按状态码报错 */
    }
    if (!res.ok || json?.code !== 0) throw new ApiError(json?.msg ? String(json.msg) : `HTTP ${res.status}`, { status: res.status, code: json?.code ?? 0 });
    return json.data;
  }
  return { request };
}

/**
 * 登录并准备本批共用的会话。缺环境变量、用户登录失败直接抛 HarnessError；
 * 管理员凭据缺失或登录失败不抛错，只是之后拿不到轨迹包（adminNote 写明原因）。
 */
export async function openSession({ env, fetch: fetchImpl, log }) {
  // 登录接口只认用户名：HARNESS_USERNAME 和 HARNESS_EMAIL 二选一即可
  const missing = REQUIRED_ENV.filter((k) => !(k === "HARNESS_EMAIL" ? env.HARNESS_USERNAME || env[k] : env[k]));
  if (missing.length) throw new HarnessError(`缺少环境变量：${missing.join("、")}。认证只从环境变量读取，不写进仓库或输出目录。`);
  const baseUrl = normalizeBaseUrl(env.HARNESS_BASE_URL);
  const api = createApi(baseUrl, fetchImpl);
  const secrets = [env.HARNESS_PASSWORD];
  const login = async (username, password) => (await api.request("POST", "/auth/login", { body: { username, password } })).token;

  let userToken;
  try {
    userToken = await login(env.HARNESS_USERNAME || env.HARNESS_EMAIL, env.HARNESS_PASSWORD);
  } catch (e) {
    if (e.status === 0) throw new HarnessError(e.message);
    throw new HarnessError(`登录失败（${env.HARNESS_USERNAME || env.HARNESS_EMAIL}）：${e.message}`);
  }
  secrets.push(userToken);
  log("dim", `→ 登录 ${env.HARNESS_USERNAME || env.HARNESS_EMAIL} ✓`);

  let adminToken = null;
  let adminNote = "缺管理员凭据";
  if (env.HARNESS_ADMIN_EMAIL && env.HARNESS_ADMIN_PASSWORD) {
    secrets.push(env.HARNESS_ADMIN_PASSWORD);
    try {
      adminToken = await login(env.HARNESS_ADMIN_USERNAME || env.HARNESS_ADMIN_EMAIL, env.HARNESS_ADMIN_PASSWORD);
      secrets.push(adminToken);
      adminNote = "";
      log("dim", `→ 管理员登录 ${env.HARNESS_ADMIN_EMAIL} ✓（用于取轨迹包）`);
    } catch (e) {
      adminNote = "管理员登录失败";
      log("warn", `! 管理员登录失败（${e.message}），run 仍会执行，报告里标「无轨迹」`);
    }
  } else {
    log("warn", "! 没有 HARNESS_ADMIN_EMAIL / HARNESS_ADMIN_PASSWORD，run 仍会执行，但拿不到轨迹包（报告里标「无轨迹（缺管理员凭据）」）");
  }
  const redact = (text) => secrets.filter((s) => s && s.length >= 6).reduce((t, s) => t.split(s).join("***"), String(text));
  return { api, baseUrl, userToken, adminToken, adminNote, email: env.HARNESS_USERNAME || env.HARNESS_EMAIL, redact };
}

// ---------- 小工具 ----------

const pad = (n) => String(n).padStart(2, "0");
/** 本地时间 YYYYMMDD-HHmmss，输出目录名用。 */
export function stamp(ms) {
  const d = new Date(ms);
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`;
}

function allocateDir(outRoot, ts, caseId, index, total) {
  fs.mkdirSync(outRoot, { recursive: true });
  const base = `${ts}-${caseId}${total > 1 ? `-${index}` : ""}`;
  for (let n = 0; ; n++) {
    const dir = path.join(outRoot, n === 0 ? base : `${base}-${n}`);
    try {
      fs.mkdirSync(dir);
      return dir;
    } catch (e) {
      if (e.code !== "EEXIST") throw e;
    }
  }
}

const harnessFailure = (type, expected, got, evidence = [], extra = {}) => ({ type: `harness.${type}`, ok: false, expected, got, evidence, ...extra });
const parsePayload = (v) => (typeof v === "string" ? JSON.parse(v) : (v ?? {}));
const safeName = (n) => /^[A-Za-z0-9_][A-Za-z0-9_.-]*$/.test(n);

// ---------- 执行一个用例 ----------

/**
 * 执行一个用例，返回结果对象（不会因为用例失败而抛错；抛错只留给 HarnessError 之外的程序 bug）。
 * 参数：def 用例（已补默认值）、file 用例文件路径、fixture 初始画布、session openSession 的返回、
 * outRoot 输出根目录、keep 是否保留画布、index/total 第几次/共几次（repeat）、clock、log(cls, text)。
 */
export async function runCase({ def, file, fixture, session, outRoot, keep = false, index = 1, total = 1, clock = defaultClock, log = () => {}, pollMs = 1000, taskPollMs = 5000 }) {
  const { api } = session;
  const startedAt = clock.now();
  const deadline = startedAt + def.timeout_sec * 1000;
  const dir = allocateDir(outRoot, stamp(startedAt), def.id, index, total);
  const st = { canvasId: null, sessionId: null, runId: null, events: [], failures: [], notes: [], tasks: {}, finalCanvas: null, bundle: null, noTraceReason: "", canceled: false };
  const elapsed = () => `[${pad(Math.floor((clock.now() - startedAt) / 1000))}s]`;
  const call = (method, p, o = {}) => api.request(method, p, { token: session.userToken, ...o });

  // ---- 取消运行并等它落到终态 ----
  async function cancelRun() {
    if (!st.runId || st.canceled) return;
    st.canceled = true;
    try {
      await call("POST", `/agent/runs/${st.runId}/cancel`);
    } catch (e) {
      log("warn", `! 取消运行失败：${e.message}`);
      return;
    }
    for (let i = 0; i < 5; i++) {
      await pullEvents();
      if (isTerminalStatus(deriveRun(st.events, st.runId).status)) return;
      await clock.sleep(pollMs);
    }
  }

  // ---- 补拉事件，返回本次新增的事件 ----
  let after = 0;
  let errStreak = 0;
  async function pullEvents() {
    const fresh = [];
    for (;;) {
      let page;
      try {
        page = await call("GET", `/agent/sessions/${st.sessionId}/events`, { query: { after, limit: EVENT_PAGE } });
        errStreak = 0;
      } catch (e) {
        if (++errStreak >= MAX_EVENT_ERRORS) throw new HarnessError(`补拉事件连续失败 ${errStreak} 次：${e.message}`);
        return fresh;
      }
      const evs = (Array.isArray(page) ? page : []).filter((e) => e.run_id == null || e.run_id === st.runId);
      for (const e of page ?? []) after = Math.max(after, e.seq);
      st.events.push(...evs);
      fresh.push(...evs);
      if ((page?.length ?? 0) < EVENT_PAGE) return fresh;
    }
  }

  // ---- 把新事件打印成终端里的进度行 ----
  function narrate(evs) {
    for (const e of evs) {
      const d = e.data ?? {};
      if (e.type === "tool.end") log(d.is_error ? "warn" : "info", `${elapsed()} tool ${d.name} ${d.is_error ? "error" : "ok"}${d.summary ? " · " + d.summary : ""}`);
      else if (e.type === "message.done" && d.tool_calls?.length) log("info", `${elapsed()} chat → ${d.tool_calls.map((c) => c.name).join(", ")}`);
      else if (e.type === "approval.created") {
        const a = approvalFromEvent(e);
        log("ask", `${elapsed()} approval ${a.kind}${a.ask_kind ? "/" + a.ask_kind : ""} pending · 「${a.text || a.kind}」`);
      }
    }
  }

  // ---- 应答一条审批；返回 false 表示应该停下 ----
  async function answer(a) {
    const hit = findAnswer(def.answers, a);
    if (!hit) {
      const text = a.text || a.kind;
      log("err", `  ✗ 没有预写的提问：「${text}」 → 取消运行`);
      st.failures.push(harnessFailure("unscripted_question", "所有审批和提问都有预写的应答", `未预写的提问：${text}`, [`审批种类：${a.kind}${a.ask_kind ? `（ask_kind=${a.ask_kind}）` : ""}`, ...(a.options.length ? [`选项：${a.options.join(" / ")}`] : [])], { suggestion: suggestAnswer(a) }));
      await cancelRun();
      return false;
    }
    try {
      const body = buildDecision(hit.entry, a);
      await call("POST", `/agent/approvals/${a.id}/decision`, { body });
      log("ok", `  ↳ 按剧本应答（answers[${hit.index}]）：${body.answer ?? body.decision}`);
      return true;
    } catch (e) {
      log("err", `  ✗ 应答失败：${e.message} → 取消运行`);
      st.failures.push(harnessFailure("run_error", "应答审批成功", `应答审批失败：${e.message}`, [`审批 ${a.id}（${a.kind}），命中 answers[${hit.index}]`]));
      await cancelRun();
      return false;
    }
  }

  // ---- 等生成任务结束（wait_tasks）。前端回填产物，harness 没有浏览器，所以直接查任务接口 ----
  async function waitTasks() {
    log("dim", "→ wait_tasks：等待生成任务……");
    for (;;) {
      const canvas = await readCanvas();
      const withTask = (canvas.nodes ?? []).filter((n) => typeof n.data?.taskId === "string" && n.data.taskId);
      let pending = 0;
      for (const n of withTask) {
        try {
          const t = await call("GET", `/generation-tasks/${n.data.taskId}`);
          st.tasks[n.id] = { task_id: n.data.taskId, status: t.status, outputs: Array.isArray(t.outputs) ? t.outputs.length : 0, error: t.error_message ?? "" };
          if (ACTIVE_TASK_STATUSES.includes(t.status)) pending++;
        } catch (e) {
          st.tasks[n.id] = { task_id: n.data.taskId, status: "unknown", outputs: 0, error: e.message };
          st.notes.push(`查询生成任务 ${n.data.taskId} 失败：${e.message}`);
        }
      }
      st.finalCanvas = canvas;
      if (pending === 0) {
        log("dim", `  生成任务 ${withTask.length}/${withTask.length} 已结束`);
        return;
      }
      if (clock.now() >= deadline) {
        st.failures.push(harnessFailure("timeout", `${def.timeout_sec} 秒内生成任务全部结束`, `超过 ${def.timeout_sec} 秒生成任务仍未结束（还有 ${pending} 个）`, Object.entries(st.tasks).filter(([, t]) => ACTIVE_TASK_STATUSES.includes(t.status)).map(([id, t]) => `${id}：任务 ${t.task_id} 状态 ${t.status}`)));
        log("err", `  ✗ 等待生成任务超时（${def.timeout_sec} 秒）`);
        return;
      }
      await clock.sleep(taskPollMs);
    }
  }

  async function readCanvas() {
    const data = await call("GET", `/canvas/${st.canvasId}`);
    return parsePayload(data.payload_json);
  }

  try {
    // 1. 建画布
    const title = `[harness] ${def.id} ${new Date(startedAt).toISOString()}`;
    const created = await call("POST", "/canvas", { body: { title, payload_json: fixture } });
    st.canvasId = created.id;
    log("dim", `→ 新建画布「${title}」 canvas=${st.canvasId}${def.fixture ? `（fixture: ${path.basename(def.fixture).replace(/\.canvas\.json$/, "")}）` : ""}`);
    // 2. 建会话
    const sessBody = { mode: def.mode };
    if (def.agent_model_key) sessBody.model_key = def.agent_model_key;
    st.sessionId = (await call("POST", `/canvas/${st.canvasId}/agent/sessions`, { body: sessBody })).id;
    // 3. 开跑
    const runBody = { message: def.message, mode: def.mode, selection: def.selection, budget_credits: def.budget };
    if (def.agent_model_key) runBody.agent_model_key = def.agent_model_key;
    st.runId = (await call("POST", `/agent/sessions/${st.sessionId}/runs`, { body: runBody, headers: { "X-Agent-Harness": def.id } })).id;
    log("dim", `→ 开跑 run=${st.runId}  模式 ${def.mode}  预算 ${def.budget}`);

    // 4. 每秒补拉事件并应答，直到终态或超时
    let stop = false;
    while (!stop) {
      const fresh = await pullEvents();
      narrate(fresh);
      const decided = new Set(fresh.filter((e) => e.type === "approval.decided").map((e) => e.data?.id));
      for (const e of fresh) {
        if (e.type !== "approval.created" || decided.has(e.data?.id)) continue;
        if (!(await answer(approvalFromEvent(e)))) {
          stop = true;
          break;
        }
      }
      if (stop) break;
      const status = deriveRun(st.events, st.runId).status;
      if (isTerminalStatus(status)) break;
      if (clock.now() >= deadline) {
        log("err", `  ✗ 超时（${def.timeout_sec} 秒），取消运行`);
        st.failures.push(harnessFailure("timeout", `${def.timeout_sec} 秒内运行结束`, `超过 ${def.timeout_sec} 秒运行仍未结束（最后状态 ${status}），已取消`));
        await cancelRun();
        break;
      }
      await clock.sleep(pollMs);
    }
    log("dim", `→ run.status = ${deriveRun(st.events, st.runId).status}`);

    // 5. 终态之后等任务
    if (def.wait_tasks && !st.canceled) await waitTasks();
  } catch (e) {
    log("err", `  ✗ ${e.message}`);
    st.failures.push(harnessFailure("run_error", "用例流程执行完成", e.message));
    if (st.runId && !isTerminalStatus(deriveRun(st.events, st.runId).status)) await cancelRun().catch(() => {});
  }

  // 6. 读最终画布；管理员取轨迹包
  if (st.canvasId && !st.finalCanvas) {
    try {
      st.finalCanvas = await readCanvas();
    } catch (e) {
      st.failures.push(harnessFailure("run_error", "能读到最终画布", `读取最终画布失败：${e.message}`));
    }
  }
  if (st.runId) {
    if (!session.adminToken) st.noTraceReason = session.adminNote;
    else {
      try {
        const data = await api.request("GET", `/admin/agent/runs/${st.runId}/trace`, { token: session.adminToken, query: { format: "json" } });
        if (data?.files && typeof data.files === "object") {
          st.bundle = data.files;
          const bytes = Object.values(st.bundle).reduce((n, v) => n + String(v).length, 0);
          log("dim", `→ 拉取轨迹（管理员，已记审计）${Object.keys(st.bundle).length} 个文件 · ${Math.round(bytes / 1024)}KB`);
        } else st.noTraceReason = "轨迹包格式不对";
      } catch (e) {
        st.noTraceReason = e.message;
        log("warn", `! 拿不到轨迹包（${e.message}），报告里标「无轨迹」`);
      }
    }
  }

  // 7. 清理测试画布（放在写报告之前，删除失败要写进报告）
  let canvasDeleted = false;
  if (st.canvasId) {
    if (keep) log("dim", `→ 保留测试画布 canvas=${st.canvasId}`);
    else {
      try {
        await call("DELETE", `/canvas/${st.canvasId}`);
        canvasDeleted = true;
        log("dim", `→ 已删除测试画布 ${st.canvasId}（加 --keep 可保留）`);
      } catch (e) {
        const msg = `测试画布删除失败，请手动清理：canvas=${st.canvasId}（${e.message}）`;
        st.notes.push(msg);
        log("warn", `! ${msg}`);
      }
    }
  }

  // 8. 执行断言，写输出
  const run = deriveRun(st.events, st.runId);
  const canvas = st.finalCanvas ?? { nodes: [], edges: [] };
  const taskOutputs = Object.fromEntries(Object.entries(st.tasks).map(([id, t]) => [id, t.outputs]));
  const results = [...runAssertions(def.expect, { canvas, run, approvals: run.approvals, taskOutputs }), ...st.failures];
  const sum = summarizeResults(results);
  const endedAt = clock.now();
  const notes = [...st.notes];
  if (st.noTraceReason) notes.unshift(`无轨迹（${st.noTraceReason}）：没有请求和响应原文`);

  const files = {};
  if (st.bundle) for (const [name, content] of Object.entries(st.bundle)) if (safeName(name)) files[name] = typeof content === "string" ? content : JSON.stringify(content, null, 2);
  const unscripted = st.failures.filter((f) => f.type === "harness.unscripted_question").map((f) => f.got.replace(/^未预写的提问：/, ""));
  const fallback = buildTraceFromEvents(st.events, { runId: st.runId });
  // harness 自己补拉到的事件单独存一份（report 命令重新断言时用它，不依赖后端导出文件的行格式）
  const pulled = st.events.map((e) => JSON.stringify(e)).join("\n") + (st.events.length ? "\n" : "");
  files["events.pulled.jsonl"] = pulled;
  if (!files["events.jsonl"]) files["events.jsonl"] = pulled;
  if (!files["trace.jsonl"]) files["trace.jsonl"] = fallback.jsonl;
  if (!files["canvas.before.json"]) files["canvas.before.json"] = JSON.stringify(fixture, null, 2);
  if (!files["canvas.after.json"]) files["canvas.after.json"] = JSON.stringify(canvas, null, 2);
  files["canvas.final.json"] = JSON.stringify(canvas, null, 2);
  if (Object.keys(st.tasks).length) files["tasks.json"] = JSON.stringify(st.tasks, null, 2);

  let bundleMeta = {};
  try {
    bundleMeta = JSON.parse(files["meta.json"] ?? "{}");
  } catch {
    /* 后端的 meta 坏了就当没有 */
  }
  const meta = st.bundle
    ? { ...bundleMeta, case_id: def.id, case_file: file, base_url: session.baseUrl }
    : { run_id: st.runId, canvas_id: st.canvasId, session_id: st.sessionId, mode: def.mode, status: run.status, harness_case: def.id, started_at: new Date(startedAt).toISOString(), ended_at: new Date(endedAt).toISOString(), trace_complete: false, exported_at: null, no_trace_reason: st.noTraceReason, case_id: def.id, case_file: file, base_url: session.baseUrl };
  files["meta.json"] = JSON.stringify(meta, null, 2) + "\n";

  const baseReport = files["report.md"] ?? buildFallbackReport({ runId: st.runId ?? "（未开跑）", message: def.message, run, spans: fallback.spans, canvas, noTraceReason: st.noTraceReason || "未取得轨迹", unscriptedQuestions: unscripted, durationMs: endedAt - startedAt });
  files["report.md"] = applyAssertionBlock(baseReport, renderAssertionBlock({ results, notes }));
  files["assertions.json"] = JSON.stringify({ case_id: def.id, title: def.title ?? "", passed: sum.ok, summary: { passed: sum.passed, total: sum.total }, run: { id: st.runId, status: run.status, steps: run.steps, spent_credits: run.spent_credits, tool_errors: run.tool_errors, error: run.error }, notes, expect: def.expect, results }, null, 2) + "\n";

  for (const [name, content] of Object.entries(files)) fs.writeFileSync(path.join(dir, name), session.redact(content));

  const failed = results.filter((r) => !r.ok);
  log(sum.ok ? "ok" : "err", `${sum.ok ? "✓" : "✗"} 断言 ${sum.passed}/${sum.total}${failed.length ? "：" + failed.map((r) => r.type).join("、") : ""}`);
  log("dim", `→ 输出 ${dir}/`);
  return {
    caseId: def.id,
    index,
    passed: sum.ok,
    results,
    summary: sum,
    runId: st.runId,
    status: run.status,
    steps: run.steps,
    credits: run.spent_credits,
    toolErrors: run.tool_errors,
    durationMs: endedAt - startedAt,
    durationText: formatDuration(endedAt - startedAt),
    dir,
    canvasId: st.canvasId,
    canvasDeleted,
    noTrace: Boolean(st.noTraceReason),
    notes,
    failReasons: failed.map((r) => `${r.type}：${r.got}`),
  };
}

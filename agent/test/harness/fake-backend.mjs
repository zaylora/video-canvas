// 假后端：用注入的 fetch 模拟 video-canvas 的 HTTP 接口，事件序列由测试脚本化。不起端口、不碰真实服务。
//
// batches 是事件脚本：每次有人 GET 事件时放出一批。放出的一批里有 approval.created 时，
// 后端就停在那里等 POST decision（或 cancel），收到决定后追加 approval.decided + run.status running 再继续放下一批。
// 所有批次放完而运行还没到终态，就一直停在 running，用来模拟超时。

export const BASE = "http://backend.test";

export function createFakeClock(start = Date.UTC(2026, 9, 20, 8, 0, 0)) {
  let t = start;
  return {
    now: () => t,
    sleep: async (ms) => {
      t += ms;
    },
    advance: (ms) => {
      t += ms;
    },
  };
}

const TERMINAL = new Set(["succeeded", "failed", "canceled", "timeout", "expired", "budget_exhausted", "step_limit", "interrupted"]);

export function createFakeBackend(opts = {}) {
  const users = opts.users ?? {
    "harness@example.com": { password: "pw-harness-secret", token: "tok-harness-secret", role: "user" },
    "admin@example.com": { password: "pw-admin-secret", token: "tok-admin-secret", role: "admin" },
  };
  const calls = [];
  const events = [];
  let nextBatch = 0;
  let blockedOn = null;
  let canceled = false;
  const state = { runs: 0, canvasId: "c1", sessionId: "s1", runId: "r1", canvasCreated: null, canvasDeleted: false, decisions: [], cancelCount: 0, taskPolls: {} };

  const push = (type, data) => {
    events.push({ session_id: state.sessionId, canvas_id: state.canvasId, run_id: state.runId, seq: events.length + 1, type, data, created_at: new Date(Date.UTC(2026, 9, 20, 8, 0, events.length + 1)).toISOString() });
  };
  const lastStatus = () => [...events].reverse().find((e) => e.type === "run.status")?.data.status ?? "queued";
  const json = (status, body) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
  const ok = (data, status = 200) => json(status, { code: 0, msg: "success", data });
  const fail = (status, code, msg) => json(status, { code, msg });

  function releaseBatch() {
    if (canceled || blockedOn || TERMINAL.has(lastStatus())) return;
    const batch = (opts.batches ?? [])[nextBatch];
    if (!batch) return;
    nextBatch++;
    for (const e of batch) {
      push(e.type, e.data);
      if (e.type === "approval.created") blockedOn = e.data.id;
    }
  }

  async function fetch(url, init = {}) {
    const u = new URL(url);
    const method = (init.method ?? "GET").toUpperCase();
    const headers = Object.fromEntries(Object.entries(init.headers ?? {}).map(([k, v]) => [k.toLowerCase(), v]));
    const body = init.body ? JSON.parse(init.body) : undefined;
    const pathname = u.pathname.replace(/^\/api\/v1/, "");
    calls.push({ method, path: pathname, query: Object.fromEntries(u.searchParams), headers, body });
    if (opts.networkDown) throw new TypeError("fetch failed");

    if (method === "POST" && pathname === "/auth/login") {
      const user = users[body.username];
      if (!user || user.password !== body.password) return fail(401, 10003, "用户名或密码错误");
      return ok({ token: user.token, expire_at: 1790000000, role: user.role });
    }
    const who = Object.values(users).find((x) => `Bearer ${x.token}` === headers.authorization);
    if (!who) return fail(401, 10001, "未登录");
    if (pathname.startsWith("/admin/") && who.role !== "admin") return fail(403, 10004, "无权限");

    if (method === "POST" && pathname === "/canvas") {
      // 同一个假后端可以连续跑多次（repeat）：新建画布时把事件脚本复位
      state.runs++;
      events.length = 0;
      nextBatch = 0;
      blockedOn = null;
      canceled = false;
      state.canvasCreated = body;
      return ok({ id: state.canvasId, title: body.title, payload_json: body.payload_json, revision: 1 });
    }
    if (method === "GET" && pathname === `/canvas/${state.canvasId}`) {
      const finalCanvas = Array.isArray(opts.finalCanvas) ? opts.finalCanvas[state.runs - 1] : opts.finalCanvas;
      const payload = TERMINAL.has(lastStatus()) && finalCanvas ? finalCanvas : (state.canvasCreated?.payload_json ?? {});
      return ok({ id: state.canvasId, title: state.canvasCreated?.title, payload_json: typeof opts.finalCanvasAsString === "boolean" && opts.finalCanvasAsString ? JSON.stringify(payload) : payload, revision: 2 });
    }
    if (method === "DELETE" && pathname === `/canvas/${state.canvasId}`) {
      if (opts.deleteFails) return fail(500, 50000, "删除失败");
      state.canvasDeleted = true;
      return ok(null);
    }
    if (method === "POST" && pathname === `/canvas/${state.canvasId}/agent/sessions`) {
      return ok({ id: state.sessionId, canvas_id: state.canvasId, title: "新对话", mode: body.mode ?? "all", model_key: body.model_key ?? "", last_seq: 0 });
    }
    if (method === "POST" && pathname === `/agent/sessions/${state.sessionId}/runs`) {
      if (opts.startRunError) return fail(409, 60005, opts.startRunError);
      push("message.user", { text: body.message });
      push("run.status", { status: "queued" });
      return ok({ id: state.runId, session_id: state.sessionId, canvas_id: state.canvasId, status: "queued", mode: body.mode, budget_credits: body.budget_credits ?? 50 }, 202);
    }
    if (method === "GET" && pathname === `/agent/sessions/${state.sessionId}/events`) {
      if (opts.eventsError && calls.filter((c) => c.path.endsWith("/events")).length <= opts.eventsError) return fail(500, 50000, "内部错误");
      releaseBatch();
      const after = Number(u.searchParams.get("after") ?? 0);
      const limit = Number(u.searchParams.get("limit") ?? 200) || 200;
      return ok(events.filter((e) => e.seq > after).slice(0, limit));
    }
    const dec = pathname.match(/^\/agent\/approvals\/([^/]+)\/decision$/);
    if (method === "POST" && dec) {
      if (opts.decisionError) return fail(400, 60010, opts.decisionError);
      state.decisions.push({ id: dec[1], body });
      const created = events.find((e) => e.type === "approval.created" && e.data.id === dec[1]);
      push("run.status", { status: "running" });
      push("approval.decided", { ...created.data, status: body.decision === "reject" ? "rejected" : "approved", decision: body });
      if (blockedOn === dec[1]) blockedOn = null;
      return ok({ ...created.data, status: "approved" });
    }
    if (method === "POST" && pathname === `/agent/runs/${state.runId}/cancel`) {
      state.cancelCount++;
      if (!TERMINAL.has(lastStatus())) push("run.status", { status: "canceled" });
      canceled = true;
      return ok({ id: state.runId, status: "canceled" });
    }
    const tk = pathname.match(/^\/generation-tasks\/([^/]+)$/);
    if (method === "GET" && tk) {
      const seq = opts.tasks?.[tk[1]];
      if (!seq) return fail(404, 40400, "任务不存在");
      const i = (state.taskPolls[tk[1]] = (state.taskPolls[tk[1]] ?? 0) + 1);
      return ok({ id: tk[1], node_id: seq.node_id, ...seq.steps[Math.min(i, seq.steps.length) - 1] });
    }
    if (method === "GET" && pathname === `/admin/agent/runs/${state.runId}/trace`) {
      if (opts.traceError) return fail(opts.traceError.status ?? 400, opts.traceError.code ?? 60020, opts.traceError.msg);
      if (!opts.trace) return fail(404, 40400, "接口不存在");
      return ok({ files: opts.trace.files });
    }
    return fail(404, 40400, `fake backend: 未实现 ${method} ${pathname}`);
  }

  return { fetch, calls, events, state, callsTo: (method, re) => calls.filter((c) => c.method === method && re.test(c.path)) };
}

/** 一份假的后端轨迹包（只含契约里的文件，内容是最小可用的）。 */
export function fakeTraceBundle(extra = {}) {
  const meta = { run_id: "r1", user_id: "u1", canvas_id: "c1", mode: "storyboard", status: "succeeded", prompt_version: 10, agent_model_key: "m-test", tools_hash: "abcd1234abcd1234", skill_pins: {}, harness_case: "storyboard-9x16-3shots", started_at: "2026-10-20T08:00:00Z", ended_at: "2026-10-20T08:00:41Z", trace_complete: true, truncated: false, exported_at: "2026-10-20T08:01:00Z", exported_by: "admin@example.com", ...extra.meta };
  return {
    files: {
      "report.md": "# Agent 运行报告 · run r1\n> 终态 succeeded · prompt v10 · 模型 m-test · 5 步 · 积分 0 · 耗时 41s\n> 用户原话：拆分镜\n\n## 可疑信号\n- 无\n\n## 时间线\n| # | span | 名称 | 耗时 | 状态 | 摘要 |\n| 1 | chat | m-test | 3100ms | ok | x |\n\n## 最终画布概要\n（后端生成）\n",
      "trace.jsonl": '{"seq":0,"kind":"system","prompt_version":10,"tools_hash":"abcd1234abcd1234","system_prompt":"…","tools":[]}\n{"seq":1,"span_id":"c1","parent_span_id":null,"kind":"chat","name":"m-test","status":"ok","summary":"x","started_at":"2026-10-20T08:00:01Z","duration_ms":3100,"error_code":"","request":{"model":"m-test"},"response":{"text":"原文"}}\n',
      "events.jsonl": "",
      "mutations.jsonl": "",
      "canvas.before.json": '{"nodes":[],"edges":[]}',
      "canvas.after.json": '{"nodes":[],"edges":[]}',
      "meta.json": JSON.stringify(meta),
      ...extra.files,
    },
  };
}

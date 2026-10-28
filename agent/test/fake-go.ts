import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { createIdCodec } from "../src/ids.ts";
import type { PlatformUsage, RunSetup, ToolDecl } from "../src/platform-client.ts";

/**
 * 假的 Go：同一个地址上既有平台接口（运行准备、工具、审批三接口、用量、释放运行），也有模型代理（OpenAI 兼容的流式端点）。
 * 引擎的测试用它：模型的每一轮回答按 turns 里的脚本给出，工具和审批的回复可以按名字设定。
 */
export interface Turn {
  text?: string;
  thinking?: string;
  tools?: Array<{ id: string; name: string; args?: unknown }>;
  /** 响应开始前等多久 */
  delayMs?: number;
  /** 每个字之间等多久（模拟流式输出的节奏） */
  charDelayMs?: number;
  /** 发完文字后停住不结束，直到请求被关闭（模拟慢模型，用来测取消） */
  hang?: boolean;
  /** 结束原因，默认按有没有工具调用给 stop / tool_calls；"length" 表示输出被截断 */
  finishReason?: string;
  /** 直接回错误 */
  error?: { status: number; message: string; type?: string; errorCode?: number };
}

export interface GoSeen {
  method: string;
  path: string;
  headers: IncomingMessage["headers"];
  body: any;
}

type ToolReply = { content?: string; is_error?: boolean; terminate?: boolean; tool_end?: Record<string, unknown>; images?: unknown[] };

export interface FakeGoOptions {
  turns: Turn[];
  /** 工具名 → 回复；函数形式可以延迟、按参数变化 */
  tools?: Record<string, ToolReply | ((body: any, headers: IncomingMessage["headers"]) => Promise<ToolReply> | ToolReply)>;
  /** 审批预估的回复，按审批种类 */
  propose?: Record<string, { is_error?: boolean; payload?: unknown; quote_credits?: number; summary?: string; content?: string; terminate?: boolean }>;
  /** 审批解析：默认拒绝 → rejected，否则 approved；预估积分取 payload.quote_credits */
  resolve?: (body: any) => Record<string, unknown> | { error: { status: number; code: number; msg: string } };
  /** 审批执行：默认批准的删除 / 生成 executed，拒绝和提问 none */
  execute?: (body: any) => Record<string, unknown> | { error: { status: number; code: number; msg: string } } | "down";
  /** 摘要请求（X-Agent-Purpose: summary）的回答；函数形式可以按请求变化或出错。不消耗 turns */
  summary?: string | ((body: any, n: number) => string | { error: { status: number; message: string } });
  /** 会话命名请求（X-Agent-Purpose: title）的回答；函数形式可以按请求变化或出错。不消耗 turns */
  title?: string | ((body: any, n: number) => string | { error: { status: number; message: string } });
  /** 活动摘要请求（X-Agent-Purpose: activity）的回答；函数形式可以按请求变化或出错。不消耗 turns */
  activity?: string | ((body: any, n: number) => string | { error: { status: number; message: string } });
  /** 会话侧：模型清单、画布归属、消息引用、撤销 */
  models?: unknown[];
  checkCanvas?: (body: any) => { error: { status: number; code: number; msg: string } } | void;
  checkMessage?: (body: any) => { selection: string[] } | { error: { status: number; code: number; msg: string } };
  /** 附件校验：默认每个素材 id 都通过，给一张 800×600 的 PNG（本地存储，image 是 base64 QUJD） */
  checkAttachments?: (body: any) => { attachments: unknown[] } | { error: { status: number; code: number; msg: string } };
  undo?: (runId: string) => Record<string, unknown> | { error: { status: number; code: number; msg: string } };
  /** 运行准备的出参覆盖项 */
  setup?: Partial<RunSetup>;
  prepareError?: { status: number; code: number; msg: string };
  /** Go 的 ID 编码密钥（ID 核对用），默认和 durable 测试里的一样 */
  idKey?: string;
  /** 每次模型调用的积分，默认 1 */
  creditsPerCall?: number;
  usage?: (runId: string, callId: string | undefined, calls: number) => PlatformUsage;
}

/** 和 Go 里登记的界面名称、短动词一致 */
const LABELS: Record<string, [string, string]> = {
  canvas_get_state: ["读取画布", "读取"], canvas_apply_ops: ["修改画布", "修改"], generate_media: ["申请生成", "生成"], canvas_delete: ["申请删除", "删除"],
  ask_user: ["向你提问", "提问"], plan_update: ["更新计划", "计划"], model_list: ["查看可用模型", "查模型"],
};

export const DECLS: ToolDecl[] = Object.keys(LABELS).map((name) => ({
  name, label: LABELS[name][0], verb: LABELS[name][1], description: "说明 " + name, executor: "server", scriptable: false,
  parameters: { type: "object", properties: { nodeIds: { type: "array", items: { type: "string" } } } },
}));

export async function fakeGo(opts: FakeGoOptions) {
  const seen: GoSeen[] = [];
  const callLog: Array<{ callId: string; purpose: "main" | "summary" | "title" | "activity" }> = [];
  let prepareError = opts.prepareError;
  const turns = [...opts.turns];
  let nextCallId = 7_000_000_000_000_000_001n;
  let calls = 0;
  const open = new Set<ServerResponse>();

  const json = (res: ServerResponse, status: number, body: unknown) => {
    res.writeHead(status, { "content-type": "application/json" });
    res.end(JSON.stringify(body));
  };
  const ok = (res: ServerResponse, data: unknown) => json(res, 200, { code: 0, data });

  const server = createServer((req, res) => {
    let raw = "";
    req.on("data", (d) => (raw += d));
    req.on("end", async () => {
      const path = req.url ?? "";
      const body = raw ? JSON.parse(raw) : undefined;
      seen.push({ method: req.method ?? "", path, headers: req.headers, body });
      const base = "/internal/platform/v1";
      try {
        if (path === `${base}/runs/prepare`) {
          if (prepareError) return json(res, prepareError.status, { code: prepareError.code, msg: prepareError.msg });
          return ok(res, {
            model: { name: "Claude", context_window: 200000, max_tokens: 8192, vision: true },
            system_prompt: "你是小镜", tools: DECLS, disabled_tools: [],
            ...(body?.message ? { first_message: `${body.message}\n\n<画布目录>（数据）</画布目录>\n最多 ${body.max_steps || 40} 步工具调用。` } : {}),
            harness: { prompt_version: 15, tools_hash: "04c13780543e", build_sha: "abc1234", key: "harnesskey01" },
            ...opts.setup,
          });
        }
        if (path.startsWith(`${base}/tools/`)) {
          const name = decodeURIComponent(path.slice(`${base}/tools/`.length));
          const h = opts.tools?.[name];
          const r = typeof h === "function" ? await h(body, req.headers) : h;
          return ok(res, {
            content: "ok", is_error: false, terminate: false, ...r,
            tool_end: r?.tool_end ?? { id: body.tool_call_id, name, is_error: !!r?.is_error, summary: "摘要 " + name, label: LABELS[name]?.[0], verb: LABELS[name]?.[1] },
          });
        }
        if (path === `${base}/approvals/propose`) {
          const p = opts.propose?.[body.kind] ?? {};
          return ok(res, { is_error: false, payload: { kind: body.kind, args: body.args }, quote_credits: 0, summary: "申请 " + body.kind, content: "已提交，等待决定。", terminate: true, ...p });
        }
        if (path === `${base}/events`) return ok(res, null); // 事件推送：只记录
        const idm = /^\/internal\/platform\/v1\/ids\/(\d+)$/.exec(path);
        if (idm) return ok(res, { encoded: createIdCodec(opts.idKey ?? "test-id-key").encode(BigInt(idm[1])) });
        if (path === `${base}/agent/models`) return ok(res, opts.models ?? [{ key: "claude", name: "Claude", vision: true, context_window: 200000 }]);
        if (path === `${base}/canvases/check`) {
          const r = opts.checkCanvas?.(body);
          if (r && isFailure(r)) return json(res, r.error.status, { code: r.error.code, msg: r.error.msg });
          return ok(res, null);
        }
        if (path === `${base}/attachments/check`) {
          const r = opts.checkAttachments?.(body) ?? { attachments: (body.asset_ids as string[]).map((id, i) => ({ asset_id: id, name: `图片 ${i + 1}`, mime_type: "image/png", width: 800, height: 600, url: `/files/u1/${id}.png`, image: { mime_type: "image/png", data: "QUJD" } })) };
          if (isFailure(r)) return json(res, r.error.status, { code: r.error.code, msg: r.error.msg });
          return ok(res, r);
        }
        if (path === `${base}/messages/check`) {
          const r = opts.checkMessage?.(body) ?? { selection: body.selection ?? [] };
          if (isFailure(r)) return json(res, r.error.status, { code: r.error.code, msg: r.error.msg });
          return ok(res, r);
        }
        if (req.method === "POST" && /\/runs\/\d+\/undo$/.test(path)) {
          const r = opts.undo?.(path.split("/").at(-2)!) ?? { reverted: 1, skipped: [], revision: 5 };
          if (isFailure(r)) return json(res, r.error.status, { code: r.error.code, msg: r.error.msg });
          return ok(res, r);
        }
        if (path === `${base}/approvals/resolve`) {
          const r = opts.resolve?.(body) ?? defaultResolve(body);
          if (isFailure(r)) return json(res, r.error.status, { code: r.error.code, msg: r.error.msg });
          return ok(res, r);
        }
        if (path === `${base}/approvals/execute`) {
          const r = opts.execute?.(body) ?? defaultExecute(body);
          if (r === "down") return void res.destroy();
          if (isFailure(r)) return json(res, r.error.status, { code: r.error.code, msg: r.error.msg });
          return ok(res, r);
        }
        if (req.method === "GET" && path.startsWith(`${base}/runs/`) && path.includes("/usage")) {
          const runId = path.slice(`${base}/runs/`.length).split("/")[0];
          const callId = new URL(path, "http://x").searchParams.get("call_id") ?? undefined;
          const u = opts.usage?.(runId, callId, calls) ?? defaultUsage(callId, calls, opts.creditsPerCall ?? 1);
          return ok(res, u);
        }
        if (req.method === "DELETE" && path.startsWith(`${base}/runs/`)) return ok(res, null);
        if (path === `${base}/model/chat/completions`) return await (req.headers["x-agent-purpose"] === "summary" ? summarize(res, body) : req.headers["x-agent-purpose"] === "title" ? nameSession(res, body) : req.headers["x-agent-purpose"] === "activity" ? summarizeActivity(res, body) : model(res, body));
        json(res, 404, { code: 10002, msg: "资源不存在：" + path });
      } catch (e) {
        json(res, 500, { code: 50000, msg: String(e) });
      }
    });
  });

  let summaries = 0;
  let titles = 0;
  let activities = 0;
  async function summarize(res: ServerResponse, body: any) {
    return sideCall(res, "summary", typeof opts.summary === "function" ? opts.summary(body, ++summaries) : (opts.summary ?? "## 目标\n摘要正文"));
  }
  async function nameSession(res: ServerResponse, body: any) {
    return sideCall(res, "title", typeof opts.title === "function" ? opts.title(body, ++titles) : (opts.title ?? "默认标题"));
  }
  async function summarizeActivity(res: ServerResponse, body: any) {
    return sideCall(res, "activity", typeof opts.activity === "function" ? opts.activity(body, ++activities) : (opts.activity ?? "默认活动说明"));
  }
  /** 摘要、命名、活动说明这类不是助手说的话的调用：不消耗 turns，照常计费 */
  function sideCall(res: ServerResponse, purpose: "summary" | "title" | "activity", r: string | { error: { status: number; message: string } }) {
    if (typeof r !== "string") {
      res.writeHead(r.error.status, { "content-type": "application/json" });
      return void res.end(JSON.stringify({ error: { message: r.error.message, type: "upstream_error" } }));
    }
    calls++;
    const callId = nextCallId++;
    callLog.push({ callId: String(callId), purpose });
    res.writeHead(200, { "content-type": "text/event-stream", "x-agent-call-id": String(callId) });
    const chunk = (delta: object, finish: string | null = null) => `data: ${JSON.stringify({ id: "x", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`;
    res.write(chunk({ role: "assistant", content: r }));
    res.write(chunk({}, "stop"));
    res.write(`data: ${JSON.stringify({ id: "x", object: "chat.completion.chunk", choices: [], usage: { prompt_tokens: 500, completion_tokens: 80, total_tokens: 580 } })}\n\n`);
    res.end("data: [DONE]\n\n");
  }

  /** 按请求的大小报输入用量（约 3 个字符一个 Token）：Pi 估算上下文大小时会用上一次回答报告的真实用量 */
  const promptTokens = (body: any) => Math.max(100, Math.ceil(JSON.stringify(body?.messages ?? []).length / 3));

  async function model(res: ServerResponse, body: any) {
    const turn = turns.shift() ?? { text: "（没有更多预设的回答）" };
    if (turn.delayMs) await new Promise((r) => setTimeout(r, turn.delayMs));
    if (turn.error) {
      const headers: Record<string, string> = { "content-type": "application/json" };
      if (turn.error.errorCode) headers["x-agent-error-code"] = String(turn.error.errorCode);
      res.writeHead(turn.error.status, headers);
      return void res.end(JSON.stringify({ error: { message: turn.error.message, type: turn.error.type ?? "agent_error" } }));
    }
    calls++;
    const callId = nextCallId++;
    callLog.push({ callId: String(callId), purpose: "main" });
    res.writeHead(200, { "content-type": "text/event-stream", "x-agent-call-id": String(callId) });
    open.add(res);
    res.on("close", () => open.delete(res));
    const chunk = (delta: object, finish: string | null = null) => `data: ${JSON.stringify({ id: "x", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: finish }] })}\n\n`;
    res.write(chunk({ role: "assistant", content: "" }));
    if (turn.thinking) res.write(chunk({ reasoning_content: turn.thinking }));
    for (const ch of Array.from(turn.text ?? "")) {
      res.write(chunk({ content: ch }));
      if (turn.charDelayMs) await new Promise((r) => setTimeout(r, turn.charDelayMs));
    }
    if (turn.hang) return; // 不结束：等调用方断开
    (turn.tools ?? []).forEach((t, i) => res.write(chunk({ tool_calls: [{ index: i, id: t.id, type: "function", function: { name: t.name, arguments: JSON.stringify(t.args ?? {}) } }] })));
    res.write(chunk({}, turn.finishReason ?? (turn.tools?.length ? "tool_calls" : "stop")));
    res.write(`data: ${JSON.stringify({ id: "x", object: "chat.completion.chunk", choices: [], usage: { prompt_tokens: promptTokens(body), completion_tokens: 20, total_tokens: promptTokens(body) + 20 } })}\n\n`);
    res.end("data: [DONE]\n\n");
  }

  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  return {
    url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`,
    seen,
    /** 每次成功的模型调用的 id 和用途，按服务顺序 */
    callLog,
    /** 之后的运行准备都失败（null 恢复） */
    setPrepareError: (e: FakeGoOptions["prepareError"] | null) => { prepareError = e ?? undefined; },
    /** 模型请求（按顺序） */
    get modelRequests() { return seen.filter((s) => s.path.endsWith("/model/chat/completions") && s.headers["x-agent-purpose"] !== "summary" && s.headers["x-agent-purpose"] !== "title" && s.headers["x-agent-purpose"] !== "activity"); },
    /** 活动摘要请求（按顺序） */
    get activityRequests() { return seen.filter((s) => s.path.endsWith("/model/chat/completions") && s.headers["x-agent-purpose"] === "activity"); },
    /** 会话命名请求（按顺序） */
    get titleRequests() { return seen.filter((s) => s.path.endsWith("/model/chat/completions") && s.headers["x-agent-purpose"] === "title"); },
    /** 摘要请求（按顺序） */
    get summaryRequests() { return seen.filter((s) => s.path.endsWith("/model/chat/completions") && s.headers["x-agent-purpose"] === "summary"); },
    paths: () => seen.map((s) => `${s.method} ${s.path.replace("/internal/platform/v1", "")}`),
    close: () => new Promise<void>((r) => { for (const o of open) o.destroy(); server.close(() => r()); }),
  };
}

function defaultUsage(callId: string | undefined, calls: number, credits: number): PlatformUsage {
  return {
    spent_credits: calls * credits, context_tokens: calls * 100,
    ...(callId ? { call: { id: callId, status: "settled" as const, credits, charged: credits, estimated: false, input_tokens: 100, output_tokens: 20 } } : {}),
  };
}

function defaultResolve(body: any) {
  const reject = body.decision?.decision === "reject";
  if (reject) return { status: "rejected", decision: { decision: "reject", ...(body.decision.answer && { answer: body.decision.answer }) }, quote_credits: 0 };
  return { status: "approved", decision: { decision: "approve", ...(body.decision?.answer && { answer: body.decision.answer }) }, quote_credits: Number(body.payload?.quote_credits ?? 0), node_ids: body.payload?.node_ids ?? [] };
}

function defaultExecute(body: any) {
  const o = body.outcome;
  if (o.status === "rejected") return { status: "none", outcome_text: "用户拒绝了这次请求。" + (o.decision?.answer ? "用户的说明：" + o.decision.answer : "") };
  if (body.kind === "ask") return { status: "none", outcome_text: "用户回答：" + o.decision?.answer };
  return { status: "executed", result: { ok: true }, outcome_text: `用户批准了${body.kind}，已执行。` };
}

type Failure = { error: { status: number; code: number; msg: string } };
const isFailure = (r: unknown): r is Failure => typeof r === "object" && r !== null && "error" in r && typeof (r as Failure).error === "object";

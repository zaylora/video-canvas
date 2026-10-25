/**
 * 平台客户端：Agent 服务调用 Go 平台能力的全部入口（工具、运行准备、审批三接口、用量、释放运行）。
 * 认证靠服务令牌；运行信息放在 X-Agent-Context 头里，Go 据此知道这是谁的哪个运行。
 * 模型请求不在这里发，见 model-provider.ts。
 *
 * 响应约定：Go 的标准信封 {code, msg, data}。code 不为 0、HTTP 非 2xx、连不上，都是 PlatformError；
 * 请求被 signal 中止时抛出的是原来的中止错误，不包装：调用方要分得清「被取消」和「出错」。
 */
import type { RelayEvent } from "./event-pusher.ts";
import { parseDecimal } from "./ids.ts";

/** 运行上下文。ID 一律是十进制字符串：雪花 ID 超过 2^53，用数字会在 JavaScript 里丢精度。 */
export interface RunContext {
  run_id: string;
  user_id: string;
  session_id: string;
  canvas_id: string;
  /** 任务模式，空按全能创作 */
  mode?: string;
  /** 本次运行的 Agent 模型 */
  model_key?: string;
  /** 用户选中的节点 */
  selection?: string[];
  /** 本轮积分预算，0 或不传表示不限制 */
  budget_credits?: number;
  /** 除模型调用外本轮已花的积分（已批准的生成） */
  spent_extra?: number;
}

export class PlatformError extends Error {
  /** HTTP 状态码；0 表示连不上 */
  status: number;
  /** Go 的业务错误码；非 JSON 响应或连不上时没有 */
  code: number | undefined;

  constructor(message: string, status: number, code?: number) {
    super(message);
    this.name = "PlatformError";
    this.status = status;
    this.code = code;
  }
}

const MAX_CONTEXT_HEADER = 16 << 10; // 和 Go 的 maxRunContextHeader 一致
const IDEM_KEY = /^[A-Za-z0-9._:-]{1,64}$/;
const ID_FIELDS = ["run_id", "user_id", "session_id", "canvas_id"] as const;

/** 运行上下文 → 请求头的值：JSON 再做 base64url（无填充）。一个头装下所有字段，也不用操心非 ASCII 字符。 */
export function encodeRunContext(rc: RunContext): string {
  for (const f of ID_FIELDS) {
    const v = rc[f];
    try {
      if (typeof v !== "string") throw new Error("not a string");
      if (parseDecimal(v) === 0n) throw new Error("zero");
    } catch {
      throw new Error(`运行上下文的 ${f} 必须是正整数的十进制字符串，当前是 ${JSON.stringify(v)}`);
    }
  }
  const header = Buffer.from(JSON.stringify(rc), "utf8").toString("base64url");
  if (header.length > MAX_CONTEXT_HEADER) throw new Error(`运行上下文太大（${header.length} 字节，上限 ${MAX_CONTEXT_HEADER}）：选中的节点太多`);
  return header;
}

export function decodeRunContext(header: string): RunContext {
  return JSON.parse(Buffer.from(header, "base64url").toString("utf8")) as RunContext;
}

// ---- 响应类型：字段名和 Go 的 JSON 一致（蛇形） ----

export interface ToolImage {
  mime_type: string;
  data?: string;
  url?: string;
}

/** tool.end 事件的内容，由 Go 给出（摘要、被改动的节点、界面名称……），Agent 原样落库 */
export type ToolEndEvent = { id: string; name: string; is_error: boolean; summary: string; label?: string; verb?: string } & Record<string, unknown>;

export interface PlatformToolResult {
  content: string;
  is_error: boolean;
  terminate: boolean;
  images?: ToolImage[];
  tool_end?: ToolEndEvent;
}

export interface ToolDecl {
  name: string;
  label: string;
  /** 短动词：tool.start / tool.end 事件带着它，前端按动词归并活动块 */
  verb: string;
  description: string;
  parameters: Record<string, unknown>;
  executor: string;
  scriptable: boolean;
}

export interface RunSetup {
  model: { name: string; context_window: number; max_tokens: number; vision: boolean };
  system_prompt: string;
  tools: ToolDecl[];
  disabled_tools?: string[];
  first_message?: string;
  harness: { prompt_version: number; tools_hash: string; build_sha: string; key: string };
}

export type ApprovalKind = "generate" | "delete" | "ask";

export interface ApprovalProposal {
  is_error: boolean;
  payload?: unknown;
  quote_credits: number;
  summary?: string;
  content: string;
  terminate: boolean;
}

export interface ApprovalDecision {
  decision: "approve" | "reject";
  items?: Array<{ index: number; approve: boolean; count?: number; value?: string }>;
  answer?: string;
  add_budget?: number;
}

export interface ApprovalOutcome {
  status: "approved" | "partially_approved" | "rejected";
  decision: Record<string, unknown>;
  quote_credits: number;
  items?: unknown[];
  node_ids?: string[];
  edge_ids?: string[];
}

export interface ApprovalExecution {
  status: "none" | "executed" | "failed";
  result?: unknown;
  outcome_text: string;
}

export interface PlatformUsage {
  spent_credits: number;
  context_tokens: number;
  call?: {
    id: string;
    status: "pending" | "settled";
    credits: number;
    charged: number;
    estimated: boolean;
    input_tokens: number;
    output_tokens: number;
    error?: string;
  };
}

export interface AgentModel {
  key: string;
  name: string;
  vision: boolean;
  /** 上下文窗口（Token），没配置是 0；画布据此显示上下文用量 */
  context_window: number;
}

export interface UndoResult {
  /** 恢复或删除了多少项 */
  reverted: number;
  skipped: Array<Record<string, unknown>>;
  /** 撤销后的画布 revision；运行根本没改过画布时为 0（调用方据此不发 run.undone） */
  revision: number;
}

export interface PlatformOptions {
  /** Go 的桥地址，如 http://backend:47001 */
  baseUrl: string;
  serviceToken: string;
  fetchImpl?: typeof fetch;
}

export interface ToolCallOptions {
  /** 模型给这次调用的 id，Go 只用于记录和展示；不是幂等键（它在不同回合里可能重复） */
  toolCallId: string;
  /** 幂等键：同一个运行里同一个键的同一种写入只会发生一次。用 Pi 的工具任务 id：每次调用唯一、同一次调用重放时不变 */
  idemKey?: string;
  signal?: AbortSignal;
}

export function createPlatform({ baseUrl, serviceToken, fetchImpl = fetch }: PlatformOptions) {
  const base = baseUrl.replace(/\/+$/, "") + "/internal/platform/v1";

  async function call<T>(method: string, path: string, rc: RunContext | undefined, body?: unknown, extra: { idemKey?: string; signal?: AbortSignal } = {}): Promise<T> {
    if (extra.idemKey !== undefined && !IDEM_KEY.test(extra.idemKey)) {
      throw new Error(`幂等键只能是 1 到 64 个字母、数字或 . _ : -，当前是 ${JSON.stringify(extra.idemKey)}`);
    }
    const headers: Record<string, string> = { "X-Agent-Service-Token": serviceToken };
    if (rc) headers["X-Agent-Context"] = encodeRunContext(rc);
    if (extra.idemKey) headers["X-Idempotency-Key"] = extra.idemKey;
    if (body !== undefined) headers["content-type"] = "application/json";
    let res: Response;
    try {
      res = await fetchImpl(base + path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), signal: extra.signal });
    } catch (e) {
      if (extra.signal?.aborted) throw e;
      throw new PlatformError(`连接平台失败（${base}）：${String((e as Error)?.message ?? e)}`, 0);
    }
    const env = (await res.json().catch(() => null)) as { code: number; msg?: string; data: T } | null;
    if (!res.ok || !env || env.code !== 0) {
      throw new PlatformError(env?.msg || `平台返回 HTTP ${res.status}`, res.status, env?.code);
    }
    return env.data;
  }

  const userCanvas = (userId: string, canvasId: string) => {
    for (const [name, v] of [["user_id", userId], ["canvas_id", canvasId]] as const) {
      try {
        if (parseDecimal(v) === 0n) throw new Error("zero");
      } catch {
        throw new Error(`${name} 必须是正整数的十进制字符串，当前是 ${JSON.stringify(v)}`);
      }
    }
    return { user_id: userId, canvas_id: canvasId };
  };

  return {
    /** 某个 id 在 Go 那边编码后的串：启动时用它核对两边的 ID 编码密钥是否一致（id-check.ts） */
    encodeId: async (n: string, signal?: AbortSignal): Promise<string> => (await call<{ encoded: string }>("GET", `/ids/${encodeURIComponent(n)}`, undefined, undefined, { signal })).encoded,
    /** 当前可用的 Agent 模型（没有时是空数组） */
    models: (signal?: AbortSignal) => call<AgentModel[]>("GET", "/agent/models", undefined, undefined, { signal }),
    /** 确认画布属于这个用户；别人的和不存在的一样抛「画布不存在」（30001） */
    checkCanvas: async (userId: string, canvasId: string, signal?: AbortSignal): Promise<void> => {
      await call<null>("POST", "/canvases/check", undefined, userCanvas(userId, canvasId), { signal });
    },
    /** 校验消息里的引用（节点、模型、技能），返回去掉了已不存在节点的选中列表 */
    checkMessage: async (userId: string, canvasId: string, message: string, selection: string[] = [], signal?: AbortSignal): Promise<string[]> =>
      (await call<{ selection: string[] }>("POST", "/messages/check", undefined, { ...userCanvas(userId, canvasId), message, selection }, { signal })).selection,
    /** 撤销某运行对画布的全部改动（改动记录在 Go）。运行是不是自己的、是否已结束由调用方校验 */
    undo: (rc: RunContext, signal?: AbortSignal) => call<UndoResult>("POST", `/runs/${rc.run_id}/undo`, rc, undefined, { signal }),
    /** 执行一次工具调用。工具层的失败是 is_error 的普通结果；调用本身出错才抛 PlatformError */
    tool: (rc: RunContext, name: string, args: unknown, o: ToolCallOptions) =>
      call<PlatformToolResult>("POST", `/tools/${encodeURIComponent(name)}`, rc, { tool_call_id: o.toolCallId, args: args ?? {} }, { idemKey: o.idemKey, signal: o.signal }),
    /** 运行准备：模型规格、系统提示词、工具声明、版本；带 message 时还有第一条用户消息。不带 message 是续跑 */
    prepare: (rc: RunContext, req: { message?: string; max_steps?: number } = {}, signal?: AbortSignal) => call<RunSetup>("POST", "/runs/prepare", rc, req, { signal }),
    /** 审批预估：工具参数 → 审批内容和预估积分，不落库 */
    propose: (rc: RunContext, kind: ApprovalKind, args: unknown, signal?: AbortSignal) => call<ApprovalProposal>("POST", "/approvals/propose", rc, { kind, args: args ?? {} }, { signal }),
    /** 审批解析：用户的决定 → 实际批准了什么（逐项勾选、张数、重新算价、检查预算） */
    resolve: (rc: RunContext, kind: ApprovalKind, payload: unknown, decision: ApprovalDecision, signal?: AbortSignal) =>
      call<ApprovalOutcome>("POST", "/approvals/resolve", rc, { kind, payload, decision }, { signal }),
    /** 审批执行：删除或创建生成任务；幂等，键是审批 id。审批 id 是十进制字符串 */
    execute: async (rc: RunContext, kind: ApprovalKind, approvalId: string, toolCallId: string, outcome: ApprovalOutcome, signal?: AbortSignal) => {
      try {
        if (parseDecimal(approvalId) === 0n) throw new Error("zero");
      } catch {
        throw new Error(`审批 id 必须是正整数的十进制字符串，当前是 ${JSON.stringify(approvalId)}`);
      }
      return call<ApprovalExecution>("POST", "/approvals/execute", rc, { kind, approval_id: approvalId, tool_call_id: toolCallId, outcome }, { signal });
    },
    /** 运行的用量；带 callId（模型调用的 id，十进制字符串）时一并给出这次调用的结算情况 */
    usage: (rc: RunContext, callId?: string, signal?: AbortSignal) =>
      call<PlatformUsage>("GET", `/runs/${rc.run_id}/usage${callId ? `?call_id=${encodeURIComponent(callId)}` : ""}`, rc, undefined, { signal }),
    /** 把事件成批推给 Go，由它经 WebSocket 发给浏览器（顺序由调用方保证）；事件已落在 Agent 自己的库里，推送失败不影响运行 */
    pushEvents: async (events: RelayEvent[], signal?: AbortSignal): Promise<void> => {
      await call<null>("POST", "/events", undefined, { events }, { signal });
    },
    /** 运行结束：释放 Go 内存里这个运行的状态（联网次数、技能固定版本）；不调用也会在闲置过期后清掉 */
    endRun: (rc: RunContext, signal?: AbortSignal) => call<null>("DELETE", `/runs/${rc.run_id}`, rc, undefined, { signal }),
  };
}

export type Platform = ReturnType<typeof createPlatform>;

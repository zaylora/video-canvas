/**
 * 桥客户端：Node 进程和 Go 的全部通信都在这里。进程里没有任何渠道密钥，只有一个一次性的桥令牌。
 * 响应约定：Go 的标准格式 {code, msg, data}，code 不为 0 或 HTTP 非 2xx 都视为失败。
 */
import type { AgentMessage } from "@earendil-works/pi-agent-core";
import type { ToolEvent, ToolEventReply, ToolReply } from "./types.ts";

/** 服务令牌的请求头；没配返回空对象 */
export function serviceHeaders(serviceToken?: string): Record<string, string> {
  return serviceToken ? { "X-Agent-Service-Token": serviceToken } : {};
}

export class BridgeError extends Error {
  status: number;
  code: number | undefined;

  constructor(message: string, status: number, code?: number) {
    super(message);
    this.name = "BridgeError";
    this.status = status;
    this.code = code;
  }
}

/** Go 的标准响应 */
interface Envelope<T> {
  code: number;
  msg?: string;
  data: T;
}

export interface BridgeOptions {
  baseUrl: string;
  token: string;
  serviceToken?: string;
  fetchImpl?: typeof fetch;
}

/**
 * 配了服务令牌（Agent 作为独立服务从内网访问桥时）就随每个请求带上 X-Agent-Service-Token；模型请求不在这里发，见 serviceHeaders。
 */
export function createBridge({ baseUrl, token, serviceToken, fetchImpl = fetch }: BridgeOptions) {
  const base = baseUrl.replace(/\/+$/, "") + "/internal/agent/bridge";

  async function post<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
    const res = await fetchImpl(base + path, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: "Bearer " + token, ...serviceHeaders(serviceToken) },
      body: JSON.stringify(body),
      signal,
    });
    const json = (await res.json().catch(() => null)) as Envelope<T> | null;
    if (!res.ok || !json || json.code !== 0) {
      throw new BridgeError(json?.msg || `桥返回 HTTP ${res.status}`, res.status, json?.code);
    }
    return json.data;
  }

  return {
    /**
     * 执行一次工具调用，返回 {content, is_error, terminate}。
     * 在 codemode 脚本里发起的嵌套调用带 parentToolCallId：Go 据此不把它计入步数。
     */
    tool: (toolCallId: string, name: string, args: unknown, signal?: AbortSignal, { parentToolCallId }: { parentToolCallId?: string } = {}) =>
      post<ToolReply>("/tool", { tool_call_id: toolCallId, name, args, ...(parentToolCallId && { parent_tool_call_id: parentToolCallId }) }, signal),
    /**
     * Agent 自己的工具（不经 /tool）开始和结束时向 Go 报备，Go 据此在时间线里记这一步、计步数。
     * 开始时返回 {allowed, content, terminate}：步数用尽等情况 allowed 为 false。
     */
    toolEvent: (payload: ToolEvent, signal?: AbortSignal) => post<ToolEventReply | null>("/tool-event", payload, signal),
    /** 每个工具结果出来和回合结束时交回对话历史 */
    state: (messages: AgentMessage[]) => post<null>("/state", { messages }),
    /** 报告片段结束：done 跑完了；paused 因工具要求停下（等审批、等回答）；error 出错 */
    finish: (status: string, message = "") => post<null>("/finish", { status, message }),
  };
}

export type Bridge = ReturnType<typeof createBridge>;

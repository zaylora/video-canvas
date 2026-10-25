import type { AgentMessage, AgentTool } from "@earendil-works/pi-agent-core";
import type { Bridge } from "./bridge-client.ts";

/** 和 Go 通信的数据结构：启动参数、工具声明、工具结果。字段名跟 Go 的 JSON 一致（蛇形），不改。 */

/** Go 下发的工具参数 schema：普通 JSON Schema，不是 typebox */
export interface JsonSchema {
  type?: string;
  items?: JsonSchema;
  properties?: Record<string, JsonSchema>;
  [key: string]: unknown;
}

/** Go 下发的工具声明：工具的名字、说明和参数都由 Go 定，这里不内置任何业务工具 */
export interface ToolDecl {
  name: string;
  label?: string;
  description: string;
  parameters: JsonSchema;
  /** 谁来执行；目前只有 server（回调 Go） */
  executor?: string;
  /** 能不能在 codemode 脚本里调用 */
  scriptable?: boolean;
}

export interface ModelInfo {
  name?: string;
  context_window: number;
  max_tokens: number;
  vision?: boolean;
}

export type RunMode = "start" | "continue" | "resume";

/** 启动参数（stdin 第一行）。设计见 docs/design 的 13.7 */
export interface RunInput {
  bridge_url: string;
  /** 一次性运行令牌 */
  token: string;
  /** 桥的服务令牌，配了就随每个桥请求发出去 */
  service_token?: string;
  system_prompt?: string;
  model: ModelInfo;
  mode: RunMode;
  prompt?: { text: string; images?: { data: string; mime_type: string }[] };
  /** continue 模式：审批后要补回去的真实工具结果 */
  tool_result?: { tool_call_id: string; content: string };
  /** 上次保存的对话历史（不含系统提示） */
  messages?: AgentMessage[];
  tools?: ToolDecl[];
  /** 停用的 Agent 自己的工具名 */
  disabled_tools?: string[];
}

export interface RunResult {
  status: "done" | "paused" | "error";
  message: string;
}

export type Log = (msg: string, extra?: Record<string, unknown>) => void;

/** 桥 /tool 的返回 */
export interface ToolReply {
  content: string;
  is_error?: boolean;
  /** 本批工具执行完后停下，不再请求模型（等审批、等回答、步数用尽） */
  terminate?: boolean;
  /** 随结果交给模型看的图片；对象存储里的图片只有 url */
  images?: { mime_type: string; data?: string; url?: string }[];
}

/** Agent 自己的工具向 Go 报备开始和结束 */
export interface ToolEvent {
  phase: "start" | "end";
  tool_call_id: string;
  name: string;
  label?: string;
  verb?: string;
  args?: unknown;
  is_error?: boolean;
  summary?: string;
  result?: string;
}

export interface ToolEventReply {
  allowed?: boolean;
  content?: string;
  terminate?: boolean;
}

/** codemode 的运行限制 */
export interface CodemodeLimits {
  timeoutMs: number;
  maxNestedCalls: number;
  memoryLimitBytes: number;
  maxResultChars: number;
}

/** 创建 Agent 自己的工具时的上下文 */
export interface ToolCtx {
  bridge: Bridge;
  /** Go 下发的工具声明 */
  decls: ToolDecl[];
  vision: boolean;
  onTerminate: () => void;
  log: Log;
  limits?: Partial<CodemodeLimits>;
}

/** Agent 自己的工具注册表里的一项 */
export interface AgentToolSpec {
  name: string;
  label: string;
  verb: string;
  /** 这次运行要不要启用 */
  applies: (ctx: ToolCtx) => boolean;
  create: (ctx: ToolCtx) => AgentTool;
}

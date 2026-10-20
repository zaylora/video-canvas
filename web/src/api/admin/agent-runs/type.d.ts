/** 后台分页列表的统一返回：当页的行与命中总数 */
export interface AgentRunPage {
  list: AgentRunItem[];
  total: number;
  page: number;
  page_size: number;
}

/**
 * Agent 运行状态，以后端 model/agent.go 的 Run 状态常量为准：
 * 前四个是活跃状态（运行没结束），其余是终态或可继续的暂停态
 */
export type AgentRunStatus =
  | "queued"
  | "running"
  | "waiting_approval"
  | "waiting_input"
  | "succeeded"
  | "failed"
  | "canceled"
  | "budget_exhausted"
  | "step_limit"
  | "timeout"
  | "interrupted"
  | "expired";

/** 运行来源：user 是真实用户（harness_case 为空），harness 是调优命令行跑的用例 */
export type AgentRunSource = "user" | "harness";

/**
 * 轨迹包能不能下载：available 可下载；expired 超过保留期、原文已清；
 * disabled 当时 agent.trace.enabled=false，从没记过
 */
export type AgentRunTrace = "available" | "expired" | "disabled";

/** Agent 运行列表请求参数；契约见设计文档 8.4 */
export interface ListAgentRunsParams {
  /** 用户邮箱（包含匹配）或用户 id */
  user?: string;
  /** 精确匹配 */
  canvas_id?: string;
  /** 精确匹配 */
  run_id?: string;
  status?: AgentRunStatus;
  source?: AgentRunSource;
  /** 创建时间范围（RFC3339）；都不传时后端默认最近 7 天 */
  from?: string;
  to?: string;
  /** 页码，从 1 起，缺省 1 */
  page?: number;
  /** 每页条数，缺省 20，最大 100 */
  page_size?: number;
}

/** 列表里的一次 Agent 运行；id 都是 idcodec 编码后的字符串，时间是 RFC3339 */
export interface AgentRunItem {
  id: string;
  created_at: string;
  /** 运行还没到终态时为 null */
  ended_at: string | null;
  user_id: string;
  user_email: string;
  canvas_id: string;
  canvas_title: string;
  mode: string;
  status: AgentRunStatus;
  steps: number;
  /** execute_tool span 里 status=error 的数量；轨迹关闭或已过期时为 0 */
  tool_errors: number;
  spent_credits: number;
  /** 运行还没到终态时为 null */
  duration_ms: number | null;
  prompt_version: number;
  source: AgentRunSource;
  /** 来源为 harness 时的用例标签，真实用户为空串 */
  harness_case: string;
  trace: AgentRunTrace;
  /** false 表示轨迹不完整（上传失败、进程崩溃或超过上限） */
  trace_complete: boolean;
}

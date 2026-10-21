import type {
  AgentRunDetail,
  AgentRunDigest,
  AgentRunStep,
  AgentRunStepTool,
  AgentStatsGroup,
} from "@/api/admin/agent-run/type.d";

/** 测试用的运行摘要：默认是一次平平无奇的成功运行 */
export const digest = (patch: Partial<AgentRunDigest> = {}): AgentRunDigest => ({
  run_id: "0123456789abcdef0123456789abcdef",
  session_id: "s1",
  canvas_id: "c1",
  user_id: 42,
  mode: "auto",
  model_key: "gpt-test",
  prompt_version: 10,
  tools_hash: "th",
  build_sha: "abc1234",
  harness_key: "a1b2c3d4e5f6",
  skills: {},
  status: "succeeded",
  error_code: "",
  request_head: "把第三个镜头改成夜景",
  steps: 5,
  tool_calls: 4,
  tool_errors: 0,
  model_calls: 5,
  input_tokens: 1200,
  output_tokens: 300,
  spent_credits: 8,
  duration_ms: 65_000,
  approvals_total: 0,
  approvals_rejected: 0,
  steer_count: 0,
  mutation_count: 0,
  undone: false,
  feedback: 0,
  ended_at: "2026-10-22T09:01:05Z",
  created_at: "2026-10-22T09:00:00Z",
  updated_at: "2026-10-22T09:01:05Z",
  ...patch,
});

/** 测试用的统计分组 */
export const group = (patch: Partial<AgentStatsGroup> = {}): AgentStatsGroup => ({
  key: "a1b2c3d4e5f6",
  n: 50,
  feedback_rate: 0.2,
  down_rate: 0.3,
  undo_rate: 0.1,
  fail_end_rate: 0.05,
  median_duration_ms: 42_000,
  median_steps: 8,
  avg_credits: 3.5,
  prompt_version: 10,
  build_sha: "abc1234",
  last_at: "2026-10-22T09:00:00Z",
  ...patch,
});

export const step = (patch: Partial<AgentRunStep> = {}): AgentRunStep => ({
  n: 1,
  kind: "assistant",
  label: "模型回复",
  text: "好的",
  ...patch,
});

export const tool = (patch: Partial<AgentRunStepTool> = {}): AgentRunStepTool => ({
  name: "canvas_patch",
  duration_ms: 820,
  is_error: false,
  recorded: true,
  args: '{"node":"n1"}',
  result: "ok-result",
  summary: "改了 1 个节点",
  ...patch,
});

/** 测试用的运行详情：默认只有摘要，其余为空 */
export const detail = (patch: Partial<AgentRunDetail> = {}): AgentRunDetail => ({
  digest: digest(),
  run: null,
  timeline: [],
  events: [],
  tool_calls: [],
  model_calls: [],
  mutations: [],
  approvals: [],
  feedback: null,
  context: [],
  runtime_tail: "",
  ...patch,
});

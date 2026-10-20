// Agent 事件的解读：从 GET /agent/sessions/:sid/events 返回的事件里推出运行状态、步数、审批。
// 事件类型见 backend/internal/service/agent/agent.go 的 emit 调用点：
//   run.status {status, error?}、run.usage {spent_credits, budget_credits}、tool.start / tool.end {id,name,is_error,summary,parent_id?}、
//   message.done、approval.created / approval.decided（data 是审批视图 {id,kind,status,payload,...}）。

/** 运行状态常量，对应 backend/internal/model/agent.go（共 12 个）。 */
export const RUN_STATUSES = ["queued", "running", "waiting_approval", "waiting_input", "succeeded", "failed", "canceled", "budget_exhausted", "step_limit", "timeout", "interrupted", "expired"];

/** 活跃状态，对应 model.ActiveRunStatuses：运行还在往前走，或在等用户。 */
export const ACTIVE_STATUSES = ["queued", "running", "waiting_approval", "waiting_input"];

/** 终态 = 不在活跃状态里。budget_exhausted / step_limit / interrupted 虽然可以「继续」，harness 不自动续跑，也按终态处理。 */
export const isTerminalStatus = (status) => RUN_STATUSES.includes(status) && !ACTIVE_STATUSES.includes(status);

const str = (v) => (typeof v === "string" ? v : "");

/** 把 approval.created / approval.decided 事件规整成统一结构，answers 匹配、断言、报告都用它。 */
export function approvalFromEvent(event) {
  const a = event.data ?? {};
  const p = a.payload && typeof a.payload === "object" ? a.payload : {};
  const kind = str(a.kind);
  const out = {
    id: str(a.id),
    seq: event.seq,
    kind,
    ask_kind: kind === "ask" ? str(p.kind) : "", // ask_user 工具的 kind：choice（选项题）或 model（选模型题）
    model_kind: kind === "ask" ? str(p.model_kind) : "", // 选模型题要选哪类：image / video
    question: kind === "ask" ? str(p.question) : "",
    options: Array.isArray(p.options) ? p.options.map(String) : [],
    models: Array.isArray(p.models) ? p.models : [],
    quote: Number(a.quote_credits) || 0,
    payload: p,
    created_at: event.created_at,
  };
  // contains 匹配的文本：提问匹配问题原文；生成 / 删除匹配理由和节点标题
  const itemLabels = Array.isArray(p.items) ? p.items.map((it) => str(it?.label)) : [];
  out.text = kind === "ask" ? out.question : [str(p.reason), ...itemLabels, ...(Array.isArray(p.labels) ? p.labels.map(String) : []), ...(Array.isArray(p.node_ids) ? p.node_ids.map(String) : [])].filter(Boolean).join(" ");
  return out;
}

/** 事件是不是属于这个运行（会话级事件 run_id 为 null，一并保留给调用方决定）。 */
const ofRun = (e, runId) => !runId || e.run_id == null || e.run_id === runId;

/**
 * 从事件推出运行信息：
 * status 取最后一条 run.status；steps 是不带 parent_id 的 tool.end 数（嵌套的脚本调用不计步，和后端一致）；
 * tool_errors 是 is_error 的 tool.end 数；spent_credits 取最后一条 run.usage；approvals 是出现过的审批。
 */
export function deriveRun(events, runId) {
  const run = { status: "queued", error: "", steps: 0, tool_errors: 0, spent_credits: 0, approvals: [], decided: {}, last_seq: 0 };
  for (const e of events) {
    if (!ofRun(e, runId)) continue;
    run.last_seq = Math.max(run.last_seq, e.seq ?? 0);
    const d = e.data ?? {};
    switch (e.type) {
      case "run.status":
        run.status = str(d.status) || run.status;
        run.error = str(d.error);
        break;
      case "run.usage":
        run.spent_credits = Number(d.spent_credits) || 0;
        break;
      case "tool.end":
        if (!d.parent_id) run.steps++;
        if (d.is_error) run.tool_errors++;
        break;
      case "approval.created":
        run.approvals.push(approvalFromEvent(e));
        break;
      case "approval.decided":
        run.decided[str(d.id)] = d;
        break;
    }
  }
  return run;
}

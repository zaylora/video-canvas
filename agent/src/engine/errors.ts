/** 引擎对外的业务错误：错误码、HTTP 状态和说明与 Go 的 errcode 一致，REST 层（3.3）原样返回。 */
export class AgentError extends Error {
  code: number;
  status: number;

  constructor(code: number, status: number, message: string) {
    super(message);
    this.name = "AgentError";
    this.code = code;
    this.status = status;
  }
}

const e = (code: number, status: number, msg: string) => () => new AgentError(code, status, msg);

export const Err = {
  invalidParams: (msg = "参数错误") => new AgentError(10001, 400, msg),
  runActive: e(60001, 409, "这个画布上已有运行中的 Agent"),
  modelNA: e(60002, 400, "Agent 模型不可用"),
  state: e(60003, 409, "当前状态不允许这个操作"),
  approvalGone: e(60004, 409, "审批已处理或已过期"),
  unavailable: e(60005, 503, "Agent 暂不可用，请稍后重试"),
  overBudget: e(60008, 402, "超出本轮积分预算"),
  sessionMissing: e(60011, 404, "会话不存在"),
  runMissing: e(60012, 404, "运行不存在"),
  approvalMissing: e(60013, 404, "审批不存在"),
};

/** Go 模型代理在「本轮额度用完」时放在响应头里的业务错误码 */
export const OVER_BUDGET_CODE = 60008;

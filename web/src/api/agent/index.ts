import service from "@/utils/requests/service";
import type {
  AgentApprovalDto,
  AgentAttachmentReq,
  AgentEventDto,
  AgentFeedbackDto,
  AgentHistoryDto,
  AgentModelDto,
  AgentRunDto,
  AgentSessionDto,
  AgentUndoResult,
  CreateAgentSessionReq,
  DecideAgentApprovalReq,
  SetAgentFeedbackReq,
  StartAgentRunReq,
} from "./type";

/**
 * 获取可用的 Agent 模型（后台已发布的 agent 类型模型）
 * @returns 模型列表
 */
export const getAgentModels = async (): Promise<AgentModelDto[]> =>
  (await service.get<AgentModelDto[] | null>("/agent/models", undefined)) ?? [];

/**
 * 列出画布上的会话，最近更新的在前
 * @param canvasId 画布 ID
 * @returns 会话列表
 */
export const listAgentSessions = async (canvasId: string): Promise<AgentSessionDto[]> =>
  (await service.get<AgentSessionDto[] | null>(`/canvas/${canvasId}/agent/sessions`, undefined)) ??
  [];

/**
 * 新建会话
 * @param canvasId 画布 ID
 * @param data 标题、模式、模型
 * @returns 新会话
 */
export const createAgentSession = (canvasId: string, data: CreateAgentSessionReq) =>
  service.post<AgentSessionDto>(`/canvas/${canvasId}/agent/sessions`, data);

/**
 * 重命名会话
 * @param sessionId 会话 ID
 * @param title 新标题
 */
export const renameAgentSession = (sessionId: string, title: string) =>
  service.patch<void>(`/agent/sessions/${sessionId}`, { title });

/**
 * 删除会话
 * @param sessionId 会话 ID
 */
export const deleteAgentSession = (sessionId: string) =>
  service.delete<void>(`/agent/sessions/${sessionId}`);

/**
 * 拉取会话事件（断线回放）：只返回 seq 大于 after 的持久化事件，按序号升序，一页最多 200 条
 * @param sessionId 会话 ID
 * @param after 已收到的最大序号
 * @returns 事件列表
 */
export const getAgentEvents = async (sessionId: string, after: number): Promise<AgentEventDto[]> =>
  (await service.get<AgentEventDto[] | null>(`/agent/sessions/${sessionId}/events`, { after })) ??
  [];

/**
 * 读聊天记录的一页：从最新往回翻。一页以「一轮」的开头为起点，条数由服务端定（默认约 100 条事件）
 * @param sessionId 会话 ID
 * @param before 已有事件里最小的序号，0 取最新一页
 * @returns 事件（升序）和更早是否还有
 */
export const getAgentHistory = async (
  sessionId: string,
  before: number,
): Promise<AgentHistoryDto> => {
  const page = await service.get<AgentHistoryDto | null>(`/agent/sessions/${sessionId}/history`, {
    before,
  });
  return { events: page?.events ?? [], has_more: page?.has_more ?? false };
};

/**
 * 发起一轮运行（HTTP 202）；画布上已有运行中的 Agent 时返回 60001
 * @param sessionId 会话 ID
 * @param data 消息、模式、选中节点
 * @returns 新建的运行
 */
export const startAgentRun = (sessionId: string, data: StartAgentRunReq) =>
  service.post<AgentRunDto>(`/agent/sessions/${sessionId}/runs`, data);

/**
 * 运行中插话：补充要求，Agent 在下一步看到
 * @param runId 运行 ID
 * @param message 补充的要求
 */
export const interjectAgentRun = (
  runId: string,
  message: string,
  attachments?: AgentAttachmentReq[],
) =>
  service.post<void>(`/agent/runs/${runId}/interject`, {
    message,
    ...(attachments?.length && { attachments }),
  });

/**
 * 停止运行
 * @param runId 运行 ID
 * @returns 停止后的运行
 */
export const cancelAgentRun = (runId: string) =>
  service.post<AgentRunDto>(`/agent/runs/${runId}/cancel`, undefined);

/**
 * 继续一个暂停的运行（中断、步数用尽，以及早先设过额度又用完的旧运行）
 * @param runId 运行 ID
 * @param addBudget 追加的额度（积分），只有设过额度又用完的旧运行用得上
 * @returns 继续后的运行
 */
export const resumeAgentRun = (runId: string, addBudget = 0) =>
  service.post<AgentRunDto>(`/agent/runs/${runId}/resume`, { add_budget: addBudget });

/**
 * 撤销本轮：把 Agent 这一轮对画布的改动倒回去，用户之后改过的字段会被跳过
 * @param runId 运行 ID
 * @returns 恢复的项数和跳过的项
 */
export const undoAgentRun = (runId: string) =>
  service.post<AgentUndoResult>(`/agent/runs/${runId}/undo`, undefined);

/**
 * 对审批做决定：批准或拒绝生成、删除，或回答提问；批准后运行自动继续
 * @param approvalId 审批 ID
 * @param data 决定
 * @returns 处理后的审批
 */
export const decideAgentApproval = (approvalId: string, data: DecideAgentApprovalReq) =>
  service.post<AgentApprovalDto>(`/agent/approvals/${approvalId}/decision`, data);

/**
 * 对一轮已结束的运行打分：再次提交覆盖上一次的评价。失败由调用方就地提示，不弹全局错误
 * @param runId 运行 ID
 * @param data 评价、原因标签、补充说明
 * @returns 服务端记下的反馈
 */
export const setAgentFeedback = async (
  runId: string,
  data: SetAgentFeedbackReq,
): Promise<AgentFeedbackDto> => {
  const saved = await service.put<AgentFeedbackDto>(`/agent/runs/${runId}/feedback`, data, {
    silent: true,
  });
  return { ...saved, reasons: saved.reasons ?? [], comment: saved.comment ?? "" };
};

/**
 * 取消一轮运行的反馈。失败由调用方就地提示，不弹全局错误
 * @param runId 运行 ID
 */
export const deleteAgentFeedback = (runId: string) =>
  service.delete<void>(`/agent/runs/${runId}/feedback`, { silent: true });

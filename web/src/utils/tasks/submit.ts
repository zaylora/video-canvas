import type { CreateTaskItem } from "@/api/generation-task/type";
import type { CanvasNodeData, NodeKind } from "@/types";
import { readOutputs } from "@/utils/canvas/outputs";

/** 提交失败后要在提交处就地告诉用户的话 */
export type SubmitErrorInfo = {
  kind: "credits" | "limit" | "unavailable" | "invalid" | "asset" | "network" | "unknown";
  message: string;
};

type ErrorLike = { code?: unknown; status?: unknown; message?: unknown };

/** 后端错误码：积分不足 / 排队任务已达上限（原“并发已满”，现在超出并发的任务会排队，只有排队也满了才拒绝）/ 模型不可用 / 参数不合法 / 素材不存在 */
export const SUBMIT_ERROR_CODE = {
  insufficientCredits: 40001,
  tooManyTasks: 40002,
  modelUnavailable: 40003,
  invalidParam: 40006,
  assetNotFound: 40007,
} as const;

/** 按后端业务错误码（其次 HTTP 状态）翻译成给用户看的话，不依赖 ApiError 类 */
export function describeSubmitError(error: unknown): SubmitErrorInfo {
  const e: ErrorLike = typeof error === "object" && error !== null ? error : {};
  const msg = typeof e.message === "string" ? e.message : "";
  const status = typeof e.status === "number" ? e.status : 0;

  switch (e.code) {
    case SUBMIT_ERROR_CODE.insufficientCredits:
      return { kind: "credits", message: "积分不足，无法生成" };
    case SUBMIT_ERROR_CODE.tooManyTasks:
      return { kind: "limit", message: "排队任务已达上限，请等前面的任务完成后再试" };
    case SUBMIT_ERROR_CODE.modelUnavailable:
      return { kind: "unavailable", message: "该模型暂不可用，换一个模型试试" };
    case SUBMIT_ERROR_CODE.invalidParam:
      return { kind: "invalid", message: msg ? `参数不合法：${msg}` : "参数不合法，请检查后重试" };
    case SUBMIT_ERROR_CODE.assetNotFound:
      return { kind: "asset", message: "所选素材不存在或已失效，请重新选择" };
  }
  if (status === 402) return { kind: "credits", message: "积分不足，无法生成" };
  if (status === 429)
    return { kind: "limit", message: "排队任务已达上限，请等前面的任务完成后再试" };
  if (e.code === "NETWORK_ERROR" || e.code === "TIMEOUT") {
    return { kind: "network", message: "网络异常，提交没有成功，请重试" };
  }
  return { kind: "unknown", message: msg || "提交失败，请稍后重试" };
}

/** 只有「请求没到服务端 / 服务端 5xx」才值得原样重试；4xx 都是确定的答复 */
export function isRetryableSubmitError(error: unknown): boolean {
  const e: ErrorLike = typeof error === "object" && error !== null ? error : {};
  if (e.code === "NETWORK_ERROR" || e.code === "TIMEOUT") return true;
  return typeof e.status === "number" && e.status >= 500;
}

/**
 * 提交并在可重试的失败上重试。
 * send 每次收到同一个 idempotencyKey：同一次点击的重试复用同一个 key，后端据此去重。
 */
export async function submitWithRetry<T>(
  send: (idempotencyKey: string) => Promise<T>,
  idempotencyKey: string,
  options: {
    retries?: number;
    delayMs?: number;
    sleep?: (ms: number) => Promise<void>;
    isRetryable?: (error: unknown) => boolean;
  } = {},
): Promise<T> {
  const {
    retries = 2,
    delayMs = 600,
    sleep = (ms) => new Promise<void>((resolve) => setTimeout(resolve, ms)),
    isRetryable = isRetryableSubmitError,
  } = options;
  for (let attempt = 0; ; attempt++) {
    try {
      return await send(idempotencyKey);
    } catch (error) {
      if (attempt >= retries || !isRetryable(error)) throw error;
      await sleep(delayMs * 2 ** attempt);
    }
  }
}

/**
 * 任务提交成功（202）后写进节点的补丁：记下 taskId、进入 running，并清掉上一次的结果。
 * 文本节点清正文；媒体节点清素材相关字段。此后节点完全由任务 store 与回填驱动。
 * @param kind 节点种类
 * @param taskIds 后端返回的任务 id（字符串）；一次提交了多个任务（生成数量 N）时传全部成功创建的：
 *   taskId 取第一个，≥2 个时 pendingTaskIds 记全部，只有 1 个时和单任务完全一致
 * @returns 节点数据补丁
 */
export function buildSubmittedPatch(
  kind: NodeKind,
  taskIds: string | string[],
  data?: CanvasNodeData,
): Partial<CanvasNodeData> {
  const ids = Array.isArray(taskIds) ? taskIds : [taskIds];
  const taskId = ids[0];
  if (kind === "script") return { taskId, status: "running", text: null, error: null };
  // 旧画布的结果只存在 src 上，清掉之前先落进历史，重新生成后还能切回去
  const outputs = data ? readOutputs(data) : [];
  return {
    ...(outputs.length ? { outputs } : {}),
    taskId,
    // 清掉上一批的残留；多个任务时记全部
    pendingTaskIds: ids.length >= 2 ? ids : undefined,
    status: "running",
    src: null,
    assetId: undefined,
    mediaType: undefined,
    uploaded: false,
    fileName: undefined,
    error: null,
  };
}

/**
 * 把一次提交的逐项结果整理成「成功创建的任务 id」和「失败的原因」。
 * 一次提交的多个任务可以都绑同一个节点（node_id 相同），所以只能按下标逐项处理，不能按 node_id 查找。
 */
export function settleSubmission(items: CreateTaskItem[]): {
  taskIds: string[];
  failures: SubmitErrorInfo[];
} {
  const taskIds: string[] = [];
  const failures: SubmitErrorInfo[] = [];
  for (const item of items) {
    if (item.task) taskIds.push(String(item.task.id));
    else failures.push(describeSubmitError(item.error ?? null));
  }
  return { taskIds, failures };
}

import { ApiError } from "@/utils/requests/request";

/** 短剧业务错误码（后端 errcode 67xxx 号段，契约 §8） */
export const DRAMA_ERR = {
  /** 短剧不存在 */
  notFound: 67001,
  /** 原文格式不支持或内容为空（Msg 写具体原因） */
  sourceInvalid: 67002,
  /** 原文超过 30 万字 */
  sourceTooLong: 67003,
  /** 已有进行中的任务 */
  jobBusy: 67004,
  /** 这一集已拆解资产，剧本已锁定 */
  episodeLocked: 67005,
  /** 前置步骤未完成（Msg 写缺什么） */
  stepNotReady: 67006,
  /** 没有可用的文本模型 */
  noTextModel: 67007,
  /** 模型输出无法解析 */
  outputInvalid: 67008,
} as const;

/** 各错误码的兜底文案：后端 Msg 为空时用 */
const FALLBACK: Record<number, string> = {
  [DRAMA_ERR.notFound]: "短剧不存在",
  [DRAMA_ERR.sourceInvalid]: "原文格式不支持或内容为空",
  [DRAMA_ERR.sourceTooLong]: "原文超过 30 万字，请删减后再试",
  [DRAMA_ERR.jobBusy]: "已有进行中的任务，完成后再试",
  [DRAMA_ERR.episodeLocked]: "这一集已拆解资产，剧本已锁定",
  [DRAMA_ERR.stepNotReady]: "前置步骤未完成",
  [DRAMA_ERR.noTextModel]: "没有可用的文本模型，请联系管理员启用",
  [DRAMA_ERR.outputInvalid]: "模型输出无法解析，失败不扣积分，可以重试",
};

/** 错误是不是这个业务码 */
export const isDramaError = (error: unknown, code: number): boolean =>
  error instanceof ApiError && error.code === code;

/** 保存撞上 revision 冲突（HTTP 409 且不是 67004 / 67005 这两个业务 409） */
export const isRevisionConflict = (error: unknown): boolean =>
  error instanceof ApiError &&
  error.status === 409 &&
  error.code !== DRAMA_ERR.jobBusy &&
  error.code !== DRAMA_ERR.episodeLocked;

/**
 * 把错误转成给用户看的一句话：
 * 67002 / 67006 后端 Msg 写了具体原因，优先用 Msg；其余优先用本地文案，Msg 兜底。
 */
export function dramaErrorMessage(error: unknown, fallback = "操作失败，请稍后重试"): string {
  if (!(error instanceof ApiError)) return fallback;
  const code = typeof error.code === "number" ? error.code : 0;
  const local = FALLBACK[code];
  if (code === DRAMA_ERR.sourceInvalid || code === DRAMA_ERR.stepNotReady) {
    return error.message || local;
  }
  return local ?? (error.message || fallback);
}

/** 进度卡上失败任务的说明：67xxx 用本地文案（67006 / 67002 后端 Msg 优先），其余用后端 Msg */
export function jobFailureMessage(job: { errorCode: number; errorMessage: string }): string {
  const local = FALLBACK[job.errorCode];
  if (job.errorCode === DRAMA_ERR.stepNotReady || job.errorCode === DRAMA_ERR.sourceInvalid) {
    return job.errorMessage || local;
  }
  return local ?? (job.errorMessage || "任务失败");
}

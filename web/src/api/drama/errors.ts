import { ApiError } from "@/utils/requests/request";

/** 短剧业务错误码（后端 errcode 67xxx 号段，契约 §8） */
export const DRAMA_ERR = {
  /** 短剧不存在 */
  notFound: 67001,
  /** 原文格式不支持或内容为空（Msg 写具体原因） */
  sourceInvalid: 67002,
  /** 原文超过 500 万字 */
  sourceTooLong: 67003,
  /** 已有进行中的任务 */
  jobBusy: 67004,
  /** 这一集已拆解资产，剧本已锁定 */
  episodeLocked: 67005,
  /** 前置步骤未完成（Msg 写缺什么） */
  stepNotReady: 67006,
  /** 没有可用的 Agent 模型 */
  noTextModel: 67007,
  /** 模型输出无法解析 */
  outputInvalid: 67008,
  /** 片段不存在 */
  segmentNotFound: 67009,
  /** 只能采用本片段已成功的结果 */
  takeInvalid: 67011,
  /** 这一集还没有可合成的片段（没有任何采用的视频） */
  nothingToCompose: 67010,
  /** 排序必须包含该集全部片段 */
  segmentOrderInvalid: 67012,
  /** 这一集已有生成结果，重新生成分镜会清空片段 */
  storyboardHasTakes: 67013,
  /** 上传的文件草稿已过期（24 小时），需要重新上传 */
  sourceExpired: 67014,
  /** 自定义提示词或技能导致输出不合格 */
  customPromptInvalid: 67015,
  /** 我的提示词不存在 */
  promptNotFound: 67016,
  /** 提示词正文为空、超长或名称重复（Msg 写具体原因） */
  promptInvalid: 67017,
  /** 技能不可用（未启用或没标这个环节） */
  skillUnavailable: 67018,
  /** 当前阶段不允许这个操作（规划中增删集、暂停 / 继续状态不对等） */
  phaseNotAllowed: 67019,
  /** 单集对应原文过长（兜底） */
  unitTooLong: 67020,
  /** 模型上下文不足（低于 128k 或装不下一个单元） */
  contextTooSmall: 67022,
  /** 不支持转写为这种格式 */
  adaptFormat: 67023,
  /** 原稿是空的，不能转写 */
  adaptEmpty: 67024,
  /** 这个片段正在转写（409） */
  adaptBusy: 67025,
  /** 转写结果不合格 */
  adaptInvalid: 67026,
  /** 还没有这个格式的转写稿 */
  adaptMissing: 67027,
} as const;

/** 各错误码的兜底文案：后端 Msg 为空时用 */
const FALLBACK: Record<number, string> = {
  [DRAMA_ERR.notFound]: "短剧不存在",
  [DRAMA_ERR.sourceInvalid]: "原文格式不支持或内容为空",
  [DRAMA_ERR.sourceTooLong]: "原文超过 500 万字，请删减后再试",
  [DRAMA_ERR.jobBusy]: "已有进行中的任务，完成后再试",
  [DRAMA_ERR.episodeLocked]: "这一集已拆解资产，剧本已锁定",
  [DRAMA_ERR.stepNotReady]: "前置步骤未完成",
  [DRAMA_ERR.noTextModel]: "没有可用的 Agent 模型，请联系管理员启用",
  [DRAMA_ERR.outputInvalid]: "模型输出无法解析，失败不扣积分，可以重试",
  [DRAMA_ERR.segmentNotFound]: "片段不存在，可能已被删除",
  [DRAMA_ERR.nothingToCompose]: "这一集还没有可合成的片段",
  [DRAMA_ERR.takeInvalid]: "只能采用本片段已成功的结果",
  [DRAMA_ERR.segmentOrderInvalid]: "排序必须包含这一集的全部片段，请刷新后重试",
  [DRAMA_ERR.storyboardHasTakes]: "这一集已有生成结果，重新生成分镜会清空片段",
  [DRAMA_ERR.sourceExpired]: "文件已过期，请重新上传",
  [DRAMA_ERR.customPromptInvalid]: "自定义提示词导致输出不合格，可以改用内置提示词重试",
  [DRAMA_ERR.promptNotFound]: "这条提示词不存在，可能已被删除",
  [DRAMA_ERR.promptInvalid]: "提示词正文为空、超长或名称重复",
  [DRAMA_ERR.skillUnavailable]: "这个技能当前不可用，请换一个",
  [DRAMA_ERR.phaseNotAllowed]: "当前阶段不能进行这个操作",
  [DRAMA_ERR.unitTooLong]: "这一集对应的原文过长",
  [DRAMA_ERR.contextTooSmall]: "模型上下文不足（低于 128k），请换一个模型",
  [DRAMA_ERR.adaptFormat]: "不支持转写为这种格式",
  [DRAMA_ERR.adaptEmpty]: "原稿是空的，先写好原稿再转写",
  [DRAMA_ERR.adaptBusy]: "这个片段正在转写，请稍候",
  [DRAMA_ERR.adaptInvalid]: "转写结果不合格，请重试",
  [DRAMA_ERR.adaptMissing]: "还没有这个格式的转写稿，请先转写",
};

/** 错误是不是这个业务码 */
export const isDramaError = (error: unknown, code: number): boolean =>
  error instanceof ApiError && error.code === code;

/** 保存撞上 revision 冲突（HTTP 409 且不是 67004 / 67005 / 67013 / 67025 这几个业务 409） */
export const isRevisionConflict = (error: unknown): boolean =>
  error instanceof ApiError &&
  error.status === 409 &&
  error.code !== DRAMA_ERR.jobBusy &&
  error.code !== DRAMA_ERR.episodeLocked &&
  error.code !== DRAMA_ERR.storyboardHasTakes &&
  error.code !== DRAMA_ERR.adaptBusy;

/** 后端 Msg 写了具体原因、优先展示 Msg 的错误码 */
const MSG_FIRST = new Set<number>([
  DRAMA_ERR.sourceInvalid,
  DRAMA_ERR.stepNotReady,
  DRAMA_ERR.promptInvalid,
]);

/**
 * 把错误转成给用户看的一句话：
 * 67002 / 67006 / 67017 后端 Msg 写了具体原因，优先用 Msg；其余优先用本地文案，Msg 兜底。
 */
export function dramaErrorMessage(error: unknown, fallback = "操作失败，请稍后重试"): string {
  if (!(error instanceof ApiError)) return fallback;
  const code = typeof error.code === "number" ? error.code : 0;
  const local = FALLBACK[code];
  if (MSG_FIRST.has(code)) return error.message || local;
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

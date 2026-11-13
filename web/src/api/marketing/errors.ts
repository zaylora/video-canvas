import { ApiError } from "@/utils/requests/request";

/** 营销工作室业务错误码（后端 errcode 68xxx 号段，契约 §6） */
export const MARKETING_ERR = {
  /** 营销项目不存在 */
  notFound: 68001,
  /** 商品图数量不对或素材无效 */
  assetsInvalid: 68002,
  /** 没有可用的看图文本模型 */
  noTextModel: 68003,
  /** 视频模型不可用或不支持所需参考 */
  videoModelUnavailable: 68004,
  /** 参考视频超出模型限制 */
  refVideoTooLong: 68005,
  /** 脚本正在出片 / 重写，不能修改 */
  scriptBusy: 68006,
  /** 这个项目正在写脚本 */
  projectScripting: 68007,
  /** 提示词超出模型上限 */
  promptTooLong: 68008,
  /** 内容已在别处修改 */
  conflict: 68009,
  /** 有进行中的任务，不能删除 */
  deleteBusy: 68010,
  /** 发到画布失败 */
  canvasFailed: 68011,
  /** 环节不存在 */
  stepNotFound: 68012,
  /** 生效前需要该候选试跑通过 */
  trialRequired: 68013,
  /** 技能或版本不可用 */
  skillUnavailable: 68014,
  /** 拆解内容不合法 */
  reverseInvalid: 68015,
  /** 没有可用的拆解，不能重写跟拍 */
  noReverse: 68016,
  /** 转写提示词失败 */
  adaptFailed: 68018,
  /** 该环节已有试跑在进行 */
  trialRunning: 68017,
} as const;

/** 积分不足（复用全局错误码） */
export const INSUFFICIENT_CREDITS = 40001;

/** 错误是不是这个业务码 */
export const isMarketingError = (error: unknown, code: number): boolean =>
  error instanceof ApiError && error.code === code;

/** 内容已在别处修改（68009）：调用方据此载入最新版 */
export const isMarketingConflict = (error: unknown): boolean =>
  isMarketingError(error, MARKETING_ERR.conflict);

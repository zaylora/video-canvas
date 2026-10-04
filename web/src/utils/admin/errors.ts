/**
 * 管理端要“翻译”的几类后端错误。请求错误的全局 toast 由拦截器弹，
 * 这里只负责让页面认出错误类型，在就地位置多做一步（引导设 Key、提示 runner 不可用……）。
 */

type ErrorLike = { code?: unknown; status?: unknown; message?: unknown };

const asErrorLike = (error: unknown): ErrorLike =>
  typeof error === "object" && error !== null ? error : {};

/** 后端业务错误码（管理端用到的） */
export const ADMIN_ERROR_CODE = {
  /** 插件版本被渠道 / 非终态任务引用，不能删除 */
  PLUGIN_VERSION_IN_USE: 50005,
  /** 内置插件的版本不能删除 */
  PLUGIN_BUILTIN: 50006,
  /** 插件文件超限 */
  PLUGIN_TOO_LARGE: 50007,
  /** 渠道 key 已存在 */
  CHANNEL_KEY_EXISTS: 50012,
  /** 渠道配置不合法（原因在 msg） */
  CHANNEL_INVALID: 50013,
  /** 渠道 Key 未设置 */
  CHANNEL_SECRET_MISSING: 50015,
  /** 插件运行器不可用 */
  RUNNER_UNAVAILABLE: 50021,
  /** 存储：已有素材引用，定位字段不能修改（被锁字段写在 msg 里） */
  STORAGE_FIELD_LOCKED: 51005,
  /** 存储：配置已被其他人修改（version 过期） */
  STORAGE_VERSION_CONFLICT: 51009,
  /** 图片处理服务：配置已被其他人修改（version 过期） */
  PROCESSOR_VERSION_CONFLICT: 52004,
} as const;

/** 插件运行器不可用：503 或业务码 50021 */
export const isRunnerDown = (error: unknown) => {
  const e = asErrorLike(error);
  return e.status === 503 || e.code === ADMIN_ERROR_CODE.RUNNER_UNAVAILABLE;
};

/** 渠道 Key 未设置：409 且业务码 50015（只看 409 会把其他冲突也算进来） */
export const isSecretMissing = (error: unknown) => {
  const e = asErrorLike(error);
  return e.code === ADMIN_ERROR_CODE.CHANNEL_SECRET_MISSING;
};

/** 文件超限：413 或业务码 50007 */
export const isTooLarge = (error: unknown) => {
  const e = asErrorLike(error);
  return e.status === 413 || e.code === ADMIN_ERROR_CODE.PLUGIN_TOO_LARGE;
};

/** 渠道 key 冲突：业务码 50012 */
export const isChannelKeyExists = (error: unknown) =>
  asErrorLike(error).code === ADMIN_ERROR_CODE.CHANNEL_KEY_EXISTS;

/** 存储配置版本冲突：业务码 51009，说明别人改过了，需要重新拉取 */
export const isStorageVersionConflict = (error: unknown) =>
  asErrorLike(error).code === ADMIN_ERROR_CODE.STORAGE_VERSION_CONFLICT;

/** 存储定位字段被锁：业务码 51005 */
export const isStorageFieldLocked = (error: unknown) =>
  asErrorLike(error).code === ADMIN_ERROR_CODE.STORAGE_FIELD_LOCKED;

/** 图片处理服务业务码（52xxx），契约见 docs/design/画布素材加载设计/图片处理服务接口契约.md 第 2.3 节 */
export const PROCESSOR_ERROR_CODE = {
  /** 处理服务不存在 */
  NOT_FOUND: 52001,
  /** 名称已存在 */
  NAME_EXISTS: 52002,
  /** 配置不合法（原因在 msg） */
  INVALID_CONFIG: 52003,
  /** 厂商与存储不匹配 */
  STORAGE_MISMATCH: 52005,
  /** 该存储已被另一个已发布的处理服务绑定（发布会替换它） */
  STORAGE_OCCUPIED: 52006,
  /** 当前草稿还没有通过校验与试跑 */
  NOT_CHECKED: 52007,
  /** 已发布的处理服务不能删除 */
  PUBLISHED_NOT_DELETABLE: 52008,
  /** 没有可回滚的版本 */
  NO_PREVIOUS_VERSION: 52009,
  /** 不是已发布状态，无法停用 / 回滚 */
  NOT_PUBLISHED: 52010,
} as const;

/** 图片处理服务版本冲突：业务码 52004，说明别人改过了，需要重新拉取 */
export const isProcessorVersionConflict = (error: unknown) =>
  asErrorLike(error).code === ADMIN_ERROR_CODE.PROCESSOR_VERSION_CONFLICT;

/** 取错误里给人看的说明；没有就用兜底文案 */
export const errorMessage = (error: unknown, fallback = "请求失败") => {
  const message = asErrorLike(error).message;
  return typeof message === "string" && message ? message : fallback;
};

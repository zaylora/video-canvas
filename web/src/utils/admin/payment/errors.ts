/**
 * 后台支付管理要“翻译”的几类后端错误。请求错误的全局 toast 由拦截器弹，
 * 这里只负责让页面认出错误类型，在就地位置多做一步（刷新版本、面板内就地提示）。
 */

/** 后台支付管理用到的业务错误码（65xxx 号段，见 backend/internal/pkg/errcode/errcode.go） */
export const PAYMENT_ERROR_CODE = {
  /** 渠道配置校验失败（原因在 msg，已脱敏） */
  CHANNEL_CONFIG: 65009,
  /** 渠道有未完成订单，暂不能修改商户身份或密钥 */
  CHANNEL_LOCKED: 65010,
  /** 配置已被他人修改（version 过期） */
  VERSION_CONFLICT: 65011,
  /** 商品已有订单，只能下架不能删除 */
  PRODUCT_HAS_ORDERS: 65012,
  /** 请先配置站点公网地址 */
  BASE_URL_MISSING: 65013,
} as const;

/** 业务错误码；不是对象或没有 code 时为 undefined */
const codeOf = (error: unknown): unknown =>
  typeof error === "object" && error !== null ? (error as { code?: unknown }).code : undefined;

/**
 * 版本冲突：业务码 65011，说明别人改过了，需要重新拉取最新版本
 * @param error 请求抛出的错误
 */
export const isPaymentVersionConflict = (error: unknown): boolean =>
  codeOf(error) === PAYMENT_ERROR_CODE.VERSION_CONFLICT;

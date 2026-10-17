/**
 * 后台支付管理接口的类型，契约见 docs/design/支付功能/支付功能.md 第 8.2 节，
 * 字段以 backend/internal/model/payment_admin.go 和 payment.go 为准。
 * 与 admin-ai 一样直接沿用后端 snake_case 字段与数字 id，不做映射：
 * 字段与后端结构一一对应，映射层只会增加漂移风险。
 */

/** 支付方式 */
export type PaymentMethod = "wechat" | "alipay";

/** 收银形态：qrcode 扫码、redirect 跳转到付款页、mixed 两种都可能 */
export type PaymentCheckout = "qrcode" | "redirect" | "mixed";

/** 全局设置 */
export interface PaymentSettings {
  /** 支付总开关 */
  enabled: boolean;
  /** 站点公网地址，拼回调和跳回地址，如 https://example.com；空串表示未配置 */
  public_base_url: string;
  /** 微信由哪个渠道承接（provider key）；空串表示不开放 */
  method_wechat: string;
  /** 支付宝由哪个渠道承接（provider key）；空串表示不开放 */
  method_alipay: string;
  /** 每个用户最多的未支付订单数，1–10 */
  max_pending_per_user: number;
}

/** 渠道配置的一个字段：定义 + 当前值 */
export interface PaymentField {
  /** 配置键 */
  key: string;
  /** 显示名 */
  label: string;
  /** 敏感字段：只写不读，value 恒为空串 */
  secret: boolean;
  /** 商户身份字段：渠道有未完成订单时禁止修改 */
  identity: boolean;
  /** 可以不填 */
  optional: boolean;
  /** 缺省值；没有时不返回 */
  default?: string;
  /** 输入提示；没有时不返回 */
  placeholder?: string;
  /** 当前值；敏感字段恒为空串 */
  value: string;
  /** 敏感字段是否已设置 */
  secret_set: boolean;
}

/** 后台渠道列表里的一项（保存 / 校验 / 启用 / 停用也返回它） */
export interface PaymentChannel {
  /** 渠道标识，如 wechat_native */
  provider: string;
  /** 显示名 */
  name: string;
  /** 能承接的支付方式；后端可能返回 null，API 层补成空数组 */
  methods: PaymentMethod[];
  /** 收银形态 */
  checkout: PaymentCheckout;
  /** 能主动查单 */
  can_query: boolean;
  /** 能在渠道侧关单 */
  can_close_upstream: boolean;
  /** 配置字段；后端可能返回 null，API 层补成空数组 */
  fields: PaymentField[];
  /** 是否保存过配置 */
  configured: boolean;
  /** 是否启用 */
  enabled: boolean;
  /** 当前配置是否已通过校验 */
  validated: boolean;
  /** 最近一次校验通过的时间（RFC3339）；null 表示没有 */
  validated_at: string | null;
  /** 乐观锁版本，保存时必须带上；未配置过为 0 */
  version: number;
  /** 未支付订单自动关闭的分钟数，5–120 */
  order_timeout_minutes: number;
  /** 未完成订单数 */
  open_orders: number;
  /** 需要在渠道商户后台登记的回调地址；未配置公网地址时为空串 */
  notify_url: string;
  /** 当前由它承接的支付方式；后端可能返回 null，API 层补成空数组 */
  serving: PaymentMethod[];
  /** 仅校验接口有意义：该渠道无法在线探测，只做了本地校验 */
  probe_skipped: boolean;
  /** 最近保存时间（RFC3339）；null 表示没有保存过 */
  updated_at: string | null;
}

/** 保存渠道配置的请求 */
export interface UpdatePaymentChannelRequest {
  /** 上次读到的版本，未配置过传 0 */
  version: number;
  /** 非敏感字段的完整取值，缺少的字段后端视为清空 */
  config: Record<string, string>;
  /** 只带用户真的填了的敏感字段；不带或空串表示保持原值 */
  secrets: Record<string, string>;
  /** 未支付自动关闭分钟数，5–120 */
  order_timeout_minutes: number;
}

/** 充值商品 */
export interface PaymentProduct {
  /** 商品 ID */
  id: number;
  /** 名称 */
  name: string;
  /** 售价，单位分 */
  price_fen: number;
  /** 基础积分 */
  base_credits: number;
  /** 赠送积分，0 表示没有 */
  bonus_credits: number;
  /** 角标，空串表示没有 */
  badge: string;
  /** 排序，越小越靠前 */
  sort: number;
  /** 是否上架 */
  on_sale: boolean;
  /** 已成功入账的笔数 */
  sold_count: number;
  /** 乐观锁版本 */
  version: number;
  /** 创建时间（RFC3339） */
  created_at: string;
  /** 更新时间（RFC3339） */
  updated_at: string;
}

/** 新建 / 修改商品的请求；修改时必须带 version，新建时后端忽略 */
export interface PaymentProductRequest {
  /** 名称，最长 64 字 */
  name: string;
  /** 售价，单位分，大于 0 */
  price_fen: number;
  /** 基础积分，大于 0 */
  base_credits: number;
  /** 赠送积分，不小于 0 */
  bonus_credits: number;
  /** 角标，最长 8 字，空串表示没有 */
  badge: string;
  /** 是否上架 */
  on_sale: boolean;
  /** 乐观锁版本；新建传 0 */
  version: number;
}

/** 订单状态；creating 是正在向渠道下单的瞬时状态 */
export type PaymentOrderStatus =
  | "creating"
  | "pending"
  | "credited"
  | "closed"
  | "create_failed"
  | "abnormal";

/** 订单关闭原因：到期、用户取消、管理员关单；空串表示没有关闭 */
export type PaymentCloseReason = "timeout" | "user" | "admin" | "";

/** 订单异常原因：金额不符、商户身份不符、交易号冲突；空串表示没有异常 */
export type PaymentAbnormalReason =
  | "amount_mismatch"
  | "identity_mismatch"
  | "trade_no_conflict"
  | "";

/** 后台看到的订单（列表的一行，也是详情的主体） */
export interface PaymentOrderAdmin {
  /** 订单号 */
  order_no: string;
  /** 下单用户 ID */
  user_id: number;
  /** 下单用户的用户名 */
  username: string;
  /** 商品名，取自下单时的快照 */
  product_name: string;
  /** 应付金额，单位分 */
  amount_fen: number;
  /** 到账积分 */
  credits: number;
  /** 支付方式 */
  method: PaymentMethod;
  /** 下单时承接的渠道标识 */
  provider: string;
  /** 渠道显示名 */
  provider_name: string;
  /** 订单状态 */
  status: PaymentOrderStatus;
  /** 渠道交易号；空串表示还没有 */
  provider_trade_no: string;
  /** 渠道报告的实付金额，单位分；0 表示还没有 */
  paid_amount_fen: number;
  /** 关单后才收到已付通知 */
  late_paid: boolean;
  /** 关单原因 */
  close_reason: PaymentCloseReason;
  /** 异常原因 */
  abnormal_reason: PaymentAbnormalReason;
  /** 下单失败原因（已脱敏）；空串表示没有 */
  fail_reason: string;
  /** 下单时的渠道配置版本 */
  channel_version: number;
  /** 到期时间（RFC3339） */
  expires_at: string;
  /** 渠道报告的付款时间（RFC3339）；null 表示未付款 */
  paid_at: string | null;
  /** 积分入账时间（RFC3339）；null 表示未入账 */
  credited_at: string | null;
  /** 关单时间（RFC3339）；null 表示未关闭 */
  closed_at: string | null;
  /** 创建时间（RFC3339） */
  created_at: string;
}

/** 渠道发来的一条通知记录 */
export interface PaymentNotification {
  /** 记录 ID */
  id: number;
  /** 渠道标识 */
  provider: string;
  /** 通知去重键 */
  event_key: string;
  /** 订单号 */
  order_no: string;
  /** 处理结果：credited / duplicate / unpaid / amount_mismatch / identity_mismatch / trade_no_conflict / not_found / state_rejected */
  result: string;
  /** 验签后的业务字段，已去掉签名和密钥 */
  payload: Record<string, unknown> | null;
  /** 收到时间（RFC3339） */
  received_at: string;
}

/** 订单详情：订单 + 渠道通知记录 */
export interface PaymentOrderDetail extends PaymentOrderAdmin {
  /** 渠道通知记录，按后端顺序；后端可能返回 null，API 层补成空数组 */
  notifications: PaymentNotification[];
}

/** 订单列表的查询参数；不传的条件不会带给后端 */
export interface ListPaymentOrdersParams {
  /** 关键字，匹配订单号和用户名 */
  q?: string;
  /** 订单状态 */
  status?: PaymentOrderStatus;
  /** 支付方式 */
  method?: PaymentMethod;
  /** 渠道标识 */
  provider?: string;
  /** 开始时间（RFC3339） */
  from?: string;
  /** 结束时间（RFC3339） */
  to?: string;
  /** 页码，从 1 开始 */
  page?: number;
  /** 每页条数，最大 100 */
  page_size?: number;
}

/** 订单分页结果 */
export interface PaymentOrderPage {
  /** 当前页订单 */
  list: PaymentOrderAdmin[];
  /** 命中总数 */
  total: number;
  /** 当前页码 */
  page: number;
  /** 每页条数 */
  page_size: number;
}

/** 管理员查单 / 关单后返回的订单简况（只用到状态，页面随后重新拉取详情） */
export interface PaymentOrderBrief {
  /** 订单号 */
  order_no: string;
  /** 操作后的订单状态 */
  status: PaymentOrderStatus;
}

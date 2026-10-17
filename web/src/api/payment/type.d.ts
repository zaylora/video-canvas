/** 支付方式：前台只有这两个，由后台指定各自的承接渠道 */
export type PayMethod = "wechat" | "alipay";

/** 订单状态 */
export type PayOrderStatus =
  /** 已落库，正在向渠道下单 */
  | "creating"
  /** 等待用户付款 */
  | "pending"
  /** 已付款且积分已入账（终态） */
  | "credited"
  /** 超时、用户取消或管理员关单 */
  | "closed"
  /** 渠道下单失败（终态） */
  | "create_failed"
  /** 金额不符、商户身份不符或交易号冲突，不自动入账，等人工处理（终态） */
  | "abnormal";

/** 收银形态 */
export type CheckoutMode =
  /** value 是二维码内容，前端画成二维码 */
  | "qrcode"
  /** value 是渠道已经画好的二维码图片地址，前端直接显示 */
  | "qrcode_image"
  /** value 是付款页地址，前端新开标签页 */
  | "redirect";

/** 后端返回的商品（snake_case） */
export interface BackendProductDto {
  /** 商品 ID */
  id: number;
  /** 名称 */
  name: string;
  /** 售价，单位分 */
  price_fen: number;
  /** 基础积分 */
  base_credits: number;
  /** 赠送积分 */
  bonus_credits: number;
  /** 角标，空串表示没有 */
  badge: string;
}

/** 后端返回的支付方式可用状态 */
export interface BackendMethodDto {
  /** wechat / alipay */
  method: PayMethod;
  /** 是否可用 */
  available: boolean;
  /** 不可用的原因，前台置灰并显示 */
  reason?: string;
}

/** 后端返回的积分商城数据 */
export interface BackendStoreDto {
  /** 支付总开关；false 时前端不打开商城 */
  enabled: boolean;
  /** 上架商品，已按后台顺序排好 */
  products: BackendProductDto[];
  /** 两种支付方式的可用状态 */
  methods: BackendMethodDto[];
}

/** 后端返回的订单 */
export interface BackendOrderDto {
  /** 订单号 */
  order_no: string;
  /** 状态 */
  status: PayOrderStatus;
  /** 应付金额，单位分 */
  amount_fen: number;
  /** 到账积分 */
  credits: number;
  /** 支付方式 */
  method: PayMethod;
  /** 收银信息，只有 pending 时有 */
  checkout?: { mode: CheckoutMode; value: string };
  /** 到期时间，RFC3339 */
  expires_at: string;
  /** 入账时间，未入账为 null */
  credited_at: string | null;
  /** 关单原因：timeout / user / admin */
  close_reason?: string;
  /** 关单后才收到付款（已照常入账） */
  late_paid: boolean;
  /** 异常原因 */
  abnormal_reason?: string;
  /** 创建时间，RFC3339 */
  created_at: string;
}

/** 商城里的一档商品 */
export interface ProductDto {
  /** 商品 ID */
  id: number;
  /** 名称 */
  name: string;
  /** 售价，单位分 */
  priceFen: number;
  /** 基础积分 */
  baseCredits: number;
  /** 赠送积分，0 表示没有 */
  bonusCredits: number;
  /** 角标，null 表示没有 */
  badge: string | null;
}

/** 支付方式的可用状态 */
export interface MethodDto {
  /** wechat / alipay */
  method: PayMethod;
  /** 是否可用 */
  available: boolean;
  /** 不可用的原因，可用时为 null */
  reason: string | null;
}

/** 积分商城数据 */
export interface StoreDto {
  /** 支付总开关 */
  enabled: boolean;
  /** 上架商品 */
  products: ProductDto[];
  /** 两种支付方式的可用状态 */
  methods: MethodDto[];
}

/** 订单 */
export interface OrderDto {
  /** 订单号 */
  orderNo: string;
  /** 状态 */
  status: PayOrderStatus;
  /** 应付金额，单位分 */
  amountFen: number;
  /** 到账积分 */
  credits: number;
  /** 支付方式 */
  method: PayMethod;
  /** 收银信息，只有 pending 时有，否则为 null */
  checkout: { mode: CheckoutMode; value: string } | null;
  /** 到期时间，毫秒时间戳 */
  expiresAt: number;
  /** 入账时间，毫秒时间戳；未入账为 null */
  creditedAt: number | null;
  /** 关单原因：timeout / user / admin；没有为 null */
  closeReason: string | null;
  /** 关单后才收到付款（已照常入账） */
  latePaid: boolean;
  /** 异常原因；没有为 null */
  abnormalReason: string | null;
}

/** 下单请求 */
export interface CreateOrderRequest {
  /** 商品 ID */
  productId: number;
  /** 支付方式 */
  method: PayMethod;
  /** 幂等键：同一次点击的重试必须复用；换商品、换方式或上一次失败后要换新的 */
  idempotencyKey: string;
}

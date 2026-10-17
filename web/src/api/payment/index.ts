import service from "@/utils/requests/service";
import type {
  BackendMethodDto,
  BackendOrderDto,
  BackendProductDto,
  BackendStoreDto,
  CreateOrderRequest,
  MethodDto,
  OrderDto,
  ProductDto,
  StoreDto,
} from "./type";

/**
 * 后端商品转前端结构：角标空串转成 null。
 * @param dto 后端商品
 * @returns 前端商品
 */
export const mapProduct = (dto: BackendProductDto): ProductDto => ({
  id: dto.id,
  name: dto.name,
  priceFen: dto.price_fen,
  baseCredits: dto.base_credits,
  bonusCredits: dto.bonus_credits,
  badge: dto.badge || null,
});

/**
 * 后端支付方式转前端结构：没有原因转成 null。
 * @param dto 后端支付方式
 * @returns 前端支付方式
 */
export const mapMethod = (dto: BackendMethodDto): MethodDto => ({
  method: dto.method,
  available: dto.available,
  reason: dto.reason || null,
});

/**
 * 后端商城数据转前端结构；列表可能是 null，统一补成空数组。
 * @param dto 后端商城数据
 * @returns 前端商城数据
 */
export const mapStore = (dto: BackendStoreDto): StoreDto => ({
  enabled: dto.enabled,
  products: (dto.products ?? []).map(mapProduct),
  methods: (dto.methods ?? []).map(mapMethod),
});

/**
 * 后端订单转前端结构：时间转毫秒时间戳，空值统一成 null。
 * @param dto 后端订单
 * @returns 前端订单
 */
export const mapOrder = (dto: BackendOrderDto): OrderDto => ({
  orderNo: dto.order_no,
  status: dto.status,
  amountFen: dto.amount_fen,
  credits: dto.credits,
  method: dto.method,
  checkout: dto.checkout ?? null,
  expiresAt: Date.parse(dto.expires_at),
  creditedAt: dto.credited_at ? Date.parse(dto.credited_at) : null,
  closeReason: dto.close_reason || null,
  latePaid: dto.late_paid,
  abnormalReason: dto.abnormal_reason || null,
});

/**
 * 积分商城数据：支付总开关、上架商品、两种支付方式的可用状态
 * @returns 商城数据
 */
export const getPaymentStore = async (): Promise<StoreDto> =>
  mapStore(await service.get<BackendStoreDto>("/payment/store", undefined));

/**
 * 下单。同一个幂等键重复提交只会产生一笔订单。
 * @param req 商品、支付方式和幂等键
 * @returns 订单（含收银信息）
 */
export const createPaymentOrder = async (req: CreateOrderRequest): Promise<OrderDto> =>
  mapOrder(
    await service.post<BackendOrderDto>("/payment/orders", {
      product_id: req.productId,
      method: req.method,
      idempotency_key: req.idempotencyKey,
    }),
  );

/**
 * 最近一笔进行中的订单，刷新页面或重新打开商城后用它恢复收银面板
 * @returns 订单；没有进行中的订单返回 null
 */
export const getPendingPaymentOrder = async (): Promise<OrderDto | null> => {
  const dto = await service.get<BackendOrderDto | null>("/payment/orders/pending", undefined);
  return dto ? mapOrder(dto) : null;
};

/**
 * 订单状态，只读库，前端每 3 秒轮询
 * @param orderNo 订单号
 * @returns 订单
 */
export const getPaymentOrder = async (orderNo: string): Promise<OrderDto> =>
  mapOrder(
    await service.get<BackendOrderDto>(`/payment/orders/${encodeURIComponent(orderNo)}`, undefined),
  );

/**
 * 主动向渠道查单（用户点「我已完成支付」或从付款页切回），已付款就入账。同一订单 5 秒内只能查一次。
 * @param orderNo 订单号
 * @returns 最新订单
 */
export const checkPaymentOrder = async (orderNo: string): Promise<OrderDto> =>
  mapOrder(
    await service.post<BackendOrderDto>(
      `/payment/orders/${encodeURIComponent(orderNo)}/check`,
      undefined,
    ),
  );

/**
 * 取消订单：后端先查单再关单，查到已付款会直接入账而不是关单
 * @param orderNo 订单号
 * @returns 最新订单
 */
export const cancelPaymentOrder = async (orderNo: string): Promise<OrderDto> =>
  mapOrder(
    await service.post<BackendOrderDto>(
      `/payment/orders/${encodeURIComponent(orderNo)}/close`,
      undefined,
    ),
  );

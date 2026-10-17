import service from "@/utils/requests/service";
import { adminPaymentEndpoints as ep } from "./endpoints";
import type {
  ListPaymentOrdersParams,
  PaymentChannel,
  PaymentOrderBrief,
  PaymentOrderDetail,
  PaymentOrderPage,
  PaymentProduct,
  PaymentProductRequest,
  PaymentSettings,
  UpdatePaymentChannelRequest,
} from "./type";

/** 去掉空值，免得把 `q=` 之类的空参数发给后端 */
const clean = (params: Record<string, unknown>) =>
  Object.fromEntries(Object.entries(params).filter(([, v]) => v !== undefined && v !== ""));

/** 后端可能把空列表序列化成 null，渠道里的三个列表统一补成空数组 */
const normalizeChannel = (channel: PaymentChannel): PaymentChannel => ({
  ...channel,
  methods: channel.methods ?? [],
  fields: channel.fields ?? [],
  serving: channel.serving ?? [],
});

/**
 * 读取全局设置
 * @returns 支付总开关、公网地址、两种支付方式的承接渠道和每人未支付订单上限
 */
export const getPaymentSettings = () => service.get<PaymentSettings>(ep.settings(), undefined);

/**
 * 保存全局设置（super_admin）：映射的渠道必须已启用且支持该方式（否则 65009），启用支付前需要公网地址（65013）
 * @param body 整份设置
 * @returns 保存后的设置
 */
export const updatePaymentSettings = (body: PaymentSettings) =>
  service.put<PaymentSettings>(ep.settings(), body);

/**
 * 全部内置渠道及其配置状态（包括还没配置过的）；敏感字段只返回是否已设置
 * @returns 渠道列表；后端可能返回 null，统一补成空数组
 */
export const listPaymentChannels = async (): Promise<PaymentChannel[]> =>
  ((await service.get<PaymentChannel[] | null>(ep.channels(), undefined)) ?? []).map(
    normalizeChannel,
  );

/**
 * 保存渠道配置（super_admin）。已启用的渠道保存后会自动停用、校验作废（只改超时时间除外）；
 * 版本冲突返回 409 / 65011，有未完成订单时改商户身份或密钥返回 409 / 65010
 * @param provider 渠道标识
 * @param body 版本、非敏感字段完整取值、本次要改的密钥和超时分钟数
 * @returns 保存后的渠道视图
 */
export const updatePaymentChannel = async (
  provider: string,
  body: UpdatePaymentChannelRequest,
): Promise<PaymentChannel> =>
  normalizeChannel(await service.put<PaymentChannel>(ep.updateChannel(provider), body));

/**
 * 校验渠道配置（super_admin）：本地格式校验 + 用不存在的订单号在线探测凭证；失败返回 400 / 65009，通过后写入校验时间
 * @param provider 渠道标识
 * @returns 渠道视图；probe_skipped 为 true 表示该渠道无法在线探测
 */
export const validatePaymentChannel = async (provider: string): Promise<PaymentChannel> =>
  normalizeChannel(await service.post<PaymentChannel>(ep.validateChannel(provider), undefined));

/**
 * 启用渠道（super_admin）：要求已校验且已配置公网地址（65013），微信渠道还要求 https
 * @param provider 渠道标识
 * @returns 启用后的渠道视图
 */
export const enablePaymentChannel = async (provider: string): Promise<PaymentChannel> =>
  normalizeChannel(await service.post<PaymentChannel>(ep.enableChannel(provider), undefined));

/**
 * 停用渠道（super_admin）：它承接的支付方式在前台立即不可用，已有订单的回调和补单照常运行
 * @param provider 渠道标识
 * @returns 停用后的渠道视图
 */
export const disablePaymentChannel = async (provider: string): Promise<PaymentChannel> =>
  normalizeChannel(await service.post<PaymentChannel>(ep.disableChannel(provider), undefined));

/**
 * 全部充值商品（含已下架），按 sort 升序
 * @returns 商品列表；后端可能返回 null，统一补成空数组
 */
export const listPaymentProducts = async (): Promise<PaymentProduct[]> =>
  (await service.get<PaymentProduct[] | null>(ep.products(), undefined)) ?? [];

/**
 * 新建充值商品
 * @param body 商品字段，version 传 0
 * @returns 新建的商品
 */
export const createPaymentProduct = (body: PaymentProductRequest) =>
  service.post<PaymentProduct>(ep.products(), body);

/**
 * 修改充值商品，必须带 version；版本冲突返回 409 / 65011
 * @param id 商品 ID
 * @param body 商品全部字段与 version
 * @returns 修改后的商品
 */
export const updatePaymentProduct = (id: number, body: PaymentProductRequest) =>
  service.put<PaymentProduct>(ep.product(id), body);

/**
 * 删除充值商品；已有订单的商品只能下架，返回 409 / 65012
 * @param id 商品 ID
 */
export const deletePaymentProduct = (id: number) => service.delete<unknown>(ep.product(id));

/**
 * 按新顺序重排商品
 * @param ids 全部商品 ID 按新顺序排成的数组，必须是全部商品的一个排列
 */
export const sortPaymentProducts = (ids: number[]) =>
  service.put<unknown>(ep.sortProducts(), { ids });

/**
 * 订单分页列表（后端分页）
 * @param params 筛选条件与分页；空值不会发给后端
 * @returns 当前页订单与命中总数；list 缺失时补成空数组
 */
export const listPaymentOrders = async (
  params: ListPaymentOrdersParams = {},
): Promise<PaymentOrderPage> => {
  const page = await service.get<PaymentOrderPage | null>(ep.orders(), clean({ ...params }));
  return {
    list: page?.list ?? [],
    total: page?.total ?? 0,
    page: page?.page ?? params.page ?? 1,
    page_size: page?.page_size ?? params.page_size ?? 20,
  };
};

/**
 * 订单详情与渠道通知记录；订单不存在返回 404 / 65006
 * @param orderNo 订单号
 * @returns 订单详情；notifications 缺失时补成空数组
 */
export const getPaymentOrder = async (orderNo: string): Promise<PaymentOrderDetail> => {
  const detail = await service.get<PaymentOrderDetail>(ep.order(orderNo), undefined);
  return { ...detail, notifications: detail.notifications ?? [] };
};

/**
 * 管理员查单：向渠道确认订单状态，已付款就入账；渠道不支持查单返回 409 / 65008
 * @param orderNo 订单号
 * @returns 操作后的订单简况
 */
export const queryPaymentOrder = (orderNo: string) =>
  service.post<PaymentOrderBrief>(ep.queryOrder(orderNo), undefined);

/**
 * 管理员关单：后端先向渠道查单，已付款则直接入账，否则关闭；订单不是待支付返回 409 / 65007
 * @param orderNo 订单号
 * @returns 操作后的订单简况
 */
export const closePaymentOrder = (orderNo: string) =>
  service.post<PaymentOrderBrief>(ep.closeOrder(orderNo), undefined);

/**
 * 后台支付管理接口路径集中在这一个文件（前缀 /api/v1 由 axios baseURL 提供），
 * 契约见 docs/design/支付功能/支付功能.md 第 8.2 节；后端路由如有出入，只需要改这里。
 * 标 ★ 的写操作只有 super_admin 能调，admin 会收到 403。
 *
 *   GET    /admin/payment/settings                        全局设置
 *   PUT    /admin/payment/settings                        ★ 保存全局设置
 *   GET    /admin/payment/channels                        全部内置渠道（含还没配置过的）
 *   PUT    /admin/payment/channels/:provider              ★ 保存渠道配置（带 version）
 *   POST   /admin/payment/channels/:provider/validate     ★ 校验渠道配置
 *   POST   /admin/payment/channels/:provider/enable       ★ 启用渠道
 *   POST   /admin/payment/channels/:provider/disable      ★ 停用渠道
 *   GET    /admin/payment/products                        充值商品（含已下架）
 *   POST   /admin/payment/products                        新建商品
 *   PUT    /admin/payment/products/sort                   商品排序，body = {ids}
 *   PUT    /admin/payment/products/:id                    编辑商品（带 version）
 *   DELETE /admin/payment/products/:id                    删除商品
 *   GET    /admin/payment/orders                          订单分页列表
 *   GET    /admin/payment/orders/:no                      订单详情 + 渠道通知记录
 *   POST   /admin/payment/orders/:no/query                管理员查单
 *   POST   /admin/payment/orders/:no/close                管理员关单
 */
const P = "/admin/payment";
const seg = (value: string | number) => encodeURIComponent(String(value));
const channel = (provider: string) => `${P}/channels/${seg(provider)}`;
const product = (id: number) => `${P}/products/${seg(id)}`;
const order = (no: string) => `${P}/orders/${seg(no)}`;

export const adminPaymentEndpoints = {
  settings: () => `${P}/settings`,
  channels: () => `${P}/channels`,
  updateChannel: channel,
  validateChannel: (provider: string) => `${channel(provider)}/validate`,
  enableChannel: (provider: string) => `${channel(provider)}/enable`,
  disableChannel: (provider: string) => `${channel(provider)}/disable`,
  products: () => `${P}/products`,
  sortProducts: () => `${P}/products/sort`,
  product,
  orders: () => `${P}/orders`,
  order,
  queryOrder: (no: string) => `${order(no)}/query`,
  closeOrder: (no: string) => `${order(no)}/close`,
};

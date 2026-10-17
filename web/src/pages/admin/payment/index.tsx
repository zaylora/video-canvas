import { useSearchParams } from "react-router";

import { DataPage } from "@/components/admin-ui/data-panel";
import { PageHeader, PageHeaderHeading, PageHeaderTitle } from "@/components/admin-ui/page-header";
import { UnderlineTab, UnderlineTabs } from "@/components/admin-ui/underline-tabs";
import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";

import { ChannelsTab } from "./channels-tab";
import { OrdersTab } from "./orders-tab";
import { ProductsTab } from "./products-tab";
import { usePaymentChannels } from "./use-payment-channels";

/** 页签：键写进 URL 的 tab 参数，刷新不丢 */
const TABS = [
  { key: "channels", label: "支付渠道" },
  { key: "products", label: "充值商品" },
  { key: "orders", label: "支付订单" },
] as const;

type TabKey = (typeof TABS)[number]["key"];

/** 未知或缺失的 tab 参数回到第一个页签 */
const parseTab = (value: string | null): TabKey =>
  TABS.find((tab) => tab.key === value)?.key ?? "channels";

/**
 * 后台 · 支付管理（/admin/payment）：支付渠道、充值商品、支付订单三个页签。
 * 全局设置和渠道的写操作只有 super_admin 能做，admin 看得到但按钮禁用并写明原因；
 * 商品管理、查单、关单 admin 也可以。渠道列表由这一层持有，渠道页与订单页共用，切页签不重复请求。
 * 请求错误的全局提示由拦截器弹，页面不重复。
 */
export default function PaymentAdminPage() {
  const role = useAdminStore((state) => state.role);
  const canWriteChannels = canManageInfra(role);
  const [params, setParams] = useSearchParams();
  const tab = parseTab(params.get("tab"));
  const channels = usePaymentChannels();

  return (
    <DataPage>
      <PageHeader>
        <PageHeaderHeading>
          <PageHeaderTitle>支付管理</PageHeaderTitle>
        </PageHeaderHeading>
      </PageHeader>

      <UnderlineTabs aria-label="支付管理" className="mb-4 px-0">
        {TABS.map((item) => (
          <UnderlineTab
            key={item.key}
            selected={tab === item.key}
            onClick={() =>
              // 换页签时丢掉上一个页签的搜索、筛选和页码
              setParams(item.key === "channels" ? {} : { tab: item.key }, { replace: true })
            }
          >
            {item.label}
          </UnderlineTab>
        ))}
      </UnderlineTabs>

      <div className="flex min-h-0 flex-1 flex-col">
        {tab === "channels" && <ChannelsTab channels={channels} canWrite={canWriteChannels} />}
        {tab === "products" && <ProductsTab />}
        {tab === "orders" && <OrdersTab channels={channels} />}
      </div>
    </DataPage>
  );
}

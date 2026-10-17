import { useState } from "react";

import { listPaymentOrders } from "@/api/admin/payment";
import type {
  ListPaymentOrdersParams,
  PaymentOrderAdmin,
  PaymentOrderPage,
} from "@/api/admin/payment/type.d";
import { FilterSelect } from "@/components/admin-ui/filter-select";
import { ListSearch } from "@/components/admin-ui/list-search";
import { Notice } from "@/components/admin-ui/notice";
import { RowAction } from "@/components/admin-ui/row-action";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { DataPanel, DataPanelBody, DataPanelPagination } from "@/components/admin-ui/data-panel";
import { TableToolbar, TableToolbarCount } from "@/components/admin-ui/table-toolbar";
import { cn } from "@/lib/utils";
import { ORDER_METHOD_FILTERS, ORDER_STATUS_FILTERS } from "@/utils/admin/payment/order";

import { useAdminList } from "../use-admin-list";
import { OrderSheet } from "./order-sheet";
import { OrderTable } from "./order-table";
import type { PaymentChannels } from "./use-payment-channels";

/** 订单列表认的 URL 筛选参数 */
const FILTER_KEYS = ["status", "method", "provider"] as const;

/**
 * 「支付订单」页签：状态分段 + 支付方式 / 渠道筛选 + 关键字（订单号或用户名）+ 分页表格 + 详情抽屉。
 * 筛选、页码、关键字都放在 URL 里，刷新不丢；翻页和筛选时保留旧数据（半透明），不闪空。
 * 页面只提供查单和关单，没有退款，也没有强制入账。
 * @param channels 页面入口持有的渠道列表（渠道筛选下拉与详情里查单 / 关单能力的来源）
 */
export function OrdersTab({ channels }: { channels: PaymentChannels }) {
  const list = useAdminList<PaymentOrderAdmin, PaymentOrderPage>(FILTER_KEYS, (query) =>
    listPaymentOrders(query as ListPaymentOrdersParams),
  );
  /** 抽屉当前对应的订单号；关闭时只收起抽屉、保留订单号，退出动画里内容不会先消失 */
  const [orderNo, setOrderNo] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const status = list.filters.status ?? "";
  const providerOptions: ReadonlyArray<readonly [string, string]> = [
    ["", "全部渠道"],
    ...channels.channels.map((channel) => [channel.provider, channel.name] as const),
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4">
      <Notice tone="info">
        平台不提供退款。订单页只提供查单和关单，关单前一定先向渠道查单；所有入账都以渠道结果为准。
      </Notice>
      <DataPanel className={cn(list.refreshing && list.state === "ready" && "opacity-60")}>
        <TableToolbar>
          <ListSearch value={list.q} onCommit={list.setQ} placeholder="搜索订单号或用户名" />
          <Segmented aria-label="订单状态">
            {ORDER_STATUS_FILTERS.map(([value, label]) => (
              <SegmentedItem
                key={value || "all"}
                slideId="payment-order-status"
                active={status === value}
                onClick={() => list.setFilter("status", value)}
              >
                {label}
              </SegmentedItem>
            ))}
          </Segmented>
          <FilterSelect
            aria-label="支付方式"
            value={list.filters.method ?? ""}
            options={ORDER_METHOD_FILTERS}
            onValueChange={(value) => list.setFilter("method", value)}
          />
          <FilterSelect
            aria-label="渠道"
            value={list.filters.provider ?? ""}
            options={providerOptions}
            onValueChange={(value) => list.setFilter("provider", value)}
          />
          {list.filtering && <RowAction onClick={list.clear}>清除筛选</RowAction>}
          <TableToolbarCount shown={list.items.length} total={list.total} />
        </TableToolbar>
        <DataPanelBody>
          <OrderTable
            orders={list.items}
            status={list.state}
            filtering={list.filtering}
            onRetry={list.reload}
            onClearFilters={list.clear}
            onOpen={(order) => {
              setOrderNo(order.order_no);
              setOpen(true);
            }}
          />
        </DataPanelBody>
        {list.state === "ready" && (
          <DataPanelPagination
            page={list.page}
            pageSize={list.pageSize}
            total={list.total}
            onPageChange={list.setPage}
            onPageSizeChange={list.setPageSize}
          />
        )}
      </DataPanel>
      <OrderSheet
        orderNo={orderNo}
        open={open}
        channels={channels.channels}
        onClose={() => setOpen(false)}
        onChanged={list.reload}
      />
    </div>
  );
}

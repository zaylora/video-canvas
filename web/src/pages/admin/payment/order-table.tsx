import type { PaymentOrderAdmin } from "@/api/admin/payment/type.d";
import { EmptyState, EmptyStateActions, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import { TableSkeletonRows } from "@/components/admin-ui/table-skeleton-rows";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { orderStatusTag } from "@/utils/admin/payment/order";
import { METHOD_LABEL } from "@/utils/admin/payment/settings";
import { formatAbsolute } from "@/utils/admin/user-rules";
import { formatCredits, formatYuan } from "@/utils/payment/format";

import type { LoadStatus } from "../use-admin";

/**
 * 订单状态标签：有补充说明（异常原因、下单失败原因、迟到入账）时，悬停显示。
 * 标签本身可聚焦，键盘用户也能看到说明。
 * @param order 订单（只用到状态相关字段）
 */
export function OrderStatusTag({ order }: { order: PaymentOrderAdmin }) {
  const tag = orderStatusTag(order);
  if (!tag.hint) return <Tag tone={tag.tone}>{tag.label}</Tag>;
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            tabIndex={0}
            className="inline-flex rounded-md outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          />
        }
      >
        <Tag tone={tag.tone}>{tag.label}</Tag>
      </TooltipTrigger>
      <TooltipContent className="max-w-64 leading-relaxed">{tag.hint}</TooltipContent>
    </Tooltip>
  );
}

/**
 * 订单列表：订单号、用户、商品、金额、到账积分、方式 / 渠道、状态、创建时间。
 * 点击行（或聚焦后按 Enter）打开详情抽屉。
 * @param orders 当前页订单
 * @param status 加载状态：首次加载显示骨架行，失败显示重试
 * @param filtering 当前有筛选或搜索（空列表时区分“还没有订单”和“没有命中”）
 * @param onRetry 加载失败后的重试
 * @param onClearFilters 清除筛选
 * @param onOpen 打开某笔订单的详情
 */
export function OrderTable({
  orders,
  status,
  filtering,
  onRetry,
  onClearFilters,
  onOpen,
}: {
  orders: PaymentOrderAdmin[];
  status: LoadStatus;
  filtering: boolean;
  onRetry: () => void;
  onClearFilters: () => void;
  onOpen: (order: PaymentOrderAdmin) => void;
}) {
  if (status === "error") {
    return (
      <EmptyState className="border-0">
        <EmptyStateTitle className="text-destructive">订单加载失败</EmptyStateTitle>
        <EmptyStateActions>
          <Button variant="outline" size="sm" onClick={onRetry}>
            重试
          </Button>
        </EmptyStateActions>
      </EmptyState>
    );
  }
  if (status === "ready" && orders.length === 0) {
    return (
      <EmptyState className="border-0">
        <EmptyStateTitle>{filtering ? "没有符合条件的订单" : "还没有支付订单"}</EmptyStateTitle>
        {filtering && (
          <EmptyStateActions>
            <Button variant="outline" size="sm" onClick={onClearFilters}>
              清除筛选
            </Button>
          </EmptyStateActions>
        )}
      </EmptyState>
    );
  }
  return (
    <Table data-slot="payment-order-table">
      <TableHeader>
        <TableRow className="bg-muted/40 hover:bg-muted/40">
          <TableHead className="px-4">订单号</TableHead>
          <TableHead>用户</TableHead>
          <TableHead>商品</TableHead>
          <TableHead className="text-right">金额</TableHead>
          <TableHead className="text-right">到账积分</TableHead>
          <TableHead>方式 / 渠道</TableHead>
          <TableHead>状态</TableHead>
          <TableHead className="px-4">创建时间</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {status === "loading" && <TableSkeletonRows columns={8} rows={6} />}
        {status === "ready" &&
          orders.map((order) => (
            <TableRow
              key={order.order_no}
              data-order-no={order.order_no}
              tabIndex={0}
              aria-label={`查看订单 ${order.order_no}`}
              className={cn(
                "focus-visible:ring-ring/50 cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-inset",
              )}
              onClick={() => onOpen(order)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && event.target === event.currentTarget) onOpen(order);
              }}
            >
              <TableCell className="px-4 py-2.5 font-mono text-xs">{order.order_no}</TableCell>
              <TableCell className="py-2.5">{order.username || `#${order.user_id}`}</TableCell>
              <TableCell className="py-2.5">{order.product_name}</TableCell>
              <TableCell className="py-2.5 text-right tabular-nums">
                {formatYuan(order.amount_fen)}
              </TableCell>
              <TableCell className="py-2.5 text-right tabular-nums">
                {formatCredits(order.credits)}
              </TableCell>
              <TableCell className="py-2.5">
                <div>{METHOD_LABEL[order.method] ?? order.method}</div>
                <div className="text-muted-foreground text-xs">
                  {order.provider_name || order.provider}
                </div>
              </TableCell>
              <TableCell className="py-2.5">
                <OrderStatusTag order={order} />
              </TableCell>
              <TableCell className="text-muted-foreground px-4 py-2.5 text-xs tabular-nums">
                {formatAbsolute(order.created_at)}
              </TableCell>
            </TableRow>
          ))}
      </TableBody>
    </Table>
  );
}

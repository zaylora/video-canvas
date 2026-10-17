import { ArrowDown, ArrowUp, Pencil, Plus, Trash2 } from "lucide-react";

import type { PaymentProduct } from "@/api/admin/payment/type.d";
import { EmptyState, EmptyStateActions, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import { RowIconAction } from "@/components/admin-ui/row-icon-action";
import { TableSkeletonRows } from "@/components/admin-ui/table-skeleton-rows";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { moveProductId, productTotalCredits } from "@/utils/admin/payment/product";
import { formatCredits, formatYuan } from "@/utils/payment/format";

import type { LoadStatus } from "../use-admin";

/** 商品行上的操作回调 */
export type ProductRowActions = {
  /** 打开编辑对话框 */
  onEdit: (product: PaymentProduct) => void;
  /** 删除（弹确认框） */
  onDelete: (product: PaymentProduct) => void;
  /** 上架开关 */
  onToggleSale: (product: PaymentProduct) => void;
  /** 上移 / 下移一位，参数是新的全部 id 顺序 */
  onMove: (ids: number[]) => void;
  /** 新建（空列表时的入口） */
  onCreate: () => void;
};

/**
 * 充值商品表格：排序（上移 / 下移）、名称、售价、基础积分、赠送、到账合计、角标、上架开关、已售笔数、操作。
 * 已有订单的商品不能删除，按钮禁用并写明原因（只能下架）；排序、切换上架进行中时相关按钮禁用，避免并发改版本。
 * @param products 商品列表（按 sort 升序）
 * @param status 加载状态：加载中显示骨架行，失败显示重试，空显示空状态
 * @param busy 有请求进行中（排序或切换上架），禁用会改数据的按钮
 * @param togglingId 正在切换上架的商品 ID，对应开关禁用
 * @param onRetry 加载失败后的重试
 * @param actions 行操作回调
 */
export function ProductTable({
  products,
  status,
  busy,
  togglingId,
  onRetry,
  actions,
}: {
  products: PaymentProduct[];
  status: LoadStatus;
  busy: boolean;
  togglingId: number | null;
  onRetry: () => void;
  actions: ProductRowActions;
}) {
  if (status === "error") {
    return (
      <EmptyState className="border-0">
        <EmptyStateTitle className="text-destructive">商品加载失败</EmptyStateTitle>
        <EmptyStateActions>
          <Button variant="outline" size="sm" onClick={onRetry}>
            重试
          </Button>
        </EmptyStateActions>
      </EmptyState>
    );
  }
  if (status === "ready" && products.length === 0) {
    return (
      <EmptyState className="border-0">
        <EmptyStateTitle>还没有充值商品</EmptyStateTitle>
        <EmptyStateActions>
          <Button size="sm" onClick={actions.onCreate}>
            <Plus />
            新建商品
          </Button>
        </EmptyStateActions>
      </EmptyState>
    );
  }
  const ids = products.map((product) => product.id);
  return (
    <Table data-slot="payment-product-table">
      <TableHeader>
        <TableRow className="bg-muted/40 hover:bg-muted/40">
          <TableHead className="w-20 px-4">排序</TableHead>
          <TableHead>名称</TableHead>
          <TableHead className="text-right">售价</TableHead>
          <TableHead className="text-right">基础积分</TableHead>
          <TableHead className="text-right">赠送</TableHead>
          <TableHead className="text-right">到账合计</TableHead>
          <TableHead>角标</TableHead>
          <TableHead>上架</TableHead>
          <TableHead className="text-right">已售笔数</TableHead>
          <TableHead className="px-4 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {status === "loading" && <TableSkeletonRows columns={10} rows={4} />}
        {status === "ready" &&
          products.map((product) => {
            const up = moveProductId(ids, product.id, -1);
            const down = moveProductId(ids, product.id, 1);
            return (
              <TableRow key={product.id} data-product-id={product.id}>
                <TableCell className="px-4 py-2">
                  <div className="flex gap-0.5">
                    <RowIconAction
                      label="上移"
                      target={product.name}
                      deny={busy ? "正在保存，请稍候" : up ? null : "已经是第一个"}
                      onClick={() => up && actions.onMove(up)}
                    >
                      <ArrowUp />
                    </RowIconAction>
                    <RowIconAction
                      label="下移"
                      target={product.name}
                      deny={busy ? "正在保存，请稍候" : down ? null : "已经是最后一个"}
                      onClick={() => down && actions.onMove(down)}
                    >
                      <ArrowDown />
                    </RowIconAction>
                  </div>
                </TableCell>
                <TableCell className="py-2 font-medium">{product.name}</TableCell>
                <TableCell className="py-2 text-right tabular-nums">
                  {formatYuan(product.price_fen)}
                </TableCell>
                <TableCell className="py-2 text-right tabular-nums">
                  {formatCredits(product.base_credits)}
                </TableCell>
                <TableCell className="py-2 text-right tabular-nums">
                  {product.bonus_credits > 0 ? `+${formatCredits(product.bonus_credits)}` : "—"}
                </TableCell>
                <TableCell className="py-2 text-right font-medium tabular-nums">
                  {formatCredits(productTotalCredits(product))}
                </TableCell>
                <TableCell className="py-2">
                  {product.badge ? <Tag tone="orange">{product.badge}</Tag> : "—"}
                </TableCell>
                <TableCell className="py-2">
                  <Switch
                    checked={product.on_sale}
                    disabled={busy}
                    aria-label={`${product.on_sale ? "下架" : "上架"} ${product.name}`}
                    onCheckedChange={() => actions.onToggleSale(product)}
                  />
                  {togglingId === product.id && <span className="sr-only">保存中</span>}
                </TableCell>
                <TableCell className="py-2 text-right tabular-nums">{product.sold_count}</TableCell>
                <TableCell className="px-4 py-2">
                  <div className="flex justify-end gap-0.5">
                    <RowIconAction
                      label="编辑"
                      target={product.name}
                      onClick={() => actions.onEdit(product)}
                    >
                      <Pencil />
                    </RowIconAction>
                    <RowIconAction
                      label="删除"
                      target={product.name}
                      destructive
                      deny={product.sold_count > 0 ? "已有订单，只能下架不能删除" : null}
                      onClick={() => actions.onDelete(product)}
                    >
                      <Trash2 />
                    </RowIconAction>
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
      </TableBody>
    </Table>
  );
}

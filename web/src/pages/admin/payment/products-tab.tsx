import { useState } from "react";
import { Plus } from "lucide-react";
import { toast } from "sonner";

import {
  deletePaymentProduct,
  sortPaymentProducts,
  updatePaymentProduct,
} from "@/api/admin/payment";
import type { PaymentProduct } from "@/api/admin/payment/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { Button } from "@/components/ui/button";
import { isPaymentVersionConflict } from "@/utils/admin/payment/errors";
import { productToRequest } from "@/utils/admin/payment/product";

import { ProductDialog } from "./product-dialog";
import { ProductTable } from "./product-table";
import { useAliveRef } from "../use-admin";
import { usePaymentProducts } from "./use-payment-products";

/**
 * 「充值商品」页签：商品表格 + 新建 / 编辑对话框。
 * 商品的增删改和上下架 admin 与 super_admin 都可以做。改动都带 version 做乐观锁，
 * 冲突（65011）的全局 toast 之后重新拉取列表；删除已有订单的商品后端会拒绝（65012），页面保持列表不变。
 * 请求错误的全局提示由拦截器弹，这里不重复。
 */
export function ProductsTab() {
  const aliveRef = useAliveRef();
  const { products, setProducts, status, reload, retry } = usePaymentProducts();
  /** 对话框对应的商品 ID；null 表示新建。关闭时只收起对话框、保留 ID，退出动画里内容不会先变 */
  const [editingId, setEditingId] = useState<number | null>(null);
  const [open, setOpen] = useState(false);
  /** 排序请求进行中 */
  const [sorting, setSorting] = useState(false);
  /** 正在切换上架的商品 */
  const [togglingId, setTogglingId] = useState<number | null>(null);
  const busy = sorting || togglingId !== null;

  /** 先按新顺序显示，请求成功后以后端数据为准；失败（全局 toast 已弹）就拉回真实顺序 */
  const move = async (ids: number[]) => {
    if (busy) return;
    const byId = new Map(products.map((product) => [product.id, product]));
    setProducts(ids.flatMap((id) => byId.get(id) ?? []));
    setSorting(true);
    try {
      await sortPaymentProducts(ids);
    } catch {
      // 失败后下面的 reload 会把列表拉回真实顺序
    } finally {
      await reload();
      if (aliveRef.current) setSorting(false);
    }
  };

  /** 上架开关：带上该商品的全部字段和 version，只翻转 on_sale */
  const toggleSale = async (product: PaymentProduct) => {
    if (busy) return;
    setTogglingId(product.id);
    try {
      const next = await updatePaymentProduct(
        product.id,
        productToRequest(product, { on_sale: !product.on_sale }),
      );
      if (!aliveRef.current) return;
      setProducts((prev) => prev.map((item) => (item.id === next.id ? next : item)));
      toast.success(next.on_sale ? `「${next.name}」已上架` : `「${next.name}」已下架`);
    } catch (error) {
      // 全局 toast 已弹；版本冲突说明别人改过，重新拉取
      if (isPaymentVersionConflict(error)) await reload();
    } finally {
      if (aliveRef.current) setTogglingId(null);
    }
  };

  const remove = (product: PaymentProduct) =>
    confirm({
      title: `删除「${product.name}」？`,
      destructive: true,
      confirmLabel: "删除",
      description: "删除后用户在积分商城里不再看到这档商品。已有订单的商品不能删除，只能下架。",
      onConfirm: async () => {
        await deletePaymentProduct(product.id);
        toast.success(`已删除「${product.name}」`);
        await reload();
      },
    });

  const editing = products.find((product) => product.id === editingId) ?? null;

  return (
    <section className="bg-card overflow-hidden rounded-xl border">
      <div className="flex items-center justify-between gap-3 p-4">
        <div>
          <h2 className="text-sm font-semibold">充值商品</h2>
          <p className="text-muted-foreground mt-0.5 text-xs">
            用户在积分商城里看到的商品，按这里的顺序排列；下架的商品用户看不到，但历史订单不受影响。
          </p>
        </div>
        <Button
          disabled={status !== "ready"}
          onClick={() => {
            setEditingId(null);
            setOpen(true);
          }}
        >
          <Plus />
          新建商品
        </Button>
      </div>
      <div className="border-t">
        <ProductTable
          products={products}
          status={status}
          busy={busy}
          togglingId={togglingId}
          onRetry={retry}
          actions={{
            onEdit: (product) => {
              setEditingId(product.id);
              setOpen(true);
            },
            onDelete: (product) => void remove(product),
            onToggleSale: (product) => void toggleSale(product),
            onMove: (ids) => void move(ids),
            onCreate: () => {
              setEditingId(null);
              setOpen(true);
            },
          }}
        />
      </div>
      <ProductDialog
        product={editing}
        open={open}
        onClose={() => setOpen(false)}
        onSaved={() => {
          setOpen(false);
          void reload();
        }}
        reload={reload}
      />
    </section>
  );
}

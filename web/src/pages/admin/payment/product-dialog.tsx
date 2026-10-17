import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { createPaymentProduct, updatePaymentProduct } from "@/api/admin/payment";
import type { PaymentProduct } from "@/api/admin/payment/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import { Notice } from "@/components/admin-ui/notice";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { isPaymentVersionConflict } from "@/utils/admin/payment/errors";
import { parseYuanToFen } from "@/utils/admin/payment/money";
import {
  PRODUCT_BADGE_MAX,
  emptyProductForm,
  productFormToRequest,
  productToForm,
  validateProductForm,
  type ProductFormValues,
} from "@/utils/admin/payment/product";
import { formatCredits, formatYuan } from "@/utils/payment/format";

import { useAliveRef } from "../use-admin";

/**
 * 新建 / 编辑商品的表单。售价按「元」输入，字符串精确换算成分，不经过浮点数。
 * 编辑时 version 取列表里的最新一份；版本冲突（65011）的全局 toast 之后，重新拉取列表并把表单刷新为最新内容，
 * 对话框保持打开，避免静默覆盖别人的改动。
 * @param product 正在编辑的商品（列表里的最新一份）；null 表示新建
 * @param onClose 关闭
 * @param onSaved 保存成功（刷新列表）
 * @param reload 重新拉取商品列表，返回最新列表（失败为 null）
 */
function ProductForm({
  product,
  onClose,
  onSaved,
  reload,
}: {
  product: PaymentProduct | null;
  onClose: () => void;
  onSaved: () => void;
  reload: () => Promise<PaymentProduct[] | null>;
}) {
  const aliveRef = useAliveRef();
  const [values, setValues] = useState<ProductFormValues>(() =>
    product ? productToForm(product) : emptyProductForm(),
  );
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  /** 点过保存才显示错误，免得一打开就满屏红字 */
  const [touched, setTouched] = useState(false);
  const errors = validateProductForm(values);
  const invalid = Object.keys(errors).length > 0;
  const shown = touched ? errors : {};
  const total = (() => {
    const base = Number(values.base);
    const bonus = values.bonus.trim() === "" ? 0 : Number(values.bonus);
    return Number.isSafeInteger(base) && Number.isSafeInteger(bonus) && base > 0 && bonus >= 0
      ? base + bonus
      : null;
  })();
  const fen = parseYuanToFen(values.price);

  const set = (key: keyof ProductFormValues, value: string) =>
    setValues((prev) => ({ ...prev, [key]: value }));

  const submit = async () => {
    setTouched(true);
    if (invalid || saving) return;
    setSaving(true);
    try {
      if (product) {
        await updatePaymentProduct(
          product.id,
          productFormToRequest(values, product.on_sale, product.version),
        );
      } else {
        // 新建默认上架：商品写好就该能卖，不想上架再到列表里关掉开关
        await createPaymentProduct(productFormToRequest(values, true, 0));
      }
      if (!aliveRef.current) return;
      toast.success(product ? "商品已保存" : "商品已创建");
      onSaved();
    } catch (error) {
      // 全局 toast 已弹；版本冲突时用最新内容刷新表单，其余错误保持当前输入
      if (product && isPaymentVersionConflict(error)) {
        const fresh = (await reload())?.find((item) => item.id === product.id);
        if (fresh && aliveRef.current) {
          setValues(productToForm(fresh));
          setConflict(true);
        }
      }
    } finally {
      if (aliveRef.current) setSaving(false);
    }
  };

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      {product && product.sold_count > 0 && (
        <Notice tone="info">已有订单：改价只影响之后的新订单，已下的订单金额不变。</Notice>
      )}
      {conflict && (
        <Notice tone="warning">商品已被他人修改，表单已刷新为最新内容，请核对后再保存。</Notice>
      )}
      <FormField label="名称" htmlFor="payment-product-name" required error={shown.name}>
        <Input
          id="payment-product-name"
          autoComplete="off"
          placeholder="如 10,000 积分"
          value={values.name}
          disabled={saving}
          aria-invalid={!!shown.name}
          onChange={(event) => set("name", event.target.value)}
        />
      </FormField>
      <div className="grid gap-4 sm:grid-cols-2">
        <FormField
          label="售价（元）"
          htmlFor="payment-product-price"
          required
          error={shown.price}
          hint="最多两位小数"
        >
          <Input
            id="payment-product-price"
            inputMode="decimal"
            autoComplete="off"
            placeholder="如 99.9"
            className="tabular-nums"
            value={values.price}
            disabled={saving}
            aria-invalid={!!shown.price}
            onChange={(event) => set("price", event.target.value)}
          />
        </FormField>
        <FormField label="基础积分" htmlFor="payment-product-base" required error={shown.base}>
          <Input
            id="payment-product-base"
            inputMode="numeric"
            autoComplete="off"
            className="tabular-nums"
            value={values.base}
            disabled={saving}
            aria-invalid={!!shown.base}
            onChange={(event) => set("base", event.target.value)}
          />
        </FormField>
        <FormField
          label="赠送积分"
          htmlFor="payment-product-bonus"
          error={shown.bonus}
          hint="可不填，不填为 0"
        >
          <Input
            id="payment-product-bonus"
            inputMode="numeric"
            autoComplete="off"
            className="tabular-nums"
            value={values.bonus}
            disabled={saving}
            aria-invalid={!!shown.bonus}
            onChange={(event) => set("bonus", event.target.value)}
          />
        </FormField>
        <FormField
          label="角标"
          htmlFor="payment-product-badge"
          error={shown.badge}
          hint={`可不填，最多 ${PRODUCT_BADGE_MAX} 个字，如「热门」`}
        >
          <Input
            id="payment-product-badge"
            autoComplete="off"
            value={values.badge}
            disabled={saving}
            aria-invalid={!!shown.badge}
            onChange={(event) => set("badge", event.target.value)}
          />
        </FormField>
      </div>
      <p className="bg-muted text-muted-foreground rounded-lg px-3 py-2 text-xs tabular-nums">
        {fen !== null && fen > 0 ? formatYuan(fen) : "—"} 购买后到账{" "}
        <b className="text-foreground">{total !== null ? formatCredits(total) : "—"}</b> 积分
      </p>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={saving} onClick={onClose}>
          取消
        </Button>
        <Button type="submit" disabled={saving}>
          {saving && <Loader2 className="animate-spin" />}
          保存
        </Button>
      </DialogFooter>
    </form>
  );
}

/**
 * 新建 / 编辑商品对话框。每次打开表单整份重置（用 key 区分新建和不同商品）。
 * @param product 正在编辑的商品；null 表示新建
 * @param open 是否打开
 * @param onClose 关闭
 * @param onSaved 保存成功（刷新列表）
 * @param reload 重新拉取商品列表
 */
export function ProductDialog({
  product,
  open,
  onClose,
  onSaved,
  reload,
}: {
  product: PaymentProduct | null;
  open: boolean;
  onClose: () => void;
  onSaved: () => void;
  reload: () => Promise<PaymentProduct[] | null>;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{product ? `编辑「${product.name}」` : "新建商品"}</DialogTitle>
          <DialogDescription>
            用户在积分商城里看到的一档充值商品，到账积分 = 基础 + 赠送。
          </DialogDescription>
        </DialogHeader>
        <ProductForm
          key={product?.id ?? "new"}
          product={product}
          onClose={onClose}
          onSaved={onSaved}
          reload={reload}
        />
      </DialogContent>
    </Dialog>
  );
}

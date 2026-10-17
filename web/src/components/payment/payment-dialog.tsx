import { useEffect, useState } from "react";
import NumberFlow from "@number-flow/react";
import { ShoppingBag } from "lucide-react";

import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useCreditsStore } from "@/store/credits";
import { closeDialog, openDialog, type DialogControl } from "@/store/dialog";
import { usePaymentStore } from "@/store/payment";
import { findProduct } from "@/utils/payment/flow";

import { ProductGrid } from "./product-grid";
import {
  AbnormalPanel,
  CheckoutPanel,
  ClosedPanel,
  ConfirmPanel,
  PanelLoading,
  SuccessPanel,
} from "./side-panel";

/** 收银面板的订单状态轮询间隔：WebSocket 推送是主通道，轮询只是兜底（断线、推送丢失） */
const POLL_INTERVAL_MS = 3000;

/** 一个余额数字：标题 + 滚动数字 */
function Stat({ label, value }: { label: string; value: number | undefined }) {
  return (
    <div className="flex flex-col leading-tight">
      <small className="text-muted-foreground text-[11px]">{label}</small>
      <b className="font-mono text-sm tabular-nums">
        {value === undefined ? "—" : <NumberFlow value={value} />}
      </b>
    </div>
  );
}

/**
 * 积分商城弹窗：左边选档位，右边是确认 → 收银 → 到账的面板。
 * 所有状态在 usePaymentStore 里，弹窗关闭再打开会回到进行中的订单；弹窗只负责展示、轮询和页面可见性的查单。
 */
export function PaymentStoreDialog({ open, onClose, onExited }: DialogControl) {
  const store = usePaymentStore((s) => s.store);
  const pay = usePaymentStore((s) => s.pay);
  const dispatch = usePaymentStore((s) => s.dispatch);
  const credits = useCreditsStore((s) => s.credits);
  const [busy, setBusy] = useState(false);

  /** 打开时拉最新商城数据和余额；挂载期间标记弹窗在显示（WebSocket 推送据此决定要不要弹 toast） */
  useEffect(() => {
    const { setDialogOpen, loadStore } = usePaymentStore.getState();
    setDialogOpen(true);
    void loadStore();
    void useCreditsStore.getState().refresh();
    return () => setDialogOpen(false);
  }, []);

  /** 收银中：每 3 秒拉一次订单状态；用户从付款页切回本页时主动向渠道查一次单 */
  useEffect(() => {
    if (!open || pay.phase !== "checkout") return;
    const { refreshOrder, checkNow } = usePaymentStore.getState();
    const timer = window.setInterval(() => void refreshOrder(), POLL_INTERVAL_MS);
    const onVisible = () => {
      if (document.visibilityState === "visible") void checkNow();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [open, pay.phase]);

  const product = findProduct(store, pay.productId);
  const submitting = pay.phase === "submitting";

  /** 取消订单；取消过程中禁用按钮，避免连点 */
  const cancel = async () => {
    setBusy(true);
    try {
      await usePaymentStore.getState().cancel();
    } finally {
      setBusy(false);
    }
  };

  /** 订单过期后重新下单：先关掉旧单（后端会先查单，已付款会直接入账），再按原选择重新下单 */
  const reorder = async () => {
    setBusy(true);
    try {
      await usePaymentStore.getState().cancel();
      if (usePaymentStore.getState().pay.phase === "confirm")
        await usePaymentStore.getState().submit();
    } finally {
      setBusy(false);
    }
  };

  const reset = () => dispatch({ type: "reset" });

  const side = () => {
    if (!store) return <PanelLoading />;
    const order = pay.order;
    switch (pay.phase) {
      case "checkout":
        return order ? (
          <CheckoutPanel
            order={order}
            busy={busy}
            onCheck={() => void usePaymentStore.getState().checkNow({ manual: true })}
            onCancel={() => void cancel()}
            onReorder={() => void reorder()}
          />
        ) : null;
      case "success":
        return order ? <SuccessPanel order={order} onContinue={reset} /> : null;
      case "closed":
        return order ? <ClosedPanel order={order} onBack={reset} /> : null;
      case "abnormal":
        return order ? <AbnormalPanel order={order} onBack={reset} /> : null;
      default:
        return (
          <ConfirmPanel
            product={product}
            methods={store.methods}
            method={pay.method}
            errorMessage={pay.errorMessage}
            submitting={submitting}
            onSelectMethod={(method) => dispatch({ type: "selectMethod", method })}
            onSubmit={() => void usePaymentStore.getState().submit()}
          />
        );
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      onOpenChangeComplete={(next) => !next && onExited()}
    >
      <DialogContent
        className="max-h-[92vh] gap-0 overflow-y-auto p-0 sm:max-w-5xl"
        showCloseButton
      >
        <div className="flex flex-wrap items-center gap-3 border-b px-5 py-3.5 pr-14">
          <span className="bg-credit text-primary-foreground grid size-8 place-items-center rounded-lg">
            <ShoppingBag className="size-4" />
          </span>
          <div>
            <DialogTitle>积分商城</DialogTitle>
            <DialogDescription className="text-xs">积分即时到账、长期有效</DialogDescription>
          </div>
          <div className="ml-auto flex gap-5">
            <Stat label="可用积分" value={credits?.available} />
            <Stat label="总余额" value={credits?.balance} />
            <Stat label="生成中冻结" value={credits?.frozen} />
          </div>
        </div>
        <div className="grid gap-4 p-5 lg:grid-cols-[minmax(0,1fr)_300px]">
          {store ? (
            <ProductGrid
              products={store.products}
              selectedId={pay.productId}
              disabled={pay.phase !== "confirm"}
              onSelect={(id) => dispatch({ type: "selectProduct", id })}
            />
          ) : (
            <div className="grid grid-cols-2 content-start gap-3 sm:grid-cols-3 lg:grid-cols-4">
              {Array.from({ length: 8 }, (_, i) => (
                <Skeleton key={i} className="h-28 rounded-xl" />
              ))}
            </div>
          )}
          <div className="self-start">{side()}</div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 打开积分商城。已经开着就什么也不做，防止从不同入口连点叠出多个弹窗。
 * 弹窗里的订单进度保存在 store，关掉再打开会回到进行中的订单。
 * @returns 弹窗 ID；已经打开时返回 null
 */
export function openPaymentStore(): number | null {
  if (usePaymentStore.getState().dialogOpen) return null;
  return openDialog(PaymentStoreDialog, {});
}

/**
 * 关闭积分商城（供外部在需要时关闭，如退出登录）。
 * @param id openPaymentStore 返回的弹窗 ID
 */
export const closePaymentStore = (id: number) => closeDialog(id);

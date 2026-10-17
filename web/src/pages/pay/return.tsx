import { useEffect, useState } from "react";
import { CircleCheckBig, CircleHelp, CircleSlash, Loader2, OctagonAlert } from "lucide-react";
import { Link, useLocation } from "react-router";

import { checkPaymentOrder, getPaymentOrder } from "@/api/payment";
import type { OrderDto } from "@/api/payment/type";
import { Button } from "@/components/ui/button";
import { useCreditsStore } from "@/store/credits";
import { describeReturn, parseReturnOrderNo, type ReturnNotice } from "@/utils/payment/return";
import { getToken } from "@/utils/storage/token";

/** 提示类型对应的图标 */
const ICONS: Record<ReturnNotice["kind"], { icon: typeof CircleCheckBig; className: string }> = {
  success: { icon: CircleCheckBig, className: "text-status-success" },
  waiting: { icon: Loader2, className: "text-muted-foreground animate-spin" },
  closed: { icon: CircleSlash, className: "text-muted-foreground" },
  abnormal: { icon: OctagonAlert, className: "text-destructive" },
  unknown: { icon: CircleHelp, className: "text-muted-foreground" },
};

/**
 * 付款完成后渠道把浏览器跳回这里（支付宝、易支付这类跳转类渠道）。
 * 这一页只是落地页，不是支付成功的依据：先读一次订单，还没到账就主动向渠道查一次单（回调可能还在路上）。
 * 要登录才能查订单，没登录就只提示回原页面查看。
 */
export default function PayReturn() {
  const { search } = useLocation();
  const orderNo = parseReturnOrderNo(search);
  const loggedIn = !!getToken();
  const [order, setOrder] = useState<OrderDto | null>(null);
  const [loading, setLoading] = useState(Boolean(orderNo) && loggedIn);

  useEffect(() => {
    if (!orderNo || !loggedIn) return;
    let alive = true;
    (async () => {
      try {
        let current = await getPaymentOrder(orderNo);
        // 回调通常比浏览器跳转慢一点点：还没到账就主动查一次渠道
        if (current.status === "pending")
          current = await checkPaymentOrder(orderNo).catch(() => current);
        if (!alive) return;
        setOrder(current);
        if (current.status === "credited") void useCreditsStore.getState().refresh();
      } catch {
        // 订单不存在或请求失败：全局 toast 已提示，页面按「找不到订单」展示
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [orderNo, loggedIn]);

  const notice: ReturnNotice =
    !loggedIn && orderNo
      ? { kind: "unknown", title: "请先登录", detail: "登录后回到原来的页面，就能看到充值结果。" }
      : describeReturn(order);
  const { icon: Icon, className } = ICONS[loading ? "waiting" : notice.kind];

  return (
    <main className="bg-background grid min-h-svh place-items-center p-6">
      <section className="bg-card flex w-full max-w-sm flex-col items-center gap-3 rounded-2xl border p-8 text-center">
        <Icon className={`size-10 ${className}`} />
        <h1 className="text-lg font-semibold">{loading ? "正在确认付款结果" : notice.title}</h1>
        <p className="text-muted-foreground text-sm">{loading ? "请稍候…" : notice.detail}</p>
        <Button className="mt-2" render={<Link to={loggedIn ? "/" : "/login"} />}>
          {loggedIn ? "回到首页" : "去登录"}
        </Button>
        <p className="text-muted-foreground text-xs">
          你也可以直接关闭这个标签页，回到原来的页面。
        </p>
      </section>
    </main>
  );
}

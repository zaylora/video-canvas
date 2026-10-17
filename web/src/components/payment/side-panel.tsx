import { useEffect, useState } from "react";
import {
  CircleCheckBig,
  CircleSlash,
  ExternalLink,
  Loader2,
  OctagonAlert,
  RefreshCw,
  Timer,
} from "lucide-react";

import type { MethodDto, OrderDto, PayMethod, ProductDto } from "@/api/payment/type";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { METHOD_LABEL } from "@/store/payment";
import { formatCredits, formatYuan } from "@/utils/payment/format";
import { formatCountdown, remainingSeconds } from "@/utils/payment/flow";

import { CheckoutQr } from "./checkout-qr";

/** 支付方式图标底色：品牌色，两个支付方式一眼能分开 */
const METHOD_BADGE: Record<PayMethod, { text: string; className: string }> = {
  wechat: { text: "微", className: "bg-[#07c160]" },
  alipay: { text: "支", className: "bg-[#1677ff]" },
};

/** 用到「当前时间」的组件：每秒走一次，只更新自己，不让整个弹窗重绘 */
function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);
  return now;
}

interface ConfirmPanelProps {
  /** 选中的商品；没有商品时为 undefined */
  product: ProductDto | undefined;
  /** 两种支付方式及可用状态 */
  methods: MethodDto[];
  /** 选中的支付方式 */
  method: PayMethod | null;
  /** 下单失败的提示 */
  errorMessage: string;
  /** 正在下单 */
  submitting: boolean;
  /** 选支付方式 */
  onSelectMethod: (m: PayMethod) => void;
  /** 立即支付 */
  onSubmit: () => void;
}

/** 确认面板：当前选择、实付金额、支付方式、立即支付 */
export function ConfirmPanel({
  product,
  methods,
  method,
  errorMessage,
  submitting,
  onSelectMethod,
  onSubmit,
}: ConfirmPanelProps) {
  const anyAvailable = methods.some((m) => m.available);
  const total = product ? product.baseCredits + product.bonusCredits : 0;
  return (
    <div className="bg-card flex flex-col gap-3 rounded-xl border p-4">
      <div className="text-muted-foreground text-xs">当前选择</div>
      {product ? (
        <dl className="grid grid-cols-[1fr_auto] gap-x-3 gap-y-1.5 text-sm">
          <dt className="text-muted-foreground">基础积分</dt>
          <dd className="font-mono tabular-nums">{formatCredits(product.baseCredits)}</dd>
          <dt className="text-muted-foreground">赠送积分</dt>
          <dd className="font-mono tabular-nums">
            {product.bonusCredits ? `+${formatCredits(product.bonusCredits)}` : "—"}
          </dd>
          <dt className="text-muted-foreground">到账合计</dt>
          <dd className="font-mono font-semibold tabular-nums">{formatCredits(total)}</dd>
        </dl>
      ) : (
        <p className="text-muted-foreground text-sm">请先选择一档积分</p>
      )}
      <div className="flex items-baseline justify-between border-t pt-3">
        <span className="text-muted-foreground text-sm">实付金额</span>
        <b className="text-2xl tabular-nums">{product ? formatYuan(product.priceFen) : "—"}</b>
      </div>
      <div className="grid gap-1.5" role="group" aria-label="支付方式">
        {methods.map((m) => {
          const badge = METHOD_BADGE[m.method];
          const selected = method === m.method && m.available;
          return (
            <button
              key={m.method}
              type="button"
              disabled={!m.available || submitting}
              aria-pressed={selected}
              onClick={() => onSelectMethod(m.method)}
              className={cn(
                "flex items-center gap-2.5 rounded-lg border px-2.5 py-2 text-left text-sm outline-none transition-colors",
                "focus-visible:ring-ring/50 focus-visible:ring-2",
                selected ? "border-foreground ring-foreground ring-1" : "hover:bg-accent/50",
                !m.available && "cursor-not-allowed opacity-55",
              )}
            >
              <span
                className={cn(
                  "grid size-6 shrink-0 place-items-center rounded-md text-xs font-bold text-white",
                  badge.className,
                )}
              >
                {badge.text}
              </span>
              <span>{METHOD_LABEL[m.method]}</span>
              {!m.available && m.reason ? (
                <span className="text-muted-foreground ml-auto text-xs">{m.reason}</span>
              ) : null}
            </button>
          );
        })}
      </div>
      {errorMessage ? (
        <div
          className="text-destructive bg-destructive/10 flex gap-2 rounded-lg px-3 py-2 text-xs"
          role="alert"
        >
          <OctagonAlert className="mt-px size-3.5 shrink-0" />
          <span>{errorMessage}</span>
        </div>
      ) : null}
      <Button
        size="lg"
        className="h-10"
        disabled={!product || !anyAvailable || submitting}
        onClick={onSubmit}
      >
        {submitting ? <Loader2 className="animate-spin" /> : null}
        {submitting ? "正在下单…" : anyAvailable ? "立即支付" : "暂未开放支付"}
      </Button>
      <p className="text-muted-foreground text-center text-[11px]">
        {anyAvailable
          ? "积分长期有效 · 支付成功即时到账 · 不支持退款"
          : "两种支付方式都不可用，请联系管理员"}
      </p>
    </div>
  );
}

interface CheckoutPanelProps {
  /** 当前订单（pending） */
  order: OrderDto;
  /** 我已完成支付：主动查单 */
  onCheck: () => void;
  /** 取消订单 */
  onCancel: () => void;
  /** 重新下单（订单已过期时） */
  onReorder: () => void;
  /** 正在取消，禁用按钮 */
  busy: boolean;
}

/** 收银面板：二维码或跳转按钮、倒计时、我已完成支付、取消 */
export function CheckoutPanel({ order, onCheck, onCancel, onReorder, busy }: CheckoutPanelProps) {
  const now = useNow(true);
  const left = remainingSeconds(order.expiresAt, now);
  const expired = left === 0;
  const checkout = order.checkout;
  const method = METHOD_BADGE[order.method];
  const isQr = checkout?.mode === "qrcode" || checkout?.mode === "qrcode_image";
  return (
    <div className="bg-card flex flex-col gap-3 rounded-xl border p-4">
      <div className="flex items-center gap-2">
        <span
          className={cn(
            "grid size-6 place-items-center rounded-md text-xs font-bold text-white",
            method.className,
          )}
        >
          {method.text}
        </span>
        <b className="text-sm">{METHOD_LABEL[order.method]}</b>
        <b className="ml-auto text-lg tabular-nums">{formatYuan(order.amountFen)}</b>
      </div>
      {checkout && isQr ? (
        <>
          <CheckoutQr
            mode={checkout.mode as "qrcode" | "qrcode_image"}
            value={checkout.value}
            expired={expired}
          />
          <p className="text-muted-foreground text-center text-xs">
            请使用{order.method === "wechat" ? "微信" : "支付宝"}扫码支付{" "}
            {formatYuan(order.amountFen)}
          </p>
        </>
      ) : checkout ? (
        <div className="grid place-items-center gap-2 rounded-lg border px-3 py-4 text-center">
          <ExternalLink className="size-5" />
          <span className="text-sm">在新标签页完成付款</span>
          <Button
            className="w-full"
            disabled={expired}
            onClick={() => window.open(checkout.value, "_blank", "noopener,noreferrer")}
          >
            前往{METHOD_LABEL[order.method]}付款
          </Button>
          <span className="text-muted-foreground text-[11px]">
            付款完成后回到这里，系统会自动确认。
          </span>
        </div>
      ) : null}
      <div className="text-muted-foreground flex items-center justify-center gap-1.5 text-xs">
        <Timer className="size-3.5" />
        <span className="font-mono tabular-nums">
          {expired ? "已过期" : `剩余 ${formatCountdown(left)}`}
        </span>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        <dt className="text-muted-foreground">到账积分</dt>
        <dd className="text-right font-mono tabular-nums">{formatCredits(order.credits)}</dd>
        <dt className="text-muted-foreground">订单号</dt>
        <dd className="text-right font-mono text-[11px] break-all">{order.orderNo}</dd>
      </dl>
      {expired ? (
        <Button onClick={onReorder} disabled={busy}>
          重新下单
        </Button>
      ) : (
        <>
          <Button onClick={onCheck} disabled={busy}>
            <RefreshCw /> 我已完成支付
          </Button>
          <Button variant="outline" onClick={onCancel} disabled={busy}>
            取消订单
          </Button>
        </>
      )}
      <p className="text-muted-foreground text-[11px]">
        关闭弹窗不影响订单：结果会自动推送，再次打开商城会回到这里。
      </p>
    </div>
  );
}

/** 到账成功面板 */
export function SuccessPanel({ order, onContinue }: { order: OrderDto; onContinue: () => void }) {
  return (
    <div className="bg-card flex flex-col items-center gap-2 rounded-xl border px-4 py-7 text-center">
      <CircleCheckBig className="text-status-success size-9" />
      <b className="text-lg">已到账 +{formatCredits(order.credits)} 积分</b>
      <p className="text-muted-foreground text-xs">
        {order.latePaid ? "这笔是订单关闭后才收到的付款，已照常入账。" : "支付成功，积分已入账。"}
      </p>
      <span className="text-muted-foreground font-mono text-[11px]">订单号 {order.orderNo}</span>
      <Button className="mt-1" onClick={onContinue}>
        继续充值
      </Button>
    </div>
  );
}

/** 订单已关闭面板（过期、取消、在别处被关闭） */
export function ClosedPanel({ order, onBack }: { order: OrderDto; onBack: () => void }) {
  const reason =
    order.closeReason === "timeout"
      ? "订单已过期。"
      : order.closeReason === "admin"
        ? "管理员关闭了这笔订单。"
        : "订单已关闭。";
  return (
    <div className="bg-card flex flex-col items-center gap-2 rounded-xl border px-4 py-7 text-center">
      <CircleSlash className="text-muted-foreground size-8" />
      <b>订单已关闭</b>
      <p className="text-muted-foreground text-xs">{reason}如果已经付款，会在确认后自动到账。</p>
      <Button variant="outline" onClick={onBack}>
        重新选择
      </Button>
    </div>
  );
}

/** 异常面板：金额等核对不通过，不自动入账，等管理员处理 */
export function AbnormalPanel({ order, onBack }: { order: OrderDto; onBack: () => void }) {
  return (
    <div className="bg-card flex flex-col items-center gap-2 rounded-xl border px-4 py-7 text-center">
      <OctagonAlert className="text-destructive size-8" />
      <b>支付金额异常</b>
      <p className="text-muted-foreground text-xs">
        已通知管理员处理，请保留付款凭证并联系管理员。
      </p>
      <span className="font-mono text-[11px]">订单号 {order.orderNo}</span>
      <Button variant="outline" onClick={onBack}>
        返回
      </Button>
    </div>
  );
}

/** 面板加载中 */
export function PanelLoading() {
  return (
    <div className="bg-card grid min-h-60 place-items-center rounded-xl border">
      <Loader2 className="text-muted-foreground size-6 animate-spin" />
    </div>
  );
}

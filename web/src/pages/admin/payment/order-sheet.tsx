import { useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { closePaymentOrder, getPaymentOrder, queryPaymentOrder } from "@/api/admin/payment";
import type { PaymentChannel, PaymentOrderDetail } from "@/api/admin/payment/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import { EmptyState, EmptyStateActions, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import { FoldableCode } from "@/components/admin-ui/foldable-code";
import { Notice } from "@/components/admin-ui/notice";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import {
  Timeline,
  TimelineContent,
  TimelineHeader,
  TimelineIndicator,
  TimelineItem,
} from "@/components/admin-ui/timeline";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  closeBlockReason,
  closeUpstreamWarning,
  formatPayload,
  notifyResultTag,
  orderStatusTag,
  orderTimeline,
  queryBlockReason,
} from "@/utils/admin/payment/order";
import { METHOD_LABEL } from "@/utils/admin/payment/settings";
import { formatAbsolute } from "@/utils/admin/user-rules";
import { formatCredits, formatYuan } from "@/utils/payment/format";

import { useAliveRef, type LoadStatus } from "../use-admin";

/** 时间线节点的圆点颜色：入账绿、异常红、其余默认 */
const TIMELINE_TONE = {
  default: "",
  success: "border-emerald-500/60 text-emerald-600 dark:text-emerald-400",
  danger: "border-red-500/60 text-red-600 dark:text-red-400",
} as const;

/**
 * 订单详情抽屉的内容：进抽屉读一次详情（订单 + 渠道通知记录）；
 * 「查单」「关单」成功后重新读详情并通知外面刷新列表。
 * 查单仅待支付 / 已关闭可用，且渠道要能查单；关单仅待支付可用，确认框写明会先向渠道查单、已付款则直接入账。
 * 禁用的按钮不隐藏，悬停写明原因。请求错误的全局提示由拦截器弹，这里不重复。
 * @param orderNo 订单号
 * @param channels 渠道列表（取该订单渠道的查单 / 关单能力）
 * @param onChanged 查单或关单成功，外面刷新订单列表
 */
function OrderSheetBody({
  orderNo,
  channels,
  onChanged,
}: {
  orderNo: string;
  channels: PaymentChannel[];
  onChanged: () => void;
}) {
  const aliveRef = useAliveRef();
  const [detail, setDetail] = useState<PaymentOrderDetail | null>(null);
  const [status, setStatus] = useState<LoadStatus>("loading");
  const [querying, setQuerying] = useState(false);

  const load = useCallback(
    () =>
      getPaymentOrder(orderNo).then(
        (next) => {
          if (!aliveRef.current) return;
          setDetail(next);
          setStatus("ready");
        },
        () => {
          // 全局 toast 已弹；已有详情时保留旧内容
          if (aliveRef.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
        },
      ),
    [aliveRef, orderNo],
  );

  useEffect(() => {
    void load();
  }, [load]);

  if (status === "loading") {
    return (
      <div className="flex flex-col gap-3 p-4" aria-busy="true">
        <Skeleton className="h-6 w-48" />
        <Skeleton className="h-40 rounded-lg" />
        <Skeleton className="h-32 rounded-lg" />
      </div>
    );
  }
  if (status === "error" || !detail) {
    return (
      <div className="p-4">
        <EmptyState>
          <EmptyStateTitle className="text-destructive">订单加载失败</EmptyStateTitle>
          <EmptyStateActions>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setStatus("loading");
                void load();
              }}
            >
              重试
            </Button>
          </EmptyStateActions>
        </EmptyState>
      </div>
    );
  }

  const channel = channels.find((item) => item.provider === detail.provider);
  const tag = orderStatusTag(detail);
  const queryReason = queryBlockReason(detail, channel);
  const closeReason = closeBlockReason(detail);
  const upstreamWarning = closeUpstreamWarning(channel);

  const query = async () => {
    if (queryReason || querying) return;
    setQuerying(true);
    try {
      const result = await queryPaymentOrder(detail.order_no);
      if (!aliveRef.current) return;
      toast.success(`查单完成：订单${orderStatusTag({ ...detail, status: result.status }).label}`);
      await load();
      onChanged();
    } catch {
      // 全局 toast 已弹（如渠道不支持查单 65008），详情保持不变
    } finally {
      if (aliveRef.current) setQuerying(false);
    }
  };

  const closeOrder = () =>
    confirm({
      title: `关闭订单 ${detail.order_no}？`,
      destructive: true,
      confirmLabel: "关单",
      description: (
        <>
          <p>会先向渠道查单：如果用户其实已经付款，会直接入账，不会关闭；否则关闭订单。</p>
          {upstreamWarning && (
            <p className="mt-2 text-amber-700 dark:text-amber-400">{upstreamWarning}</p>
          )}
        </>
      ),
      onConfirm: async () => {
        const result = await closePaymentOrder(detail.order_no);
        toast.success(
          `关单完成：订单${orderStatusTag({ ...detail, status: result.status }).label}`,
        );
        await load();
        onChanged();
      },
    });

  const timeline = orderTimeline(detail);

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-4 pb-4">
        {tag.hint && detail.status === "abnormal" && (
          <Notice tone="danger" title={`异常：${tag.hint}`}>
            订单不会自动入账。核实后请在渠道商户后台退款，或用「调整积分」手动处理并写备注。
          </Notice>
        )}
        {detail.status === "create_failed" && detail.fail_reason && (
          <Notice tone="warning" title="渠道下单失败">
            {detail.fail_reason}
          </Notice>
        )}

        <section aria-label="订单信息">
          <h3 className="mb-3 text-sm font-semibold">订单信息</h3>
          <DescriptionList className="grid-cols-2 md:grid-cols-2 2xl:grid-cols-2">
            <DescriptionItem>
              <DescriptionTerm>用户</DescriptionTerm>
              <DescriptionDetails>
                {detail.username || "—"}
                <span className="text-muted-foreground text-xs font-normal">#{detail.user_id}</span>
              </DescriptionDetails>
            </DescriptionItem>
            <DescriptionItem>
              <DescriptionTerm>商品</DescriptionTerm>
              <DescriptionDetails>{detail.product_name}</DescriptionDetails>
            </DescriptionItem>
            <DescriptionItem>
              <DescriptionTerm>应付金额</DescriptionTerm>
              <DescriptionDetails className="tabular-nums">
                {formatYuan(detail.amount_fen)}
              </DescriptionDetails>
            </DescriptionItem>
            <DescriptionItem>
              <DescriptionTerm>渠道报告实付</DescriptionTerm>
              <DescriptionDetails
                className={cn(
                  "tabular-nums",
                  detail.paid_amount_fen > 0 &&
                    detail.paid_amount_fen !== detail.amount_fen &&
                    "text-red-600 dark:text-red-400",
                )}
              >
                {detail.paid_amount_fen > 0 ? formatYuan(detail.paid_amount_fen) : "—"}
              </DescriptionDetails>
            </DescriptionItem>
            <DescriptionItem>
              <DescriptionTerm>到账积分</DescriptionTerm>
              <DescriptionDetails className="tabular-nums">
                {formatCredits(detail.credits)}
              </DescriptionDetails>
            </DescriptionItem>
            <DescriptionItem>
              <DescriptionTerm>状态</DescriptionTerm>
              <DescriptionDetails>
                <Tag tone={tag.tone}>{tag.label}</Tag>
              </DescriptionDetails>
            </DescriptionItem>
            <DescriptionItem>
              <DescriptionTerm>支付方式</DescriptionTerm>
              <DescriptionDetails>
                {METHOD_LABEL[detail.method] ?? detail.method}
              </DescriptionDetails>
            </DescriptionItem>
            <DescriptionItem>
              <DescriptionTerm>渠道</DescriptionTerm>
              <DescriptionDetails>
                {detail.provider_name || detail.provider}
                <span className="text-muted-foreground text-xs font-normal">
                  配置 v{detail.channel_version}
                </span>
              </DescriptionDetails>
            </DescriptionItem>
            <DescriptionItem className="col-span-2">
              <DescriptionTerm>渠道交易号</DescriptionTerm>
              <DescriptionDetails className="font-mono text-xs break-all">
                {detail.provider_trade_no || "—"}
              </DescriptionDetails>
            </DescriptionItem>
            <DescriptionItem className="col-span-2">
              <DescriptionTerm>到期时间</DescriptionTerm>
              <DescriptionDetails className="tabular-nums">
                {formatAbsolute(detail.expires_at)}
              </DescriptionDetails>
            </DescriptionItem>
          </DescriptionList>
        </section>

        <section aria-label="状态时间线">
          <h3 className="mb-3 text-sm font-semibold">状态时间线</h3>
          <Timeline>
            {timeline.map((entry) => (
              <TimelineItem key={entry.key}>
                <TimelineIndicator className={TIMELINE_TONE[entry.tone]}>
                  <span className="size-1.5 rounded-full bg-current" />
                </TimelineIndicator>
                <TimelineContent className="pt-1">
                  <TimelineHeader>
                    <span className="text-sm font-medium">{entry.label}</span>
                    <span className="text-muted-foreground text-xs tabular-nums">
                      {entry.at ? formatAbsolute(entry.at) : ""}
                    </span>
                  </TimelineHeader>
                </TimelineContent>
              </TimelineItem>
            ))}
          </Timeline>
        </section>

        <section aria-label="渠道通知记录">
          <h3 className="mb-3 text-sm font-semibold">
            渠道通知记录
            <span className="text-muted-foreground ml-1.5 font-normal tabular-nums">
              {detail.notifications.length}
            </span>
          </h3>
          {detail.notifications.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              还没有收到这笔订单的渠道通知。付了款却没到账时，可以用下方的「查单」向渠道确认。
            </p>
          ) : (
            <ul className="flex flex-col gap-3">
              {detail.notifications.map((note) => {
                const result = notifyResultTag(note.result);
                return (
                  <li key={note.id} className="rounded-lg border p-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <Tag tone={result.tone}>{result.label}</Tag>
                      <span className="text-muted-foreground text-xs tabular-nums">
                        {formatAbsolute(note.received_at)}
                      </span>
                      <span className="text-muted-foreground ml-auto truncate font-mono text-xs">
                        {note.event_key}
                      </span>
                    </div>
                    <FoldableCode
                      className="mt-2"
                      text={formatPayload(note.payload)}
                      maxChars={200}
                    />
                  </li>
                );
              })}
            </ul>
          )}
        </section>
      </div>
      <SheetFooter className="flex-row justify-end border-t">
        <ReasonTooltip reason={queryReason}>
          <Button
            variant="outline"
            disabled={!!queryReason || querying}
            onClick={() => void query()}
          >
            {querying && <Loader2 className="animate-spin" />}
            查单
          </Button>
        </ReasonTooltip>
        <ReasonTooltip reason={closeReason}>
          <Button
            variant="destructive"
            disabled={!!closeReason || querying}
            onClick={() => void closeOrder()}
          >
            关单
          </Button>
        </ReasonTooltip>
      </SheetFooter>
    </>
  );
}

/**
 * 订单详情抽屉。每次打开（或换订单）内容重新挂载并重新读详情。
 * @param orderNo 订单号；关闭动画期间保持上一次的订单号，避免内容先消失
 * @param open 是否打开
 * @param channels 渠道列表（取查单 / 关单能力）
 * @param onClose 关闭
 * @param onChanged 查单或关单成功，外面刷新订单列表
 */
export function OrderSheet({
  orderNo,
  open,
  channels,
  onClose,
  onChanged,
}: {
  orderNo: string | null;
  open: boolean;
  channels: PaymentChannel[];
  onClose: () => void;
  onChanged: () => void;
}) {
  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent className="gap-0 data-[side=right]:sm:max-w-[560px]">
        <SheetHeader>
          <SheetTitle>订单详情</SheetTitle>
          <SheetDescription className="font-mono text-xs break-all">{orderNo}</SheetDescription>
        </SheetHeader>
        {orderNo && (
          <OrderSheetBody
            key={orderNo}
            orderNo={orderNo}
            channels={channels}
            onChanged={onChanged}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

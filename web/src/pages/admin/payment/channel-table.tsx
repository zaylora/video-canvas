import { Settings2 } from "lucide-react";

import type { PaymentChannel } from "@/api/admin/payment/type.d";
import { EmptyState, EmptyStateActions, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
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
import { cn } from "@/lib/utils";
import {
  OPS_ONLY_REASON,
  capabilityTags,
  channelStatus,
  checkoutLabel,
  methodsText,
  servingText,
} from "@/utils/admin/payment/channel";

import type { LoadStatus } from "../use-admin";

/** 渠道行上的操作回调 */
export type ChannelRowActions = {
  /** 打开配置抽屉（admin 是只读查看） */
  onOpen: (channel: PaymentChannel) => void;
  /** 停用（弹确认框） */
  onDisable: (channel: PaymentChannel) => void;
};

/**
 * 支付渠道列表：渠道名与 provider、支付方式、收银形态、能力、状态与承接、自动关闭时间、未完成订单数、操作。
 * 点击行（或聚焦后按 Enter）打开配置抽屉；「停用」只对已启用渠道可用，其余禁用并写明原因，不隐藏。
 * @param channels 渠道列表
 * @param status 加载状态：加载中显示骨架行，失败显示重试
 * @param canWrite 是否有写权限（super_admin）；admin 的配置按钮变成「查看」，停用禁用
 * @param onRetry 加载失败后的重试
 * @param actions 行操作回调
 */
export function ChannelTable({
  channels,
  status,
  canWrite,
  onRetry,
  actions,
}: {
  channels: PaymentChannel[];
  status: LoadStatus;
  canWrite: boolean;
  onRetry: () => void;
  actions: ChannelRowActions;
}) {
  if (status === "error") {
    return (
      <EmptyState className="border-0">
        <EmptyStateTitle className="text-destructive">渠道加载失败</EmptyStateTitle>
        <EmptyStateActions>
          <Button variant="outline" size="sm" onClick={onRetry}>
            重试
          </Button>
        </EmptyStateActions>
      </EmptyState>
    );
  }
  if (status === "ready" && channels.length === 0) {
    return (
      <EmptyState className="border-0">
        <EmptyStateTitle>没有可用的支付渠道</EmptyStateTitle>
      </EmptyState>
    );
  }
  return (
    <Table data-slot="payment-channel-table">
      <TableHeader>
        <TableRow className="bg-muted/40 hover:bg-muted/40">
          <TableHead className="px-4">渠道</TableHead>
          <TableHead>支付方式</TableHead>
          <TableHead>收银形态</TableHead>
          <TableHead>能力</TableHead>
          <TableHead>状态</TableHead>
          <TableHead className="text-right">自动关闭</TableHead>
          <TableHead className="text-right">未完成订单</TableHead>
          <TableHead className="px-4 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {status === "loading" && <TableSkeletonRows columns={8} rows={4} />}
        {status === "ready" &&
          channels.map((channel) => (
            <ChannelRow
              key={channel.provider}
              channel={channel}
              canWrite={canWrite}
              actions={actions}
            />
          ))}
      </TableBody>
    </Table>
  );
}

function ChannelRow({
  channel,
  canWrite,
  actions,
}: {
  channel: PaymentChannel;
  canWrite: boolean;
  actions: ChannelRowActions;
}) {
  const state = channelStatus(channel);
  const disableReason = !canWrite
    ? OPS_ONLY_REASON
    : channel.enabled
      ? null
      : "渠道未启用，无需停用";
  return (
    <TableRow
      data-provider={channel.provider}
      tabIndex={0}
      aria-label={`${canWrite ? "配置" : "查看"} ${channel.name}`}
      className={cn(
        "focus-visible:ring-ring/50 cursor-pointer align-top outline-none focus-visible:ring-2 focus-visible:ring-inset",
      )}
      onClick={() => actions.onOpen(channel)}
      onKeyDown={(event) => {
        if (event.key === "Enter" && event.target === event.currentTarget) actions.onOpen(channel);
      }}
    >
      <TableCell className="px-4 py-3">
        <div className="font-medium">{channel.name}</div>
        <div className="text-muted-foreground font-mono text-xs">{channel.provider}</div>
      </TableCell>
      <TableCell className="py-3">{methodsText(channel.methods)}</TableCell>
      <TableCell className="py-3">{checkoutLabel(channel.checkout)}</TableCell>
      <TableCell className="py-3">
        <div className="flex flex-wrap gap-1">
          {capabilityTags(channel).map((tag) => (
            <Tag key={tag.label} tone={tag.tone}>
              {tag.label}
            </Tag>
          ))}
        </div>
      </TableCell>
      <TableCell className="py-3">
        <Tag tone={state.tone}>{state.label}</Tag>
        <div className="text-muted-foreground mt-1 text-xs">{servingText(channel.serving)}</div>
      </TableCell>
      <TableCell className="py-3 text-right tabular-nums">
        {channel.order_timeout_minutes} 分钟
      </TableCell>
      <TableCell className="py-3 text-right tabular-nums">{channel.open_orders}</TableCell>
      <TableCell className="px-4 py-3">
        {/* 禁用按钮外的说明层也在这一格里，点它不该打开行的抽屉 */}
        <div className="flex justify-end gap-1.5" onClick={(event) => event.stopPropagation()}>
          <Button variant="outline" size="xs" onClick={() => actions.onOpen(channel)}>
            <Settings2 />
            {canWrite ? "配置" : "查看"}
          </Button>
          <ReasonTooltip reason={disableReason}>
            <Button
              variant="outline"
              size="xs"
              disabled={!!disableReason}
              onClick={() => actions.onDisable(channel)}
            >
              停用
            </Button>
          </ReasonTooltip>
        </div>
      </TableCell>
    </TableRow>
  );
}

import { KeyRound, Loader2, Pencil, Power, Trash2 } from "lucide-react";

import type { ChannelLoad, ChannelOption } from "@/api/admin/ai/type.d";
import { Notice } from "@/components/admin-ui/notice";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { formatLoad, formatRateLimit } from "@/utils/admin/channel-form";

import { SUPER_ONLY } from "./channel-list";

/**
 * 主从页右栏头部：名称与启停状态、操作按钮、Base URL、各类配置标签、当前负载；停用时有警示条。
 * 写操作（编辑、设置 Key、启停、删除）只对 super_admin 可用，admin 看到置灰按钮并写明原因，不隐藏。
 * @param channel 当前渠道
 * @param load 当前渠道的负载记录；没有未完成任务时为 undefined
 * @param loadStale 负载最近一次刷新失败，显示的是旧数据
 * @param canWrite 是否 super_admin
 * @param toggling 正在启停
 */
export function ChannelHeader({
  channel,
  load,
  loadStale,
  canWrite,
  toggling,
  onEdit,
  onSetKey,
  onToggle,
  onDelete,
}: {
  channel: ChannelOption;
  load: ChannelLoad | undefined;
  loadStale: boolean;
  canWrite: boolean;
  toggling: boolean;
  onEdit: () => void;
  onSetKey: () => void;
  onToggle: () => void;
  onDelete: () => void;
}) {
  const reason = canWrite ? null : SUPER_ONLY;
  const headerCount = Object.keys(channel.headers ?? {}).length;

  return (
    <section data-slot="channel-header" className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <h2 className="min-w-0 text-xl font-semibold break-words">{channel.name}</h2>
        <Tag tone={channel.enabled ? "success" : "danger"}>
          {channel.enabled ? "已启用" : "已停用"}
        </Tag>
        <div className="ml-auto flex flex-wrap items-center gap-1.5">
          <ReasonTooltip reason={reason}>
            <Button size="sm" variant="outline" disabled={!canWrite} onClick={onEdit}>
              <Pencil />
              编辑渠道
            </Button>
          </ReasonTooltip>
          <ReasonTooltip reason={reason}>
            <Button size="sm" variant="outline" disabled={!canWrite} onClick={onSetKey}>
              <KeyRound />
              设置 Key
            </Button>
          </ReasonTooltip>
          <ReasonTooltip reason={reason}>
            <Button size="sm" variant="outline" disabled={!canWrite || toggling} onClick={onToggle}>
              {toggling ? <Loader2 className="animate-spin" /> : <Power />}
              {channel.enabled ? "停用" : "启用"}
            </Button>
          </ReasonTooltip>
          <ReasonTooltip reason={reason}>
            <Button size="sm" variant="destructive" disabled={!canWrite} onClick={onDelete}>
              <Trash2 />
              删除
            </Button>
          </ReasonTooltip>
        </div>
      </div>

      <div className="text-muted-foreground font-mono text-xs break-all">{channel.base_url}</div>

      <div className="flex flex-wrap items-center gap-1.5">
        <Tag tone={channel.secret_set ? "success" : "warning"}>
          API Key {channel.secret_set ? "已配置" : "未设置"}
        </Tag>
        <Tag tone={channel.secret_key_set ? "success" : "neutral"}>
          Secret Key {channel.secret_key_set ? "已设置" : "未设置（可选）"}
        </Tag>
        <Tag>请求头 {headerCount} 条</Tag>
        <Tag>限流 {formatRateLimit(channel.rate_limit)}</Tag>
        <Tag
          tone={loadStale ? "warning" : "neutral"}
          className="tabular-nums"
          title={loadStale ? "负载刷新失败，显示的是旧数据" : "每 15 秒刷新"}
        >
          {formatLoad(load, channel.rate_limit?.max_running)}
          {loadStale && "（旧数据）"}
        </Tag>
      </div>

      {!channel.enabled && (
        <Notice tone="warning" className="mt-1">
          渠道已停用：它的所有模型在画布里不可见；进行中的任务按快照继续。
        </Notice>
      )}
    </section>
  );
}

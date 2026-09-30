import { Download, Eye, KeyRound, Network, Pencil, Plus, Stethoscope } from "lucide-react";

import type { ChannelView, PluginView } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { availableUpgrade, channelMeta } from "@/utils/admin/plugin";

import { Tag } from "../shared";
import type { LoadStatus } from "../use-admin";
import { CheckResult } from "./check-result";
import type { CheckState } from "./use-channel-check";

/**
 * 渠道列表。列：名称与 key、插件与固定版本（有新版本带“可升级”）、base_url、Key 状态、
 * 风险开关（图标加文字）、启用、操作。写操作（检查、新建）只对运维渲染；导入 admin 也能用。
 */
export function ChannelTable({
  channels,
  plugins,
  status,
  canWrite,
  checks,
  onNew,
  onEdit,
  onCheck,
  onImport,
  onRetry,
}: {
  channels: ChannelView[];
  plugins: PluginView[];
  status: LoadStatus;
  canWrite: boolean;
  checks: Record<string, CheckState>;
  onNew: () => void;
  onEdit: (key: string) => void;
  onCheck: (key: string) => void;
  onImport: (channel: ChannelView) => void;
  onRetry: () => void;
}) {
  if (status === "loading") {
    return (
      <div className="flex flex-col gap-2 rounded-lg border p-3" aria-busy="true">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton key={index} className="h-10" />
        ))}
      </div>
    );
  }
  if (status === "error") {
    return (
      <p className="text-destructive text-sm">
        加载失败，
        <button type="button" className="underline" onClick={onRetry}>
          重试
        </button>
      </p>
    );
  }
  if (channels.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 rounded-lg border p-12 text-center">
        <p className="font-medium">还没有渠道</p>
        <p className="text-muted-foreground max-w-md text-sm">
          渠道把一个插件版本、一个地址和一个 Key 绑在一起，模型通过渠道调用上游。
          {!canWrite && "请联系运维新建。"}
        </p>
        {canWrite && (
          <Button onClick={onNew}>
            <Plus />
            新建渠道
          </Button>
        )}
      </div>
    );
  }

  return (
    <div className="rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>渠道</TableHead>
            <TableHead>插件与版本</TableHead>
            <TableHead>base_url</TableHead>
            <TableHead>Key</TableHead>
            <TableHead>风险开关</TableHead>
            <TableHead>状态</TableHead>
            <TableHead className="text-right">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {channels.map((channel) => {
            const plugin = plugins.find((item) => item.key === channel.plugin_key);
            const upgrade = availableUpgrade(plugins, channel);
            const meta = channelMeta(plugins, channel);
            const importUnsupported = !!meta && !meta.import;
            const needsKey = (meta?.auth?.type ?? "none") !== "none";
            return (
              <TableRow key={channel.key} data-channel={channel.key}>
                <TableCell>
                  <div className="font-medium">{channel.name}</div>
                  <div className="text-muted-foreground font-mono text-xs">{channel.key}</div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span>{plugin?.name ?? channel.plugin_key}</span>
                    <span className="font-mono text-xs">v{channel.plugin_version}</span>
                    {upgrade && (
                      <Tag tone="info" title="插件有新版本，可在渠道里切换">
                        可升级 → {upgrade.version}
                      </Tag>
                    )}
                  </div>
                </TableCell>
                <TableCell className="max-w-56 truncate font-mono text-xs" title={channel.base_url}>
                  {channel.base_url}
                </TableCell>
                <TableCell>
                  {channel.secret_set ? (
                    <Tag tone="success">已设置</Tag>
                  ) : needsKey || !meta ? (
                    <Tag tone="warning" title="未设置 Key，模型无法发布">
                      未设置
                    </Tag>
                  ) : (
                    <Tag title="这个插件不需要 Key">无需</Tag>
                  )}
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {channel.trusted_internal && (
                      <Tag tone="warning" title="trusted_internal：允许 base_url 解析到内网地址">
                        <Network className="size-3" />
                        内网
                      </Tag>
                    )}
                    {channel.allow_credentials && (
                      <Tag tone="warning" title="allow_credentials：插件代码能读取这个渠道的 Key">
                        <KeyRound className="size-3" />
                        凭证
                      </Tag>
                    )}
                    {!channel.trusted_internal && !channel.allow_credentials && (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </div>
                </TableCell>
                <TableCell>
                  <Tag tone={channel.enabled ? "success" : "neutral"}>{channel.enabled ? "启用" : "停用"}</Tag>
                </TableCell>
                <TableCell>
                  <div className="flex items-center justify-end gap-1.5">
                    <CheckResult compact state={checks[channel.key]} />
                    {canWrite && (
                      <Button
                        size="xs"
                        variant="outline"
                        disabled={checks[channel.key]?.busy}
                        aria-label={`检查 ${channel.name}`}
                        onClick={() => onCheck(channel.key)}
                      >
                        <Stethoscope />
                        检查
                      </Button>
                    )}
                    <Button
                      size="xs"
                      variant="outline"
                      disabled={importUnsupported}
                      title={importUnsupported ? "该插件不支持导入模型" : undefined}
                      aria-label={`导入模型 ${channel.name}`}
                      onClick={() => onImport(channel)}
                    >
                      <Download />
                      导入模型
                    </Button>
                    <Button
                      size="xs"
                      variant="outline"
                      aria-label={`${canWrite ? "编辑" : "查看"} ${channel.name}`}
                      onClick={() => onEdit(channel.key)}
                    >
                      {canWrite ? <Pencil /> : <Eye />}
                      {canWrite ? "编辑" : "查看"}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

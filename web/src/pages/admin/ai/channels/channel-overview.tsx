import { Network, KeyRound, ShieldCheck } from "lucide-react";
import { Link } from "react-router";

import type { ChannelLoad, ChannelView, PluginView } from "@/api/admin/ai/type.d";
import { CopyButton } from "@/components/admin-ui/copy-button";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import { Tag } from "@/components/admin-ui/tag";
import { cn } from "@/lib/utils";
import { availableUpgrade, channelMeta } from "@/utils/admin/plugin";
import { formatShortTime } from "@/utils/time";

import { CheckResult } from "./check-result";
import type { CheckState } from "./use-channel-check";

/** 这个渠道是否缺 Key：插件要求鉴权（或读不到插件 meta）且没设置 */
export const isKeyMissing = (plugins: PluginView[], channel: ChannelView) => {
  const meta = channelMeta(plugins, channel);
  return !channel.secret_set && ((meta?.auth?.type ?? "none") !== "none" || !meta);
};

const WARN_TEXT = "text-amber-600 dark:text-amber-400";

/**
 * 渠道弹窗的“概览”页签：只读的字段表，问题写在对应字段旁边（未设置 · 设置、升级到 vX），
 * 不再用横幅。检查过的话，结果在最上面一行。
 * @param onUpgrade 点“升级到 vX”：切到配置页签（那里改固定版本）
 * @param onSetKey 点“设置”：打开设置 Key 弹窗
 */
export function ChannelOverview({
  channel,
  plugins,
  canWrite,
  check,
  load,
  onUpgrade,
  onSetKey,
}: {
  channel: ChannelView;
  plugins: PluginView[];
  canWrite: boolean;
  check: CheckState | undefined;
  load: ChannelLoad | undefined;
  onUpgrade: () => void;
  onSetKey: () => void;
}) {
  const plugin = plugins.find((item) => item.key === channel.plugin_key);
  const meta = channelMeta(plugins, channel);
  const upgrade = availableUpgrade(plugins, channel);
  const keyMissing = isKeyMissing(plugins, channel);
  const rate = channel.rate_limit;
  const running = load?.running ?? 0;
  const waiting = load?.waiting ?? 0;
  const runningLimit = rate?.max_running ?? 0;
  const full = runningLimit > 0 && running >= runningLimit;
  const settingSpecs = meta?.channelSettings ? Object.entries(meta.channelSettings) : [];
  const unlimited = <span className="text-muted-foreground">不限</span>;

  return (
    <div className="flex flex-col gap-4">
      {check && <CheckResult state={check} />}
      <DescriptionList className="grid-cols-1 gap-y-5 sm:grid-cols-2 md:grid-cols-2 2xl:grid-cols-2">
        <DescriptionItem className="sm:col-span-2">
          <DescriptionTerm>地址</DescriptionTerm>
          <DescriptionDetails className="gap-1.5 font-normal">
            <code className="bg-muted text-foreground truncate rounded px-2 py-0.5 font-mono text-[13px]">
              {channel.base_url}
            </code>
            <CopyButton iconOnly text={channel.base_url} label="复制地址" />
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>插件</DescriptionTerm>
          <DescriptionDetails>
            <Link
              to={`/admin/ai/plugins?key=${encodeURIComponent(channel.plugin_key)}`}
              className="hover:underline hover:underline-offset-4"
            >
              {plugin?.name ?? channel.plugin_key}
            </Link>
            <span className="text-muted-foreground font-mono text-xs font-normal">
              v{channel.plugin_version}
            </span>
            {upgrade && canWrite && (
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground text-xs font-normal underline underline-offset-4"
                onClick={onUpgrade}
              >
                升级到 v{upgrade.version}
              </button>
            )}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>API Key</DescriptionTerm>
          <DescriptionDetails>
            {channel.secret_set ? (
              <>
                <ShieldCheck className="size-4 text-emerald-600 dark:text-emerald-400" />
                已设置
              </>
            ) : keyMissing ? (
              <>
                <span className={WARN_TEXT}>未设置</span>
                {canWrite && (
                  <>
                    <span className="text-muted-foreground font-normal">·</span>
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 text-sm font-normal underline underline-offset-4"
                      onClick={onSetKey}
                    >
                      <KeyRound className="size-3.5" />
                      设置
                    </button>
                  </>
                )}
              </>
            ) : (
              <span className="text-muted-foreground font-normal">无需</span>
            )}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>限流</DescriptionTerm>
          <DescriptionDetails className="font-normal tabular-nums">
            RPS {rate?.rps || unlimited} · 同时请求 {rate?.max_concurrency || unlimited} · 同时生成{" "}
            {rate?.max_running || unlimited}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>当前负载</DescriptionTerm>
          <DescriptionDetails className="font-normal tabular-nums">
            <span className={cn(full && WARN_TEXT)}>
              生成中 {running}
              {runningLimit > 0 && `/${runningLimit}`}
            </span>
            <span className="text-muted-foreground">·</span>
            <span className={cn(waiting > 0 && WARN_TEXT)}>排队 {waiting}</span>
            {full && <Tag tone="warning">已满</Tag>}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>安全开关</DescriptionTerm>
          <DescriptionDetails className="font-normal">
            {channel.trusted_internal && (
              <Tag tone="warning" title="trusted_internal：允许 base_url 解析到内网地址">
                <Network />
                内网
              </Tag>
            )}
            {channel.allow_credentials && (
              <Tag tone="warning" title="allow_credentials：插件代码能读取这个渠道的 Key">
                <KeyRound />
                凭证
              </Tag>
            )}
            {!channel.trusted_internal && !channel.allow_credentials && (
              <span className="text-muted-foreground">均关闭</span>
            )}
          </DescriptionDetails>
        </DescriptionItem>
        <DescriptionItem>
          <DescriptionTerm>最近修改</DescriptionTerm>
          <DescriptionDetails className="font-normal tabular-nums">
            {formatShortTime(channel.updated_at)}
          </DescriptionDetails>
        </DescriptionItem>
        {settingSpecs.length > 0 && (
          <DescriptionItem className="sm:col-span-2">
            <DescriptionTerm>插件设置</DescriptionTerm>
            <DescriptionDetails className="gap-2 font-normal">
              {settingSpecs.map(([key, spec]) => (
                <span
                  key={key}
                  className="inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs"
                >
                  <span className="text-muted-foreground">{spec.label}</span>
                  <span className="font-mono">{String(channel.settings?.[key] ?? "—")}</span>
                </span>
              ))}
            </DescriptionDetails>
          </DescriptionItem>
        )}
      </DescriptionList>
    </div>
  );
}

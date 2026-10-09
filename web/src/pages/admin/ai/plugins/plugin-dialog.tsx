import { useState, type ReactNode } from "react";
import {
  Check,
  CircleArrowUp,
  Fingerprint,
  GitCommitHorizontal,
  Lock,
  Plus,
  RadioTower,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import { Link } from "react-router";

import { deletePluginVersion } from "@/api/admin/ai";
import type { ChannelView, PluginVersionView, PluginView } from "@/api/admin/ai/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { CopyButton } from "@/components/admin-ui/copy-button";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import { RowAction } from "@/components/admin-ui/row-action";
import { Tag } from "@/components/admin-ui/tag";
import {
  Timeline,
  TimelineContent,
  TimelineHeader,
  TimelineIndicator,
  TimelineItem,
} from "@/components/admin-ui/timeline";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useRetained } from "@/hooks/use-retained";
import { cn } from "@/lib/utils";
import type { PluginTab } from "@/utils/admin/channel-route";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import { describeAuth, latestVersion, shortSha, versionDeleteBlock } from "@/utils/admin/plugin";
import { formatShortTime } from "@/utils/time";

import { KIND_STYLE, KindIcons, PLUGIN_KIND_ORDER } from "../kind";
import { confirmUpgradeChannels, outdatedChannels } from "./upgrade-channels";

/** 上传人：内置插件（0）显示“内置”，其余显示用户 ID */
const uploaderLabel = (version: PluginVersionView) =>
  version.created_by === 0 ? "内置" : `用户 #${version.created_by}`;

/** 版本声明里支持的能力 */
const kindsOf = (version: PluginVersionView | undefined) =>
  Object.keys(version?.meta?.endpoints ?? {});

/**
 * 插件弹窗：头部（名称、来源、启停开关、删除）+ 两个页签。
 * 概览：支持的生成方式（四格）、插件信息；版本历史：时间线，每个版本的能力、在用渠道、删除。
 * 状态靠开关和表格表达，不再有“已停用”横幅；颜色只给能力图标，其余中性。
 * 点击“在用渠道”会跳到渠道页，本页随之卸载，弹窗跟着关闭。
 * @param plugin 打开的插件；关闭时为 null，弹窗按最后一次的内容播完退出动画
 * @param channels 全部渠道，用来列出每个版本的“在用渠道”
 * @param plugins 全部插件（批量升级时算设置项迁移）
 * @param canWrite 是否有运维权限；没有则开关只读、不显示删除
 * @param toggling 正在启停（开关禁用）
 * @param onToggle 开关；停用前的影响确认由页面的 usePluginActions 负责
 * @param onChanged 删除版本成功后刷新清单
 * @param onChannelsChanged 批量升级渠道后刷新渠道清单
 * @param onDelete 删除整个插件：页面先关弹窗再打开删除对话框
 */
export function PluginDialog({
  plugin,
  tab,
  onTabChange,
  plugins,
  channels,
  canWrite,
  toggling,
  onToggle,
  onChanged,
  onChannelsChanged,
  onDelete,
  onClose,
}: {
  plugin: PluginView | null;
  tab: PluginTab;
  onTabChange: (tab: PluginTab) => void;
  plugins: PluginView[];
  channels: ChannelView[];
  canWrite: boolean;
  toggling: boolean;
  onToggle: (plugin: PluginView, enabled: boolean) => void;
  onChanged: () => Promise<void>;
  onChannelsChanged: () => Promise<void>;
  onDelete: (plugin: PluginView) => void;
  onClose: () => void;
}) {
  // 关闭时 plugin 会被置空，内容按最后一次的对象留到退出动画播完
  const shown = useRetained(plugin);
  return (
    <Dialog open={!!plugin} onOpenChange={(next) => !next && onClose()}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[min(88svh,760px)] max-w-[calc(100%-1.5rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl"
      >
        {shown && (
          <PluginDialogBody
            key={shown.key}
            plugin={plugin ?? shown}
            tab={tab}
            onTabChange={onTabChange}
            plugins={plugins}
            channels={channels}
            canWrite={canWrite}
            toggling={toggling}
            onToggle={onToggle}
            onChanged={onChanged}
            onChannelsChanged={onChannelsChanged}
            onDelete={onDelete}
            onClose={onClose}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function PluginDialogBody({
  plugin,
  tab,
  onTabChange,
  plugins,
  channels,
  canWrite,
  toggling,
  onToggle,
  onChanged,
  onChannelsChanged,
  onDelete,
  onClose,
}: {
  plugin: PluginView;
  tab: PluginTab;
  onTabChange: (tab: PluginTab) => void;
  plugins: PluginView[];
  channels: ChannelView[];
  canWrite: boolean;
  toggling: boolean;
  onToggle: (plugin: PluginView, enabled: boolean) => void;
  onChanged: () => Promise<void>;
  onChannelsChanged: () => Promise<void>;
  onDelete: (plugin: PluginView) => void;
  onClose: () => void;
}) {
  const latest = latestVersion(plugin);
  const meta = latest?.meta;
  const outdated = outdatedChannels(plugin, channels);
  // 页签先在本地切换（立刻响应），再写回 URL
  const [current, setCurrent] = useState<PluginTab>(tab);
  const changeTab = (next: PluginTab) => {
    setCurrent(next);
    onTabChange(next);
  };

  // 409：有渠道 / 非终态任务引用或内置。全局 toast 已弹，原因由 confirm 留在对话框里
  const requestDeleteVersion = (version: PluginVersionView) =>
    void confirm({
      title: "删除插件版本？",
      destructive: true,
      confirmLabel: "删除",
      description: (
        <>
          将永久删除{" "}
          <b>
            {plugin.name} v{version.version}
          </b>
          ，不能撤销。若仍有非终态任务在使用这个版本，后端会拒绝删除。
        </>
      ),
      onConfirm: async () => {
        await deletePluginVersion(plugin.key, version.version);
        await onChanged();
      },
    });

  const endpoints = meta?.endpoints ?? {};
  const hasAsync = Object.values(endpoints).some((value) => value?.mode === "async");
  const poll = meta?.poll ?? {};
  const hosts = Array.isArray(meta?.allowedHosts) ? meta.allowedHosts : [];
  const info: Array<{ term: string; value: ReactNode }> = [
    { term: "插件标识", value: <span className="font-mono">{plugin.key}</span> },
    {
      term: "最新版本",
      value: <span className="font-mono">{latest ? `v${latest.version}` : "—"}</span>,
    },
    { term: "契约版本", value: meta?.apiVersion ? `v${meta.apiVersion}` : "—" },
    {
      term: "来源",
      value:
        plugin.source === "builtin"
          ? "内置（随系统发布）"
          : `上传${latest ? ` · ${uploaderLabel(latest)}` : ""}`,
    },
    { term: "鉴权方式", value: describeAuth(meta?.auth) },
    {
      term: "从渠道导入模型",
      value: meta?.import ? (
        <span className="inline-flex items-center gap-1">
          <Check className="size-3.5" />
          支持
        </span>
      ) : (
        <span className="text-muted-foreground">不支持</span>
      ),
    },
    {
      term: "异步任务查询",
      value: hasAsync ? (
        `提交 ${poll.firstDelay ?? 10} 秒后开始，每 ${poll.interval ?? 5} 秒一次，最长间隔 ${poll.maxInterval ?? 15} 秒`
      ) : (
        <span className="text-muted-foreground">全部同步返回，无需查询</span>
      ),
    },
    {
      term: "允许下载结果的域名",
      value: hosts.length ? (
        <span className="flex flex-wrap gap-1">
          {hosts.map((host) => (
            <Tag key={host} mono>
              {host}
            </Tag>
          ))}
        </span>
      ) : (
        <span className="text-muted-foreground">仅渠道的 Base URL</span>
      ),
    },
  ];

  return (
    <>
      <div className="flex items-start gap-3 px-4 pt-4 pb-3">
        <div className="min-w-0 flex-1">
          <DialogTitle className="flex flex-wrap items-center gap-2 text-lg font-semibold tracking-tight">
            {plugin.name}
            <Tag>{plugin.source === "builtin" ? "内置" : "已上传"}</Tag>
            <span className="text-muted-foreground font-mono text-xs font-normal">
              {plugin.key}
              {latest && ` · v${latest.version}`}
            </span>
          </DialogTitle>
          <DialogDescription className={cn("mt-1", !meta?.description && "sr-only")}>
            {meta?.description ?? "插件详情：概览与版本历史"}
          </DialogDescription>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <label className="flex items-center gap-2 text-xs">
            启用
            <Switch
              checked={plugin.enabled}
              disabled={!canWrite || toggling}
              title={canWrite ? undefined : "需要运维权限"}
              aria-label={canWrite ? `启用插件 ${plugin.name}` : "插件启用状态"}
              onCheckedChange={(checked) => onToggle(plugin, checked)}
            />
          </label>
          {canWrite && (
            <RowAction
              tone="danger"
              title="删除插件（含全部版本）"
              aria-label={`删除插件 ${plugin.name}`}
              onClick={() => onDelete(plugin)}
            >
              <Trash2 />
              删除
            </RowAction>
          )}
          <Button type="button" variant="ghost" size="icon-sm" aria-label="关闭" onClick={onClose}>
            <X />
          </Button>
        </div>
      </div>

      <Tabs
        value={current}
        onValueChange={(next) => changeTab(next as PluginTab)}
        className="min-h-0 flex-1 gap-0"
      >
        <div className="border-b px-4 pb-3">
          <TabsList>
            <TabsTrigger value="overview" className="px-3">
              概览
            </TabsTrigger>
            <TabsTrigger value="versions" className="px-3">
              版本历史
              <span className="text-muted-foreground text-xs tabular-nums">
                {plugin.versions.length}
              </span>
            </TabsTrigger>
          </TabsList>
        </div>

        <TabsContent value="overview" className="min-h-0 flex-1 overflow-y-auto p-4">
          <section>
            <h3 className="text-muted-foreground mb-3 text-xs font-medium">支持的生成方式</h3>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              {PLUGIN_KIND_ORDER.map((kind) => {
                const style = KIND_STYLE[kind];
                const mode = endpoints[kind]?.mode;
                return (
                  <div
                    key={kind}
                    className={cn(
                      "flex items-center gap-3 rounded-lg border p-3",
                      mode ? "bg-muted/40" : "text-muted-foreground/50 border-dashed",
                    )}
                  >
                    <span
                      className={cn(
                        "grid size-9 shrink-0 place-items-center rounded-md",
                        mode && cn(style.soft, style.text),
                      )}
                    >
                      <style.icon className="size-4" />
                    </span>
                    <div>
                      <div className={cn("text-sm font-medium", mode && "text-foreground")}>
                        {MODEL_KIND_LABEL[kind]}
                      </div>
                      <div className={cn("text-xs", mode && "text-muted-foreground")}>
                        {!mode
                          ? "不支持"
                          : mode === "async"
                            ? "异步 · 提交后查询"
                            : "同步 · 直接返回"}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </section>

          <section className="mt-6">
            <h3 className="text-muted-foreground mb-3 text-xs font-medium">插件信息</h3>
            <DescriptionList className="grid-cols-1 gap-0 overflow-hidden rounded-lg border sm:grid-cols-2 md:grid-cols-2 2xl:grid-cols-2">
              {info.map(({ term, value }) => (
                <DescriptionItem
                  key={term}
                  className="border-b px-4 py-3.5 sm:odd:border-r nth-last-[-n+2]:sm:border-b-0 max-sm:last:border-b-0"
                >
                  <DescriptionTerm>{term}</DescriptionTerm>
                  <DescriptionDetails className="font-normal">{value}</DescriptionDetails>
                </DescriptionItem>
              ))}
            </DescriptionList>
          </section>
        </TabsContent>

        <TabsContent value="versions" className="min-h-0 flex-1 overflow-y-auto p-4">
          {outdated.length > 0 && canWrite && (
            <div className="mb-4 flex justify-end">
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  void confirmUpgradeChannels({
                    plugin,
                    channels,
                    plugins,
                    onDone: onChannelsChanged,
                  })
                }
              >
                <CircleArrowUp />
                升级全部 {outdated.length} 个渠道到 v{latest?.version}
              </Button>
            </div>
          )}
          <Timeline>
            {plugin.versions.map((version, index) => {
              const isLatest = version.id === latest?.id;
              const block = versionDeleteBlock(plugin, version);
              const kinds = kindsOf(version);
              const previous = plugin.versions[index + 1];
              const added = previous
                ? kinds.filter((kind) => !kindsOf(previous).includes(kind))
                : [];
              const pinned = channels.filter(
                (channel) =>
                  channel.plugin_key === plugin.key && channel.plugin_version === version.version,
              );
              return (
                <TimelineItem key={version.id}>
                  <TimelineIndicator active={isLatest}>
                    {isLatest ? <Sparkles /> : <GitCommitHorizontal />}
                  </TimelineIndicator>
                  <TimelineContent>
                    <TimelineHeader>
                      <span className="font-mono text-base font-semibold">v{version.version}</span>
                      {isLatest && <Tag>最新</Tag>}
                      <span className="text-muted-foreground text-xs">
                        {version.channel_count > 0
                          ? `${version.channel_count} 个渠道在用`
                          : "无渠道使用"}
                      </span>
                      <span className="text-muted-foreground ml-auto text-xs tabular-nums">
                        {formatShortTime(version.created_at)} · {uploaderLabel(version)}
                      </span>
                    </TimelineHeader>
                    <div className="bg-muted/20 mt-2 rounded-lg border p-3">
                      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
                        <KindIcons kinds={kinds} />
                        {added.length > 0 && (
                          <span className="text-muted-foreground inline-flex items-center gap-1">
                            <Plus className="size-3" />
                            新增 {added.map((kind) => MODEL_KIND_LABEL[kind] ?? kind).join("、")}
                          </span>
                        )}
                        <span className="text-muted-foreground ml-auto inline-flex items-center gap-1">
                          <span
                            className="bg-muted inline-flex items-center gap-1 rounded px-1.5 py-0.5 font-mono text-[11px]"
                            title={version.sha256}
                          >
                            <Fingerprint className="size-3" />
                            {shortSha(version.sha256)}
                          </span>
                          <CopyButton iconOnly text={version.sha256} label="复制完整 sha256" />
                        </span>
                      </div>
                      {pinned.length > 0 && (
                        <div className="mt-2.5 flex flex-wrap items-center gap-1.5 border-t pt-2.5 text-xs">
                          <span className="text-muted-foreground">在用渠道</span>
                          {pinned.map((channel) => (
                            <Link
                              key={channel.key}
                              to={`/admin/ai/channels?key=${encodeURIComponent(channel.key)}`}
                              className="bg-background hover:bg-accent inline-flex items-center gap-1 rounded-md border px-2 py-0.5"
                            >
                              <RadioTower className="size-3" />
                              {channel.name}
                            </Link>
                          ))}
                          {!isLatest && latest && (
                            <span className="text-muted-foreground ml-auto inline-flex items-center gap-1">
                              <CircleArrowUp className="size-3.5" />
                              可升级到 v{latest.version}
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                    {canWrite && (
                      <div className="mt-1 flex items-center gap-2 text-xs">
                        {block ? (
                          <span className="text-muted-foreground inline-flex items-center gap-1">
                            <Lock className="size-3" />
                            {block}
                          </span>
                        ) : (
                          <RowAction
                            tone="danger"
                            aria-label={`删除 ${version.version}`}
                            onClick={() => requestDeleteVersion(version)}
                          >
                            <Trash2 />
                            删除这个版本
                          </RowAction>
                        )}
                      </div>
                    )}
                  </TimelineContent>
                </TimelineItem>
              );
            })}
          </Timeline>
        </TabsContent>
      </Tabs>
    </>
  );
}

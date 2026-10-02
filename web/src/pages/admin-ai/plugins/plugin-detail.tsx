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
} from "lucide-react";
import { Link } from "react-router";
import { toast } from "sonner";

import { deletePluginVersion, setPluginEnabled } from "@/api/admin-ai";
import type {
  ChannelView,
  ConfigListItem,
  PluginVersionView,
  PluginView,
} from "@/api/admin-ai/type";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { CopyButton } from "@/components/admin-ui/copy-button";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import { Notice } from "@/components/admin-ui/notice";
import { Tag, toneClasses } from "@/components/admin-ui/tag";
import {
  Timeline,
  TimelineContent,
  TimelineHeader,
  TimelineIndicator,
  TimelineItem,
} from "@/components/admin-ui/timeline";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import {
  describeAuth,
  latestVersion,
  pluginChannelCount,
  shortSha,
  versionDeleteBlock,
} from "@/utils/admin/plugin";
import { formatShortTime } from "@/utils/time";

import { KIND_ORDER, KIND_STYLE, KindIcons } from "../kind";
import { ReadOnlyNotice } from "../shared";
import { useAliveRef } from "../use-admin";
import { confirmUpgradeChannels, outdatedChannels } from "./upgrade-channels";

/** 上传人：内置插件（0）显示“内置”，其余显示用户 ID */
const uploaderLabel = (version: PluginVersionView) =>
  version.created_by === 0 ? "内置" : `用户 #${version.created_by}`;

/** 版本声明里支持的能力 */
const kindsOf = (version: PluginVersionView | undefined) =>
  Object.keys(version?.meta?.endpoints ?? {});

/**
 * 插件页右栏（设计稿样式）：头部卡片（启停 + 支持的生成方式）、插件信息、版本历史时间线。
 * @param channels 全部渠道，用来列出每个版本的“在用渠道”
 * @param models 全部模型，停用前算出会波及几个在线模型；没加载到时为空数组
 * @param canWrite 是否有运维权限；没有则启停开关只读、不显示删除
 * @param onChanged 启停或删除版本成功后刷新清单
 * @param onDelete 删除整个插件（打开删除对话框）
 * @param plugins 全部插件（批量升级时算设置项迁移）
 * @param onChannelsChanged 批量升级渠道后刷新渠道清单
 */
export function PluginDetail({
  plugin,
  plugins,
  channels,
  models,
  canWrite,
  onChanged,
  onChannelsChanged,
  onDelete,
}: {
  plugin: PluginView;
  plugins: PluginView[];
  channels: ChannelView[];
  models: ConfigListItem[];
  canWrite: boolean;
  onChanged: () => Promise<void>;
  onChannelsChanged: () => Promise<void>;
  onDelete: () => void;
}) {
  const outdated = outdatedChannels(plugin, channels);
  const aliveRef = useAliveRef();
  const latest = latestVersion(plugin);
  const meta = latest?.meta;
  const [toggling, setToggling] = useState(false);

  const setEnabled = async (enabled: boolean) => {
    setToggling(true);
    try {
      await setPluginEnabled(plugin.key, enabled);
      toast.success(`插件「${plugin.name}」已${enabled ? "启用" : "停用"}`);
      await onChanged();
    } finally {
      if (aliveRef.current) setToggling(false);
    }
  };

  /** 启用直接生效；停用影响面大（所有用它的渠道、渠道下的模型），先算影响再确认 */
  const toggle = (enabled: boolean) => {
    if (enabled) return void setEnabled(true);
    const pinned = channels.filter((channel) => channel.plugin_key === plugin.key);
    const online = models.filter(
      (item) => item.enabled && pinned.some((channel) => channel.key === item.channel),
    );
    void confirm({
      title: `停用插件「${plugin.name}」？`,
      destructive: true,
      confirmLabel: "停用",
      description: (
        <>
          停用后{" "}
          <b>
            {pinned.length} 个渠道{online.length > 0 && `、${online.length} 个在线模型`}
          </b>{" "}
          会不可用，进行中的任务不受影响。
        </>
      ),
      onConfirm: () => setEnabled(false),
    });
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
        <span className="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
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
    <div className="flex flex-col gap-4">
      {!canWrite && <ReadOnlyNotice what="上传、启停插件与删除版本" />}

      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-2">
            <span className="text-xl font-semibold tracking-tight">{plugin.name}</span>
            <Tag tone={plugin.source === "builtin" ? "neutral" : "violet"}>
              {plugin.source === "builtin" ? "内置" : "已上传"}
            </Tag>
            <span className="text-muted-foreground font-mono text-xs font-normal">
              {plugin.key}
              {latest && ` · v${latest.version}`}
            </span>
          </CardTitle>
          {meta?.description && <CardDescription>{meta.description}</CardDescription>}
          <CardAction className="flex items-center gap-3 rounded-lg border px-3 py-2">
            <div className="text-right">
              <div className="text-sm font-medium">{plugin.enabled ? "已启用" : "已停用"}</div>
              <div className="text-muted-foreground text-xs">
                {pluginChannelCount(plugin)} 个渠道在用
              </div>
            </div>
            <Switch
              checked={plugin.enabled}
              disabled={!canWrite || toggling}
              aria-label={canWrite ? `启用插件 ${plugin.name}` : "插件启用状态"}
              onCheckedChange={(checked) => toggle(checked)}
            />
            {canWrite && (
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label={`删除插件 ${plugin.name}`}
                title="删除插件（含全部版本）"
                className="text-muted-foreground hover:text-destructive"
                onClick={onDelete}
              >
                <Trash2 />
              </Button>
            )}
          </CardAction>
        </CardHeader>
        {!plugin.enabled && (
          <CardContent>
            <Notice tone="warning">
              已停用：所有使用它的渠道不再接新任务，进行中的任务不受影响。
            </Notice>
          </CardContent>
        )}
        <CardContent className="border-t pt-4">
          <div className="text-muted-foreground mb-3 text-xs font-medium">支持的生成方式</div>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {KIND_ORDER.map((kind) => {
              const style = KIND_STYLE[kind];
              const mode = endpoints[kind]?.mode;
              return (
                <div
                  key={kind}
                  className={cn(
                    "flex items-center gap-3 rounded-lg border p-3",
                    mode ? toneClasses[style.tone] : "border-dashed opacity-40",
                  )}
                >
                  <span
                    className={cn(
                      "grid size-9 shrink-0 place-items-center rounded-md",
                      mode ? "bg-background/50" : "bg-muted",
                    )}
                  >
                    <style.icon className="size-4" />
                  </span>
                  <div>
                    <div className="text-sm font-medium">{MODEL_KIND_LABEL[kind]}</div>
                    <div className="text-xs opacity-80">
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
        </CardContent>
      </Card>

      <Card className="gap-0 pb-0">
        <CardHeader className="pb-3">
          <CardTitle>插件信息</CardTitle>
          <CardDescription className="text-xs">读取自最新版本的声明</CardDescription>
        </CardHeader>
        <DescriptionList className="gap-0 border-t sm:grid-cols-2 md:grid-cols-2 2xl:grid-cols-4">
          {info.map(({ term, value }) => (
            <DescriptionItem
              key={term}
              className="border-b px-4 py-3.5 sm:odd:border-r 2xl:border-r 2xl:[&:nth-child(4n)]:border-r-0"
            >
              <DescriptionTerm>{term}</DescriptionTerm>
              <DescriptionDetails className="font-normal">{value}</DescriptionDetails>
            </DescriptionItem>
          ))}
        </DescriptionList>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>版本历史</CardTitle>
          <CardAction className="text-muted-foreground flex items-center gap-2 self-center text-xs">
            {outdated.length > 0 && canWrite ? (
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
            ) : (
              "渠道固定在某个版本上，新版本要切换后才生效"
            )}
          </CardAction>
        </CardHeader>
        <CardContent>
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
                      {isLatest && <Tag tone="info">最新</Tag>}
                      {version.channel_count > 0 ? (
                        <Tag tone="success">{version.channel_count} 个渠道在用</Tag>
                      ) : (
                        <Tag>无渠道使用</Tag>
                      )}
                      <span className="text-muted-foreground ml-auto text-xs tabular-nums">
                        {formatShortTime(version.created_at)} · {uploaderLabel(version)}
                      </span>
                    </TimelineHeader>
                    <div className="bg-muted/20 mt-2 rounded-lg border p-3">
                      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
                        <KindIcons kinds={kinds} />
                        {added.length > 0 && (
                          <span className="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
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
                              to={`/admin/ai/channels?edit=${encodeURIComponent(channel.key)}`}
                              className="bg-background hover:bg-accent inline-flex items-center gap-1 rounded-md border px-2 py-0.5"
                            >
                              <RadioTower className="size-3" />
                              {channel.name}
                            </Link>
                          ))}
                          {!isLatest && latest && (
                            <span className="ml-auto inline-flex items-center gap-1 text-sky-600 dark:text-sky-400">
                              <CircleArrowUp className="size-3.5" />
                              可升级到 v{latest.version}
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                    {canWrite && (
                      <div className="mt-2 flex items-center gap-2 text-xs">
                        {block ? (
                          <span className="text-muted-foreground inline-flex items-center gap-1">
                            <Lock className="size-3" />
                            {block}
                          </span>
                        ) : (
                          <button
                            type="button"
                            className="inline-flex items-center gap-1 text-red-600 hover:underline dark:text-red-400"
                            aria-label={`删除 ${version.version}`}
                            onClick={() => requestDeleteVersion(version)}
                          >
                            <Trash2 className="size-3" />
                            删除这个版本
                          </button>
                        )}
                      </div>
                    )}
                  </TimelineContent>
                </TimelineItem>
              );
            })}
          </Timeline>
        </CardContent>
      </Card>
    </div>
  );
}

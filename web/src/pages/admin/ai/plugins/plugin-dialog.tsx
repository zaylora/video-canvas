import { useState, type ReactNode } from "react";
import {
  Check,
  CircleArrowUp,
  Fingerprint,
  GitCommitHorizontal,
  History,
  Info,
  Lock,
  Plus,
  RadioTower,
  Sparkles,
  Trash2,
} from "lucide-react";
import { Link } from "react-router";

import { deletePluginVersion } from "@/api/admin/ai";
import type { ChannelView, PluginVersionView, PluginView } from "@/api/admin/ai/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { CopyButton } from "@/components/admin-ui/copy-button";
import {
  DetailDialog,
  DetailDialogBody,
  DetailDialogHeader,
} from "@/components/admin-ui/detail-dialog";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import { RowAction } from "@/components/admin-ui/row-action";
import {
  FormSection,
  FormSectionDescription,
  FormSectionHeader,
  FormSectionTitle,
} from "@/components/admin-ui/form-section";
import { InitialAvatar } from "@/components/admin-ui/initial-avatar";
import { Tag } from "@/components/admin-ui/tag";
import { UnderlineTab, UnderlineTabs } from "@/components/admin-ui/underline-tabs";
import {
  Timeline,
  TimelineContent,
  TimelineHeader,
  TimelineIndicator,
  TimelineItem,
} from "@/components/admin-ui/timeline";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useRetained } from "@/hooks/use-retained";
import { cn } from "@/lib/utils";
import type { PluginTab } from "@/utils/admin/channel-route";
import { dialogEntry } from "@/utils/admin/dialog-entry";
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
 * 插件弹窗：头部（名称、来源、标识、版本）+ 两个页签。
 * 点击那一刻就打开：头部用列表里已有的 `summary`，内容区在详情接口返回前显示骨架，返回后换成 `detail`。
 * 概览：支持的生成方式（四格）、插件信息；版本历史：时间线，每个版本的能力、在用渠道、删除。
 * 状态靠开关和表格表达，不再有“已停用”横幅；颜色只给能力图标，其余中性。
 * 点击“在用渠道”会跳到渠道页，本页随之卸载，弹窗跟着关闭。
 * @param open 弹窗是否打开
 * @param pluginKey 要看的插件 key；关闭时为 null
 * @param summary 列表里已有的这一行，用来先画头部；列表里找不到（深链、还没加载完）时为 null
 * @param detail 详情接口返回的插件；还没返回时为 null
 * @param channels 全部渠道，用来列出每个版本的“在用渠道”
 * @param plugins 全部插件（批量升级时算设置项迁移）
 * @param canWrite 是否有运维权限；没有则不显示版本删除、升级渠道
 * @param onChanged 删除版本成功后刷新清单
 * @param onChannelsChanged 批量升级渠道后刷新渠道清单
 */
export function PluginDialog({
  open,
  pluginKey,
  summary,
  detail,
  tab,
  plugins,
  channels,
  canWrite,
  onChanged,
  onChannelsChanged,
  onClose,
}: {
  open: boolean;
  pluginKey: string | null;
  summary: PluginView | null;
  detail: PluginView | null;
  tab: PluginTab;
  plugins: PluginView[];
  channels: ChannelView[];
  canWrite: boolean;
  onChanged: () => Promise<void>;
  onChannelsChanged: () => Promise<void>;
  onClose: () => void;
}) {
  // 关闭后内容留到退出动画播完；打开着时只认当前的值，没有就是还在加载
  const keptKey = useRetained(pluginKey);
  const keptSummary = useRetained(summary);
  const keptDetail = useRetained(detail);
  return (
    <DetailDialog open={open} onClose={onClose}>
      <PluginDialogBody
        key={keptKey ?? ""}
        summary={open ? summary : keptSummary}
        detail={open ? detail : keptDetail}
        tab={tab}
        plugins={plugins}
        channels={channels}
        canWrite={canWrite}
        onChanged={onChanged}
        onChannelsChanged={onChannelsChanged}
        onClose={onClose}
      />
    </DetailDialog>
  );
}

/** 内容区骨架：版式照“概览”页签画（说明、四格、信息栅格），换上内容时不跳 */
function PluginBodySkeleton() {
  return (
    <div className="flex flex-col gap-6">
      <Skeleton className="h-4 w-2/3" />
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {PLUGIN_KIND_ORDER.map((kind) => (
          <Skeleton key={kind} className="h-[66px] rounded-lg" />
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton key={index} className="h-10" />
        ))}
      </div>
    </div>
  );
}

function PluginDialogBody({
  summary,
  detail,
  tab,
  plugins,
  channels,
  canWrite,
  onChanged,
  onChannelsChanged,
  onClose,
}: {
  summary: PluginView | null;
  detail: PluginView | null;
  tab: PluginTab;
  plugins: PluginView[];
  channels: ChannelView[];
  canWrite: boolean;
  onChanged: () => Promise<void>;
  onChannelsChanged: () => Promise<void>;
  onClose: () => void;
}) {
  const [current, changeTab] = useState<PluginTab>(tab);
  // 头部和页签角标：详情回来前用列表里的，回来后用最新的
  const head = detail ?? summary;
  const headLatest = head ? latestVersion(head) : undefined;
  return (
    <>
      <DetailDialogHeader
        fallbackTitle="插件详情"
        title={head?.name ?? null}
        avatar={
          head && <InitialAvatar name={head.name} seed={head.key} className="size-10 text-base" />
        }
        badges={head && <Tag>{head.source === "builtin" ? "内置" : "已上传"}</Tag>}
        description={
          head && (
            <>
              <span className="font-mono">{head.key}</span>
              {headLatest && ` · v${headLatest.version}`}
            </>
          )
        }
        onClose={onClose}
      />

      <UnderlineTabs>
        <UnderlineTab selected={current === "overview"} onClick={() => changeTab("overview")}>
          <Info />
          概览
        </UnderlineTab>
        <UnderlineTab selected={current === "versions"} onClick={() => changeTab("versions")}>
          <History />
          版本历史
          {head && (
            <span className="text-muted-foreground text-xs tabular-nums">
              {head.versions.length}
            </span>
          )}
        </UnderlineTab>
      </UnderlineTabs>

      <DetailDialogBody loading={!detail} skeleton={<PluginBodySkeleton />}>
        {detail && (
          <PluginDetailContent
            plugin={detail}
            current={current}
            plugins={plugins}
            channels={channels}
            canWrite={canWrite}
            onChanged={onChanged}
            onChannelsChanged={onChannelsChanged}
          />
        )}
      </DetailDialogBody>
    </>
  );
}

/** 详情回来后的两个页签内容 */
function PluginDetailContent({
  plugin,
  current,
  plugins,
  channels,
  canWrite,
  onChanged,
  onChannelsChanged,
}: {
  plugin: PluginView;
  current: PluginTab;
  plugins: PluginView[];
  channels: ChannelView[];
  canWrite: boolean;
  onChanged: () => Promise<void>;
  onChannelsChanged: () => Promise<void>;
}) {
  const latest = latestVersion(plugin);
  const meta = latest?.meta;
  const outdated = outdatedChannels(plugin, channels);

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
  const info: Array<{ term: string; value: ReactNode }> = [
    {
      term: "插件标识",
      value: <span className="font-mono">{plugin.key}</span>,
    },
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
  ];

  return (
    <>
      {current === "overview" && (
        <>
          {meta?.description && (
            <p className="text-muted-foreground mb-5 text-sm">{meta.description}</p>
          )}
          <FormSection>
            <FormSectionHeader>
              <FormSectionTitle>生成方式</FormSectionTitle>
              <FormSectionDescription>插件声明的能力与调用方式</FormSectionDescription>
            </FormSectionHeader>
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
          </FormSection>

          <FormSection>
            <FormSectionHeader>
              <FormSectionTitle>插件信息</FormSectionTitle>
              <FormSectionDescription>标识、鉴权与下载限制</FormSectionDescription>
            </FormSectionHeader>
            <DescriptionList className="grid-cols-1 sm:grid-cols-2 md:grid-cols-2 2xl:grid-cols-2">
              {info.map(({ term, value }) => (
                <DescriptionItem key={term}>
                  <DescriptionTerm>{term}</DescriptionTerm>
                  <DescriptionDetails className="font-normal">{value}</DescriptionDetails>
                </DescriptionItem>
              ))}
            </DescriptionList>
          </FormSection>
        </>
      )}

      {current === "versions" && (
        <>
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
                              to="/admin/ai/channels"
                              state={dialogEntry({ kind: "edit", key: channel.key })}
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
        </>
      )}
    </>
  );
}

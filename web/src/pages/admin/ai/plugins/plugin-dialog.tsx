import { useEffect, useState, type ReactNode } from "react";
import {
  CircleArrowUp,
  Fingerprint,
  GitCommitHorizontal,
  History,
  Info,
  Lock,
  Plus,
  Sparkles,
  Trash2,
  Boxes,
} from "lucide-react";
import { Link } from "react-router";

import { deletePluginVersion, listModelOptions } from "@/api/admin/ai";
import type { ConfigListItem, PluginVersionView, PluginView } from "@/api/admin/ai/type.d";
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
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
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
import { channelsPagePath } from "@/utils/admin/channel-route";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import {
  describeAuth,
  endpointLabel,
  latestVersion,
  outdatedModelCount,
  settingRows,
  shortSha,
  vendorOf,
  versionDeleteBlock,
} from "@/utils/admin/plugin";
import { formatShortTime } from "@/utils/time";

import { KIND_STYLE, KindIcons, PLUGIN_KIND_ORDER } from "../kind";
import { SUPER_ONLY } from "./plugin-table";
import { confirmUpgradeModels } from "./upgrade-models";

/** 插件弹窗的页签 */
export type PluginTab = "overview" | "versions";

/** 一个版本下最多列出几个固定在它上面的模型，其余写“等 N 个” */
const MAX_PINNED_CHIPS = 8;

/** 上传人：内置插件（0）显示“内置”，其余显示用户 ID */
const uploaderLabel = (version: PluginVersionView) =>
  version.created_by === 0 ? "内置" : `用户 #${version.created_by}`;

/** 版本声明里支持的能力 */
const kindsOf = (version: PluginVersionView | undefined) =>
  Object.keys(version?.meta?.endpoints ?? {});

/** 模型在清单里的展示名 */
const modelName = (model: ConfigListItem) => model.label || model.name || model.key;

/**
 * 插件弹窗：头部（名称、来源、标识、版本）+ 两个页签。
 * 点击那一刻就打开：头部用列表里已有的 `summary`，内容区在详情接口返回前显示骨架，返回后换成 `detail`。
 * 概览：各能力的同步 / 异步与请求路径、厂商、鉴权方式、协议设置项声明；
 * 版本历史：时间线，每个版本的能力、被多少个模型固定、删除（被固定的版本不能删）。
 * 状态靠开关和表格表达，不再有“已停用”横幅；颜色只给能力图标，其余中性。
 * 点击固定在某个版本上的模型会跳到渠道页的该模型，本页随之卸载，弹窗跟着关闭。
 * @param open 弹窗是否打开
 * @param pluginKey 要看的插件 key；关闭时为 null
 * @param summary 列表里已有的这一行，用来先画头部；列表里找不到（深链、还没加载完）时为 null
 * @param detail 详情接口返回的插件；还没返回时为 null
 * @param canWrite 是否是 super_admin；不是则“升级模型”“删除版本”置灰并写明原因
 * @param onChanged 删除版本、升级模型成功后刷新清单
 */
export function PluginDialog({
  open,
  pluginKey,
  summary,
  detail,
  tab,
  canWrite,
  onChanged,
  onClose,
}: {
  open: boolean;
  pluginKey: string | null;
  summary: PluginView | null;
  detail: PluginView | null;
  tab: PluginTab;
  canWrite: boolean;
  onChanged: () => Promise<void>;
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
        canWrite={canWrite}
        onChanged={onChanged}
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
  canWrite,
  onChanged,
  onClose,
}: {
  summary: PluginView | null;
  detail: PluginView | null;
  tab: PluginTab;
  canWrite: boolean;
  onChanged: () => Promise<void>;
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
            canWrite={canWrite}
            onChanged={onChanged}
          />
        )}
      </DetailDialogBody>
    </>
  );
}

/**
 * 全部模型的列表项，用来在版本历史里列出“固定在这个版本上的模型”。
 * 只在版本历史页签打开后才请求；取不到时不列模型（数量仍由版本自带的 model_count 给出）。
 */
function usePinnedModels(enabled: boolean) {
  const [models, setModels] = useState<ConfigListItem[]>([]);
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    listModelOptions()
      .then((list) => !cancelled && setModels(list))
      .catch(() => {
        // 全局 toast 已弹；没有清单就只显示数量
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);
  return models;
}

/** 详情回来后的两个页签内容 */
function PluginDetailContent({
  plugin,
  current,
  canWrite,
  onChanged,
}: {
  plugin: PluginView;
  current: PluginTab;
  canWrite: boolean;
  onChanged: () => Promise<void>;
}) {
  const latest = latestVersion(plugin);
  const meta = latest?.meta;
  const outdated = outdatedModelCount(plugin);
  const models = usePinnedModels(current === "versions");

  // 409：有模型 / 非终态任务引用或内置。全局 toast 已弹，原因由 confirm 留在对话框里
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
  const settings = settingRows(meta);
  const vendor = vendorOf(meta);
  const info: Array<{ term: string; value: ReactNode }> = [
    {
      term: "厂商",
      value: vendor ? vendor : <span className="text-muted-foreground">—</span>,
    },
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
              <FormSectionTitle>能力与请求</FormSectionTitle>
              <FormSectionDescription>
                插件声明的能力、调用方式与请求路径（路径只用于展示）
              </FormSectionDescription>
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
                    <div className="min-w-0">
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
                      {mode && (
                        <div
                          className="text-muted-foreground mt-0.5 truncate font-mono text-[11px]"
                          title={endpointLabel(meta, kind)}
                        >
                          {endpointLabel(meta, kind)}
                        </div>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </FormSection>

          <FormSection>
            <FormSectionHeader>
              <FormSectionTitle>插件信息</FormSectionTitle>
              <FormSectionDescription>厂商、标识与鉴权</FormSectionDescription>
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

          <FormSection>
            <FormSectionHeader>
              <FormSectionTitle>协议设置项</FormSectionTitle>
              <FormSectionDescription>
                在模型的“协议设置”里填，值存在模型配置里
              </FormSectionDescription>
            </FormSectionHeader>
            {settings.length === 0 ? (
              <p className="text-muted-foreground text-sm">这个协议没有声明设置项</p>
            ) : (
              <ul className="flex flex-col gap-2">
                {settings.map((row) => (
                  <li key={row.name} className="bg-muted/20 rounded-lg border p-3 text-sm">
                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                      <span className="font-medium">{row.label}</span>
                      <code className="text-muted-foreground font-mono text-xs">{row.name}</code>
                      <Tag>{row.typeLabel}</Tag>
                      {row.required && <Tag tone="warning">必填</Tag>}
                    </div>
                    {row.description && (
                      <p className="text-muted-foreground mt-1 text-xs">{row.description}</p>
                    )}
                    {(row.defaultText || row.options.length > 0) && (
                      <p className="text-muted-foreground mt-1 text-xs">
                        {row.options.length > 0 && <>可选：{row.options.join(" / ")}</>}
                        {row.options.length > 0 && row.defaultText && " · "}
                        {row.defaultText && <>默认：{row.defaultText}</>}
                      </p>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </FormSection>
        </>
      )}

      {current === "versions" && (
        <>
          {outdated > 0 && (
            <div className="mb-4 flex justify-end">
              <ReasonTooltip reason={canWrite ? null : SUPER_ONLY}>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!canWrite}
                  onClick={() => void confirmUpgradeModels({ plugin, onDone: onChanged })}
                >
                  <CircleArrowUp />
                  升级全部 {outdated} 个模型到 v{latest?.version}
                </Button>
              </ReasonTooltip>
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
              const pinned = models.filter(
                (model) => model.plugin === plugin.key && model.plugin_version === version.version,
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
                        {version.model_count > 0
                          ? `${version.model_count} 个模型固定在此版本`
                          : "无模型使用"}
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
                      {version.model_count > 0 && (
                        <div className="mt-2.5 flex flex-wrap items-center gap-1.5 border-t pt-2.5 text-xs">
                          <span className="text-muted-foreground">固定的模型</span>
                          {pinned.slice(0, MAX_PINNED_CHIPS).map((model) => (
                            <Link
                              key={model.key}
                              to={channelsPagePath({
                                channel: model.channel || undefined,
                                model: model.key,
                              })}
                              className="bg-background hover:bg-accent inline-flex items-center gap-1 rounded-md border px-2 py-0.5"
                            >
                              <Boxes className="size-3" />
                              {modelName(model)}
                            </Link>
                          ))}
                          {pinned.length > MAX_PINNED_CHIPS && (
                            <span className="text-muted-foreground">
                              等 {version.model_count} 个
                            </span>
                          )}
                          {!isLatest && latest && (
                            <span className="text-muted-foreground ml-auto inline-flex items-center gap-1">
                              <CircleArrowUp className="size-3.5" />
                              可升级到 v{latest.version}
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                    <div className="mt-1 flex items-center gap-2 text-xs">
                      {block ? (
                        <span className="text-muted-foreground inline-flex items-center gap-1">
                          <Lock className="size-3" />
                          {block}
                        </span>
                      ) : (
                        <ReasonTooltip reason={canWrite ? null : SUPER_ONLY}>
                          <RowAction
                            tone="danger"
                            disabled={!canWrite}
                            aria-label={`删除 ${version.version}`}
                            onClick={() => requestDeleteVersion(version)}
                          >
                            <Trash2 />
                            删除这个版本
                          </RowAction>
                        </ReasonTooltip>
                      )}
                    </div>
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

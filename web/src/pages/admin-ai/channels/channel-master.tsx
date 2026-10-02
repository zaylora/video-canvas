import { useState } from "react";
import {
  CircleArrowUp,
  Download,
  Ellipsis,
  Eye,
  KeyRound,
  Network,
  Pause,
  Pencil,
  Play,
  Plus,
  ShieldAlert,
  ShieldCheck,
  Stethoscope,
  Trash2,
} from "lucide-react";
import { Link } from "react-router";

import type {
  ChannelLoad,
  ChannelView,
  ConfigListItem,
  ConfigRevision,
  PluginView,
} from "@/api/admin-ai/type";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";
import { CopyButton } from "@/components/admin-ui/copy-button";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateDescription,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import {
  ListPanel,
  ListPanelContent,
  ListPanelCount,
  ListPanelEmpty,
  ListPanelHeader,
  ListPanelItem,
  ListPanelItemRow,
  ListPanelTitle,
} from "@/components/admin-ui/list-panel";
import { Notice } from "@/components/admin-ui/notice";
import { SearchInput } from "@/components/admin-ui/search-input";
import { StatusLabel } from "@/components/admin-ui/status-dot";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { channelHealth, HEALTH_UI_TONE } from "@/utils/admin/health";
import { availableUpgrade, channelMeta, channelSupportsKind } from "@/utils/admin/plugin";
import { formatShortTime } from "@/utils/time";

import { KIND_ORDER } from "../kind";
import { ModelRows } from "../models/model-rows";
import type { LoadStatus } from "../use-admin";
import { CheckResult } from "./check-result";
import type { CheckState } from "./use-channel-check";

/** 这个渠道是否缺 Key：插件要求鉴权（或读不到插件 meta）且没设置 */
const isKeyMissing = (plugins: PluginView[], channel: ChannelView) => {
  const meta = channelMeta(plugins, channel);
  return !channel.secret_set && ((meta?.auth?.type ?? "none") !== "none" || !meta);
};

/**
 * 渠道页主体：左侧渠道列表（搜索、新建），右侧选中渠道的详情卡片与操作。
 * 列表与详情显示的是渠道的**实际**可用性：插件停用、缺 Key 时即使渠道“启用”也标成不可用。
 * 选中项由页面放在 URL 里（selectedKey / onSelect）。
 * 写操作（检查、新建、停用、删除）只对运维渲染；导入 admin 也能用。
 */
export function ChannelMaster({
  channels,
  plugins,
  status,
  canWrite,
  checks,
  loads,
  selectedKey,
  onSelect,
  onNew,
  onEdit,
  onCheck,
  onImport,
  onRetry,
  models,
  modelsStatus,
  busyModelKey,
  onSetKey,
  onToggleChannel,
  onDeleteChannel,
  onEditModel,
  onTestModel,
  onNewModel,
  onToggleModel,
  onRollbackModel,
  onDeleteModel,
}: {
  channels: ChannelView[];
  plugins: PluginView[];
  status: LoadStatus;
  canWrite: boolean;
  checks: Record<string, CheckState>;
  /** 各渠道当前的生成中 / 排队数，按渠道 key 索引 */
  loads: Record<string, ChannelLoad>;
  /** 选中的渠道；为空或找不到时选第一个 */
  selectedKey: string | null;
  onSelect: (key: string) => void;
  onNew: () => void;
  onEdit: (key: string) => void;
  onCheck: (key: string) => void;
  onImport: (channel: ChannelView) => void;
  onRetry: () => void;
  /** 全部模型（列表项的模型数、“使用这个渠道的模型”用） */
  models: ConfigListItem[];
  modelsStatus: LoadStatus;
  busyModelKey: string | null;
  onSetKey: (channel: ChannelView) => void;
  onToggleChannel: (channel: ChannelView) => void;
  onDeleteChannel: (channel: ChannelView) => void;
  onEditModel: (key: string) => void;
  onTestModel: (key: string) => void;
  onNewModel: (channelKey: string) => void;
  onToggleModel: (key: string, enabled: boolean) => void;
  onRollbackModel: (key: string, revision: ConfigRevision) => void;
  onDeleteModel: (item: ConfigListItem) => void;
}) {
  const [query, setQuery] = useState("");
  const [modelKind, setModelKind] = useState("");

  if (status === "loading") {
    return (
      <div className="grid gap-4 xl:grid-cols-[17rem_minmax(0,1fr)]" aria-busy="true">
        <Skeleton className="h-96" />
        <Skeleton className="h-96" />
      </div>
    );
  }
  if (status === "error") {
    return (
      <EmptyState>
        <EmptyStateTitle className="text-destructive">加载失败</EmptyStateTitle>
        <EmptyStateActions>
          <Button variant="outline" size="sm" onClick={onRetry}>
            重试
          </Button>
        </EmptyStateActions>
      </EmptyState>
    );
  }
  if (channels.length === 0) {
    return (
      <EmptyState>
        <EmptyStateTitle>还没有渠道</EmptyStateTitle>
        <EmptyStateDescription>
          渠道把一个插件版本、一个地址和一个 Key 绑在一起，模型通过渠道调用上游。
          {!canWrite && "请联系运维新建。"}
        </EmptyStateDescription>
        {canWrite && (
          <EmptyStateActions>
            <Button onClick={onNew}>
              <Plus />
              新建渠道
            </Button>
          </EmptyStateActions>
        )}
      </EmptyState>
    );
  }

  const q = query.trim().toLowerCase();
  const list = channels.filter(
    (item) => !q || [item.name, item.key, item.base_url].some((s) => s.toLowerCase().includes(q)),
  );
  const selected = channels.find((item) => item.key === selectedKey) ?? channels[0];
  const plugin = plugins.find((item) => item.key === selected.plugin_key);
  const meta = channelMeta(plugins, selected);
  const upgrade = availableUpgrade(plugins, selected);
  const importUnsupported = !!meta && !meta.import;
  const keyMissing = isKeyMissing(plugins, selected);
  const health = channelHealth(selected, plugins);
  const pluginOff = !!plugin && !plugin.enabled;
  const rl = selected.rate_limit;
  const load = loads[selected.key];
  const running = load?.running ?? 0;
  const waiting = load?.waiting ?? 0;
  const runningLimit = rl?.max_running ?? 0;
  // 满了：配了上限，且正在生成的已经顶到上限
  const full = runningLimit > 0 && running >= runningLimit;
  const settingSpecs = meta?.channelSettings ? Object.entries(meta.channelSettings) : [];
  const check = checks[selected.key];
  const usedModels = models.filter((item) => item.channel === selected.key);
  const shownModels = usedModels.filter((item) => !modelKind || item.kind === modelKind);
  const kinds = KIND_ORDER.filter((kind) => channelSupportsKind(plugins, selected, kind) !== false);
  const modelCount = (key: string) => models.filter((item) => item.channel === key).length;
  const fixLink = (label: string) =>
    canWrite ? (
      <button
        type="button"
        className="text-xs font-medium underline underline-offset-4"
        onClick={() => onEdit(selected.key)}
      >
        {label}
      </button>
    ) : undefined;

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[17rem_minmax(0,1fr)]">
      <ListPanel className="xl:sticky xl:top-0 xl:max-h-[calc(100svh-7rem)]">
        <ListPanelHeader>
          <ListPanelTitle>
            全部渠道
            <ListPanelCount>{channels.length}</ListPanelCount>
          </ListPanelTitle>
          {canWrite && (
            <Button onClick={onNew}>
              <Plus />
              新建渠道
            </Button>
          )}
          <SearchInput
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索名称、key 或地址"
            aria-label="搜索渠道"
          />
        </ListPanelHeader>
        <ListPanelContent className="max-xl:max-h-72">
          {list.length === 0 && <ListPanelEmpty>没有匹配的渠道</ListPanelEmpty>}
          {list.map((item) => {
            const itemPlugin = plugins.find((p) => p.key === item.plugin_key);
            const missing = isKeyMissing(plugins, item);
            const canUpgrade = !!availableUpgrade(plugins, item);
            const itemHealth = channelHealth(item, plugins);
            return (
              <ListPanelItem
                key={item.key}
                data-channel={item.key}
                active={item.key === selected.key}
                onClick={() => onSelect(item.key)}
              >
                <ListPanelItemRow>
                  <span className="truncate text-sm font-medium">{item.name}</span>
                  <StatusLabel
                    tone={HEALTH_UI_TONE[itemHealth.tone]}
                    className="ml-auto"
                    title={itemHealth.reason ?? undefined}
                  >
                    {itemHealth.label}
                  </StatusLabel>
                </ListPanelItemRow>
                <div className="text-muted-foreground mt-0.5 truncate font-mono text-xs">
                  {item.key}
                </div>
                <ListPanelItemRow className="text-muted-foreground mt-1.5 text-xs">
                  <span className="truncate">
                    {itemPlugin?.name ?? item.plugin_key}{" "}
                    <span className="font-mono">v{item.plugin_version}</span>
                    {modelsStatus === "ready" && ` · ${modelCount(item.key)} 个模型`}
                  </span>
                  {missing ? (
                    <span className="ml-auto inline-flex shrink-0 items-center gap-1 text-amber-600 dark:text-amber-400">
                      <KeyRound className="size-3" />
                      未设 Key
                    </span>
                  ) : (
                    canUpgrade && (
                      <span className="ml-auto inline-flex shrink-0 items-center gap-1 text-sky-600 dark:text-sky-400">
                        <CircleArrowUp className="size-3" />
                        可升级
                      </span>
                    )
                  )}
                </ListPanelItemRow>
              </ListPanelItem>
            );
          })}
        </ListPanelContent>
      </ListPanel>

      <div className="min-w-0 space-y-4">
        <Card data-channel={selected.key}>
          <CardHeader>
            <CardTitle className="flex flex-wrap items-center gap-2">
              <span className="text-xl font-semibold tracking-tight">{selected.name}</span>
              <Tag tone={HEALTH_UI_TONE[health.tone]} title={health.reason ?? undefined}>
                {health.label}
              </Tag>
              <span className="text-muted-foreground font-mono text-xs font-normal">
                {selected.key}
              </span>
            </CardTitle>
            <CardDescription className="flex items-center gap-1.5">
              <code className="bg-muted text-foreground truncate rounded px-2 py-0.5 font-mono text-[13px]">
                {selected.base_url}
              </code>
              <CopyButton iconOnly text={selected.base_url} label="复制地址" />
            </CardDescription>
            <CardAction className="flex flex-wrap items-center gap-2">
              {canWrite && (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={check?.busy}
                  aria-label={`检查 ${selected.name}`}
                  onClick={() => onCheck(selected.key)}
                >
                  <Stethoscope />
                  连通性检查
                </Button>
              )}
              <Button
                size="sm"
                variant="outline"
                disabled={importUnsupported}
                title={importUnsupported ? "该插件不支持导入模型" : undefined}
                aria-label={`导入模型 ${selected.name}`}
                onClick={() => onImport(selected)}
              >
                <Download />
                导入模型
              </Button>
              <Button
                size="sm"
                variant="outline"
                aria-label={`${canWrite ? "编辑" : "查看"} ${selected.name}`}
                onClick={() => onEdit(selected.key)}
              >
                {canWrite ? <Pencil /> : <Eye />}
                {canWrite ? "编辑" : "查看配置"}
              </Button>
              {canWrite && (
                <DropdownMenu modal={false}>
                  <DropdownMenuTrigger
                    render={<Button size="icon-sm" variant="outline" aria-label="更多操作" />}
                  >
                    <Ellipsis />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-44">
                    <DropdownMenuGroup>
                      <DropdownMenuItem onClick={() => onSetKey(selected)}>
                        <KeyRound />
                        {selected.secret_set ? "更新 Key" : "设置 Key"}
                      </DropdownMenuItem>
                    </DropdownMenuGroup>
                    <DropdownMenuSeparator />
                    <DropdownMenuGroup>
                      <DropdownMenuItem
                        variant={selected.enabled ? "destructive" : "default"}
                        onClick={() => onToggleChannel(selected)}
                      >
                        {selected.enabled ? <Pause /> : <Play />}
                        {selected.enabled ? "停用渠道" : "启用渠道"}
                      </DropdownMenuItem>
                    </DropdownMenuGroup>
                    <DropdownMenuSeparator />
                    <DropdownMenuGroup>
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => onDeleteChannel(selected)}
                      >
                        <Trash2 />
                        删除渠道…
                      </DropdownMenuItem>
                    </DropdownMenuGroup>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </CardAction>
          </CardHeader>

          {(check || keyMissing || upgrade || pluginOff) && (
            <CardContent className="space-y-2">
              {check && <CheckResult state={check} />}
              {pluginOff && (
                <Notice
                  tone="danger"
                  title={`插件「${plugin.name}」已停用`}
                  action={
                    <Link
                      to={`/admin/ai/plugins?key=${encodeURIComponent(plugin.key)}`}
                      className="text-xs font-medium underline underline-offset-4"
                    >
                      去插件页
                    </Link>
                  }
                >
                  这个渠道不接新任务，使用它的 {usedModels.length} 个模型都不可用。
                </Notice>
              )}
              {keyMissing && !check && (
                <Notice tone="warning" title="未设置 Key" action={fixLink("设置 Key")}>
                  使用这个渠道的模型无法发布。
                </Notice>
              )}
              {upgrade && (
                <Notice title={`插件有新版本 ${upgrade.version}`} action={fixLink("去切换")}>
                  切换后新任务用新版本，进行中的任务按旧版本跑完。
                </Notice>
              )}
            </CardContent>
          )}

          <CardContent className="border-t pt-4">
            <DescriptionList>
              <DescriptionItem>
                <DescriptionTerm>插件</DescriptionTerm>
                <DescriptionDetails>
                  <Link
                    to={`/admin/ai/plugins?key=${encodeURIComponent(selected.plugin_key)}`}
                    className="hover:underline hover:underline-offset-4"
                  >
                    {plugin?.name ?? selected.plugin_key}
                  </Link>
                  <span className="text-muted-foreground font-mono text-xs font-normal">
                    v{selected.plugin_version}
                  </span>
                </DescriptionDetails>
              </DescriptionItem>
              <DescriptionItem>
                <DescriptionTerm>API Key</DescriptionTerm>
                <DescriptionDetails>
                  {selected.secret_set ? (
                    <>
                      <ShieldCheck className="size-4 text-emerald-500" />
                      已设置
                    </>
                  ) : keyMissing ? (
                    <>
                      <ShieldAlert className="size-4 text-amber-500" />
                      <span className="text-amber-600 dark:text-amber-400">未设置</span>
                    </>
                  ) : (
                    <span className="text-muted-foreground">无需</span>
                  )}
                </DescriptionDetails>
              </DescriptionItem>
              <DescriptionItem>
                <DescriptionTerm>限流</DescriptionTerm>
                <DescriptionDetails className="tabular-nums">
                  RPS {rl?.rps || <span className="text-muted-foreground">不限</span>} · 同时请求{" "}
                  {rl?.max_concurrency || <span className="text-muted-foreground">不限</span>} ·
                  同时生成 {rl?.max_running || <span className="text-muted-foreground">不限</span>}
                </DescriptionDetails>
              </DescriptionItem>
              <DescriptionItem>
                <DescriptionTerm>当前负载</DescriptionTerm>
                <DescriptionDetails className="tabular-nums">
                  生成中 {running}
                  {runningLimit > 0 && `/${runningLimit}`}
                  <span className="text-muted-foreground font-normal">·</span>
                  <span className={waiting > 0 ? "text-amber-600 dark:text-amber-400" : undefined}>
                    排队 {waiting}
                  </span>
                  {full && <Tag tone="warning">已满</Tag>}
                </DescriptionDetails>
              </DescriptionItem>
              <DescriptionItem>
                <DescriptionTerm>安全开关</DescriptionTerm>
                <DescriptionDetails className="font-normal">
                  {selected.trusted_internal && (
                    <Tag tone="warning" title="trusted_internal：允许 base_url 解析到内网地址">
                      <Network />
                      内网
                    </Tag>
                  )}
                  {selected.allow_credentials && (
                    <Tag tone="warning" title="allow_credentials：插件代码能读取这个渠道的 Key">
                      <KeyRound />
                      凭证
                    </Tag>
                  )}
                  {!selected.trusted_internal && !selected.allow_credentials && (
                    <span className="text-muted-foreground">均关闭</span>
                  )}
                </DescriptionDetails>
              </DescriptionItem>
              <DescriptionItem>
                <DescriptionTerm>最近修改</DescriptionTerm>
                <DescriptionDetails className="tabular-nums">
                  {formatShortTime(selected.updated_at)}
                </DescriptionDetails>
              </DescriptionItem>
            </DescriptionList>
          </CardContent>

          {settingSpecs.length > 0 && (
            <CardContent className="border-t pt-4">
              <p className="text-muted-foreground mb-2 text-xs">插件设置</p>
              <div className="flex flex-wrap gap-2">
                {settingSpecs.map(([key, spec]) => (
                  <span
                    key={key}
                    className="inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs"
                  >
                    <span className="text-muted-foreground">{spec.label}</span>
                    <span className="font-mono">{String(selected.settings?.[key] ?? "—")}</span>
                  </span>
                ))}
              </div>
            </CardContent>
          )}
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              使用这个渠道的模型
              <ListPanelCount>{usedModels.length}</ListPanelCount>
            </CardTitle>
            <CardDescription>点击模型直接打开编辑弹窗。</CardDescription>
            <CardAction className="flex flex-wrap items-center gap-2">
              <Segmented aria-label="按能力筛选">
                <SegmentedItem active={!modelKind} onClick={() => setModelKind("")}>
                  全部
                </SegmentedItem>
                {kinds.map((kind) => (
                  <SegmentedItem
                    key={kind}
                    active={modelKind === kind}
                    onClick={() => setModelKind(kind)}
                  >
                    {MODEL_KIND_LABEL[kind]}
                  </SegmentedItem>
                ))}
              </Segmented>
              <Button size="sm" variant="outline" onClick={() => onNewModel(selected.key)}>
                <Plus />
                用这个渠道新建模型
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent>
            <ModelRows
              models={shownModels}
              status={modelsStatus}
              channels={channels}
              plugins={plugins}
              hideChannel
              busyKey={busyModelKey}
              empty="还没有模型使用这个渠道"
              onEdit={onEditModel}
              onTest={onTestModel}
              onToggleEnabled={onToggleModel}
              onRollback={onRollbackModel}
              onDelete={onDeleteModel}
            />
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

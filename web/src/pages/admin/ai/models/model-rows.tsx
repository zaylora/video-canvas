import { useState, type ReactNode } from "react";
import { Ellipsis, FlaskConical, Pencil, Trash2 } from "lucide-react";

import { listModelRevisions } from "@/api/admin-ai";
import type {
  ChannelView,
  ConfigListItem,
  ConfigRevision,
  PluginView,
} from "@/api/admin-ai/type.d";
import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { StatusDot } from "@/components/admin-ui/status-dot";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
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
  HEALTH_UI_TONE,
  MODEL_STATUS_LABEL,
  MODEL_STATUS_TONE,
  modelHealth,
  type ModelHealth,
} from "@/utils/admin/health";

import { KindTag } from "../kind";

/**
 * 状态列：对运营只露一个状态（在线 / 不可用 / 已下线 / 未上线），渠道层面的问题写在下面一行，
 * 有未上线的修改再加一个标签。插件停用、渠道缺 Key 等会传到这里，不会再显示“已上架”却用不了。
 */
function StatusCell({ item, health }: { item: ConfigListItem; health: ModelHealth }) {
  return (
    <div className="flex flex-col items-start gap-1">
      <div className="flex flex-wrap items-center gap-1">
        <Tag tone={HEALTH_UI_TONE[MODEL_STATUS_TONE[health.status]]}>
          <StatusDot tone={HEALTH_UI_TONE[MODEL_STATUS_TONE[health.status]]} />
          {MODEL_STATUS_LABEL[health.status]}
          {item.published_revision_no !== null && (
            <span className="font-mono opacity-70">v{item.published_revision_no}</span>
          )}
        </Tag>
        {item.has_unpublished_draft && <Tag tone="info">有未上线的修改</Tag>}
      </div>
      {health.reason && health.status !== "offline" && (
        <span
          className={cn(
            "text-xs",
            health.status === "broken" ? "text-red-600 dark:text-red-400" : "text-muted-foreground",
          )}
        >
          {health.status === "unpublished" ? `上线前要处理：${health.reason}` : health.reason}
        </span>
      )}
    </div>
  );
}

/** 行尾“更多”：编辑 + 版本历史（归档版本可回滚，打开时才拉历史）+ 删除 */
function RowMenu({
  item,
  onEdit,
  onRollback,
  onDelete,
}: {
  item: ConfigListItem;
  onEdit: () => void;
  onRollback: (revision: ConfigRevision) => void;
  onDelete?: () => void;
}) {
  const [revisions, setRevisions] = useState<ConfigRevision[] | null>(null);
  return (
    <DropdownMenu
      modal={false}
      onOpenChange={(open) => {
        if (open) void listModelRevisions(item.key).then(setRevisions, () => setRevisions([]));
      }}
    >
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon-sm" aria-label={`更多：${item.key}`} />}
      >
        <Ellipsis />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuGroup>
          <DropdownMenuItem onClick={onEdit}>
            <Pencil />
            编辑
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuLabel>版本历史</DropdownMenuLabel>
          {revisions === null && (
            <p className="text-muted-foreground px-2 py-1.5 text-sm">加载中…</p>
          )}
          {revisions?.length === 0 && (
            <p className="text-muted-foreground px-2 py-1.5 text-sm">还没有发布过</p>
          )}
          {revisions?.map((revision) => {
            const tag =
              revision.status === "draft" ? (
                <Tag tone="info">草稿</Tag>
              ) : revision.status === "published" ? (
                <Tag tone="success">线上</Tag>
              ) : (
                <Tag>已归档</Tag>
              );
            const label: ReactNode = (
              <>
                <span className="w-8 font-mono">v{revision.revision_no}</span>
                {tag}
              </>
            );
            return revision.status === "archived" ? (
              <DropdownMenuItem key={revision.id} onClick={() => onRollback(revision)}>
                {label}
                <span className="text-muted-foreground ml-auto text-xs">回滚</span>
              </DropdownMenuItem>
            ) : (
              <div key={revision.id} className="flex items-center gap-2 px-2 py-1.5 text-sm">
                {label}
              </div>
            );
          })}
        </DropdownMenuGroup>
        {onDelete && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem variant="destructive" onClick={onDelete}>
                <Trash2 />
                删除…
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * 模型表格（设计稿 modelTable）：模型 / 能力 / 渠道 / 状态 / 上线 / 操作，点整行打开编辑弹窗。
 * 行内按钮是“测试”（编辑靠点整行，更多菜单里也有）；删除在更多菜单底部。
 * 模型页和渠道页“使用这个渠道的模型”共用；渠道页传 hideChannel 去掉渠道列。
 * 传 selected 时第一列是勾选框（批量操作用）。
 */
export function ModelRows({
  models,
  status = "ready",
  channels,
  plugins,
  hideChannel,
  selected,
  onSelectedChange,
  empty = "没有匹配的模型",
  busyKey,
  onEdit,
  onTest,
  onToggleEnabled,
  onRollback,
  onDelete,
  channelsReady = true,
  onRetry,
}: {
  models: ConfigListItem[];
  status?: "loading" | "ready" | "error";
  channels: ChannelView[];
  plugins: PluginView[];
  hideChannel?: boolean;
  selected?: string[];
  onSelectedChange?: (keys: string[]) => void;
  empty?: ReactNode;
  /** 正在切换上架的模型 */
  busyKey?: string | null;
  onEdit: (key: string) => void;
  onTest: (key: string) => void;
  onToggleEnabled: (key: string, enabled: boolean) => void;
  onRollback: (key: string, revision: ConfigRevision) => void;
  /** 删除；不传则不显示删除入口 */
  onDelete?: (item: ConfigListItem) => void;
  /** 渠道与插件清单已加载；没加载完不下“不可用”的结论 */
  channelsReady?: boolean;
  onRetry?: () => void;
}) {
  const selectable = !!selected && !!onSelectedChange;
  const keys = models.map((item) => item.key);
  const all = selectable && keys.length > 0 && keys.every((key) => selected.includes(key));
  const some = selectable && keys.some((key) => selected.includes(key));
  const columns = 5 + (hideChannel ? 0 : 1) + (selectable ? 1 : 0);
  const toggle = (key: string, on: boolean) =>
    onSelectedChange?.(on ? [...(selected ?? []), key] : (selected ?? []).filter((k) => k !== key));

  return (
    <div className="overflow-hidden rounded-md border">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            {selectable && (
              <TableHead className="w-10 pl-3">
                <Checkbox
                  aria-label="选择当前页全部模型"
                  checked={all}
                  indeterminate={!all && some}
                  onCheckedChange={(value) =>
                    onSelectedChange?.(
                      value
                        ? [...selected, ...keys.filter((key) => !selected.includes(key))]
                        : selected.filter((key) => !keys.includes(key)),
                    )
                  }
                />
              </TableHead>
            )}
            <TableHead className="text-muted-foreground px-3 text-xs">模型</TableHead>
            <TableHead className="text-muted-foreground px-3 text-xs">能力</TableHead>
            {!hideChannel && (
              <TableHead className="text-muted-foreground px-3 text-xs">渠道</TableHead>
            )}
            <TableHead className="text-muted-foreground px-3 text-xs">状态</TableHead>
            <TableHead className="text-muted-foreground px-3 text-xs">上线</TableHead>
            <TableHead className="text-muted-foreground px-3 text-right text-xs">操作</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {status === "loading" &&
            Array.from({ length: 4 }, (_, index) => (
              <TableRow key={index}>
                <TableCell colSpan={columns} className="px-3 py-3">
                  <Skeleton className="h-9" />
                </TableCell>
              </TableRow>
            ))}
          {status === "error" && (
            <TableRow>
              <TableCell colSpan={columns} className="text-destructive h-32 text-center">
                加载失败
                {onRetry && (
                  <button type="button" className="ml-1 underline" onClick={onRetry}>
                    重试
                  </button>
                )}
              </TableCell>
            </TableRow>
          )}
          {status === "ready" && models.length === 0 && (
            <TableRow className="hover:bg-transparent">
              <TableCell
                colSpan={columns}
                className="text-muted-foreground h-32 text-center text-sm"
              >
                {empty}
              </TableCell>
            </TableRow>
          )}
          {status === "ready" &&
            models.map((item) => {
              const label = item.label || item.name || item.key;
              const channel = item.channel
                ? channels.find((c) => c.key === item.channel)
                : undefined;
              const plugin = channel
                ? plugins.find((p) => p.key === channel.plugin_key)
                : undefined;
              const neverPublished = item.published_revision_no === null;
              const health = modelHealth(item, channels, plugins, channelsReady);
              return (
                <TableRow
                  key={item.key}
                  data-model={item.key}
                  data-state={selected?.includes(item.key) ? "selected" : undefined}
                  className="cursor-pointer"
                  onClick={() => onEdit(item.key)}
                >
                  {selectable && (
                    <TableCell className="pl-3" onClick={(event) => event.stopPropagation()}>
                      <Checkbox
                        aria-label={`选择 ${label}`}
                        checked={selected.includes(item.key)}
                        onCheckedChange={(value) => toggle(item.key, !!value)}
                      />
                    </TableCell>
                  )}
                  <TableCell className="px-3 py-3">
                    <div className="flex items-center gap-3">
                      <VendorAvatar vendor={item.vendor} name={label} seed={item.key} />
                      <div className="min-w-0">
                        <div className="truncate font-medium">{label}</div>
                        <div className="text-muted-foreground truncate font-mono text-xs">
                          {item.key}
                        </div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="px-3 py-3">
                    <KindTag kind={item.kind ?? ""} />
                  </TableCell>
                  {!hideChannel && (
                    <TableCell className="px-3 py-3">
                      <div className="text-sm">{channel?.name ?? item.channel ?? "—"}</div>
                      {plugin && <div className="text-muted-foreground text-xs">{plugin.name}</div>}
                    </TableCell>
                  )}
                  <TableCell className="px-3 py-3">
                    <StatusCell item={item} health={health} />
                  </TableCell>
                  <TableCell className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
                    <Switch
                      checked={!!item.enabled}
                      disabled={neverPublished || busyKey === item.key}
                      title={neverPublished ? "还没上线过：打开编辑，点「上线」" : undefined}
                      aria-label={`上线 ${label}`}
                      onCheckedChange={(checked) => onToggleEnabled(item.key, checked)}
                    />
                  </TableCell>
                  <TableCell
                    className="px-3 py-3 text-right whitespace-nowrap"
                    onClick={(event) => event.stopPropagation()}
                  >
                    <Button size="sm" variant="outline" onClick={() => onTest(item.key)}>
                      <FlaskConical />
                      测试
                    </Button>
                    <span className="ml-1 inline-block">
                      <RowMenu
                        item={item}
                        onEdit={() => onEdit(item.key)}
                        onRollback={(revision) => onRollback(item.key, revision)}
                        onDelete={onDelete && (() => onDelete(item))}
                      />
                    </span>
                  </TableCell>
                </TableRow>
              );
            })}
        </TableBody>
      </Table>
    </div>
  );
}

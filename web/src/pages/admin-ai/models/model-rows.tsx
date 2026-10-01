import { useState, type ReactNode } from "react";
import { Ellipsis, FlaskConical, Pencil } from "lucide-react";

import { listModelRevisions } from "@/api/admin-ai";
import type { ChannelView, ConfigListItem, ConfigRevision, PluginView } from "@/api/admin-ai/type";
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

import { KindTag } from "../kind";

/** 版本标签：已发布 vN（带点）/ 未发布，有草稿再加一个“草稿 vN” */
function VersionTags({ item }: { item: ConfigListItem }) {
  return (
    <div className="flex flex-wrap gap-1">
      {item.published_revision_no !== null ? (
        <Tag tone="success">
          <StatusDot tone="success" />v{item.published_revision_no}
        </Tag>
      ) : (
        <Tag>未发布</Tag>
      )}
      {item.has_unpublished_draft && item.draft_revision_no !== null && (
        <Tag tone="info">草稿 v{item.draft_revision_no}</Tag>
      )}
    </div>
  );
}

/** 行尾“更多”：测试模型 + 版本历史（归档版本可回滚）；打开时才拉历史 */
function RowMenu({
  item,
  onTest,
  onRollback,
}: {
  item: ConfigListItem;
  onTest: () => void;
  onRollback: (revision: ConfigRevision) => void;
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
          <DropdownMenuItem onClick={onTest}>
            <FlaskConical />
            测试模型
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
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * 模型表格（设计稿 modelTable）：模型 / 能力 / 渠道 / 版本 / 上架 / 操作，点整行打开编辑弹窗。
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
            <TableHead className="text-muted-foreground px-3 text-xs">版本</TableHead>
            <TableHead className="text-muted-foreground px-3 text-xs">上架</TableHead>
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
                    <VersionTags item={item} />
                  </TableCell>
                  <TableCell className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
                    <Switch
                      checked={!!item.enabled}
                      disabled={neverPublished || busyKey === item.key}
                      title={neverPublished ? "发布后才能上架" : undefined}
                      aria-label={`上架 ${label}`}
                      onCheckedChange={(checked) => onToggleEnabled(item.key, checked)}
                    />
                  </TableCell>
                  <TableCell
                    className="px-3 py-3 text-right whitespace-nowrap"
                    onClick={(event) => event.stopPropagation()}
                  >
                    <Button size="sm" variant="outline" onClick={() => onEdit(item.key)}>
                      <Pencil />
                      编辑
                    </Button>
                    <span className="ml-1 inline-block">
                      <RowMenu
                        item={item}
                        onTest={() => onTest(item.key)}
                        onRollback={(revision) => onRollback(item.key, revision)}
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

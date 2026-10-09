import { Activity, Loader2, Star, Trash2 } from "lucide-react";

import type { StoragePreset, StorageView } from "@/api/admin/storage/type.d";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
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
  accessLabel,
  defaultBlockReason,
  deleteBlockReason,
  formatCount,
  providerLabel,
  storageLocation,
} from "@/utils/admin/storage-rules";

import { StorageStatus } from "./storage-status";

/** 一行上的操作回调 */
export type StorageRowActions = {
  /** 点击行打开弹窗：super_admin 是编辑，admin 是只读查看；内置存储来自 config.yaml，不可点开 */
  onOpen: (storage: StorageView) => void;
  /** 用已存密钥重新测试 */
  onCheck: (storage: StorageView) => void;
  /** 设为默认（弹确认框） */
  onSetDefault: (storage: StorageView) => void;
  /** 删除（先做删除预检） */
  onDelete: (storage: StorageView) => void;
};

/**
 * 存储列表。点击行（或聚焦后按 Enter）打开弹窗：super_admin 编辑，admin 只读查看；
 * 操作列的写操作（测试、设为默认、删除）只对 super_admin 渲染，admin 没有操作列；
 * 设为默认、删除被禁用时，悬停按钮能看到原因，不让人猜。
 * @param storages 存储列表
 * @param presets 服务商预设（取服务商显示名）
 * @param canWrite 是否有写权限
 * @param busyId 正在测试或预检的存储 ID，对应行的按钮转圈
 * @param actions 行操作回调
 */
export function StorageTable({
  storages,
  presets,
  canWrite,
  busyId,
  actions,
}: {
  storages: StorageView[];
  presets: StoragePreset[];
  canWrite: boolean;
  busyId: number | null;
  actions: StorageRowActions;
}) {
  return (
    <div data-slot="storage-table" className="bg-card overflow-hidden rounded-xl border">
      <Table>
        <TableHeader>
          <TableRow className="bg-muted/40 hover:bg-muted/40">
            <TableHead className="px-4">名称</TableHead>
            <TableHead>服务商</TableHead>
            <TableHead>桶 · 地域</TableHead>
            <TableHead>访问方式</TableHead>
            <TableHead>直传</TableHead>
            <TableHead className="text-right">素材数</TableHead>
            <TableHead>连通状态</TableHead>
            {canWrite && <TableHead className="px-4 text-right">操作</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {storages.map((storage) => (
            <StorageRow
              key={storage.id}
              storage={storage}
              presets={presets}
              canWrite={canWrite}
              busy={busyId === storage.id}
              actions={actions}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function StorageRow({
  storage,
  presets,
  canWrite,
  busy,
  actions,
}: {
  storage: StorageView;
  presets: StoragePreset[];
  canWrite: boolean;
  busy: boolean;
  actions: StorageRowActions;
}) {
  const where = storageLocation(storage);
  const defaultReason = defaultBlockReason(storage);
  const deleteReason = deleteBlockReason(storage);
  const openable = !storage.builtin;
  /** 操作列里的点击不该打开弹窗；浮层 portal 出去的事件也会沿 React 树冒泡到这里 */
  const stop = { onClick: (event: React.MouseEvent) => event.stopPropagation() };
  return (
    <TableRow
      data-storage-id={storage.id}
      tabIndex={openable ? 0 : undefined}
      aria-label={openable ? `${canWrite ? "编辑" : "查看"} ${storage.name}` : undefined}
      className={cn(
        "align-top",
        openable &&
          "focus-visible:ring-ring/50 cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-inset",
      )}
      onClick={openable ? () => actions.onOpen(storage) : undefined}
      onKeyDown={
        openable
          ? (event) => {
              if (event.key === "Enter" && event.target === event.currentTarget)
                actions.onOpen(storage);
            }
          : undefined
      }
    >
      <TableCell className="px-4 py-3 whitespace-normal">
        <div className="flex flex-wrap items-center gap-1.5 font-medium">
          {storage.name}
          {storage.is_default && <Tag tone="info">默认</Tag>}
          {storage.builtin && <Tag>内置</Tag>}
        </div>
        {storage.builtin && (
          <div className="text-muted-foreground text-[11px]">来自 config.yaml，只读</div>
        )}
      </TableCell>
      <TableCell className="py-3">{providerLabel(storage.provider, presets)}</TableCell>
      <TableCell className="py-3">
        <div className="font-mono text-xs">{where.primary}</div>
        {where.secondary && (
          <div className="text-muted-foreground text-[11px]">{where.secondary}</div>
        )}
      </TableCell>
      <TableCell className="py-3">
        {storage.provider === "local" ? (
          <span className="text-muted-foreground">{accessLabel(storage)}</span>
        ) : (
          <Tag>{accessLabel(storage)}</Tag>
        )}
      </TableCell>
      <TableCell className="py-3">
        {storage.provider === "local" ? (
          <span className="text-muted-foreground">—</span>
        ) : storage.direct_upload ? (
          <Tag tone="success">开</Tag>
        ) : (
          <span className="text-muted-foreground">关</span>
        )}
      </TableCell>
      <TableCell className="py-3 text-right font-mono tabular-nums">
        {formatCount(storage.asset_count)}
      </TableCell>
      <TableCell className="py-3">
        <StorageStatus storage={storage} />
      </TableCell>
      {canWrite && (
        <TableCell className="px-4 py-3" {...stop}>
          <div className="flex items-center justify-end gap-1">
            <Button
              variant="ghost"
              size="xs"
              disabled={busy}
              aria-label={`测试 ${storage.name}`}
              onClick={() => actions.onCheck(storage)}
            >
              {busy ? <Loader2 className="animate-spin" /> : <Activity />}
              测试
            </Button>
            <ReasonTooltip reason={defaultReason}>
              <Button
                variant="ghost"
                size="xs"
                disabled={!!defaultReason}
                aria-label={`把 ${storage.name} 设为默认`}
                onClick={() => actions.onSetDefault(storage)}
              >
                <Star />
                设为默认
              </Button>
            </ReasonTooltip>
            <ReasonTooltip reason={deleteReason}>
              <Button
                variant="ghost"
                size="icon-xs"
                className="text-destructive hover:text-destructive"
                disabled={!!deleteReason || busy}
                aria-label={`删除 ${storage.name}`}
                onClick={() => actions.onDelete(storage)}
              >
                <Trash2 />
              </Button>
            </ReasonTooltip>
          </div>
        </TableCell>
      )}
    </TableRow>
  );
}

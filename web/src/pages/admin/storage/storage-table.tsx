import { Activity, Loader2, Star, Trash2 } from "lucide-react";
import { Link } from "react-router";

import type { ProcessorOption, ProcessorPreset } from "@/api/admin/image-processor/type.d";
import type { StoragePreset, StorageView } from "@/api/admin/storage/type.d";
import { EmptyState, EmptyStateActions, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import { TableSkeletonRows } from "@/components/admin-ui/table-skeleton-rows";
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
import { bindingOfStorage, enableTarget } from "@/utils/admin/image-processor";
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
 * @param loading 加载中显示骨架行
 * @param busyId 正在测试或预检的存储 ID，对应行的按钮转圈
 * @param filtering 当前有搜索词（空列表时区分“没有存储”和“没有命中”）
 * @param onClearFilters 清除搜索
 * @param processors 全部处理服务的精简视图（「图片处理」列用）
 * @param processorPresets 图片处理厂商预设（判定哪些存储能启用）
 * @param actions 行操作回调
 */
export function StorageTable({
  storages,
  presets,
  canWrite,
  loading = false,
  busyId,
  filtering,
  onClearFilters,
  processors,
  processorPresets,
  actions,
}: {
  storages: StorageView[];
  presets: StoragePreset[];
  canWrite: boolean;
  /** 加载中：表头照常显示，表格体换成骨架行 */
  loading?: boolean;
  busyId: number | null;
  filtering: boolean;
  onClearFilters: () => void;
  processors: ProcessorOption[];
  processorPresets: ProcessorPreset[];
  actions: StorageRowActions;
}) {
  if (!loading && storages.length === 0) {
    return (
      <EmptyState data-slot="storage-table">
        <EmptyStateTitle>{filtering ? "没有符合条件的存储" : "还没有存储"}</EmptyStateTitle>
        {filtering && (
          <EmptyStateActions>
            <Button variant="outline" size="sm" onClick={onClearFilters}>
              清除搜索
            </Button>
          </EmptyStateActions>
        )}
      </EmptyState>
    );
  }
  return (
    <div data-slot="storage-table" className="overflow-hidden">
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
            <TableHead>图片处理</TableHead>
            {canWrite && <TableHead className="px-4 text-right">操作</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading && <TableSkeletonRows columns={canWrite ? 9 : 8} />}
          {storages.map((storage) => (
            <StorageRow
              key={storage.id}
              storage={storage}
              presets={presets}
              canWrite={canWrite}
              busy={busyId === storage.id}
              processors={processors}
              processorPresets={processorPresets}
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
  processors,
  processorPresets,
  actions,
}: {
  storage: StorageView;
  presets: StoragePreset[];
  canWrite: boolean;
  busy: boolean;
  processors: ProcessorOption[];
  processorPresets: ProcessorPreset[];
  actions: StorageRowActions;
}) {
  const where = storageLocation(storage);
  const defaultReason = defaultBlockReason(storage);
  const deleteReason = deleteBlockReason(storage);
  const openable = !storage.builtin;
  /** 操作列里的点击不该打开弹窗；浮层 portal 出去的事件也会沿 React 树冒泡到这里 */
  const stop = {
    onClick: (event: React.MouseEvent) => event.stopPropagation(),
  };
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
          <div className="text-muted-foreground text-xs">来自 config.yaml，只读</div>
        )}
      </TableCell>
      <TableCell className="py-3">{providerLabel(storage.provider, presets)}</TableCell>
      <TableCell className="py-3">
        <div className="font-mono text-xs">{where.primary}</div>
        {where.secondary && <div className="text-muted-foreground text-xs">{where.secondary}</div>}
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
      <TableCell className="py-3" {...stop}>
        <ProcessingCell
          storage={storage}
          processors={processors}
          presets={processorPresets}
          canWrite={canWrite}
        />
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

const PROCESSING_UI = {
  published: { label: "已发布", tone: "success" },
  draft: { label: "草稿", tone: "neutral" },
  disabled: { label: "已停用", tone: "warning" },
} as const;

/**
 * 「图片处理」列：该存储上的处理服务状态（已发布 > 草稿 > 已停用），点标签去图片服务页；
 * 没有处理服务时给「启用」，跳到图片服务页并预选这个存储；本地磁盘等接不了的存储按钮禁用并写明原因。
 * 没有写权限时不给「启用」，只显示“未启用”。
 */
function ProcessingCell({
  storage,
  processors,
  presets,
  canWrite,
}: {
  storage: StorageView;
  processors: ProcessorOption[];
  presets: ProcessorPreset[];
  canWrite: boolean;
}) {
  const { published, draft, disabled } = bindingOfStorage(storage.id, processors);
  const current = published ?? draft ?? disabled;
  if (current) {
    const ui = PROCESSING_UI[current.status];
    return (
      <Link
        to="/admin/settings/image-processor"
        className="focus-visible:ring-ring/50 inline-flex rounded-md outline-none focus-visible:ring-2"
        aria-label={`${storage.name} 的图片处理：${ui.label}，前往图片服务`}
      >
        <Tag tone={ui.tone}>{ui.label}</Tag>
      </Link>
    );
  }
  if (!canWrite) return <span className="text-muted-foreground text-xs">未启用</span>;
  const { reason } = enableTarget(storage, presets, processors);
  return (
    <ReasonTooltip reason={reason}>
      <Button
        variant="outline"
        size="xs"
        disabled={!!reason}
        nativeButton={!!reason}
        render={
          reason ? undefined : <Link to={`/admin/settings/image-processor?new=${storage.id}`} />
        }
      >
        启用
      </Button>
    </ReasonTooltip>
  );
}

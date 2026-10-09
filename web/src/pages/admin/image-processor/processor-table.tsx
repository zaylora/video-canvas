import { FlaskConical, Pause, Trash2, Undo2 } from "lucide-react";

import type { ProcessorPreset, ProcessorView } from "@/api/admin/image-processor/type.d";
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
import { isOwnStorageVendor, processorActions, trialSummary } from "@/utils/admin/image-processor";

import { ProcessorStatusTags } from "./processor-status";

/** 一行上的操作回调 */
export type ProcessorRowActions = {
  /** 点击行打开弹窗：有写权限是编辑（已发布的编辑产生草稿），否则只读查看 */
  onOpen: (processor: ProcessorView) => void;
  /** 打开弹窗并直接进入“校验与发布”一步 */
  onVerify: (processor: ProcessorView) => void;
  /** 回滚（弹确认框） */
  onRollback: (processor: ProcessorView) => void;
  /** 停用（弹确认框） */
  onDisable: (processor: ProcessorView) => void;
  /** 删除（弹确认框） */
  onDelete: (processor: ProcessorView) => void;
};

/**
 * 处理服务列表。点击行（或聚焦后按 Enter）打开弹窗：super_admin 编辑，admin 只读查看；
 * 操作列按钮是否出现由 processorActions 按状态与权限决定，admin 没有操作列：
 * 回滚要有上一个版本，停用只对已发布的，删除只对草稿 / 已停用的。
 * @param processors 处理服务列表
 * @param presets 厂商预设（取厂商显示名）
 * @param canWrite 是否有写权限
 * @param actions 行操作回调
 */
export function ProcessorTable({
  processors,
  presets,
  canWrite,
  actions,
}: {
  processors: ProcessorView[];
  presets: ProcessorPreset[];
  canWrite: boolean;
  actions: ProcessorRowActions;
}) {
  return (
    <div data-slot="processor-table" className="bg-card overflow-hidden rounded-xl border">
      <Table>
        <TableHeader>
          <TableRow className="bg-muted/40 hover:bg-muted/40">
            <TableHead className="px-4">名称</TableHead>
            <TableHead>厂商</TableHead>
            <TableHead>绑定存储</TableHead>
            <TableHead>状态</TableHead>
            <TableHead>最近试跑</TableHead>
            {canWrite && <TableHead className="px-4 text-right">操作</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {processors.map((processor) => (
            <ProcessorRow
              key={processor.id}
              processor={processor}
              vendorName={
                presets.find((item) => item.vendor === processor.vendor)?.name ?? processor.vendor
              }
              canWrite={canWrite}
              actions={actions}
            />
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function ProcessorRow({
  processor,
  vendorName,
  canWrite,
  actions,
}: {
  processor: ProcessorView;
  vendorName: string;
  canWrite: boolean;
  actions: ProcessorRowActions;
}) {
  const can = processorActions(processor, canWrite);
  const trial = trialSummary(processor.check);
  /** 操作列里的点击不该打开弹窗；浮层 portal 出去的事件也会沿 React 树冒泡到这里 */
  const stop = { onClick: (event: React.MouseEvent) => event.stopPropagation() };
  return (
    <TableRow
      data-processor-id={processor.id}
      tabIndex={0}
      aria-label={`${canWrite ? "编辑" : "查看"} ${processor.name}`}
      className={cn(
        "cursor-pointer align-top",
        "focus-visible:ring-ring/50 outline-none focus-visible:ring-2 focus-visible:ring-inset",
      )}
      onClick={() => actions.onOpen(processor)}
      onKeyDown={(event) => {
        if (event.key === "Enter" && event.target === event.currentTarget)
          actions.onOpen(processor);
      }}
    >
      <TableCell className="px-4 py-3 whitespace-normal">
        <div className="font-medium">{processor.name}</div>
        <div className="text-muted-foreground text-[11px] tabular-nums">
          {processor.published_version > 0 && `v${processor.published_version} · `}
          {new Date(processor.updated_at).toLocaleString("zh-CN")}
        </div>
      </TableCell>
      <TableCell className="py-3 whitespace-normal">
        <div>{vendorName}</div>
        {isOwnStorageVendor(processor.vendor) && <Tag tone="warning">必须使用自家存储</Tag>}
      </TableCell>
      <TableCell className="py-3 whitespace-normal">{processor.storage_name}</TableCell>
      <TableCell className="py-3">
        <ProcessorStatusTags processor={processor} />
      </TableCell>
      <TableCell className="py-3 whitespace-normal">
        {processor.check ? (
          <div className="font-mono text-xs tabular-nums">
            <div>{trial.image ?? <span className="text-muted-foreground">图片：无素材</span>}</div>
            <div>{trial.video ?? <span className="text-muted-foreground">视频：无封面</span>}</div>
          </div>
        ) : (
          <span className="text-muted-foreground">未试跑</span>
        )}
      </TableCell>
      {canWrite && (
        <TableCell className="px-4 py-3" {...stop}>
          <div className="flex flex-wrap items-center justify-end gap-1">
            {can.verify && (
              <Button
                variant="ghost"
                size="xs"
                aria-label={`校验或发布 ${processor.name}`}
                onClick={() => actions.onVerify(processor)}
              >
                <FlaskConical />
                校验 / 发布
              </Button>
            )}
            {can.rollback && (
              <Button
                variant="ghost"
                size="xs"
                aria-label={`回滚 ${processor.name}`}
                onClick={() => actions.onRollback(processor)}
              >
                <Undo2 />
                回滚
              </Button>
            )}
            {can.disable && (
              <Button
                variant="ghost"
                size="xs"
                aria-label={`停用 ${processor.name}`}
                onClick={() => actions.onDisable(processor)}
              >
                <Pause />
                停用
              </Button>
            )}
            {can.remove && (
              <Button
                variant="ghost"
                size="icon-xs"
                className="text-destructive hover:text-destructive"
                aria-label={`删除 ${processor.name}`}
                onClick={() => actions.onDelete(processor)}
              >
                <Trash2 />
              </Button>
            )}
          </div>
        </TableCell>
      )}
    </TableRow>
  );
}

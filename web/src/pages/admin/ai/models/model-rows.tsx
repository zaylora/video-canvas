import { useState, type ReactNode } from "react";
import { Reorder, useDragControls } from "motion/react";
import { FlaskConical, GripVertical, Trash2 } from "lucide-react";

import type { ModelHealth, ModelListItem } from "@/api/admin/ai/type.d";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { RowIconAction } from "@/components/admin-ui/row-icon-action";
import { StatusDot } from "@/components/admin-ui/status-dot";
import { Tag } from "@/components/admin-ui/tag";
import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { Checkbox } from "@/components/ui/checkbox";
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
import { HEALTH_UI_TONE, MODEL_STATUS_LABEL, MODEL_STATUS_TONE } from "@/utils/admin/health";
import { reorderKeepsGroups } from "@/utils/admin/model-list";

import { KindTag } from "../kind";

/** 待配置的模型不能启用：先把能力和协议配好 */
const PENDING_DENY = "还没有选择能力或协议，先编辑模型";

/**
 * 状态列：一个启用开关 + 状态标签。状态由后端算好（在线 / 不可用 / 未上线 / 待配置），
 * 渠道或协议层面的问题（插件停用、渠道缺 Key 等）写在下面一行，不会再显示“在线”却用不了。
 * 待配置的模型开关置灰并写明原因。
 */
function StatusCell({
  item,
  health,
  busy,
  label,
  onToggle,
}: {
  item: ModelListItem;
  health: ModelHealth;
  busy: boolean;
  label: string;
  onToggle: (enabled: boolean) => void;
}) {
  const pending = health.status === "pending";
  const tone = HEALTH_UI_TONE[MODEL_STATUS_TONE[health.status]];
  return (
    <div className="flex items-start gap-2.5" onClick={(event) => event.stopPropagation()}>
      <ReasonTooltip reason={pending ? PENDING_DENY : null}>
        <Switch
          checked={!!item.enabled}
          disabled={busy || pending}
          aria-label={`上线 ${label}`}
          onCheckedChange={onToggle}
        />
      </ReasonTooltip>
      <div className="flex min-w-0 flex-col items-start gap-1">
        <Tag tone={tone}>
          <StatusDot tone={tone} />
          {MODEL_STATUS_LABEL[health.status]}
        </Tag>
        {health.reason && (
          <span
            className={cn(
              "text-xs",
              health.status === "broken"
                ? "text-red-600 dark:text-red-400"
                : "text-muted-foreground",
            )}
          >
            {health.status === "offline" ? `上线前要处理：${health.reason}` : health.reason}
          </span>
        )}
      </div>
    </div>
  );
}

/** 请求协议列：“插件名 @版本”；Agent 走宿主网关；没选协议是黄色“待配置” */
function ProtocolCell({ item }: { item: ModelListItem }) {
  if (item.kind === "agent") return <span className="text-muted-foreground text-sm">宿主网关</span>;
  if (!item.plugin) return <Tag tone="warning">待配置</Tag>;
  return (
    <span className="text-sm">
      {item.plugin_name || item.plugin}
      {item.plugin_version && (
        <span className="text-muted-foreground ml-1 font-mono text-xs">@{item.plugin_version}</span>
      )}
    </span>
  );
}

/** 测试按钮被禁用的原因；可以测试返回 null */
function testDeny(item: ModelListItem): string | null {
  if (!item.kind) return "还没有选择能力，先编辑模型";
  if (item.kind === "agent") return "Agent 模型暂不支持试跑";
  if (!item.plugin) return "还没有选择协议，先编辑模型";
  return null;
}

/**
 * 模型表格（设计稿 renderTable）：☐ / 模型（名称 + key）/ 能力 / 请求协议 / 状态（启用开关）/ 操作，点整行打开编辑弹窗。
 * 行内常驻“测试 / 删除”（编辑靠点整行）。传 selected 时第一列是勾选框（批量操作用）。
 * 传 onReorder 时每行左侧有拖动手柄，拖动时只改本地顺序，松手顺序变了才回调（新顺序是当前行的 key 列表）；
 * 列表按启用开关分组（已启用在前），只能组内拖动，跨组松手会弹回原顺序；
 * 只有手柄能拖，免得点开关、点行时误触发排序。服务端的列表变了就以服务端为准。
 */
export function ModelRows({
  models,
  status = "ready",
  className,
  selected,
  onSelectedChange,
  empty = "没有匹配的模型",
  busyKey,
  onReorder,
  onEdit,
  onTest,
  onToggleEnabled,
  onDelete,
  onRetry,
}: {
  /** 当页的模型（状态和渠道名由后端算好） */
  models: ModelListItem[];
  status?: "loading" | "ready" | "error";
  /** 外层容器的额外类名（嵌进别的卡片时去掉自带的边框） */
  className?: string;
  selected?: string[];
  onSelectedChange?: (keys: string[]) => void;
  empty?: ReactNode;
  /** 正在切换上线的模型 */
  busyKey?: string | null;
  /** 拖动排序松手后顺序变了的回调；不传则不能拖 */
  onReorder?: (keys: string[]) => void;
  onEdit: (key: string) => void;
  onTest: (key: string) => void;
  onToggleEnabled: (key: string, enabled: boolean) => void;
  /** 删除；不传则不显示删除入口 */
  onDelete?: (item: ModelListItem) => void;
  onRetry?: () => void;
}) {
  const selectable = !!selected && !!onSelectedChange;
  const keys = models.map((item) => item.key);
  const [local, setLocal] = useState(models);
  const [prevModels, setPrevModels] = useState(models);
  /** 服务端的列表变了就重置本地顺序（在渲染里同步，免得闪一帧旧顺序） */
  if (models !== prevModels) {
    setPrevModels(models);
    setLocal(models);
  }
  const draggable = !!onReorder && status === "ready" && models.length > 1;
  /** 松手：顺序真的变了才回调；有行跨过启用 / 未启用的分组边界就弹回服务端顺序，不保存 */
  const commit = () => {
    if (!local.some((item, index) => item.key !== models[index]?.key)) return;
    if (!reorderKeepsGroups(models, local)) {
      setLocal(models);
      return;
    }
    onReorder?.(local.map((item) => item.key));
  };
  const all = selectable && keys.length > 0 && keys.every((key) => selected.includes(key));
  const some = selectable && keys.some((key) => selected.includes(key));
  const columns = 5 + (selectable ? 1 : 0) + (onReorder ? 1 : 0);
  const toggle = (key: string, on: boolean) =>
    onSelectedChange?.(on ? [...(selected ?? []), key] : (selected ?? []).filter((k) => k !== key));

  /** 一行的所有单元格；startDrag 有值表示这一行可拖，最左边多一个手柄 */
  const renderCells = (item: ModelListItem, startDrag?: (event: React.PointerEvent) => void) => {
    const label = item.label || item.name || item.key;
    const deny = testDeny(item);
    return (
      <>
        {onReorder && (
          <TableCell className="w-8 pr-0 pl-3" onClick={(event) => event.stopPropagation()}>
            {startDrag && (
              <button
                type="button"
                aria-label={`拖动排序 ${label}`}
                title="拖动排序"
                onPointerDown={startDrag}
                className="text-muted-foreground hover:bg-background hover:text-foreground focus-visible:ring-ring/50 grid h-8 w-5 cursor-grab touch-none place-items-center rounded-md outline-none focus-visible:ring-2 active:cursor-grabbing"
              >
                <GripVertical className="size-4" />
              </button>
            )}
          </TableCell>
        )}
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
              {item.tags && item.tags.length > 0 && (
                <div className="mt-0.5 flex flex-wrap gap-1">
                  {item.tags.map((tag) => (
                    <span
                      key={tag}
                      className="bg-muted text-muted-foreground rounded px-1.5 py-px text-xs"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              )}
            </div>
          </div>
        </TableCell>
        <TableCell className="px-3 py-3">
          {item.kind ? <KindTag kind={item.kind} /> : <Tag tone="warning">待配置</Tag>}
        </TableCell>
        <TableCell className="px-3 py-3">
          <ProtocolCell item={item} />
        </TableCell>
        <TableCell className="px-3 py-3">
          <StatusCell
            item={item}
            health={item.health}
            busy={busyKey === item.key}
            label={label}
            onToggle={(checked) => onToggleEnabled(item.key, checked)}
          />
        </TableCell>
        <TableCell className="px-3 py-3" onClick={(event) => event.stopPropagation()}>
          <div className="flex items-center justify-end gap-0.5">
            <RowIconAction label="测试" deny={deny} target={label} onClick={() => onTest(item.key)}>
              <FlaskConical />
            </RowIconAction>
            {onDelete && (
              <RowIconAction label="删除" destructive target={label} onClick={() => onDelete(item)}>
                <Trash2 />
              </RowIconAction>
            )}
          </div>
        </TableCell>
      </>
    );
  };

  return (
    <div className={cn("overflow-hidden rounded-md border", className)}>
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            {onReorder && <TableHead className="w-8 pr-0 pl-3" />}
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
            <TableHead className="text-muted-foreground px-3 text-xs">请求协议</TableHead>
            <TableHead className="text-muted-foreground px-3 text-xs">状态</TableHead>
            <TableHead className="text-muted-foreground px-3 text-right text-xs">操作</TableHead>
          </TableRow>
        </TableHeader>
        {draggable ? (
          <Reorder.Group
            as="tbody"
            axis="y"
            values={local}
            onReorder={setLocal}
            data-slot="table-body"
            className="[&_tr:last-child]:border-0"
          >
            {local.map((item) => (
              <SortableRow
                key={item.key}
                item={item}
                selected={!!selected?.includes(item.key)}
                onDragEnd={commit}
                onEdit={() => onEdit(item.key)}
              >
                {(startDrag) => renderCells(item, startDrag)}
              </SortableRow>
            ))}
          </Reorder.Group>
        ) : (
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
              models.map((item) => (
                <TableRow
                  key={item.key}
                  data-model={item.key}
                  data-state={selected?.includes(item.key) ? "selected" : undefined}
                  className="cursor-pointer"
                  onClick={() => onEdit(item.key)}
                >
                  {renderCells(item)}
                </TableRow>
              ))}
          </TableBody>
        )}
      </Table>
    </div>
  );
}

/** 可拖动的一行：只有手柄（startDrag）能触发拖动，点整行仍是打开编辑 */
function SortableRow({
  item,
  selected,
  onDragEnd,
  onEdit,
  children,
}: {
  item: ModelListItem;
  selected: boolean;
  onDragEnd: () => void;
  onEdit: () => void;
  children: (startDrag: (event: React.PointerEvent) => void) => ReactNode;
}) {
  const controls = useDragControls();
  return (
    <Reorder.Item
      as="tr"
      value={item}
      dragListener={false}
      dragControls={controls}
      onDragEnd={onDragEnd}
      data-slot="table-row"
      data-model={item.key}
      data-state={selected ? "selected" : undefined}
      className="hover:bg-muted/50 data-[state=selected]:bg-muted cursor-pointer border-b transition-colors"
      onClick={onEdit}
    >
      {children((event) => controls.start(event))}
    </Reorder.Item>
  );
}

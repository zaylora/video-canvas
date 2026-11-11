import { useState } from "react";
import { Reorder, useDragControls } from "motion/react";
import { GripVertical, Pencil, Trash2 } from "lucide-react";

import type { AnnouncementAdminItem } from "@/api/admin/announcement/type";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { STATUS_META, windowLabel } from "@/utils/admin/announcement";

/**
 * 一行公告：拖动手柄、状态、标题、生效时间、启用开关、编辑、删除。
 * 只有手柄能拖动，免得想点开关、选文字时误触发排序。停用的行标题半透明。
 */
function AnnouncementRow({
  item,
  busy,
  onDragEnd,
  onToggle,
  onEdit,
  onRemove,
}: {
  item: AnnouncementAdminItem;
  busy: boolean;
  onDragEnd: () => void;
  onToggle: () => void;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const controls = useDragControls();
  const meta = STATUS_META[item.status];

  return (
    <Reorder.Item
      value={item}
      dragListener={false}
      dragControls={controls}
      onDragEnd={onDragEnd}
      className={cn(
        "hover:bg-muted group/row relative flex items-center gap-3 rounded-[10px] p-2 transition-colors duration-120 max-md:flex-wrap",
        busy && "pointer-events-none opacity-50",
      )}
    >
      <button
        type="button"
        aria-label="拖动排序"
        title="拖动排序"
        onPointerDown={(event) => controls.start(event)}
        className="text-muted-foreground hover:bg-background hover:text-foreground focus-visible:ring-ring/50 grid h-8 w-5 shrink-0 cursor-grab touch-none place-items-center rounded-md outline-none focus-visible:ring-2 active:cursor-grabbing"
      >
        <GripVertical className="size-4" />
      </button>
      <div className="min-w-0 flex-1">
        <p className={cn("truncate text-sm font-medium", !item.enabled && "opacity-50")}>
          {item.title}
        </p>
        <div className="text-muted-foreground mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-xs">
          <Tag tone={meta.tone}>{meta.label}</Tag>
          <span className="tabular-nums">{windowLabel(item.starts_at, item.ends_at)}</span>
        </div>
      </div>
      <Switch
        checked={item.enabled}
        disabled={busy}
        title={item.enabled ? "已启用" : "未启用"}
        aria-label="启用"
        onCheckedChange={onToggle}
      />
      <div className="flex shrink-0 gap-0.5">
        <Button variant="ghost" size="icon" aria-label="编辑" title="编辑" onClick={onEdit}>
          <Pencil />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          aria-label="删除"
          title="删除"
          className="hover:text-destructive"
          onClick={onRemove}
        >
          <Trash2 />
        </Button>
      </div>
    </Reorder.Item>
  );
}

/**
 * 公告列表（motion 的 Reorder，和登录页展示同一套）：拖动时只改本地顺序，松手才提交；
 * 服务端的列表变了（新增、删除、重新拉取）就以服务端为准。
 * @param items 服务端的公告顺序
 * @param busyIds 正在保存的公告
 * @param onReorder 松手后顺序变了就提交新顺序
 */
export function AnnouncementList({
  items,
  busyIds,
  onReorder,
  onToggle,
  onEdit,
  onRemove,
}: {
  items: AnnouncementAdminItem[];
  busyIds: ReadonlySet<number>;
  onReorder: (items: AnnouncementAdminItem[]) => void;
  onToggle: (item: AnnouncementAdminItem) => void;
  onEdit: (item: AnnouncementAdminItem) => void;
  onRemove: (item: AnnouncementAdminItem) => void;
}) {
  const [local, setLocal] = useState(items);
  const [prevItems, setPrevItems] = useState(items);

  /** 服务端的列表变了就重置本地顺序（在渲染里同步，不在 effect 里，免得闪一帧旧顺序） */
  if (items !== prevItems) {
    setPrevItems(items);
    setLocal(items);
  }

  /** 松手：顺序真的变了才提交 */
  const commit = () => {
    const changed = local.some((item, index) => item.id !== items[index]?.id);
    if (changed) onReorder(local);
  };

  return (
    <Reorder.Group axis="y" values={local} onReorder={setLocal} className="m-0 list-none p-1.5">
      {local.map((item) => (
        <AnnouncementRow
          key={item.id}
          item={item}
          busy={busyIds.has(item.id)}
          onDragEnd={commit}
          onToggle={() => onToggle(item)}
          onEdit={() => onEdit(item)}
          onRemove={() => onRemove(item)}
        />
      ))}
    </Reorder.Group>
  );
}

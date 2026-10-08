import { useState } from "react";
import { Reorder, useDragControls } from "motion/react";
import { GripVertical, Pencil, Trash2, TriangleAlert } from "lucide-react";

import type { ShowcaseAdminItem } from "@/api/admin/showcase/type";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { showcaseWarnings, type ShowcaseWarning } from "@/utils/showcase/rules";

/** 体积 / 画幅提示的文案 */
const WARNING_TEXT: Record<ShowcaseWarning, string> = {
  heavy: "体积偏大，首屏会变慢",
  portrait: "竖屏素材，横屏会被裁切",
};

/**
 * 一行的缩略图：封面在上，序号角标；悬停时才挂上视频并开始播放，
 * 视频垫在封面下面，播起来以后再把封面隐藏，避免闪黑。没有封面就直接用视频首帧。
 */
function Thumb({ item, index }: { item: ShowcaseAdminItem; index: number }) {
  const [hovering, setHovering] = useState(false);
  const [playing, setPlaying] = useState(false);

  return (
    <div
      className="bg-muted relative aspect-video w-28 shrink-0 overflow-hidden rounded-lg max-md:w-22"
      onMouseEnter={() => setHovering(true)}
      onMouseLeave={() => {
        setHovering(false);
        setPlaying(false);
      }}
    >
      {item.video_url && (hovering || !item.poster_url) && (
        <video
          src={item.poster_url ? item.video_url : `${item.video_url}#t=0.1`}
          muted
          loop
          playsInline
          autoPlay={hovering}
          preload="metadata"
          onPlaying={() => setPlaying(true)}
          className="absolute inset-0 size-full object-cover"
        />
      )}
      {item.poster_url && (
        <img
          src={item.poster_url}
          alt=""
          className={cn(
            "absolute inset-0 size-full object-cover transition-opacity duration-120",
            playing && "opacity-0",
          )}
        />
      )}
      <span className="bg-cover-scrim text-cover-foreground absolute top-1 left-1 grid h-4.5 min-w-4.5 place-items-center rounded-md px-1 text-[11px] tabular-nums">
        {index + 1}
      </span>
    </div>
  );
}

/**
 * 一行作品：拖动手柄、缩略图、提示词、标签、启用开关、编辑、移除。
 * 只有手柄能拖动，免得想点开关、选文字时误触发排序。停用的行缩略图和提示词半透明。
 */
function ShowcaseRow({
  item,
  index,
  busy,
  canWrite,
  onDragEnd,
  onToggle,
  onEdit,
  onRemove,
}: {
  item: ShowcaseAdminItem;
  index: number;
  busy: boolean;
  canWrite: boolean;
  onDragEnd: () => void;
  onToggle: () => void;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const controls = useDragControls();
  const warnings = showcaseWarnings(item.byte_size, item.width, item.height);

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
      {canWrite && (
        <button
          type="button"
          aria-label="拖动排序"
          title="拖动排序"
          onPointerDown={(event) => controls.start(event)}
          className="text-muted-foreground hover:bg-background hover:text-foreground focus-visible:ring-ring/50 grid h-8 w-5 shrink-0 cursor-grab touch-none place-items-center rounded-md outline-none focus-visible:ring-2 active:cursor-grabbing"
        >
          <GripVertical className="size-4" />
        </button>
      )}
      <div className={cn(!item.enabled && "[&_img]:opacity-50 [&_video]:opacity-50")}>
        <Thumb item={item} index={index} />
      </div>
      <div className="min-w-0 flex-1">
        <p
          className={cn("line-clamp-2 text-[13.5px] leading-normal", !item.enabled && "opacity-50")}
        >
          {item.prompt}
        </p>
        <div className="text-muted-foreground mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-xs">
          {item.model_label && (
            <Tag className="group-hover/row:bg-background">{item.model_label}</Tag>
          )}
          {item.byte_size > 0 && (
            <span className="tabular-nums">{(item.byte_size / 1024 / 1024).toFixed(1)} MB</span>
          )}
          {item.start_sec > 0 && <span className="tabular-nums">从 {item.start_sec}s 开始</span>}
          {warnings.map((warning) => (
            <span key={warning} className="text-status-warning inline-flex items-center gap-1">
              <TriangleAlert className="size-3.5" />
              {WARNING_TEXT[warning]}
            </span>
          ))}
        </div>
      </div>
      <Switch
        checked={item.enabled}
        disabled={!canWrite || busy}
        title={item.enabled ? "已启用" : "未启用"}
        aria-label="启用"
        onCheckedChange={onToggle}
      />
      {canWrite && (
        <div className="flex shrink-0 gap-0.5">
          <Button variant="ghost" size="icon" aria-label="编辑" title="编辑" onClick={onEdit}>
            <Pencil />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label="移除"
            title="移除"
            className="hover:text-destructive"
            onClick={onRemove}
          >
            <Trash2 />
          </Button>
        </div>
      )}
    </Reorder.Item>
  );
}

/**
 * 作品列表（motion 的 Reorder）：拖动时只改本地顺序，松手才提交，免得拖一下发一堆请求；
 * 服务端的列表变了（新增、删除、重新拉取）就以服务端为准。
 * @param items 服务端的作品顺序
 * @param busyIds 正在保存的作品
 * @param canWrite 是否能改（super_admin）
 * @param onReorder 松手后顺序变了就提交新顺序
 */
export function ShowcaseList({
  items,
  busyIds,
  canWrite,
  onReorder,
  onToggle,
  onEdit,
  onRemove,
}: {
  items: ShowcaseAdminItem[];
  busyIds: ReadonlySet<number>;
  canWrite: boolean;
  onReorder: (items: ShowcaseAdminItem[]) => void;
  onToggle: (item: ShowcaseAdminItem) => void;
  onEdit: (item: ShowcaseAdminItem) => void;
  onRemove: (item: ShowcaseAdminItem) => void;
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
      {local.map((item, index) => (
        <ShowcaseRow
          key={item.id}
          item={item}
          index={index}
          busy={busyIds.has(item.id)}
          canWrite={canWrite}
          onDragEnd={commit}
          onToggle={() => onToggle(item)}
          onEdit={() => onEdit(item)}
          onRemove={() => onRemove(item)}
        />
      ))}
    </Reorder.Group>
  );
}

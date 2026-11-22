import { memo, useState } from "react";
import { motion, Reorder, useDragControls } from "motion/react";
import { GripVertical } from "lucide-react";

import { cn } from "@/lib/utils";
import type { StoryboardShot } from "@/types";
import { formatShotNumber } from "@/utils/storyboard/shots";

import { DurationInput } from "./duration-input";
import { DurationHint } from "./duration-hint";
import { ShotImageCell } from "./shot-image-cell";
import { ShotRowMenu } from "./shot-row-menu";
import { ShotVideoCell } from "./shot-video-cell";
import { CameraMoveSelect, ShotSizeSelect } from "./shot-select";

/**
 * 表格的列宽（设计 §3.1）：拖拽柄、镜号、分镜图、画面描述（弹性）、景别、运镜、动作/走位、对白、时长、视频、行操作。
 * 表头和行共用这一份，列才对得齐。视频列放得下「生成视频」和旁边的导入按钮（152px）。
 */
export const SHOT_GRID_CLASS =
  "grid grid-cols-[28px_48px_160px_minmax(240px,1fr)_104px_104px_200px_200px_88px_152px_40px] items-start gap-x-1";

/**
 * 横向滚动时钉在左边的三列：始终知道在改第几镜。偏移 = 前面的列宽加列间距 4px。
 * 底色用实心的弹窗底盖住滚过来的格子，悬停时再叠一层行的悬停色，和整行一致。
 */
const STICKY_BG =
  "bg-popover group-hover/row:bg-[image:linear-gradient(var(--chrome-hover),var(--chrome-hover))] group-data-[highlight=true]/row:bg-[image:linear-gradient(color-mix(in_oklab,var(--status-running)_10%,transparent),color-mix(in_oklab,var(--status-running)_10%,transparent))]";
export const STICKY_CELLS = [
  `sticky left-0 z-[1] ${STICKY_BG}`,
  `sticky left-8 z-[1] ${STICKY_BG}`,
  `sticky left-[84px] z-[1] ${STICKY_BG}`,
] as const;

/** 改一行时给出的字段 */
export type ShotPatch = Partial<Omit<StoryboardShot, "id">>;

/** 行操作的回调，按行 id 区分；由弹窗给出稳定的引用，行才能靠 memo 跳过重渲染 */
export type ShotRowActions = {
  onChange: (id: string, patch: ShotPatch) => void;
  onInsert: (id: string, side: "before" | "after") => void;
  onDuplicate: (id: string) => void;
  onRemove: (id: string) => void;
  /** 拖拽松手：把眼下的顺序提交到节点 */
  onDragEnd: () => void;
  /** 生成分镜图 / 重新生成（复用这一行已关联的图片节点） */
  onGenerateImage: (id: string) => void;
  /** 生成视频：这一行还没有视频节点就建一个（不自动开始生成） */
  onGenerateVideo: (id: string, options?: { regenerate?: boolean }) => void;
  /** 把画布上已有的视频节点挂到这一镜（分镜表后来才加、或素材不是从表里生成的时候用） */
  onImportVideo: (id: string) => void;
  /** 把画布上已有的图片节点挂到这一镜的分镜图 */
  onImportImage: (id: string) => void;
  /** 点上传：选一张图作为这一行的分镜图 */
  onPickImage: (id: string) => void;
  /** 拖进来的文件：取第一张图片上传 */
  onDropFiles: (id: string, files: Iterable<File>) => void;
  /** 移除手传的分镜图 */
  onRemoveImage: (id: string) => void;
  /** 关掉弹窗，把画布飞到这个节点 */
  onLocate: (nodeId: string) => void;
  /** 放大看这一行的分镜图原图 */
  onPreviewImage: (id: string) => void;
};

/**
 * 文本单元格：平时无边框无底色，悬停出底色、聚焦出描边（设计 §3.2）。
 * 打字只改本地，失焦才提交，50 行时每个字不会引起整张表和画布重渲染。
 * required 的格子空着失焦后标红（不拦保存）。
 */
function TextCell({
  value,
  label,
  placeholder,
  required,
  autoFocus,
  onCommit,
}: {
  value: string;
  label: string;
  placeholder: string;
  required?: boolean;
  autoFocus?: boolean;
  onCommit: (value: string) => void;
}) {
  const [text, setText] = useState(value);
  const [shown, setShown] = useState(value);
  const [touched, setTouched] = useState(false);
  // 外面改了内容（撤销等）时跟着变
  if (value !== shown) {
    setShown(value);
    setText(value);
  }
  const invalid = required && touched && !text.trim();

  return (
    <textarea
      aria-label={label}
      aria-invalid={invalid || undefined}
      rows={1}
      value={text}
      placeholder={placeholder}
      autoFocus={autoFocus}
      onChange={(e) => setText(e.target.value)}
      onBlur={() => {
        setTouched(true);
        if (text !== value) onCommit(text);
      }}
      className={cn(
        "nodrag hover:bg-chrome-hover focus:bg-background focus:ring-node-ring/60 placeholder:text-muted-foreground/70 field-sizing-content max-h-[6.5rem] min-h-9 w-full resize-none overflow-y-auto rounded-lg bg-transparent px-2 py-1.5 text-sm leading-6 outline-none focus:ring-1",
        invalid && "ring-destructive/50 placeholder:text-destructive/70 ring-1",
      )}
    />
  );
}

/**
 * 表格的一行镜头。按 shot 引用和镜号比较，改别的行时这一行不重渲染（AC13）。
 * 只能从拖拽柄拖，免得和文本框里选字冲突。
 */
export const ShotRow = memo(function ShotRow({
  shot,
  number,
  autoFocus,
  highlight,
  uploading,
  actions,
}: {
  shot: StoryboardShot;
  number: number;
  autoFocus: boolean;
  /** 这一行正在上传分镜图的进度；没在上传为 undefined */
  uploading?: number;
  /** 刚由拆分镜写进来：底色短暂高亮，标出哪些是新来的 */
  highlight?: boolean;
  actions: ShotRowActions;
}) {
  const drag = useDragControls();
  const change = (patch: ShotPatch) => actions.onChange(shot.id, patch);
  // 拖着图片文件经过这一行：整行都是命中区，分镜图格画出虚线框；非图片文件不高亮、不接收
  const [fileOver, setFileOver] = useState(false);
  const hasImageFile = (event: React.DragEvent) =>
    Array.from(event.dataTransfer.items ?? []).some(
      (item) => item.kind === "file" && item.type.startsWith("image/"),
    ) || Array.from(event.dataTransfer.files ?? []).some((f) => f.type.startsWith("image/"));

  return (
    <Reorder.Item
      value={shot}
      data-shot-id={shot.id}
      data-highlight={highlight || undefined}
      dragListener={false}
      dragControls={drag}
      onDragEnd={actions.onDragEnd}
      onDragOver={(event) => {
        if (!hasImageFile(event)) return;
        event.preventDefault();
        setFileOver(true);
      }}
      onDragLeave={(event) => {
        // 在行内子元素之间移动也会触发 leave：只在真正离开整行时才取消高亮
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setFileOver(false);
      }}
      onDrop={(event) => {
        setFileOver(false);
        if (!hasImageFile(event)) return;
        event.preventDefault();
        actions.onDropFiles(shot.id, Array.from(event.dataTransfer.files));
      }}
      whileDrag={{ scale: 1.01, boxShadow: "0 24px 48px -12px rgb(0 0 0 / 0.35)", zIndex: 10 }}
      className={cn(
        SHOT_GRID_CLASS,
        "group/row hover:bg-chrome-hover relative min-h-[72px] border-b border-border/60 px-2 py-2.5 transition-colors duration-700",
        highlight ? "bg-status-running/10" : "bg-popover",
      )}
    >
      <div className={cn(STICKY_CELLS[0], "flex h-[81px] items-center")}>
        <button
          type="button"
          aria-label={`拖动第 ${number} 镜排序`}
          onPointerDown={(e) => drag.start(e)}
          className="nodrag text-muted-foreground cursor-grab touch-none opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 active:cursor-grabbing"
        >
          <GripVertical className="size-4" />
        </button>
      </div>
      <div className={cn(STICKY_CELLS[1], "flex h-[81px] items-center justify-center")}>
        {/* 镜号变了淡入一下，让人看到「重新编号了」 */}
        <motion.span
          key={number}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.15 }}
          className="text-sm font-semibold tabular-nums"
        >
          {formatShotNumber(number)}
        </motion.span>
      </div>
      <div className={STICKY_CELLS[2]}>
        <ShotImageCell
          shot={shot}
          number={number}
          uploading={uploading}
          dragging={fileOver}
          onGenerate={() => actions.onGenerateImage(shot.id)}
          onUpload={() => actions.onPickImage(shot.id)}
          onImport={() => actions.onImportImage(shot.id)}
          onRemove={() => actions.onRemoveImage(shot.id)}
          onLocate={actions.onLocate}
          onPreview={() => actions.onPreviewImage(shot.id)}
        />
      </div>
      <TextCell
        label={`第 ${number} 镜的画面描述`}
        value={shot.scene}
        placeholder="必填"
        required
        autoFocus={autoFocus}
        onCommit={(scene) => change({ scene })}
      />
      <ShotSizeSelect value={shot.shot} onChange={(value) => change({ shot: value })} />
      <CameraMoveSelect value={shot.camera} onChange={(value) => change({ camera: value })} />
      <TextCell
        label={`第 ${number} 镜的动作和走位`}
        value={shot.action}
        placeholder="角色动作、走位、朝向"
        onCommit={(action) => change({ action })}
      />
      <TextCell
        label={`第 ${number} 镜的对白`}
        value={shot.dialogue}
        placeholder="台词或旁白"
        onCommit={(dialogue) => change({ dialogue })}
      />
      <div className="flex flex-col">
        <DurationInput value={shot.duration} onChange={(duration) => change({ duration })} />
        <DurationHint shot={shot} />
      </div>
      <ShotVideoCell
        shot={shot}
        onGenerate={() => actions.onGenerateVideo(shot.id)}
        onImport={() => actions.onImportVideo(shot.id)}
        onLocate={actions.onLocate}
      />
      <div className="flex h-9 items-center justify-center">
        <ShotRowMenu
          number={number}
          onInsert={(side) => actions.onInsert(shot.id, side)}
          onDuplicate={() => actions.onDuplicate(shot.id)}
          onRemove={() => actions.onRemove(shot.id)}
          onGenerateImage={() => actions.onGenerateImage(shot.id)}
          hasImageNode={!!shot.imageNodeId}
          onGenerateVideo={() =>
            actions.onGenerateVideo(shot.id, { regenerate: !!shot.videoNodeId })
          }
          hasVideoNode={!!shot.videoNodeId}
          onImportVideo={() => actions.onImportVideo(shot.id)}
          onImportImage={() => actions.onImportImage(shot.id)}
        />
      </div>
    </Reorder.Item>
  );
});

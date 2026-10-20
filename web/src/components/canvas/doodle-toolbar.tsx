import { useState } from "react";
import { Pencil, Redo2, Square, Type, Undo2, X } from "lucide-react";

import { ChromeButton, ChromePill, ChromeSeparator } from "@/components/canvas/chrome/chrome";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Slider } from "@/components/ui/slider";
import { cn } from "@/lib/utils";
import { DOODLE_COLORS, DOODLE_WIDTH_MAX, DOODLE_WIDTH_MIN } from "@/utils/canvas/doodle";
import type { DoodleTool } from "@/utils/canvas/doodle-engine";

/** 三个工具的图标和名字，按工具栏上的顺序排 */
const TOOLS: Array<{ id: DoodleTool; label: string; icon: typeof Pencil }> = [
  { id: "pen", label: "画笔", icon: Pencil },
  { id: "rect", label: "框选", icon: Square },
  { id: "text", label: "文字", icon: Type },
];

/** 工具按钮：图标加文字，选中时底色加深；窄屏只留图标 */
const TOOL_BUTTON_CLASS = "text-foreground px-2.5 max-md:px-2 data-active:bg-chrome-hover";

/**
 * 涂鸦标注工具栏（设计稿 5.1）：浮在节点上方的一条胶囊，从左到右是
 * 「× 标注」关闭、画笔 / 框选 / 文字、颜色、粗细、撤销、重做、保存。
 * 只管长相和回调，画布与状态在 DoodleOverlay。点工具栏时先通知外面结束正在编辑的文字。
 * @param tool 当前工具
 * @param onToolChange 换工具
 * @param color 当前颜色
 * @param onColorChange 换颜色
 * @param width 当前粗细（屏幕粗细）
 * @param onWidthChange 调粗细
 * @param canUndo 有没有可撤销的；没有时撤销按钮置灰
 * @param canRedo 有没有可重做的；没有时重做按钮置灰
 * @param onUndo 撤销
 * @param onRedo 重做
 * @param onSave 保存
 * @param onClose 关闭（放弃涂鸦）
 * @param onInteract 按下工具栏上任何地方时调用：外面据此结束正在编辑的文字
 * @param saveDisabled 保存按钮置灰（画布还没加载好）
 */
export function DoodleToolbar({
  tool,
  onToolChange,
  color,
  onColorChange,
  width,
  onWidthChange,
  canUndo,
  canRedo,
  onUndo,
  onRedo,
  onSave,
  onClose,
  onInteract,
  saveDisabled,
}: {
  tool: DoodleTool;
  onToolChange: (tool: DoodleTool) => void;
  color: string;
  onColorChange: (color: string) => void;
  width: number;
  onWidthChange: (width: number) => void;
  canUndo: boolean;
  canRedo: boolean;
  onUndo: () => void;
  onRedo: () => void;
  onSave: () => void;
  onClose: () => void;
  onInteract: () => void;
  saveDisabled?: boolean;
}) {
  const [paletteOpen, setPaletteOpen] = useState(false);
  const undoTitle = canUndo ? "撤销" : "没有可撤销的操作";
  const redoTitle = canRedo ? "重做" : "没有可重做的操作";

  return (
    <ChromePill
      role="toolbar"
      aria-label="图片标注工具栏"
      className="nodrag nopan max-w-[calc(100vw-1.5rem)] gap-0.5"
      onPointerDownCapture={onInteract}
    >
      <ChromeButton
        aria-label="关闭标注"
        title="关闭标注（不保存）"
        className="text-foreground px-2.5 max-md:px-2"
        onClick={onClose}
      >
        <X />
        <span className="max-md:hidden">标注</span>
      </ChromeButton>
      <ChromeSeparator />
      {TOOLS.map((item) => (
        <ChromeButton
          key={item.id}
          aria-label={item.label}
          aria-pressed={tool === item.id}
          title={item.label}
          active={tool === item.id}
          className={TOOL_BUTTON_CLASS}
          onClick={() => onToolChange(item.id)}
        >
          <item.icon />
          <span className="max-md:hidden">{item.label}</span>
        </ChromeButton>
      ))}
      <ChromeSeparator />
      <Popover open={paletteOpen} onOpenChange={setPaletteOpen}>
        <PopoverTrigger
          render={
            <ChromeButton aria-label="颜色" title="颜色" className="text-foreground px-2.5" />
          }
        >
          <span
            className="ring-foreground/25 size-4 rounded-full ring-1"
            style={{ backgroundColor: color }}
          />
        </PopoverTrigger>
        <PopoverContent
          side="bottom"
          align="center"
          sideOffset={8}
          className="w-auto flex-row gap-1.5 rounded-xl p-2"
        >
          {DOODLE_COLORS.map((item) => (
            <button
              key={item}
              type="button"
              aria-label={`颜色 ${item}`}
              aria-pressed={color === item}
              onClick={() => {
                onColorChange(item);
                setPaletteOpen(false);
              }}
              className={cn(
                "ring-foreground/20 focus-visible:ring-node-ring/70 size-6 rounded-full ring-1 outline-none focus-visible:ring-2",
                color === item && "ring-foreground ring-2 ring-offset-2 ring-offset-popover",
              )}
              style={{ backgroundColor: item }}
            />
          ))}
        </PopoverContent>
      </Popover>
      <div className="flex w-24 items-center px-2 max-md:w-16" title="粗细">
        <Slider
          aria-label="粗细"
          min={DOODLE_WIDTH_MIN}
          max={DOODLE_WIDTH_MAX}
          step={1}
          value={[width]}
          onValueChange={(next) => onWidthChange(Array.isArray(next) ? next[0] : next)}
        />
      </div>
      <ChromeSeparator />
      <span title={undoTitle} className="inline-flex">
        <ChromeButton aria-label="撤销" disabled={!canUndo} onClick={onUndo}>
          <Undo2 />
        </ChromeButton>
      </span>
      <span title={redoTitle} className="inline-flex">
        <ChromeButton aria-label="重做" disabled={!canRedo} onClick={onRedo}>
          <Redo2 />
        </ChromeButton>
      </span>
      <ChromeSeparator />
      <ChromeButton
        aria-label="保存"
        title="保存为新的图片节点"
        disabled={saveDisabled}
        className="bg-foreground text-background hover:bg-foreground/90 hover:text-background px-3.5 font-medium"
        onClick={onSave}
      >
        保存
      </ChromeButton>
    </ChromePill>
  );
}

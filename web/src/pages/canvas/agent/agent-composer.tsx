import type { Ref } from "react";
import { motion } from "motion/react";
import { ArrowUp, Check, ChevronDown, Hand, Square, X } from "lucide-react";
import { toast } from "sonner";

import type { AgentMode } from "@/api/agent/type";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { Chip } from "@/utils/agent/chips";

import { AgentEditor, type AgentEditorHandle, type NodeOption } from "./agent-editor";
import { AttachPopover } from "./attach-popover";
import { ModelPopover } from "./model-popover";
import { SkillPopover } from "./skill-popover";
import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { AGENT_MODES } from "@/constants/agent";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";

/** 任务模式下拉：向上弹出，每项有名称和一句说明 */
function ModeMenu({
  mode,
  onChange,
  disabled,
}: {
  mode: AgentMode;
  onChange: (m: AgentMode) => void;
  disabled: boolean;
}) {
  const current = AGENT_MODES.find((m) => m.value === mode) ?? AGENT_MODES[0];
  return (
    <Popover>
      <PopoverTrigger
        disabled={disabled}
        className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground data-popup-open:bg-chrome-hover focus-visible:ring-node-ring/60 flex h-8 items-center gap-1 rounded-lg px-2 text-xs outline-none focus-visible:ring-2 disabled:opacity-40"
      >
        {current.label}
        <ChevronDown className="size-3.5" />
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={8}
        className="w-[min(280px,calc(100vw-24px))] origin-bottom-left gap-0.5 p-1.5"
      >
        {AGENT_MODES.map((m) => (
          <button
            key={m.value}
            type="button"
            onClick={() => onChange(m.value)}
            className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex items-start gap-2 rounded-lg px-2.5 py-2 text-left outline-none focus-visible:ring-2"
          >
            <span className="min-w-0 flex-1">
              <span className="block text-[13px] font-medium">{m.label}</span>
              <span className="text-muted-foreground block text-xs leading-snug">{m.hint}</span>
            </span>
            {m.value === mode && <Check className="mt-0.5 size-4 shrink-0" />}
          </button>
        ))}
      </PopoverContent>
    </Popover>
  );
}

/**
 * 输入区：一整块大圆角容器。上方是上下文行（选中了几个节点），中间是编辑区，底部是模式、审批开关和发送键。
 * 运行中：编辑区为空时发送键是「停止」，有内容时是「插话」。
 */
export function AgentComposer({
  text,
  onTextChange,
  onSend,
  onStop,
  busy,
  sending,
  mode,
  onModeChange,
  selectionCount,
  useSelection,
  onToggleSelection,
  editorRef,
  nodes,
  onAttachImages,
}: {
  /** 编辑器里现在的消息文本 */
  text: string;
  onTextChange: (text: string) => void;
  onSend: () => void;
  onStop: () => void;
  /** Agent 正在运行（或在等你） */
  busy: boolean;
  /** 正在发出请求 */
  sending: boolean;
  mode: AgentMode;
  onModeChange: (mode: AgentMode) => void;
  /** 画布上选中的节点数 */
  selectionCount: number;
  /** 发送时是否带上选中的节点 */
  useSelection: boolean;
  onToggleSelection: () => void;
  /** 编辑器的操作：面板用它预填文字，弹层用它插入 chip */
  editorRef: Ref<AgentEditorHandle>;
  /** 画布上能用 @ 引用的节点 */
  nodes: NodeOption[];
  /** 把图片传到画布并引用：返回新建节点的 chip */
  onAttachImages: (files: File[]) => Promise<Chip[]>;
}) {
  const handle = editorRef as { current: AgentEditorHandle | null };
  const empty = text.trim() === "";
  const stopMode = busy && empty;

  const sendLabel = stopMode ? "停止" : busy ? "插话" : "发送";
  return (
    <div className="ring-foreground/12 focus-within:ring-foreground/24 bg-foreground/3 mx-3 mb-3 flex flex-col gap-1.5 rounded-2xl p-2.5 ring-1 transition-shadow duration-120">
      {selectionCount > 0 && useSelection && (
        <div className="flex">
          <span className="bg-foreground/7 ring-chrome-border inline-flex h-6 items-center gap-1 rounded-md pr-1 pl-2 text-xs ring-1">
            已选中 {selectionCount} 个节点
            <button
              type="button"
              aria-label="不带选中的节点"
              onClick={onToggleSelection}
              className="hover:bg-foreground/10 focus-visible:ring-node-ring/60 rounded p-0.5 outline-none focus-visible:ring-2"
            >
              <X className="size-3" />
            </button>
          </span>
        </div>
      )}
      <AgentEditor
        handleRef={editorRef}
        nodes={nodes}
        placeholder={
          busy ? "补充要求，Agent 会在下一步看到" : "描述你的想法，@ 引用节点，或插入模型、技能"
        }
        onChange={onTextChange}
        onSubmit={() => !empty && !sending && onSend()}
      />
      <div className="flex items-center gap-1">
        <AttachPopover
          onInsertText={(t) => handle.current?.insertText(t)}
          onAttachImages={async (files) => {
            for (const chip of await onAttachImages(files)) handle.current?.insertChip(chip);
          }}
        />
        <ModeMenu mode={mode} onChange={onModeChange} disabled={busy} />
        <ModelPopover onPick={(chip) => handle.current?.insertChip(chip)} />
        <SkillPopover onPick={(chip) => handle.current?.insertChip(chip)} />
        <ChromeTooltip label="生成前需要你确认（开）· 自动生成（关）二期开放" side="top">
          <button
            type="button"
            aria-label="生成前需要确认：开"
            aria-pressed
            onClick={() => toast.info("目前生成一律需要你确认，自动生成二期开放")}
            className="bg-foreground/10 text-foreground focus-visible:ring-node-ring/60 grid size-8 place-items-center rounded-lg outline-none focus-visible:ring-2"
          >
            <Hand className="size-4" />
          </button>
        </ChromeTooltip>
        <span className="flex-1" />
        <ChromeTooltip label={sendLabel} shortcut={stopMode ? undefined : "Enter"} side="top">
          <motion.button
            type="button"
            aria-label={sendLabel}
            disabled={!stopMode && (empty || sending)}
            whileTap={TAP}
            onClick={stopMode ? onStop : onSend}
            className={cn(
              "focus-visible:ring-node-ring/60 grid size-9 place-items-center rounded-full outline-none transition-[background-color,color] duration-120 focus-visible:ring-2",
              !stopMode && empty
                ? "bg-foreground/10 text-muted-foreground cursor-not-allowed"
                : "bg-primary text-primary-foreground",
            )}
          >
            {stopMode ? (
              <Square className="size-3.5 fill-current" />
            ) : (
              <ArrowUp className="size-4.5" />
            )}
          </motion.button>
        </ChromeTooltip>
      </div>
    </div>
  );
}

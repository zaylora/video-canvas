import { useLayoutEffect, type KeyboardEvent, type RefObject } from "react";
import { motion } from "motion/react";
import { ArrowUp, Check, ChevronDown, Hand, Square, X } from "lucide-react";
import { toast } from "sonner";

import type { AgentMode } from "@/api/agent/type";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { AGENT_MODES } from "@/constants/agent";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";

/** 编辑区高度范围（像素） */
const MIN_H = 60;
const MAX_H = 140;

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
  value,
  onChange,
  onSend,
  onStop,
  busy,
  sending,
  mode,
  onModeChange,
  selectionCount,
  useSelection,
  onToggleSelection,
  inputRef,
}: {
  value: string;
  onChange: (value: string) => void;
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
  /** 编辑区，面板用它在引导项预填文字后把光标放回去 */
  inputRef: RefObject<HTMLTextAreaElement | null>;
}) {
  const ref = inputRef;
  const empty = value.trim() === "";
  const stopMode = busy && empty;

  // 随内容长高，超过上限后内部滚动
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(MAX_H, Math.max(MIN_H, el.scrollHeight))}px`;
  }, [ref, value]);

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // 输入法选字时的回车不算发送
    if (e.key !== "Enter" || e.shiftKey || e.nativeEvent.isComposing) return;
    e.preventDefault();
    if (!empty && !sending) onSend();
  };

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
      <textarea
        ref={ref}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
        rows={1}
        maxLength={20000}
        placeholder={
          busy ? "补充要求，Agent 会在下一步看到" : "描述你的想法，Enter 发送，Shift+Enter 换行"
        }
        aria-label="给 Agent 的消息"
        className="placeholder:text-muted-foreground/70 w-full resize-none bg-transparent px-1 text-[13.5px] leading-[1.75] outline-none"
        style={{ minHeight: MIN_H, maxHeight: MAX_H }}
      />
      <div className="flex items-center gap-1">
        <ModeMenu mode={mode} onChange={onModeChange} disabled={busy} />
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

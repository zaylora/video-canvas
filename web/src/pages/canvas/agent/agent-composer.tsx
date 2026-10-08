import type { Ref } from "react";
import { motion } from "motion/react";
import { ArrowUp, Bot, Check, ChevronDown, Square, X } from "lucide-react";

import type { AgentMode, AgentModelDto } from "@/api/agent/type";
import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { AGENT_MODES } from "@/constants/agent";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentSettings } from "@/store/agent-settings";
import type { Chip } from "@/utils/agent/chips";

import { AgentEditor, type AgentEditorHandle, type NodeOption } from "./agent-editor";
import { AttachPopover } from "./attach-popover";
import { BudgetRing } from "./budget-ring";
import { ModelPopover } from "./model-popover";
import { SkillPopover } from "./skill-popover";
import { AGENT_BAR_BTN, AGENT_POP } from "./styles";

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
      <PopoverTrigger disabled={disabled} className={AGENT_BAR_BTN}>
        {current.label}
        <ChevronDown className="size-3.5" />
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={8}
        className={cn(
          AGENT_POP,
          "w-[min(280px,calc(100vw-24px))] origin-bottom-left gap-0.5 p-1.5",
        )}
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

/** Agent 大语言模型：底栏右侧，向上弹出单选；运行中不能换。浮窗太窄时只显示图标 */
function AgentModelMenu({
  models,
  modelKey,
  busy,
}: {
  models: AgentModelDto[];
  modelKey: string;
  busy: boolean;
}) {
  const patch = useAgentSettings((s) => s.patch);
  const current = models.find((m) => m.key === modelKey);
  return (
    <Popover>
      <ChromeTooltip label={busy ? "运行中不能换模型" : "Agent 模型"}>
        <PopoverTrigger
          disabled={busy}
          aria-label="Agent 模型"
          className={cn(AGENT_BAR_BTN, "min-w-0 shrink")}
        >
          <Bot className="size-4 shrink-0" />
          <span className="max-w-24 truncate @max-[360px]:hidden">{current?.name ?? "未选"}</span>
          <ChevronDown className="size-3.5 shrink-0" />
        </PopoverTrigger>
      </ChromeTooltip>
      <PopoverContent
        side="top"
        align="end"
        sideOffset={8}
        className={cn(
          AGENT_POP,
          "w-[min(260px,calc(100vw-24px))] origin-bottom-right gap-0.5 p-1.5",
        )}
      >
        <p className="text-muted-foreground px-2.5 pt-1 pb-0.5 text-xs">Agent 模型</p>
        <div role="radiogroup" aria-label="Agent 模型" className="flex flex-col">
          {models.map((m) => (
            <button
              key={m.key}
              type="button"
              role="radio"
              aria-checked={m.key === modelKey}
              onClick={() => patch({ modelKey: m.key })}
              className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[13px] outline-none focus-visible:ring-2"
            >
              <span className="flex-1 truncate">{m.name}</span>
              {!m.vision && <span className="text-muted-foreground text-[11px]">不能看图</span>}
              {m.key === modelKey && <Check className="size-4" />}
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}

/**
 * 输入区（设计稿 6.8）：上方是上下文行（选中了几个节点），中间是编辑区；
 * 底栏左侧 ＋ 附件、模式、插入模型、插入技能，右侧 Agent 模型、预算环、发送。
 * 运行中：编辑区为空时发送键是「停止」，有内容时是「插话」。外层容器（大圆角、聚焦光晕）由浮窗给。
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
  models,
  modelKey,
  usage,
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
  /** 可选的 Agent 模型和当前选中的 */
  models: AgentModelDto[];
  modelKey: string;
  /** 本轮已花和预算 */
  usage: { spent: number; budget: number };
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
  const ready = !stopMode && !empty;

  const sendLabel = stopMode ? "停止" : busy ? "插话" : "发送";
  return (
    <div className="flex flex-col gap-1.5 p-2.5">
      {selectionCount > 0 && useSelection && (
        <div className="flex">
          <span className="agent-inset bg-foreground/6 inline-flex h-6 items-center gap-1.5 rounded-md pr-1 pl-2 text-xs">
            <span aria-hidden className="bg-preset size-1.5 rounded-full" />
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
      <div className="flex min-w-0 items-center gap-0.5">
        <AttachPopover
          onInsertText={(t) => handle.current?.insertText(t)}
          onAttachImages={async (files) => {
            for (const chip of await onAttachImages(files)) handle.current?.insertChip(chip);
          }}
        />
        <ModeMenu mode={mode} onChange={onModeChange} disabled={busy} />
        <ModelPopover onPick={(chip) => handle.current?.insertChip(chip)} />
        <SkillPopover onPick={(chip) => handle.current?.insertChip(chip)} />
        <span className="min-w-1 flex-1" />
        <AgentModelMenu models={models} modelKey={modelKey} busy={busy} />
        <BudgetRing spent={usage.spent} budget={usage.budget} busy={busy} />
        <ChromeTooltip label={sendLabel} shortcut={stopMode ? "Esc" : "Enter"} side="top">
          <motion.button
            type="button"
            aria-label={sendLabel}
            disabled={!stopMode && (empty || sending)}
            whileTap={TAP}
            onClick={stopMode ? onStop : onSend}
            className={cn(
              "focus-visible:ring-node-ring/60 ml-0.5 grid size-9 shrink-0 place-items-center rounded-full outline-none transition-[background-color,color,box-shadow] duration-120 focus-visible:ring-2",
              ready
                ? "agent-send"
                : stopMode
                  ? "bg-foreground/12 text-foreground hover:bg-foreground/16"
                  : "bg-foreground/10 text-muted-foreground cursor-not-allowed",
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

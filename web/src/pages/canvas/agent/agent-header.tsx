import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import {
  Check,
  ChevronDown,
  Minus,
  PanelRight,
  PictureInPicture2,
  Settings,
  SquarePen,
  Trash2,
} from "lucide-react";

import type { AgentSessionDto } from "@/api/agent/type";
import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import type { AgentController } from "@/hooks/use-agent-controller";
import { cn } from "@/lib/utils";
import { useAgentSettings } from "@/store/agent-settings";

import { AGENT_ICON_BTN as ICON_BTN, AGENT_POP } from "./styles";

/**
 * 会话标题即历史入口（设计稿 6.12，原为单独的「历史会话」图标）：
 * 单击向下弹出历史会话；双击原地改名，Enter 或失焦提交，Esc 取消。
 * 历史里当前会话打勾；运行中不能切换到别的会话；删除要点两次。
 */
function TitleHistory({ ctl, title }: { ctl: AgentController; title: string }) {
  const [open, setOpen] = useState(false);
  const [confirming, setConfirming] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(title);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (editing) input.current?.select();
  }, [editing]);
  const commit = () => {
    setEditing(false);
    if (draft.trim() && draft.trim() !== title) void ctl.rename(draft.trim());
  };
  if (editing) {
    return (
      <Input
        ref={input}
        value={draft}
        maxLength={40}
        aria-label="会话标题"
        className="h-7 min-w-0 flex-1 text-sm"
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") commit();
          if (e.key === "Escape") setEditing(false);
        }}
      />
    );
  }
  return (
    <Popover
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        setConfirming(null);
      }}
    >
      <ChromeTooltip label="历史会话 · 双击改名" side="bottom">
        <PopoverTrigger
          aria-label={`历史会话，当前：${title}`}
          onDoubleClick={() => {
            setOpen(false);
            setDraft(title);
            setEditing(true);
          }}
          className="hover:bg-chrome-hover data-popup-open:bg-chrome-hover focus-visible:ring-node-ring/60 -ml-1.5 flex h-8 max-w-full min-w-0 items-center gap-1 rounded-lg px-1.5 outline-none transition-colors duration-120 focus-visible:ring-2"
        >
          <h2 className="min-w-0 truncate text-sm font-semibold tracking-[-0.01em]">{title}</h2>
          <ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
        </PopoverTrigger>
      </ChromeTooltip>
      <PopoverContent
        side="bottom"
        align="start"
        className={cn(AGENT_POP, "w-[min(290px,calc(100vw-24px))] origin-top-left gap-0.5 p-1.5")}
      >
        <p className="text-muted-foreground px-2.5 pt-1 pb-0.5 text-xs">历史会话</p>
        {ctl.sessions.length === 0 && (
          <p className="text-muted-foreground px-2.5 py-3 text-center text-xs">还没有会话</p>
        )}
        {ctl.sessions.map((s: AgentSessionDto) => {
          const current = s.id === ctl.session?.id;
          const locked = ctl.busy && !current;
          return (
            <div key={s.id} className="group flex items-center gap-1">
              <button
                type="button"
                disabled={locked}
                title={locked ? "运行中不能切换会话" : undefined}
                onClick={() => {
                  setOpen(false);
                  ctl.openSession(s);
                }}
                className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex min-w-0 flex-1 items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] outline-none focus-visible:ring-2 disabled:opacity-40"
              >
                <span className="min-w-0 flex-1 truncate">{s.title}</span>
                {current && <Check className="size-4 shrink-0" />}
              </button>
              <button
                type="button"
                aria-label={confirming === s.id ? `确认删除「${s.title}」` : `删除「${s.title}」`}
                disabled={locked || (current && ctl.busy)}
                onClick={() => {
                  if (confirming !== s.id) return setConfirming(s.id);
                  setConfirming(null);
                  void ctl.remove(s);
                }}
                className={cn(
                  "text-muted-foreground hover:text-destructive focus-visible:ring-node-ring/60 flex h-8 shrink-0 items-center gap-1 rounded-lg px-2 text-xs outline-none focus-visible:ring-2 disabled:opacity-30",
                  confirming === s.id && "text-destructive",
                )}
              >
                <Trash2 className="size-3.5" />
                {confirming === s.id && "删除?"}
              </button>
            </div>
          );
        })}
      </PopoverContent>
    </Popover>
  );
}

/** 设置：显示思考过程、生成前确认（固定开）。Agent 模型和上下文用量在输入框右下角 */
function SettingsPopover() {
  const settings = useAgentSettings();
  return (
    <Popover>
      <ChromeTooltip label="设置" side="bottom">
        <PopoverTrigger aria-label="Agent 设置" className={ICON_BTN}>
          <Settings />
        </PopoverTrigger>
      </ChromeTooltip>
      <PopoverContent
        side="bottom"
        align="end"
        className={cn(AGENT_POP, "w-[min(260px,calc(100vw-24px))] origin-top-right gap-3 p-3")}
      >
        <section className="flex items-center justify-between">
          <label htmlFor="agent-thinking" className="text-[13px]">
            显示思考过程
          </label>
          <Switch
            id="agent-thinking"
            size="sm"
            checked={settings.showThinking}
            onCheckedChange={(v) => settings.patch({ showThinking: v })}
          />
        </section>
        <section className="flex items-center justify-between">
          <span className="text-[13px]">生成前确认</span>
          <span className="text-muted-foreground text-xs">固定开</span>
        </section>
        <p className="text-muted-foreground text-[11.5px]">Agent 模型和上下文用量在输入框右下角</p>
      </PopoverContent>
    </Popover>
  );
}

/**
 * 顶栏：品牌标识 + 标题（即历史入口）+ 新对话 / 停靠 / 设置 / 收起。
 * 浮窗时拖顶栏移动（按在按钮或输入框上不触发拖动）；消息流滚离顶部后，下方出现一条两端渐隐的分隔线。
 */
export function AgentHeader({
  ctl,
  title,
  docked,
  canDock,
  scrolled,
  onToggleDock,
  onClose,
  onDragStart,
}: {
  ctl: AgentController;
  title: string;
  /** 现在是否停靠在右侧 */
  docked: boolean;
  /** 能不能停靠：null 不显示按钮（窄屏）；false 是窗口太窄，按钮变淡并说明原因 */
  canDock: boolean | null;
  /** 消息流已滚离顶部 */
  scrolled: boolean;
  onToggleDock: () => void;
  onClose: () => void;
  /** 在顶栏空白处按下：开始拖动；停靠和窄屏不传 */
  onDragStart?: (e: ReactPointerEvent) => void;
}) {
  const dockLabel = docked ? "改为浮窗" : canDock ? "停靠到右侧" : "窗口太窄，无法停靠";
  return (
    <header
      className={cn(
        "relative flex h-[52px] shrink-0 items-center gap-0.5 pr-3 pl-3.5",
        onDragStart && "cursor-grab active:cursor-grabbing",
      )}
      onPointerDown={(e) => {
        if (onDragStart && !(e.target as HTMLElement).closest("button, input")) onDragStart(e);
      }}
    >
      <span
        aria-hidden
        className="agent-orb mr-2 size-5 shrink-0 rounded-[7px] shadow-[inset_0_1px_0_var(--sheen),0_0_0_1px_var(--chrome-border),0_2px_8px_color-mix(in_oklab,var(--beam)_35%,transparent)]"
      />
      <div className="flex min-w-0 flex-1">
        <TitleHistory ctl={ctl} title={title} />
      </div>
      <ChromeTooltip label={ctl.busy ? "运行中不能新建对话" : "新对话"} side="bottom">
        <button
          type="button"
          aria-label="新对话"
          disabled={ctl.busy}
          onClick={ctl.newSession}
          className={ICON_BTN}
        >
          <SquarePen />
        </button>
      </ChromeTooltip>
      {canDock !== null && (
        <ChromeTooltip label={dockLabel} side="bottom">
          <button
            type="button"
            aria-label={dockLabel}
            aria-pressed={docked}
            // 不用 disabled：禁用的按钮不触发 Tooltip，用户就看不到为什么不能点
            aria-disabled={!docked && !canDock}
            onClick={() => (docked || canDock) && onToggleDock()}
            className={cn(ICON_BTN, !docked && !canDock && "opacity-40")}
          >
            {docked ? <PictureInPicture2 /> : <PanelRight />}
          </button>
        </ChromeTooltip>
      )}
      <SettingsPopover />
      <ChromeTooltip label="收起" shortcut="⌘/" side="bottom">
        <button type="button" aria-label="收起 Agent" onClick={onClose} className={ICON_BTN}>
          <Minus />
        </button>
      </ChromeTooltip>
      <span
        aria-hidden
        className={cn(
          "via-chrome-border absolute inset-x-4 bottom-0 h-px bg-linear-to-r from-transparent to-transparent transition-opacity duration-120",
          scrolled ? "opacity-100" : "opacity-0",
        )}
      />
    </header>
  );
}

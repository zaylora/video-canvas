import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { Check, History, Minus, Settings, SquarePen, Trash2 } from "lucide-react";

import type { AgentModelDto, AgentSessionDto } from "@/api/agent/type";
import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import type { AgentController } from "@/hooks/use-agent-controller";
import { DEFAULT_BUDGET, useAgentSettings } from "@/store/agent-settings";
import { cn } from "@/lib/utils";

const ICON_BTN =
  "text-muted-foreground hover:bg-chrome-hover hover:text-foreground data-popup-open:bg-foreground/10 focus-visible:ring-node-ring/60 grid size-8 shrink-0 place-items-center rounded-lg outline-none transition-colors duration-120 focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-40 [&_svg]:size-[17px]";

/** 会话标题：双击原地改名，Enter 或失焦提交，Esc 取消 */
function Title({ title, onRename }: { title: string; onRename: (t: string) => void }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(title);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (editing) input.current?.select();
  }, [editing]);
  const commit = () => {
    setEditing(false);
    if (draft.trim() && draft.trim() !== title) onRename(draft.trim());
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
    <h2
      className="min-w-0 flex-1 truncate text-[14.5px] font-semibold"
      title="双击改名"
      onDoubleClick={() => {
        setDraft(title);
        setEditing(true);
      }}
    >
      {title}
    </h2>
  );
}

/** 历史：当前会话打勾；运行中不能切换到别的会话；删除要点两次 */
function HistoryPopover({ ctl }: { ctl: AgentController }) {
  const [confirming, setConfirming] = useState<string | null>(null);
  return (
    <Popover onOpenChange={() => setConfirming(null)}>
      <ChromeTooltip label="历史会话" side="bottom">
        <PopoverTrigger aria-label="历史会话" className={ICON_BTN}>
          <History />
        </PopoverTrigger>
      </ChromeTooltip>
      <PopoverContent
        side="bottom"
        align="end"
        className="w-[min(290px,calc(100vw-24px))] origin-top-right gap-0.5 p-1.5"
      >
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
                onClick={() => ctl.openSession(s)}
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

/** 设置：Agent 模型、本轮预算（运行中不能改）、显示思考过程、生成前确认（固定开） */
function SettingsPopover({ models, ctl }: { models: AgentModelDto[]; ctl: AgentController }) {
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
        className="w-[min(290px,calc(100vw-24px))] origin-top-right gap-3 p-3"
      >
        <section className="flex flex-col gap-1">
          <h3 className="text-muted-foreground text-xs">Agent 模型</h3>
          <div role="radiogroup" className="flex flex-col">
            {models.map((m) => (
              <button
                key={m.key}
                type="button"
                role="radio"
                aria-checked={m.key === ctl.modelKey}
                disabled={ctl.busy}
                onClick={() => settings.patch({ modelKey: m.key })}
                className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex items-center gap-2 rounded-lg px-2 py-1.5 text-left text-[13px] outline-none focus-visible:ring-2 disabled:opacity-50"
              >
                <span className="flex-1 truncate">{m.name}</span>
                {!m.vision && <span className="text-muted-foreground text-[11px]">不能看图</span>}
                {m.key === ctl.modelKey && <Check className="size-4" />}
              </button>
            ))}
          </div>
        </section>
        <section className="flex items-center justify-between gap-3">
          <label htmlFor="agent-budget" className="text-[13px]">
            本轮预算
            <span className="text-muted-foreground block text-[11px]">
              {ctl.busy ? "运行中不能改" : "积分，用完会暂停"}
            </span>
          </label>
          <Input
            id="agent-budget"
            type="number"
            min={0}
            max={100000}
            disabled={ctl.busy}
            value={settings.budget}
            onChange={(e) =>
              settings.patch({
                budget: Math.max(0, Math.min(100000, Math.round(Number(e.target.value) || 0))),
              })
            }
            onBlur={() => settings.budget === 0 && settings.patch({ budget: DEFAULT_BUDGET })}
            className="h-8 w-24 text-right tabular-nums"
          />
        </section>
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
      </PopoverContent>
    </Popover>
  );
}

/** 顶栏：标题 + 新对话 / 历史 / 设置 / 最小化。桌面上拖顶栏移动浮窗（按在按钮或输入框上不触发拖动） */
export function AgentHeader({
  ctl,
  models,
  title,
  onClose,
  onDragStart,
}: {
  ctl: AgentController;
  models: AgentModelDto[];
  title: string;
  onClose: () => void;
  /** 在顶栏空白处按下：开始拖动；窄屏不传 */
  onDragStart?: (e: ReactPointerEvent) => void;
}) {
  return (
    <header
      className={cn(
        "flex h-[52px] shrink-0 items-center gap-1 px-4 pr-3",
        onDragStart && "cursor-grab active:cursor-grabbing",
      )}
      onPointerDown={(e) => {
        if (onDragStart && !(e.target as HTMLElement).closest("button, input")) onDragStart(e);
      }}
    >
      <Title title={title} onRename={(t) => void ctl.rename(t)} />
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
      <HistoryPopover ctl={ctl} />
      <SettingsPopover models={models} ctl={ctl} />
      <ChromeTooltip label="收起" shortcut="⌘/" side="bottom">
        <button type="button" aria-label="收起 Agent" onClick={onClose} className={ICON_BTN}>
          <Minus />
        </button>
      </ChromeTooltip>
    </header>
  );
}

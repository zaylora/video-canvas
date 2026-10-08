import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { motion } from "motion/react";
import { ChevronRight, MessageCircleQuestion, Sparkles, Trash2 } from "lucide-react";

import type {
  AgentApprovalDto,
  AgentAskPayload,
  AgentDeletePayload,
  AgentGeneratePayload,
  DecideAgentApprovalReq,
} from "@/api/agent/type";
import { Checkbox } from "@/components/ui/checkbox";
import type { AgentController } from "@/hooks/use-agent-controller";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import { useCreditsStore } from "@/store/credits";

import { approvalTitle } from "./approval-row";

/** 决定框里的一个编号选项 */
type Option = {
  key: string;
  label: string;
  /** 次要说明（选模型时的简介） */
  sub?: string;
  /** 右侧的积分 */
  meta?: string;
  /** 不能选时，下方给出原因 */
  note?: string;
  disabled?: boolean;
  danger?: boolean;
  /** 选中后原地变成输入框，占位写这个 */
  input?: string;
  /** 选它就提交的决定；input 选项提交时带上输入的文字 */
  decide: (text?: string) => DecideAgentApprovalReq;
};

/** 不同种类的色调：生成琥珀、删除红、提问蓝 */
const TONE = {
  generate: "var(--status-warning)",
  delete: "var(--destructive)",
  ask: "var(--status-running)",
} as const;

const ICON = { generate: Sparkles, delete: Trash2, ask: MessageCircleQuestion } as const;

/**
 * 决定框：等你批准生成 / 删除、或回答提问时，替换输入框钉在原位，不会被滚到看不见的地方。
 * 编号选项：数字键直接选，↑↓ 移动、Enter 确认；「…并告诉 Agent 怎么改」原地变输入框，Esc 退回。
 * 明细默认 ≤3 项展开、更多收起；可以逐项取消勾选。超预算、余额不足时批准项禁用并写明原因。
 */
export function DecisionDock({
  approval,
  ctl,
}: {
  approval: AgentApprovalDto;
  ctl: AgentController;
}) {
  const credits = useCreditsStore((s) => s.credits);
  const [busy, setBusy] = useState(false);
  /** 服务端说超出了本轮预算（本地算不准时以它为准） */
  const [overBudget, setOverBudget] = useState(false);
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState("");
  const gen = approval.kind === "generate" ? (approval.payload as AgentGeneratePayload) : null;
  const del = approval.kind === "delete" ? (approval.payload as AgentDeletePayload) : null;
  const ask = approval.kind === "ask" ? (approval.payload as AgentAskPayload) : null;
  const items = gen?.items.length ?? del?.node_ids.length ?? 0;
  /** 每一项勾没勾 */
  const [picks, setPicks] = useState(() => Array.from({ length: items }, () => true));
  const [detail, setDetail] = useState(items <= 3);
  const chosen = picks.filter(Boolean).length;

  const options: Option[] = [];
  if (gen) {
    const quote = gen.items.reduce((sum, it, i) => sum + (picks[i] ? it.price * it.count : 0), 0);
    const remaining = Math.max(0, ctl.usage.budget - ctl.usage.spent);
    const over = overBudget || quote > remaining;
    const short = credits !== null && quote > credits.available;
    const approve = (addBudget = 0): DecideAgentApprovalReq => ({
      decision: "approve",
      items: picks.map((on, index) => ({ index, approve: on, count: gen.items[index].count })),
      add_budget: addBudget,
    });
    options.push({
      key: "approve",
      label: "批准生成",
      meta: `✦ ${quote}`,
      disabled: chosen === 0 || over || short,
      note: short
        ? `余额不足（需要 ${quote}，可用 ${credits?.available}）`
        : over
          ? `超出本轮预算（剩 ✦${remaining}）`
          : undefined,
      decide: () => approve(),
    });
    if (over && !short && chosen > 0)
      options.push({
        key: "topup",
        label: `追加 ✦${quote - remaining} 预算并批准`,
        decide: () => approve(quote - remaining),
      });
    options.push(
      { key: "reject", label: "拒绝", decide: () => ({ decision: "reject" }) },
      {
        key: "explain",
        label: "拒绝，并告诉 Agent 怎么改…",
        input: "比如：改用 Seedream，只生成林夏",
        decide: (answer) => ({ decision: "reject", answer }),
      },
    );
  } else if (del) {
    const count = chosen + del.edge_ids.length;
    options.push(
      {
        key: "approve",
        label: `删除 ${count} 项`,
        danger: true,
        disabled: count === 0,
        decide: () => ({
          decision: "approve",
          items: picks.map((on, index) => ({ index, approve: on })),
        }),
      },
      { key: "reject", label: "保留", decide: () => ({ decision: "reject" }) },
      {
        key: "explain",
        label: "保留，并告诉 Agent 怎么改…",
        input: "比如：只删空的那个文本节点",
        decide: (answer) => ({ decision: "reject", answer }),
      },
    );
  } else if (ask?.kind === "model") {
    for (const m of ask.models ?? [])
      options.push({
        key: m.key,
        label: m.name,
        sub: m.hint,
        decide: () => ({ decision: "approve", answer: `${m.name}（模型 key：${m.key}）` }),
      });
  } else if (ask) {
    for (const o of ask.options ?? [])
      options.push({ key: o, label: o, decide: () => ({ decision: "approve", answer: o }) });
    if (ask.allow_custom || (ask.options ?? []).length === 0)
      options.push({
        key: "explain",
        label: "其他…",
        input: "自己写",
        decide: (answer) => ({ decision: "approve", answer }),
      });
  }

  const firstEnabled = Math.max(
    0,
    options.findIndex((o) => !o.disabled),
  );
  const [idx, setIdx] = useState(firstEnabled);
  const active = Math.min(idx, options.length - 1);
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const input = useRef<HTMLInputElement>(null);

  // 钉上来时把焦点放到第一个能选的选项：键盘马上就能操作
  useEffect(() => {
    buttons.current.find((b) => b && !b.disabled)?.focus({ preventScroll: true });
  }, [approval.id]);
  // 「…并告诉 Agent 怎么改」变成输入框后，焦点跟过去
  useEffect(() => {
    if (editing) input.current?.focus();
  }, [editing]);

  const submit = async (option: Option, answer?: string) => {
    setBusy(true);
    const result = await ctl.decide(approval.id, option.decide(answer));
    setBusy(false);
    if (result === "over_budget") setOverBudget(true);
  };

  const choose = (i: number) => {
    const option = options[i];
    if (!option || option.disabled || busy) return;
    setIdx(i);
    if (option.input) {
      setEditing(true);
      return;
    }
    void submit(option);
  };

  const move = (step: 1 | -1) => {
    let next = active;
    for (let n = 0; n < options.length; n++) {
      next = (next + step + options.length) % options.length;
      if (!options[next].disabled) break;
    }
    setIdx(next);
    buttons.current[next]?.focus();
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if (editing || (e.target as HTMLElement).closest("input, [role=checkbox]")) return;
    if (/^[1-9]$/.test(e.key) && Number(e.key) <= options.length) {
      e.preventDefault();
      choose(Number(e.key) - 1);
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      move(e.key === "ArrowDown" ? 1 : -1);
    }
  };

  const Icon = ICON[approval.kind];
  const reason = gen?.reason || del?.reason || "";

  return (
    <div
      role="group"
      aria-label={approvalTitle(approval)}
      onKeyDown={onKeyDown}
      style={{ "--tone": TONE[approval.kind] } as CSSProperties}
      className="relative flex flex-col gap-2 bg-[linear-gradient(180deg,color-mix(in_oklab,var(--tone)_10%,transparent),transparent_72px)] p-3"
      onMouseEnter={() => del && useAgentHighlight.getState().setDanger(del.node_ids)}
      onMouseLeave={() => del && useAgentHighlight.getState().setDanger([])}
    >
      <span
        aria-hidden
        className="absolute inset-x-4 top-0 h-px bg-[linear-gradient(90deg,transparent,color-mix(in_oklab,var(--tone)_70%,transparent),transparent)]"
      />
      <div className="flex items-center gap-2 text-[13.5px] font-semibold">
        <Icon className="size-4 shrink-0 text-(--tone)" />
        <span className="min-w-0 flex-1">{approvalTitle(approval)}</span>
        {gen && (
          <span className="text-credit shrink-0 font-medium tabular-nums">{options[0]?.meta}</span>
        )}
      </div>
      {reason && <p className="text-muted-foreground -mt-1 text-[12.5px]">{reason}</p>}

      {items > 0 && (
        <>
          <button
            type="button"
            aria-expanded={detail}
            onClick={() => setDetail((v) => !v)}
            className="text-muted-foreground hover:text-foreground focus-visible:ring-node-ring/60 flex items-center gap-1 self-start rounded-md text-xs outline-none focus-visible:ring-2"
          >
            <ChevronRight
              className={cn("size-3.5 transition-transform duration-120", detail && "rotate-90")}
            />
            明细 {items} 项
            {del && del.outputs.some(Boolean) && (
              <span className="text-destructive">
                · 含 {del.outputs.filter(Boolean).length} 个产物
              </span>
            )}
          </button>
          {detail && (
            <ul className="flex max-h-32 flex-col gap-0.5 overflow-y-auto">
              {Array.from({ length: items }, (_, i) => {
                const label = gen ? gen.items[i].label : (del?.labels[i] ?? del?.node_ids[i]);
                return (
                  <li key={i} className="flex items-center gap-2 py-0.5 text-xs">
                    <Checkbox
                      checked={picks[i]}
                      disabled={busy}
                      onCheckedChange={(on) =>
                        setPicks((prev) => prev.map((v, j) => (j === i ? on === true : v)))
                      }
                      aria-label={`${gen ? "生成" : "删除"}「${label}」`}
                    />
                    <span className="min-w-0 flex-1 truncate">{label}</span>
                    {gen && (
                      <>
                        <span className="text-muted-foreground shrink-0">
                          {gen.items[i].model_name}
                          {gen.items[i].count > 1 && ` × ${gen.items[i].count}`}
                        </span>
                        <span className="text-credit shrink-0 tabular-nums">
                          ✦ {gen.items[i].price * gen.items[i].count}
                        </span>
                      </>
                    )}
                    {del?.outputs[i] && <span className="text-destructive shrink-0">含产物</span>}
                  </li>
                );
              })}
            </ul>
          )}
        </>
      )}

      <div role="listbox" aria-label="你的决定" className="flex flex-col">
        {options.map((option, i) => {
          const selected = i === active;
          if (editing && selected && option.input)
            return (
              <form
                key={option.key}
                className="flex h-9 items-center gap-2.5 rounded-lg px-2.5"
                onSubmit={(e) => {
                  e.preventDefault();
                  if (text.trim()) void submit(option, text.trim());
                }}
              >
                <Num n={i + 1} />
                <input
                  ref={input}
                  value={text}
                  maxLength={2000}
                  disabled={busy}
                  placeholder={option.input}
                  aria-label={option.label}
                  onChange={(e) => setText(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key !== "Escape") return;
                    e.stopPropagation();
                    setEditing(false);
                    requestAnimationFrame(() => buttons.current[i]?.focus());
                  }}
                  className="placeholder:text-muted-foreground min-w-0 flex-1 bg-transparent text-[13px] outline-none"
                />
              </form>
            );
          return (
            <div key={option.key}>
              <button
                ref={(el) => {
                  buttons.current[i] = el;
                }}
                type="button"
                role="option"
                aria-selected={selected}
                disabled={option.disabled || busy}
                onClick={() => choose(i)}
                onMouseEnter={() => !option.disabled && setIdx(i)}
                onFocus={() => setIdx(i)}
                className={cn(
                  "relative flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-[13px] outline-none disabled:opacity-45",
                  option.danger && "text-destructive",
                )}
              >
                {selected && (
                  <motion.span
                    layoutId={`decision-hl-${approval.id}`}
                    transition={SPRING}
                    aria-hidden
                    className="agent-inset bg-foreground/7 absolute inset-0 rounded-lg"
                  />
                )}
                <Num n={i + 1} />
                <span className="relative min-w-0 truncate">{option.label}</span>
                {option.sub && (
                  <span className="text-muted-foreground relative min-w-0 truncate text-xs">
                    {option.sub}
                  </span>
                )}
                {option.meta && (
                  <span className="text-credit relative ml-auto shrink-0 text-xs tabular-nums">
                    {option.meta}
                  </span>
                )}
              </button>
              {option.note && (
                <p className="text-destructive -mt-0.5 pr-2.5 pb-1 pl-[38px] text-[11.5px]">
                  {option.note}
                </p>
              )}
            </div>
          );
        })}
      </div>

      <div className="text-muted-foreground flex items-center gap-2 pt-0.5 text-[11px]">
        <span>
          1–{options.length} 选择 · ↑↓ 移动 · Enter 确认{editing && " · Esc 返回"}
        </span>
        <button
          type="button"
          onClick={() => void ctl.stop()}
          className="hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 ml-auto h-6 rounded-md px-2 outline-none transition-colors duration-120 focus-visible:ring-2"
        >
          停止本轮
        </button>
      </div>
    </div>
  );
}

/** 选项的序号 */
function Num({ n }: { n: number }) {
  return (
    <span className="text-muted-foreground ring-chrome-border relative grid size-[18px] shrink-0 place-items-center rounded-[5px] text-[11px] tabular-nums ring-1">
      {n}
    </span>
  );
}

import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { motion } from "motion/react";
import {
  ChevronRight,
  ClipboardList,
  FilePen,
  MessageCircleQuestion,
  KeyRound,
  Play,
  Sparkles,
  Trash2,
} from "lucide-react";

import type {
  AgentApprovalDto,
  AgentAskPayload,
  AgentGeneratePayload,
  AdminSecretPayload,
  AdminTestRunPayload,
  DecideAgentApprovalReq,
} from "@/api/agent/type";
import { Checkbox } from "@/components/ui/checkbox";
import type { AgentController } from "@/hooks/use-agent-controller";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useAgentHighlight } from "@/store/agent-highlight";
import { useAdminStore } from "@/store/admin";
import { useCreditsStore } from "@/store/credits";
import {
  adminChangePayload,
  buildApproveDecision,
  buildRejectDecision,
  defaultSelection,
  selectedCount,
  resolveSelection,
  roleSatisfies,
  toggleSelection,
} from "@/utils/admin/assistant/change-order";
import { editChanges, editPayload, type EditPayload } from "@/utils/agent/drama";

import { AdminChangeDetail } from "./admin-change-detail";
import { AdminSecretDetail, AdminTestRunDetail } from "./admin-approval-detail";
import { approvalTitle, deletePayload } from "./approval-row";

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

/** 不同种类的色调（设计稿 6.12）：琥珀只用来标「需要你行动」，生成和提问都用它；删除是红 */
const TONE = {
  generate: "var(--status-warning)",
  delete: "var(--destructive)",
  ask: "var(--status-warning)",
  edit: "var(--status-warning)",
  admin_change: "var(--status-warning)",
  admin_secret: "var(--status-warning)",
  admin_test_run: "var(--status-warning)",
} as const;

const ICON = {
  generate: Sparkles,
  delete: Trash2,
  ask: MessageCircleQuestion,
  edit: FilePen,
  admin_change: ClipboardList,
  admin_secret: KeyRound,
  admin_test_run: Play,
} as const;

/** 不认识的审批种类按通用修改卡（edit）处理 */
const kindOf = (kind: string): keyof typeof TONE =>
  kind in TONE ? (kind as keyof typeof TONE) : "edit";

/**
 * 修改审批的前后对比：后端给的是整个对象的 JSON，这里只列真正改动的字段，
 * 每个字段一组「修改前（删除线、红）/ 修改后（绿）」；解析不了的原文照旧显示。
 */
function EditDiff({ edit }: { edit: EditPayload }) {
  const changes = editChanges(edit);
  const line = "rounded-md px-2 py-1.5 leading-5 wrap-anywhere whitespace-pre-wrap";
  if (changes && changes.length === 0) {
    return <p className="text-muted-foreground text-xs">没有实际改动</p>;
  }
  if (changes) {
    return (
      <div aria-label="修改前后" className="flex max-h-56 flex-col gap-2.5 overflow-y-auto text-xs">
        {changes.map((change) => (
          <div key={change.label} className="flex flex-col gap-1">
            <span className="text-muted-foreground font-medium">{change.label}</span>
            {change.before && (
              <p
                className={cn(
                  line,
                  "bg-destructive/10 border-destructive text-foreground/70 border-l-2 line-through decoration-(--decoration)",
                )}
                style={
                  {
                    "--decoration": "color-mix(in oklab, var(--destructive) 55%, transparent)",
                  } as CSSProperties
                }
              >
                {change.before}
              </p>
            )}
            {change.after && (
              <p className={cn(line, "bg-status-success/10 border-status-success border-l-2")}>
                {change.after}
              </p>
            )}
          </div>
        ))}
      </div>
    );
  }
  return (
    <div aria-label="修改前后" className="flex max-h-44 flex-col gap-1 overflow-y-auto text-xs">
      {edit.before && (
        <p className={cn(line, "bg-destructive/10 text-foreground/80")}>
          <span className="text-destructive mr-1.5 font-medium">修改前</span>
          {edit.before}
        </p>
      )}
      {edit.after && (
        <p className={cn(line, "bg-status-success/10 text-foreground")}>
          <span className="text-status-success mr-1.5 font-medium">修改后</span>
          {edit.after}
        </p>
      )}
    </div>
  );
}

/**
 * 决定框：等你批准生成 / 删除、或回答提问时，替换输入框钉在原位，不会被滚到看不见的地方。
 * 编号选项：数字键直接选，↑↓ 移动、Enter 确认；「…并告诉 Agent 怎么改」原地变输入框，Esc 退回。
 * 明细默认 ≤3 项展开、更多收起；可以逐项取消勾选。余额不足时批准项禁用并写明原因。
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
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState("");
  const gen = approval.kind === "generate" ? (approval.payload as AgentGeneratePayload) : null;
  const del = approval.kind === "delete" ? deletePayload(approval.payload) : null;
  const ask = approval.kind === "ask" ? (approval.payload as AgentAskPayload) : null;
  const kind = kindOf(approval.kind);
  const edit = kind === "edit" ? editPayload(approval.payload) : null;
  // 后台助手的变更单：勾选的是条目下标集合（没动过时按默认：能勾选且有实际改动的）
  const role = useAdminStore((s) => s.role);
  const admin = kind === "admin_change" ? adminChangePayload(approval) : null;
  const secret = kind === "admin_secret" ? (approval.payload as AdminSecretPayload) : null;
  const trial = kind === "admin_test_run" ? (approval.payload as AdminTestRunPayload) : null;
  const [adminPicked, setAdminPicked] = useState<ReadonlySet<number> | null>(null);
  const adminSel = admin
    ? resolveSelection(admin.items, role, adminPicked ?? defaultSelection(admin.items, role))
    : null;
  const adminCount = admin && adminSel ? selectedCount(admin.items, role, adminSel) : 0;
  const items = gen?.items.length ?? del?.node_ids.length ?? 0;
  /** 每一项勾没勾 */
  const [picks, setPicks] = useState(() => Array.from({ length: items }, () => true));
  const [detail, setDetail] = useState(items <= 3);
  const chosen = picks.filter(Boolean).length;

  const options: Option[] = [];
  if (gen) {
    const quote = gen.items.reduce((sum, it, i) => sum + (picks[i] ? it.price * it.count : 0), 0);
    const short = credits !== null && quote > credits.available;
    const approve = (): DecideAgentApprovalReq => ({
      decision: "approve",
      items: picks.map((on, index) => ({ index, approve: on, count: gen.items[index].count })),
    });
    options.push({
      key: "approve",
      label: "批准生成",
      meta: `✦ ${quote}`,
      disabled: chosen === 0 || short,
      note: short ? `余额不足（需要 ${quote}，可用 ${credits?.available}）` : undefined,
      decide: () => approve(),
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
  } else if (kind === "admin_change") {
    // 后台助手的变更单：应用勾选的条目，或拒绝（可附上给助手的说明）
    options.push(
      {
        key: "approve",
        label: `应用 ${adminCount} 项变更`,
        disabled: !admin || !adminSel || adminCount === 0,
        decide: () =>
          admin && adminSel
            ? buildApproveDecision(admin.items, role, adminSel)
            : { decision: "reject" },
      },
      { key: "reject", label: "拒绝", decide: () => buildRejectDecision("") },
      {
        key: "explain",
        label: "拒绝，并告诉助手怎么改…",
        input: "比如：只调可灵，别动其他模型",
        decide: (answer) => buildRejectDecision(answer ?? ""),
      },
    );
  } else if (trial) {
    const allowed = roleSatisfies(trial.required_role, role);
    options.push(
      {
        key: "approve",
        label: "同意试跑",
        disabled: !allowed,
        note: allowed ? undefined : "需 admin 才能同意试跑",
        decide: () => ({ decision: "approve" }),
      },
      { key: "reject", label: "不试跑", decide: () => ({ decision: "reject" }) },
    );
  } else if (edit) {
    // 短剧助手的修改：批准后才写入；提交任务带积分估价，余额不足时不能批准
    const quote = approval.quote_credits;
    const short = quote > 0 && credits !== null && quote > credits.available;
    options.push(
      {
        key: "approve",
        label: "批准修改",
        meta: quote > 0 ? `✦ ${quote}` : undefined,
        disabled: short,
        note: short ? `余额不足（需要 ${quote}，可用 ${credits?.available}）` : undefined,
        decide: () => ({ decision: "approve" }),
      },
      { key: "reject", label: "不用了", decide: () => ({ decision: "reject" }) },
      {
        key: "explain",
        label: "不用了，并告诉 Agent 怎么改…",
        input: "比如：只改结尾，保留前两段",
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
    try {
      await ctl.decide(approval.id, option.decide(answer));
    } finally {
      setBusy(false);
    }
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
    if (secret || editing || (e.target as HTMLElement).closest("input, [role=checkbox]")) return;
    if (/^[1-9]$/.test(e.key) && Number(e.key) <= options.length) {
      e.preventDefault();
      choose(Number(e.key) - 1);
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      move(e.key === "ArrowDown" ? 1 : -1);
    }
  };

  const Icon = ICON[kind];
  const reason =
    gen?.reason ||
    del?.reason ||
    edit?.reason ||
    admin?.reason ||
    secret?.reason ||
    trial?.reason ||
    "";
  const isAdmin = kind.startsWith("admin_");

  return (
    <div
      role="group"
      aria-label={approvalTitle(approval)}
      onKeyDown={onKeyDown}
      style={{ "--tone": TONE[kind] } as CSSProperties}
      className={cn(
        "relative flex flex-col gap-2 p-3",
        !isAdmin &&
          "bg-[linear-gradient(180deg,color-mix(in_oklab,var(--tone)_10%,transparent),transparent_72px)]",
      )}
      onMouseEnter={() => del && useAgentHighlight.getState().setDanger(del.node_ids)}
      onMouseLeave={() => del && useAgentHighlight.getState().setDanger([])}
    >
      <span
        aria-hidden
        className={cn(
          "absolute inset-x-4 top-0 h-px",
          isAdmin
            ? "bg-chrome-border"
            : "bg-[linear-gradient(90deg,transparent,color-mix(in_oklab,var(--tone)_70%,transparent),transparent)]",
        )}
      />
      <div className="flex items-center gap-2 text-[13.5px] font-semibold">
        <Icon className="size-4 shrink-0 text-(--tone)" />
        <span className="min-w-0 flex-1">{approvalTitle(approval)}</span>
        {(gen || edit) && options[0]?.meta && (
          <span className="text-credit shrink-0 font-medium tabular-nums">{options[0].meta}</span>
        )}
      </div>
      {reason && <p className="text-muted-foreground -mt-1 text-[12.5px]">{reason}</p>}
      {edit && (edit.before || edit.after) && <EditDiff edit={edit} />}
      {secret && (
        <AdminSecretDetail
          key={approval.id}
          payload={secret}
          approvalId={approval.id}
          role={role}
          ctl={ctl}
        />
      )}
      {trial && <AdminTestRunDetail payload={trial} />}
      {admin && adminSel && (
        <AdminChangeDetail
          payload={admin}
          role={role}
          picked={adminSel}
          disabled={busy}
          onToggle={(index) => setAdminPicked(toggleSelection(adminSel, index, admin.items, role))}
        />
      )}

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

      {!secret && (
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
                    "relative flex min-h-9 w-full items-start gap-2.5 rounded-lg px-2.5 py-2 text-left text-[13px] outline-none disabled:opacity-45",
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
                  <Num n={i + 1} className="relative mt-px" />
                  <span className="relative min-w-0 leading-5 wrap-anywhere">{option.label}</span>
                  {option.sub && (
                    <span className="text-muted-foreground relative min-w-0 text-xs leading-5 wrap-anywhere">
                      {option.sub}
                    </span>
                  )}
                  {option.meta && (
                    <span className="text-credit relative ml-auto shrink-0 text-xs leading-5 tabular-nums">
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
      )}

      <div className="text-muted-foreground flex items-center gap-2 pt-0.5 text-[11px]">
        {!isAdmin && (
          <span>
            1–{options.length} 选择 · ↑↓ 移动 · Enter 确认{editing && " · Esc 返回"}
          </span>
        )}
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
function Num({ n, className }: { n: number; className?: string }) {
  return (
    <span
      className={cn(
        "text-muted-foreground ring-chrome-border relative grid size-[18px] shrink-0 place-items-center rounded-[5px] text-[11px] tabular-nums ring-1",
        className,
      )}
    >
      {n}
    </span>
  );
}

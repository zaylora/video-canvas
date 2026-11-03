import { useState, type CSSProperties } from "react";
import { MessageCircleQuestion } from "lucide-react";

import type { AgentApprovalDto, AgentAskPayload, AgentAskQuestion } from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import { cn } from "@/lib/utils";
import {
  CUSTOM,
  answeredCount,
  askQuestions,
  askState,
  buildAskDecision,
  canSubmit,
  emptyDraft,
  type AskDraft,
} from "@/utils/agent/ask";

import { approvalTitle } from "./approval-title";

/** 选项前的字母标：A、B、C… */
const letter = (i: number) => String.fromCharCode(65 + i);

/**
 * 多题提问卡：编号题目竖着排，选项用 A/B/C；选了带 follow 的选项，输入框原地展开；
 * 必填题都答好后「提交」才能点，进度写成「n / N」。提交后原地收成每题一行的记录。
 */
export function AskForm({ approval, ctl }: { approval: AgentApprovalDto; ctl: AgentController }) {
  const qs = askQuestions(approval.payload as AgentAskPayload);
  const [drafts, setDrafts] = useState<Record<string, AskDraft>>({});
  const [busy, setBusy] = useState(false);
  const draftOf = (q: AgentAskQuestion) => drafts[q.id] ?? emptyDraft();
  const patch = (q: AgentAskQuestion, change: (d: AskDraft) => AskDraft) =>
    setDrafts((prev) => ({ ...prev, [q.id]: change(prev[q.id] ?? emptyDraft()) }));

  const ready = canSubmit(qs, drafts);
  const submit = async () => {
    if (!ready || busy) return;
    setBusy(true);
    try {
      await ctl.decide(approval.id, buildAskDecision(qs, drafts));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      role="group"
      aria-label={approvalTitle(approval)}
      style={{ "--tone": "var(--status-warning)" } as CSSProperties}
      className="bg-chrome-hover ring-chrome-border relative flex flex-col gap-3 rounded-2xl p-3 ring-1"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <div className="flex items-center gap-2 text-[13.5px] font-semibold">
        <MessageCircleQuestion className="size-4 shrink-0 text-(--tone)" />
        <span className="min-w-0 flex-1 wrap-anywhere">{approvalTitle(approval)}</span>
      </div>

      {qs.map((q, qi) => (
        <QuestionBlock
          key={q.id}
          q={q}
          index={qi}
          draft={draftOf(q)}
          disabled={busy}
          onChange={(change) => patch(q, change)}
        />
      ))}

      <div className="flex items-center justify-between gap-2">
        <span aria-label="进度" className="text-muted-foreground text-xs tabular-nums">
          {answeredCount(qs, drafts)} / {qs.length}
        </span>
        <button
          type="submit"
          disabled={!ready || busy}
          className="bg-primary text-primary-foreground hover:bg-primary/88 h-[30px] rounded-full px-3.5 text-[12.5px] font-medium transition-colors disabled:pointer-events-none disabled:opacity-45"
        >
          提交
        </button>
      </div>
    </form>
  );
}

function QuestionBlock({
  q,
  index,
  draft,
  disabled,
  onChange,
}: {
  q: AgentAskQuestion;
  index: number;
  draft: AskDraft;
  disabled: boolean;
  onChange: (change: (d: AskDraft) => AskDraft) => void;
}) {
  const state = askState(q, draft);
  const multi = q.kind === "choice" && q.multi === true;
  const pick = (key: string) =>
    onChange((d) => {
      const has = d.picked.includes(key);
      const picked = multi ? (has ? d.picked.filter((k) => k !== key) : [...d.picked, key]) : [key];
      return { ...d, picked };
    });

  // 选项列表：选项题（含「其他」）或模型
  const choices: Array<{
    key: string;
    label: string;
    sub?: string;
    input?: { placeholder: string; value: string; set: (v: string) => void };
  }> = [];
  if (q.kind === "choice") {
    for (const o of q.options ?? [])
      choices.push({
        key: o.label,
        label: o.label,
        input: o.follow
          ? {
              placeholder: o.follow,
              value: draft.follow[o.label] ?? "",
              set: (v) => onChange((d) => ({ ...d, follow: { ...d.follow, [o.label]: v } })),
            }
          : undefined,
      });
    if (q.allow_custom)
      choices.push({
        key: CUSTOM,
        label: "其他，我来说",
        input: {
          placeholder: "自己写",
          value: draft.custom,
          set: (v) => onChange((d) => ({ ...d, custom: v })),
        },
      });
  } else if (q.kind === "model") {
    for (const m of q.models ?? []) choices.push({ key: m.key, label: m.name, sub: m.hint });
  }

  return (
    <div
      role="group"
      aria-label={q.question}
      className={cn("flex flex-col gap-1.5", index > 0 && "border-chrome-border border-t pt-3")}
    >
      <div className="flex items-start gap-2 text-[13px] font-medium">
        <span className="bg-chrome-hover ring-chrome-border mt-px grid size-[18px] shrink-0 place-items-center rounded-full text-[11px] ring-1">
          {index + 1}
        </span>
        <span className="min-w-0 leading-5 wrap-anywhere">{q.question}</span>
        {!q.required && (
          <span className="text-muted-foreground shrink-0 text-[11.5px] leading-5 font-normal">
            选填
          </span>
        )}
      </div>

      {choices.length > 0 && (
        <div role={multi ? "group" : "radiogroup"} className="flex flex-col gap-1">
          {choices.map((c, ci) => {
            const on = draft.picked.includes(c.key);
            return (
              <div key={c.key} className="flex flex-col gap-1">
                <button
                  type="button"
                  role={multi ? "checkbox" : "radio"}
                  aria-checked={on}
                  disabled={disabled}
                  onClick={() => pick(c.key)}
                  className={cn(
                    "ring-chrome-border hover:bg-chrome-hover flex min-h-9 w-full items-start gap-2.5 rounded-lg px-2.5 py-2 text-left text-[13px] ring-1 outline-none transition-colors",
                    on && "bg-chrome-hover ring-foreground/35",
                  )}
                >
                  <span
                    className={cn(
                      "ring-chrome-border mt-px grid size-[18px] shrink-0 place-items-center rounded-[5px] text-[11px] ring-1",
                      on
                        ? "bg-primary text-primary-foreground ring-transparent"
                        : "text-muted-foreground",
                    )}
                  >
                    {letter(ci)}
                  </span>
                  <span className="min-w-0 leading-5 wrap-anywhere">{c.label}</span>
                  {c.sub && (
                    <span className="text-muted-foreground min-w-0 text-xs leading-5 wrap-anywhere">
                      {c.sub}
                    </span>
                  )}
                </button>
                {on && c.input && (
                  <TextInput
                    autoFocus
                    label={`${c.label}：补充`}
                    placeholder={c.input.placeholder}
                    value={c.input.value}
                    disabled={disabled}
                    invalid={false}
                    onChange={c.input.set}
                  />
                )}
              </div>
            );
          })}
        </div>
      )}

      {(q.kind === "text" || q.kind === "url" || q.kind === "number") && (
        <TextInput
          label={q.question}
          placeholder={q.placeholder ?? ""}
          value={draft.text}
          disabled={disabled}
          invalid={state === "bad"}
          numeric={q.kind === "number"}
          onChange={(v) => onChange((d) => ({ ...d, text: v }))}
        />
      )}
    </div>
  );
}

function TextInput({
  label,
  placeholder,
  value,
  disabled,
  invalid,
  numeric,
  autoFocus,
  onChange,
}: {
  label: string;
  placeholder: string;
  value: string;
  disabled: boolean;
  invalid: boolean;
  numeric?: boolean;
  autoFocus?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <input
      value={value}
      maxLength={2000}
      disabled={disabled}
      autoFocus={autoFocus}
      inputMode={numeric ? "decimal" : undefined}
      placeholder={placeholder}
      aria-label={label}
      aria-invalid={invalid || undefined}
      onChange={(e) => onChange(e.target.value)}
      className={cn(
        "ring-chrome-border placeholder:text-muted-foreground focus:ring-foreground/35 h-[34px] w-full rounded-lg bg-transparent px-2.5 text-[13px] ring-1 outline-none",
        invalid && "ring-destructive/60 focus:ring-destructive/60",
      )}
    />
  );
}

import { Loader2 } from "lucide-react";
import { useEffect, useRef, useState, type ReactElement, type ReactNode } from "react";

import { MotionButton } from "@/components/admin-ui/motion-button";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { CreditMode } from "@/api/admin/users/type.d";
import { cn } from "@/lib/utils";
import {
  creditPreview,
  NOTE_MAX,
  parseAmount,
  stackChip,
  validateNote,
} from "@/utils/admin/user-rules";

import type { ActionUser, UserActions } from "./use-user-actions";

/** 未提交的内容保留多久（毫秒）：点浮层外关闭后 10 秒内重新打开同一个用户会恢复 */
const DRAFT_TTL = 10_000;

/** 增加模式的快捷档 */
const CHIPS = [50, 100, 500, 1000] as const;

type Draft = { mode: CreditMode; amount: string; note: string };

const drafts = new Map<number, Draft>();
const draftTimers = new Map<number, ReturnType<typeof setTimeout>>();

const loadDraft = (id: number): Draft => drafts.get(id) ?? { mode: "add", amount: "", note: "" };

/** 存草稿并重新计时：DRAFT_TTL 之后没再改动就丢掉 */
const saveDraft = (id: number, draft: Draft) => {
  drafts.set(id, draft);
  clearTimeout(draftTimers.get(id));
  draftTimers.set(
    id,
    setTimeout(() => {
      drafts.delete(id);
      draftTimers.delete(id);
    }, DRAFT_TTL),
  );
};

const dropDraft = (id: number) => {
  drafts.delete(id);
  clearTimeout(draftTimers.get(id));
  draftTimers.delete(id);
};

const MODES: { value: CreditMode; label: string }[] = [
  { value: "add", label: "增加" },
  { value: "sub", label: "扣减" },
  { value: "set", label: "设为" },
];

/**
 * 积分浮层（行内积分格和弹窗的「调整积分」按钮共用）：增加 / 扣减 / 设为三种方式都以可用积分为准，
 * 预览实时计算，Enter 提交，Esc 或点外面关闭但保留已填内容。
 * 触发器通过 render 传入（积分格或按钮），打开状态由调用方持有，方便 Esc 逐层关闭。
 */
function CreditPopover({
  user,
  actions,
  open,
  onOpenChange,
  render,
  children,
  align = "center",
}: {
  user: ActionUser;
  actions: UserActions;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 触发器元素，如 <button className="…" /> */
  render: ReactElement;
  /** 触发器内容 */
  children: ReactNode;
  align?: "start" | "center" | "end";
}) {
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger render={render}>{children}</PopoverTrigger>
      <PopoverContent
        align={align}
        sideOffset={6}
        className="w-[min(288px,calc(100vw-32px))] gap-0 p-3"
      >
        <CreditForm
          key={user.id}
          user={user}
          actions={actions}
          onDone={() => onOpenChange(false)}
        />
      </PopoverContent>
    </Popover>
  );
}

function CreditForm({
  user,
  actions,
  onDone,
}: {
  user: ActionUser;
  actions: UserActions;
  onDone: () => void;
}) {
  const [initial] = useState(() => loadDraft(user.id));
  const [mode, setMode] = useState(initial.mode);
  const [amount, setAmount] = useState(initial.amount);
  const [note, setNote] = useState(initial.note);
  const amountRef = useRef<HTMLInputElement>(null);
  const noteRef = useRef<HTMLInputElement>(null);
  const busy = actions.isBusy(user.id, "credit");

  useEffect(() => {
    amountRef.current?.focus();
  }, []);

  /** 每次输入都存草稿 */
  const save = (next: Partial<Draft>) => saveDraft(user.id, { mode, amount, note, ...next });

  const preview = creditPreview({ mode, amount: parseAmount(amount), available: user.available });
  const canSubmit = preview.valid && validateNote(note) === null && !busy;

  const submit = async () => {
    if (!canSubmit) return;
    const value = parseAmount(amount);
    if (value === null) return;
    const ok = await actions.adjustCredits(user, { mode, amount: value, note: note.trim() });
    if (ok) {
      dropDraft(user.id);
      onDone();
    }
  };

  const onEnter = (event: React.KeyboardEvent) => {
    if (event.key !== "Enter") return;
    event.preventDefault();
    void submit();
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="min-w-0 truncate text-sm font-medium">调整积分 · {user.username}</span>
        <span className="text-muted-foreground shrink-0 text-xs tabular-nums">
          可用 {user.available}
        </span>
      </div>
      <Segmented aria-label="调整方式" className="w-full">
        {MODES.map((item) => (
          <SegmentedItem
            key={item.value}
            slideId="credit-mode"
            active={mode === item.value}
            className="flex-1 px-2 py-1"
            disabled={busy}
            onClick={() => {
              setMode(item.value);
              save({ mode: item.value });
              amountRef.current?.focus();
            }}
          >
            {item.label}
          </SegmentedItem>
        ))}
      </Segmented>
      {mode === "add" && (
        <div className="flex gap-1.5">
          {CHIPS.map((chip) => (
            <Button
              key={chip}
              type="button"
              variant="outline"
              size="xs"
              className="flex-1 tabular-nums"
              disabled={busy}
              onClick={() => {
                const next = stackChip(amount, chip);
                setAmount(next);
                save({ amount: next });
                noteRef.current?.focus();
              }}
            >
              +{chip}
            </Button>
          ))}
        </div>
      )}
      <Input
        ref={amountRef}
        inputMode="numeric"
        autoComplete="off"
        maxLength={9}
        aria-label={mode === "set" ? "把可用积分设为" : "积分数值"}
        aria-invalid={!!preview.error}
        placeholder={mode === "set" ? "把可用积分设为" : "数值"}
        value={amount}
        disabled={busy}
        className="font-mono tabular-nums"
        onChange={(event) => {
          const next = event.target.value.replace(/\D/g, "");
          setAmount(next);
          save({ amount: next });
        }}
        onKeyDown={onEnter}
      />
      <Input
        ref={noteRef}
        aria-label="备注"
        autoComplete="off"
        maxLength={NOTE_MAX}
        placeholder="备注（必填，用户可见）"
        aria-describedby="credit-note-hint"
        value={note}
        disabled={busy}
        onChange={(event) => {
          setNote(event.target.value);
          save({ note: event.target.value });
        }}
        onKeyDown={onEnter}
      />
      <p id="credit-note-hint" className="text-muted-foreground -mt-1 text-xs">
        该备注会显示在用户的积分流水中
      </p>
      <div className="mt-1 flex items-center justify-between gap-2">
        <span
          className={cn(
            "min-w-0 text-xs tabular-nums",
            preview.error ? "text-destructive" : "text-muted-foreground",
          )}
          aria-live="polite"
        >
          {preview.error ? (
            preview.error
          ) : preview.after === null ? (
            "输入数值查看结果"
          ) : (
            <>
              {user.available} → <b className="text-foreground font-semibold">{preview.after}</b>
            </>
          )}
        </span>
        <MotionButton size="xs" disabled={!canSubmit} onClick={() => void submit()}>
          {busy ? (
            <>
              <Loader2 className="animate-spin" />
              提交中
            </>
          ) : (
            <>
              确认
              <span className="rounded border border-current/40 px-1 text-[10px] opacity-60">
                ⏎
              </span>
            </>
          )}
        </MotionButton>
      </div>
    </div>
  );
}

export { CreditPopover };

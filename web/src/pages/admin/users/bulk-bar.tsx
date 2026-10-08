import { Ban, CircleCheck, Gift, Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import type { UserListItem } from "@/api/admin/users/type.d";
import { DataTableBulkActions } from "@/components/admin-ui/data-table-bulk-actions";
import { MotionButton } from "@/components/admin-ui/motion-button";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  batchPlan,
  NOTE_MAX,
  parseAmount,
  stackChip,
  validateNote,
  type Actor,
} from "@/utils/admin/user-rules";

import type { UserActions } from "./use-user-actions";

const CHIPS = [50, 100, 500, 1000] as const;

/**
 * 勾选后浮在底部的批量条：发积分… / 封禁 / 启用 / 清除。
 * 封禁 / 启用立即执行并带撤销；发积分先打开弹窗。无权限的账号自动跳过，由 toast 说明原因。
 * 批量封禁不提供「取消任务」：要取消任务请逐个处理。
 */
function BulkBar({
  users,
  actor,
  actions,
  creditOpen,
  onCreditOpenChange,
  onClear,
}: {
  /** 当前勾选的用户 */
  users: UserListItem[];
  actor: Actor | null;
  actions: UserActions;
  /** 批量发积分弹窗是否打开；状态在页面里，方便 Esc 逐层关闭 */
  creditOpen: boolean;
  onCreditOpenChange: (open: boolean) => void;
  onClear: () => void;
}) {
  const busy = actions.busy === "bulk:status";

  const run = async (to: "active" | "disabled") => {
    if (await actions.bulkStatus(users, to)) onClear();
  };

  return (
    <>
      <DataTableBulkActions count={users.length} entityName="用户" onClear={onClear}>
        <MotionButton
          variant="ghost"
          size="xs"
          disabled={busy}
          onClick={() => onCreditOpenChange(true)}
        >
          <Gift />
          发积分…
        </MotionButton>
        <MotionButton
          variant="ghost"
          size="xs"
          disabled={busy}
          className="text-destructive hover:text-destructive"
          onClick={() => void run("disabled")}
        >
          {busy ? <Loader2 className="animate-spin" /> : <Ban />}
          封禁
        </MotionButton>
        <MotionButton variant="ghost" size="xs" disabled={busy} onClick={() => void run("active")}>
          <CircleCheck />
          启用
        </MotionButton>
      </DataTableBulkActions>
      <BulkCreditDialog
        open={creditOpen && users.length > 0}
        users={users}
        actor={actor}
        actions={actions}
        onOpenChange={onCreditOpenChange}
        onDone={() => {
          onCreditOpenChange(false);
          onClear();
        }}
      />
    </>
  );
}

/** 批量发积分：只支持增加，不支持批量扣减和设为（误操作代价太高）；备注必填 */
function BulkCreditDialog({
  open,
  users,
  actor,
  actions,
  onOpenChange,
  onDone,
}: {
  open: boolean;
  users: UserListItem[];
  actor: Actor | null;
  actions: UserActions;
  onOpenChange: (open: boolean) => void;
  onDone: () => void;
}) {
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const amountRef = useRef<HTMLInputElement>(null);
  const noteRef = useRef<HTMLInputElement>(null);
  const busy = actions.busy === "bulk:credit";

  /** 关闭后清空，下次打开是干净的 */
  useEffect(() => {
    if (!open) return;
    setAmount("");
    setNote("");
  }, [open]);

  const plan = batchPlan(actor, users, "credit");
  const value = parseAmount(amount) ?? 0;
  const canSubmit = value > 0 && validateNote(note) === null && plan.run.length > 0 && !busy;

  const submit = async () => {
    if (!canSubmit) return;
    if (await actions.bulkCredits(users, value, note.trim())) onDone();
  };

  const onEnter = (event: React.KeyboardEvent) => {
    if (event.key !== "Enter") return;
    event.preventDefault();
    void submit();
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent className="sm:max-w-md" initialFocus={amountRef}>
        <DialogHeader>
          <DialogTitle>给 {users.length} 个用户发积分</DialogTitle>
          <DialogDescription>只支持增加。每人都会写一条流水和审计记录。</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
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
                  setAmount(stackChip(amount, chip));
                  noteRef.current?.focus();
                }}
              >
                +{chip}
              </Button>
            ))}
          </div>
          <Input
            ref={amountRef}
            inputMode="numeric"
            autoComplete="off"
            maxLength={9}
            aria-label="每人增加的积分"
            placeholder="每人增加多少积分"
            value={amount}
            disabled={busy}
            className="font-mono tabular-nums"
            onChange={(event) => setAmount(event.target.value.replace(/\D/g, ""))}
            onKeyDown={onEnter}
          />
          <Input
            ref={noteRef}
            autoComplete="off"
            maxLength={NOTE_MAX}
            aria-label="备注"
            placeholder="备注（必填，用户可见，如：国庆活动补发）"
            aria-describedby="bulk-note-hint"
            value={note}
            disabled={busy}
            onChange={(event) => setNote(event.target.value)}
            onKeyDown={onEnter}
          />
          <p id="bulk-note-hint" className="text-muted-foreground -mt-1 text-xs">
            该备注会显示在用户的积分流水中
          </p>
          <div className="bg-muted rounded-lg px-3 py-2 text-xs" aria-live="polite">
            <span className="tabular-nums">
              {plan.run.length} 人 × {value} ={" "}
              <b className="text-foreground font-semibold">{plan.run.length * value}</b> 积分
            </span>
            {plan.skipped.length > 0 && (
              <div className="text-status-warning mt-1">
                {plan.skipText}（{plan.skipped.map((item) => item.user.username).join("、")}）
              </div>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <MotionButton disabled={!canSubmit} onClick={() => void submit()}>
            {busy && <Loader2 className="animate-spin" />}
            {busy ? "发放中" : "发放"}
          </MotionButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export { BulkBar };

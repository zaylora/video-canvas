import { useState } from "react";
import { Check, Lock } from "lucide-react";

import { submitJob } from "@/api/drama";
import { dramaErrorMessage } from "@/api/drama/errors";
import type { DramaRatio } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { useDramaJobsStore } from "@/store/drama";
import {
  breakdownOptions,
  breakdownSubmit,
  defaultBreakdownSelection,
  ratioFixed,
} from "@/utils/drama/episodes";
import { RATIOS } from "@/utils/drama/options";

import { useWorkspace } from "./context";
import { useEstimate } from "./use-estimate";

/** 对话框主体：每次打开重新挂载，选择状态回到默认 */
function BreakdownBody({ onClose, onStarted }: { onClose: () => void; onStarted: () => void }) {
  const { drama, refresh } = useWorkspace();
  const fixed = ratioFixed(drama.episodes);
  const options = breakdownOptions(drama.episodes);
  const [ratio, setRatio] = useState<DramaRatio>(drama.ratio);
  const [selected, setSelected] = useState<number[]>(() =>
    defaultBreakdownSelection(drama.episodes),
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = breakdownSubmit(ratio, selected);
  const estimate = useEstimate(
    drama.id,
    submit.ok ? { step: "breakdown", episodes: selected, ratio } : null,
  );

  const toggle = (no: number) =>
    setSelected((prev) =>
      prev.includes(no) ? prev.filter((n) => n !== no) : [...prev, no].sort((a, b) => a - b),
    );
  const available = options.filter((option) => !option.disabled).map((option) => option.no);

  const start = async () => {
    if (!submit.ok || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const job = await submitJob(drama.id, { step: "breakdown", episodes: selected, ratio });
      useDramaJobsStore.getState().applyJob(drama.id, job);
      void refresh();
      onClose();
      onStarted();
    } catch (e) {
      setError(dramaErrorMessage(e, "提交失败，请稍后重试"));
      setSubmitting(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>资产拆解</DialogTitle>
      </DialogHeader>

      <section className="grid gap-2">
        <div className="flex items-center gap-2 text-sm">
          <b>成片比例</b>
          <span className="bg-status-warning/15 text-status-warning rounded-full px-2 py-px text-xs">
            必选
          </span>
          {fixed && (
            <span className="text-muted-foreground ml-auto text-xs">
              已有资产按这个比例生成，不能再改
            </span>
          )}
        </div>
        <div className="grid grid-cols-6 gap-1.5 max-sm:grid-cols-3">
          {RATIOS.map((item) => {
            const max = Math.max(item.w, item.h);
            return (
              <button
                key={item.key}
                type="button"
                aria-pressed={ratio === item.key}
                disabled={fixed && drama.ratio !== item.key}
                onClick={() => setRatio(item.key)}
                className={cn(
                  "focus-visible:ring-ring/50 flex h-14 flex-col items-center justify-center gap-1 rounded-lg border text-xs tabular-nums outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40",
                  ratio === item.key
                    ? "border-foreground bg-muted font-medium"
                    : "hover:bg-muted/60",
                )}
              >
                <span
                  aria-hidden
                  className="rounded-[3px] border-[1.5px] border-current"
                  style={{ width: (item.w / max) * 22, height: (item.h / max) * 22 }}
                />
                {item.key}
              </button>
            );
          })}
        </div>
      </section>

      <section className="grid gap-2">
        <div className="flex items-center gap-2 text-sm">
          <b>解析范围</b>
          <span className="text-muted-foreground text-xs tabular-nums">
            已选 {selected.length} 集
          </span>
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto"
            disabled={available.length === 0}
            onClick={() => setSelected(available)}
          >
            全选可用
          </Button>
        </div>
        <div className="grid max-h-56 grid-cols-2 gap-1.5 overflow-y-auto sm:grid-cols-3">
          {options.map((option) => {
            const on = selected.includes(option.no);
            return (
              <button
                key={option.no}
                type="button"
                aria-pressed={on}
                disabled={option.disabled}
                title={option.reason ?? undefined}
                onClick={() => toggle(option.no)}
                className={cn(
                  "focus-visible:ring-ring/50 flex min-h-9 items-center gap-2 rounded-lg border px-2 text-left text-sm outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-45",
                  on ? "border-foreground bg-muted" : "hover:bg-muted/60",
                )}
              >
                <span className="bg-background grid size-4 shrink-0 place-items-center rounded border">
                  {on ? (
                    <Check className="size-3" />
                  ) : option.reason === "已拆解" ? (
                    <Lock className="size-2.5" />
                  ) : null}
                </span>
                <span className="text-muted-foreground tabular-nums">{option.no}.</span>
                <span className="truncate">{option.title}</span>
              </button>
            );
          })}
        </div>
      </section>

      <div className="bg-status-warning/10 flex items-center gap-2 rounded-lg px-3 py-2 text-sm">
        <Lock className="text-status-warning size-4 shrink-0" />
        <div className="min-w-0 flex-1">
          拆解后这 <b className="tabular-nums">{selected.length}</b> 集的剧本会<b>锁定</b>
          ，不能再修改。
        </div>
        <div className="shrink-0 text-xs tabular-nums">
          {estimate.status === "ready" ? (
            <>
              约 <b className="text-credit text-sm">{estimate.credits}</b> 积分
            </>
          ) : estimate.status === "loading" ? (
            <span className="text-muted-foreground">估算中…</span>
          ) : estimate.status === "error" ? (
            <span className="text-destructive">{estimate.message}</span>
          ) : null}
        </div>
      </div>

      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}

      <DialogFooter>
        <Button variant="ghost" onClick={onClose}>
          取消
        </Button>
        <Button disabled={!submit.ok || submitting} onClick={() => void start()}>
          {submit.label}
        </Button>
      </DialogFooter>
    </>
  );
}

/** 资产拆解对话框（剧本区底栏、资产区顶栏共用）：比例必选，已有锁定集时比例固定；集数多选，锁定的、没有剧本的禁用 */
export function BreakdownDialog({
  open,
  onOpenChange,
  onStarted,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 拆解任务提交成功后（对话框已关闭）：调用方带用户去看进度 */
  onStarted: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        {open && <BreakdownBody onClose={() => onOpenChange(false)} onStarted={onStarted} />}
      </DialogContent>
    </Dialog>
  );
}

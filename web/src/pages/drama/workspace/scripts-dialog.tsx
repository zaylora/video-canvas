import { useState } from "react";
import { Check, Lock } from "lucide-react";

import { submitJob } from "@/api/drama";
import { dramaErrorMessage } from "@/api/drama/errors";
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
import { defaultScriptSelection, scriptOptions, scriptSubmit } from "@/utils/drama/episodes";

import { useWorkspace } from "./context";
import { useEstimate } from "./use-estimate";

/** 对话框主体：每次打开重新挂载，选择回到默认（还没有剧本的集） */
function ScriptsBody({ onClose }: { onClose: () => void }) {
  const { drama, refresh } = useWorkspace();
  const options = scriptOptions(drama.episodes);
  const [selected, setSelected] = useState<number[]>(() => defaultScriptSelection(drama.episodes));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = scriptSubmit(selected, drama.episodes);
  const estimate = useEstimate(
    drama.id,
    submit.ok ? { step: "scripts", episodes: selected } : null,
  );

  const toggle = (no: number) =>
    setSelected((prev) =>
      prev.includes(no) ? prev.filter((n) => n !== no) : [...prev, no].sort((a, b) => a - b),
    );
  const available = options.filter((option) => !option.disabled).map((option) => option.no);
  const unscripted = defaultScriptSelection(drama.episodes);

  const start = async () => {
    if (!submit.ok || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const job = await submitJob(drama.id, { step: "scripts", episodes: selected });
      useDramaJobsStore.getState().applyJob(drama.id, job);
      void refresh();
      onClose();
    } catch (e) {
      setError(dramaErrorMessage(e, "提交失败，请稍后重试"));
      setSubmitting(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>生成分集剧本</DialogTitle>
      </DialogHeader>

      <section className="grid gap-2">
        <div className="flex items-center gap-1 text-sm">
          <b>选择集数</b>
          <span className="text-muted-foreground ml-1 text-xs tabular-nums">
            已选 {selected.length} 集
          </span>
          <span className="flex-1" />
          <Button
            variant="ghost"
            size="sm"
            disabled={unscripted.length === 0}
            onClick={() => setSelected(unscripted)}
          >
            没有剧本的
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={available.length === 0}
            onClick={() => setSelected(available)}
          >
            全选
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={selected.length === 0}
            onClick={() => setSelected([])}
          >
            清空
          </Button>
        </div>
        <div className="grid max-h-72 grid-cols-2 gap-1.5 overflow-y-auto sm:grid-cols-3">
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
                <span className="min-w-0 flex-1 truncate">{option.title}</span>
                {option.hasScript && !option.disabled && (
                  <span className="bg-muted text-muted-foreground shrink-0 rounded-full px-1.5 text-[10px] leading-4">
                    已有剧本
                  </span>
                )}
              </button>
            );
          })}
        </div>
      </section>

      <div
        className={cn(
          "flex items-center gap-2 rounded-lg px-3 py-2 text-sm",
          submit.overwrite > 0 ? "bg-status-warning/10" : "bg-muted/50",
        )}
      >
        <div className="min-w-0 flex-1">
          {submit.overwrite > 0 ? (
            <>
              其中 <b className="tabular-nums">{submit.overwrite}</b> 集会<b>覆盖</b>现有剧本
            </>
          ) : (
            <>
              生成 <b className="tabular-nums">{selected.length}</b> 集
            </>
          )}
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

/**
 * 生成分集剧本对话框：多选集数，默认勾选还没有剧本的集；已拆解、生成中的集不能选；
 * 已有剧本的可以选来重新生成，选中时提醒会覆盖。
 */
export function ScriptsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        {open && <ScriptsBody onClose={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

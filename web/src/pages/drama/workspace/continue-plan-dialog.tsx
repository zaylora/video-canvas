import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";

import { estimatePlanning, setPlanningRange } from "@/api/drama";
import { dramaErrorMessage } from "@/api/drama/errors";
import type { SourceEstimate } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import {
  charsLabel,
  durationLabel,
  episodesLabel,
  normalizeRangeEnd,
  unitWord,
} from "@/utils/drama/start-settings";

import { useWorkspace } from "./context";

/** 防抖毫秒数：连续改终点时只在停下后请求一次预估 */
const DEBOUNCE_MS = 300;

/** 继续规划的预估：终点变化后停 300ms 才请求，旧请求的结果作废 */
function useContinueEstimate(dramaId: string, rangeEnd: number) {
  const [done, setDone] = useState<{
    rangeEnd: number;
    value: SourceEstimate | { error: string };
  } | null>(null);
  useEffect(() => {
    let active = true;
    const timer = setTimeout(() => {
      estimatePlanning(dramaId, rangeEnd)
        .then((value) => active && setDone({ rangeEnd, value }))
        .catch(
          (e: unknown) =>
            active && setDone({ rangeEnd, value: { error: dramaErrorMessage(e, "估算失败") } }),
        );
    }, DEBOUNCE_MS);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [dramaId, rangeEnd]);
  return done?.rangeEnd === rangeEnd ? done.value : null;
}

/** 对话框主体：每次打开重新挂载，选择回到「全部剩余」 */
export function ContinueBody({ onClose }: { onClose: () => void }) {
  const { drama, refresh } = useWorkspace();
  const word = unitWord(drama.unitKind);
  const total = drama.unitsTotal;
  const from = drama.rangeEnd + 1;
  const [all, setAll] = useState(true);
  const [draft, setDraft] = useState(String(Math.min(total, drama.rangeEnd + 120)));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const typed = Number(draft);
  const to = normalizeRangeEnd(Math.max(typed, from), total);
  /** 0 = 全部：后端把 0 归一为单元总数 */
  const rangeEnd = all ? 0 : to;
  const end = all ? total : to;
  const estimate = useContinueEstimate(drama.id, rangeEnd);
  const est = estimate && !("error" in estimate) ? estimate : null;
  const perUnit = total > 0 ? drama.sourceChars / total : 0;

  const start = async () => {
    if (submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      await setPlanningRange(drama.id, rangeEnd);
      await refresh();
      onClose();
    } catch (e) {
      setError(dramaErrorMessage(e, "开始失败，请稍后重试"));
      setSubmitting(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>继续规划</DialogTitle>
      </DialogHeader>

      <div
        role="tablist"
        aria-label="规划范围"
        className="bg-muted/60 flex w-fit gap-0.5 rounded-full p-0.5"
      >
        {[
          [true, "全部剩余"],
          [false, `规划到第 N ${word}`],
        ].map(([value, label]) => (
          <button
            key={String(value)}
            type="button"
            role="tab"
            aria-selected={all === value}
            onClick={() => setAll(value as boolean)}
            className={cn(
              "focus-visible:ring-ring/50 h-8 rounded-full px-4 text-[13px] outline-none focus-visible:ring-2",
              all === value
                ? "bg-background font-medium shadow-sm"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {label as string}
          </button>
        ))}
      </div>

      {!all && (
        <div className="flex items-center gap-1.5 text-sm">
          规划到第
          <Input
            type="number"
            min={from}
            max={total}
            value={draft}
            aria-label={`规划到第几${word}`}
            onChange={(event) => setDraft(event.target.value)}
            className="h-9 w-28 tabular-nums"
          />
          {word}
        </div>
      )}

      <div className="bg-muted/50 flex items-center gap-2 rounded-lg px-3 py-2 text-sm">
        <span className="min-w-0 flex-1 tabular-nums">
          第 {from.toLocaleString("en-US")}–{end.toLocaleString("en-US")} {word} · 约{" "}
          {charsLabel(Math.round(perUnit * Math.max(0, end - drama.rangeEnd)))}
          {est && (
            <>
              {" "}
              · 预计约 {episodesLabel(est)} 集 · 约 {durationLabel(est.minutes)}
            </>
          )}
        </span>
        <span className="shrink-0 text-xs tabular-nums">
          {est ? (
            <>
              约 <b className="text-credit text-sm">{est.credits.toLocaleString("en-US")}</b> 积分
            </>
          ) : estimate && "error" in estimate ? (
            <span className="text-destructive">{estimate.error}</span>
          ) : (
            <span className="text-muted-foreground">估算中…</span>
          )}
        </span>
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
        <Button disabled={submitting} onClick={() => void start()}>
          {submitting && <Loader2 className="animate-spin" />}
          开始规划
        </Button>
      </DialogFooter>
    </>
  );
}

/**
 * 「继续规划」确认框：规划范围完成且还有剩余原文时，在分集栏末尾打开。
 * 选「全部剩余」或「规划到第 N 章」，显示剩余字数、预计集数、规划积分和耗时；确认后扩大范围、规划继续。
 */
export function ContinuePlanDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {open && <ContinueBody onClose={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

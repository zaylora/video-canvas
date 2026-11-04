import { useState } from "react";
import { Link2Off } from "lucide-react";

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
import {
  defaultScriptSelection,
  gapBefore,
  MAX_JOB_EPISODES,
  scriptOptions,
  scriptSubmit,
  selectByRange,
} from "@/utils/drama/episodes";

import { useWorkspace } from "./context";
import { EpisodeGrid, RangeSelect } from "./episode-pick";
import { useEstimate } from "./use-estimate";

/** 衔接提示：选中的集里有上一集既没剧本、也不在本次选择里的 */
function gapText(gaps: readonly number[]): string {
  if (gaps.length === 1) {
    return `第 ${gaps[0] - 1} 集还没有剧本，第 ${gaps[0]} 集会按它的梗概和结尾悬念衔接`;
  }
  const shown = gaps.slice(0, 6).join("、");
  return `第 ${shown}${gaps.length > 6 ? " 等" : ""} 集的上一集还没有剧本，会按上一集的梗概和结尾悬念衔接`;
}

/**
 * 对话框主体：每次打开重新挂载，选择回到默认（initial 指定的集；没有就是还没有剧本的集）。
 * 只列已规划的集；集多时网格是虚拟列表，可以「按范围选：第 a–b 集」。
 */
export function ScriptsBody({
  onClose,
  initial,
}: {
  onClose: () => void;
  /** 预先选中的集（从某一集的空状态点「生成剧本」进来） */
  initial?: number[];
}) {
  const { drama, refresh } = useWorkspace();
  const options = scriptOptions(drama.episodes);
  const [selected, setSelected] = useState<number[]>(() =>
    initial && initial.length > 0 ? initial : defaultScriptSelection(drama.episodes),
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = scriptSubmit(selected, drama.episodes);
  const estimate = useEstimate(
    drama.id,
    submit.ok ? { step: "scripts", episodes: selected } : null,
  );
  /** 衔接缺口：估价接口返回了就用它，还没回来先用本地算的（同一口径） */
  const gaps =
    estimate.status === "ready" ? estimate.gapEpisodes : gapBefore(selected, drama.episodes);

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
            onClick={() => setSelected(unscripted.slice(0, MAX_JOB_EPISODES))}
          >
            没有剧本的
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={available.length === 0}
            onClick={() => setSelected(available.slice(0, MAX_JOB_EPISODES))}
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
        <RangeSelect
          max={options.at(-1)?.no ?? 1}
          onSelect={(from, to) => setSelected(selectByRange(options, from, to))}
        />
        <EpisodeGrid options={options} selected={selected} onToggle={toggle} />
      </section>

      {gaps.length > 0 && (
        <div
          role="note"
          className="bg-status-warning/10 flex items-center gap-2 rounded-lg px-3 py-2 text-sm"
        >
          <Link2Off className="text-status-warning size-4 shrink-0" />
          <span className="min-w-0 flex-1">{gapText(gaps)}</span>
        </div>
      )}

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
 * 已有剧本的可以选来重新生成，选中时提醒会覆盖；选中集的上一集没有剧本且不在选择里时提示衔接方式。
 */
export function ScriptsDialog({
  open,
  onOpenChange,
  initial,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 预先选中的集 */
  initial?: number[];
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        {open && <ScriptsBody initial={initial} onClose={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

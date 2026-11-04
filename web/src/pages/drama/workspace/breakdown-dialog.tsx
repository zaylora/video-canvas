import { useState } from "react";
import { Lock } from "lucide-react";

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
import { useDramaJobsStore } from "@/store/drama";
import {
  breakdownOptions,
  breakdownSubmit,
  defaultBreakdownSelection,
  MAX_JOB_EPISODES,
  selectByRange,
} from "@/utils/drama/episodes";

import { useWorkspace } from "./context";
import { EpisodeGrid, RangeSelect } from "./episode-pick";
import { useEstimate } from "./use-estimate";

/**
 * 对话框主体：每次打开重新挂载，选择状态回到默认。
 * 只列已规划的集；集多时网格是虚拟列表，可以「按范围选：第 a–b 集」。比例建剧时已定，这里不再选。
 */
export function BreakdownBody({
  onClose,
  onStarted,
}: {
  onClose: () => void;
  onStarted: () => void;
}) {
  const { drama, refresh } = useWorkspace();
  const options = breakdownOptions(drama.episodes);
  const [selected, setSelected] = useState<number[]>(() =>
    defaultBreakdownSelection(drama.episodes),
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = breakdownSubmit(selected);
  const estimate = useEstimate(
    drama.id,
    submit.ok ? { step: "breakdown", episodes: selected } : null,
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
      const job = await submitJob(drama.id, { step: "breakdown", episodes: selected });
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
          <b>解析范围</b>
          <span className="text-muted-foreground text-xs tabular-nums">
            已选 {selected.length} 集
          </span>
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto"
            disabled={available.length === 0}
            onClick={() => setSelected(available.slice(0, MAX_JOB_EPISODES))}
          >
            全选可用
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
        <EpisodeGrid
          options={options}
          selected={selected}
          onToggle={toggle}
          maxHeightClass="max-h-56"
        />
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

/**
 * 资产拆解对话框（剧本区底栏、资产区顶栏共用）：集数多选，锁定的、没有剧本的禁用。
 * 比例在建剧（开拍设置）时就定了，这里没有比例选择。
 */
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

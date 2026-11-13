import {
  Box,
  BookOpen,
  Check,
  CircleX,
  FlaskConical,
  Loader2,
  TriangleAlert,
  X,
} from "lucide-react";

import type { StepDetail, TrialCandidate, TrialView } from "@/api/admin/steps/type";
import type { ModelInfo } from "@/api/model/type";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { FoldableCode } from "@/components/admin-ui/foldable-code";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import {
  bindingLabel,
  candidateLabel,
  isTrialPending,
  sampleAllowed,
  trialModelAllowed,
} from "@/utils/admin/steps";

import type { TrialRun } from "./use-steps";

/** 试跑结果的一列：候选或当前绑定，写格式校验、耗时 / token / 积分、硬错误、软问题和输出预览 */
function TrialColumn({
  trial,
  title,
  candidate,
  highlight,
}: {
  trial: TrialView;
  title: string;
  candidate: TrialCandidate;
  highlight: boolean;
}) {
  const pending = isTrialPending(trial);
  const errored = trial.status === "failed";
  const errors = trial.errors ?? [];
  const flags = trial.flags ?? [];
  const Icon = candidate.source === "builtin" ? Box : BookOpen;
  return (
    <div
      className={cn(
        "bg-chrome-hover ring-chrome-border grid content-start gap-3 rounded-2xl p-3.5 text-sm ring-1",
        highlight && "ring-beam/40",
      )}
    >
      <div className="flex flex-wrap items-center gap-1.5">
        <Icon className="size-4" />
        <b className="font-medium">
          {title} · {candidateLabel(candidate)}
        </b>
        <span className="flex-1" />
        {pending ? null : errored ? (
          <Tag tone="danger">
            <X />
            试跑调用失败
          </Tag>
        ) : trial.passed ? (
          <Tag tone="success">
            <Check />
            格式校验通过
          </Tag>
        ) : (
          <Tag tone="danger">
            <X />
            格式校验失败
          </Tag>
        )}
      </div>
      {pending ? (
        <div className="text-muted-foreground flex items-center gap-1.5">
          <Loader2 className="size-4 animate-spin" />
          试跑中…
        </div>
      ) : (
        <>
          <div className="grid grid-cols-4 gap-2 text-xs">
            {[
              ["耗时", `${(trial.duration_ms / 1000).toFixed(1)}s`],
              ["输入", trial.input_tokens.toLocaleString()],
              ["输出", trial.output_tokens.toLocaleString()],
              ["积分", `✦${trial.credits}`],
            ].map(([label, value]) => (
              <div
                key={label}
                className="bg-chrome ring-chrome-border rounded-xl px-2.5 py-1.5 ring-1"
              >
                <div className="text-muted-foreground">{label}</div>
                <b className="text-sm font-medium tabular-nums">{value}</b>
              </div>
            ))}
          </div>
          {errors.length > 0 && (
            <div className="text-destructive grid gap-1">
              {errors.map((error) => (
                <span key={error} className="flex items-start gap-1.5">
                  <CircleX className="mt-0.5 size-3.5 shrink-0" />
                  {error}
                </span>
              ))}
            </div>
          )}
          {flags.length > 0 && (
            <div className="grid gap-1 text-amber-700 dark:text-amber-400">
              {flags.map((flag) => (
                <span key={flag} className="flex items-start gap-1.5">
                  <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
                  软问题：{flag}
                </span>
              ))}
            </div>
          )}
          {trial.preview && (
            <div className="grid gap-1">
              <div className="text-muted-foreground text-xs">输出预览</div>
              <FoldableCode text={trial.preview} defaultOpen maxChars={1200} />
            </div>
          )}
        </>
      )}
    </div>
  );
}

/**
 * 试跑标签：候选（可回方法标签更换）、样例、文本模型、是否同时跑当前绑定对比，
 * 运行后两列并排显示结果（候选 | 当前）。拆解 / 跟拍只能选带参考片的样例，
 * 要看图 / 看视频的环节只能选能看图的文本模型。
 */
export function TrialTab({
  detail,
  candidate,
  sampleKey,
  textModel,
  compare,
  textModels,
  textLoading,
  run,
  candidateIsCurrent,
  onChange,
  onChangeCandidate,
}: {
  detail: StepDetail;
  candidate: TrialCandidate;
  sampleKey: string;
  textModel: string;
  compare: boolean;
  textModels: ModelInfo[];
  textLoading: boolean;
  run: TrialRun | null;
  /** 候选就是当前绑定（且没有退回内置）：对比没有意义 */
  candidateIsCurrent: boolean;
  onChange: (patch: { sampleKey?: string; textModel?: string; compare?: boolean }) => void;
  onChangeCandidate: () => void;
}) {
  const fallback = !!detail.effective.fallback;
  const baseTitle = "当前";
  const baseCandidate: TrialCandidate = fallback ? { source: "builtin" } : detail.binding;
  const Icon = candidate.source === "builtin" ? Box : BookOpen;
  return (
    <div className="grid gap-4">
      <div className="bg-chrome-hover ring-chrome-border grid grid-cols-[4.5rem_1fr] items-center gap-x-3 gap-y-3.5 rounded-2xl p-3.5 text-sm ring-1">
        <span className="text-muted-foreground">候选</span>
        <div className="flex items-center gap-2">
          <Icon className="size-4" />
          <b className="font-medium">{candidateLabel(candidate)}</b>
          {candidateIsCurrent && <Tag>即当前绑定</Tag>}
          <Button variant="ghost" size="xs" onClick={onChangeCandidate}>
            更换
          </Button>
        </div>

        <span className="text-muted-foreground">样例</span>
        <Segmented aria-label="样例" className="flex-wrap justify-self-start rounded-2xl">
          {detail.samples.map((sample) => {
            const allowed = sampleAllowed(detail.key, sample);
            return (
              <SegmentedItem
                key={sample.key}
                slideId="trial-sample"
                active={sampleKey === sample.key}
                disabled={!allowed}
                title={allowed ? undefined : "这个环节需要带参考片的样例"}
                onClick={() => onChange({ sampleKey: sample.key })}
              >
                {sample.name}
              </SegmentedItem>
            );
          })}
        </Segmented>

        <span className="text-muted-foreground">文本模型</span>
        <Segmented aria-label="文本模型" className="flex-wrap justify-self-start rounded-2xl">
          {textModels.map((model) => {
            const allowed = trialModelAllowed(detail.media, { vision: model.capabilities.vision });
            return (
              <SegmentedItem
                key={model.key}
                slideId="trial-model"
                active={textModel === model.key}
                disabled={!allowed}
                title={allowed ? undefined : "这个环节要看图"}
                onClick={() => onChange({ textModel: model.key })}
              >
                {model.label}
                {detail.key.endsWith(".reverse") &&
                model.capabilities.vision &&
                !model.capabilities.video
                  ? "（退回截帧）"
                  : ""}
              </SegmentedItem>
            );
          })}
          {textModels.length === 0 && (
            <span className="text-muted-foreground px-3 py-1.5">
              {textLoading ? "加载中…" : "没有可用的文本模型"}
            </span>
          )}
        </Segmented>

        <span className="text-muted-foreground">对比</span>
        <label className="flex items-center gap-2">
          <Checkbox
            checked={compare && !candidateIsCurrent}
            disabled={candidateIsCurrent}
            onCheckedChange={(checked) => onChange({ compare: checked === true })}
          />
          同时跑当前绑定（{fallback ? "内置方法" : bindingLabel(detail.binding)}）
        </label>
      </div>

      {run ? (
        <div className="grid gap-3 md:grid-cols-2">
          <TrialColumn trial={run.cand} title="候选" candidate={candidate} highlight />
          {run.base && (
            <TrialColumn
              trial={run.base}
              title={baseTitle}
              candidate={baseCandidate}
              highlight={false}
            />
          )}
        </div>
      ) : (
        <div className="bg-chrome-hover ring-chrome-border text-muted-foreground grid place-items-center gap-1.5 rounded-2xl p-8 text-sm ring-1">
          <FlaskConical className="size-6" />
          还没有试跑结果
        </div>
      )}
    </div>
  );
}

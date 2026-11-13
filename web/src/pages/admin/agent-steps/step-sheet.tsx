import { useEffect, useMemo, useState } from "react";
import { BookOpen, Check, FlaskConical, Loader2, Lock, Play } from "lucide-react";
import { toast } from "sonner";

import { bindStep } from "@/api/admin/steps";
import type { StepSummary, TrialCandidate } from "@/api/admin/steps/type";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { LoadFailed } from "@/components/admin-ui/load-failed";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/admin-ui/sheet";
import { UnderlineTab, UnderlineTabs } from "@/components/admin-ui/underline-tabs";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useRemoteModels } from "@/hooks/use-models";
import {
  activateState,
  bindingLabel,
  candidateLabel,
  sameCandidate,
  sampleAllowed,
  trialModelAllowed,
} from "@/utils/admin/steps";

import { ContractTab, MethodTab } from "./step-tabs";
import { TrialTab } from "./trial-tab";
import { trialInput, useStepDetail, useTrialRun } from "./use-steps";

/** 抽屉里的三个标签 */
type Tab = "contract" | "method" | "trial";

const TABS: Array<{ id: Tab; label: string; icon: typeof Lock }> = [
  { id: "contract", label: "协议", icon: Lock },
  { id: "method", label: "方法", icon: BookOpen },
  { id: "trial", label: "试跑", icon: FlaskConical },
];

/**
 * 环节详情抽屉，三个标签：协议（只读）、方法（当前绑定、候选与版本、绑定历史）、试跑（选样例和文本模型，
 * 可同时跑当前绑定对比，运行后轮询结果）。「设为生效」只在候选试跑通过时可点，点后二次确认。
 * @param stepKey 打开的环节 key；null 关闭
 * @param summary 列表里这个环节的概要（抽屉标题先用它）
 * @param onClose 关闭
 * @param onBound 设为生效成功后：刷新列表
 */
export function StepSheet({
  stepKey,
  summary,
  onClose,
  onBound,
}: {
  stepKey: string | null;
  summary: StepSummary | undefined;
  onClose: () => void;
  onBound: () => void;
}) {
  return (
    <Sheet open={!!stepKey} onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="sm:max-w-3xl" showCloseButton>
        {stepKey && (
          <SheetBody key={stepKey} stepKey={stepKey} summary={summary} onBound={onBound} />
        )}
      </SheetContent>
    </Sheet>
  );
}

/** 抽屉正文：每个环节一份独立状态（key 变了整体重建） */
function SheetBody({
  stepKey,
  summary,
  onBound,
}: {
  stepKey: string;
  summary: StepSummary | undefined;
  onBound: () => void;
}) {
  const { detail, setDetail, status, reload } = useStepDetail(stepKey);
  const text = useRemoteModels("agent");
  const trials = useTrialRun(stepKey);
  const [tab, setTab] = useState<Tab>("contract");
  const [candidate, setCandidate] = useState<TrialCandidate>({ source: "builtin" });
  const [sampleKey, setSampleKey] = useState("");
  const [textModel, setTextModel] = useState("");
  const [compare, setCompare] = useState(true);

  /** 详情第一次到：候选默认当前生效的方法（已退回内置的取内置），样例取第一个能选的 */
  const loadedKey = detail?.key;
  useEffect(() => {
    if (!detail) return;
    setCandidate(detail.effective.fallback ? { source: "builtin" } : { ...detail.binding });
    setSampleKey(detail.samples.find((item) => sampleAllowed(detail.key, item))?.key ?? "");
    // 只在换环节后第一次拿到详情时初始化，详情刷新（设为生效后）另行处理
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadedKey]);

  const allowedText = useMemo(
    () =>
      detail
        ? text.models.filter((model) =>
            trialModelAllowed(detail.media, { vision: model.capabilities.vision }),
          )
        : [],
    [detail, text.models],
  );
  /** 文本模型默认取第一个能用的；已选的不能用了也换掉 */
  useEffect(() => {
    if (allowedText.length === 0) return;
    if (!allowedText.some((model) => model.key === textModel)) setTextModel(allowedText[0].key);
  }, [allowedText, textModel]);

  const fallback = !!detail?.effective.fallback;
  const candidateIsCurrent = !!detail && !fallback && sameCandidate(candidate, detail.binding);

  /** 试跑结果只在候选、样例、模型都没变时有效，变了就清掉（设为生效按钮跟着禁用） */
  const change = (patch: { sampleKey?: string; textModel?: string; compare?: boolean }) => {
    if (patch.sampleKey !== undefined) setSampleKey(patch.sampleKey);
    if (patch.textModel !== undefined) setTextModel(patch.textModel);
    if (patch.compare !== undefined) setCompare(patch.compare);
    trials.reset();
  };
  const changeCandidate = (next: TrialCandidate) => {
    if (!sameCandidate(next, candidate) || next.version !== candidate.version) {
      setCandidate(next);
      trials.reset();
    }
  };

  const running = trials.starting || trials.pending;
  const activation = detail
    ? activateState({
        candidate,
        binding: detail.binding,
        fallback,
        trial: trials.run?.cand ?? null,
      })
    : { disabled: true, reason: "" };
  const cost = trials.run ? trials.run.cand.credits + (trials.run.base?.credits ?? 0) : 0;

  const start = () => {
    if (!detail || !sampleKey || !textModel) return;
    void trials.start(trialInput(candidate, sampleKey, textModel, compare && !candidateIsCurrent));
  };

  const activate = () => {
    if (!detail || !trials.run) return;
    const trialId = trials.run.cand.id;
    const from = fallback ? "内置方法" : bindingLabel(detail.binding);
    void confirm({
      title: `把「${detail.title}」换成 ${candidateLabel(candidate)}？`,
      description:
        "立即生效：之后新开始的环节都用它，进行中的不受影响。输出格式不变，下游不需要任何调整。",
      confirmLabel: "设为生效",
      children: (
        <div className="bg-chrome-hover ring-chrome-border flex items-center gap-2 rounded-2xl px-3.5 py-2.5 text-sm ring-1">
          {from} <span aria-hidden>→</span>
          <b className="font-medium">{candidateLabel(candidate)}</b>
        </div>
      ),
      onConfirm: async () => {
        const next = await bindStep(stepKey, {
          source: candidate.source,
          skill_name: candidate.skill_name,
          version: candidate.version,
          trial_id: trialId,
        });
        setDetail(next);
        trials.reset();
        setTab("method");
        onBound();
        toast.success(`「${detail.title}」已换成 ${candidateLabel(candidate)}`);
      },
    });
  };

  return (
    <>
      <SheetHeader className="gap-3 pb-0">
        <div className="flex items-center gap-2 pr-10">
          <SheetTitle className="text-lg">{detail?.title ?? summary?.title ?? stepKey}</SheetTitle>
          <span className="text-muted-foreground font-mono text-xs">{stepKey}</span>
        </div>
        <UnderlineTabs className="-mx-4 px-4">
          {TABS.map(({ id, label, icon: Icon }) => (
            <UnderlineTab key={id} selected={tab === id} onClick={() => setTab(id)}>
              <Icon />
              {label}
            </UnderlineTab>
          ))}
        </UnderlineTabs>
      </SheetHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4">
        {status === "error" && !detail ? (
          <LoadFailed onRetry={() => void reload()} />
        ) : !detail ? (
          <div className="grid gap-3">
            <Skeleton className="h-24 rounded-2xl" />
            <Skeleton className="h-40 rounded-2xl" />
          </div>
        ) : tab === "contract" ? (
          <ContractTab detail={detail} />
        ) : tab === "method" ? (
          <MethodTab
            detail={detail}
            candidate={candidate}
            onCandidate={changeCandidate}
            onRestore={(restored) => {
              changeCandidate(restored);
              setTab("trial");
              toast.warning("恢复也需要先试跑通过");
            }}
          />
        ) : (
          <TrialTab
            detail={detail}
            candidate={candidate}
            sampleKey={sampleKey}
            textModel={textModel}
            compare={compare}
            textModels={text.models}
            textLoading={text.status === "loading" || text.status === "idle"}
            run={trials.run}
            candidateIsCurrent={candidateIsCurrent}
            onChange={change}
            onChangeCandidate={() => setTab("method")}
          />
        )}
      </div>

      <div className="border-chrome-border flex shrink-0 items-center gap-2 border-t px-4 py-3">
        {tab !== "trial" ? (
          <>
            <span className="text-muted-foreground text-xs">候选：{candidateLabel(candidate)}</span>
            <span className="flex-1" />
            <Button disabled={!detail} onClick={() => setTab("trial")}>
              <FlaskConical />
              去试跑
            </Button>
          </>
        ) : (
          <>
            <span className="text-muted-foreground text-xs tabular-nums">
              {trials.run && !running
                ? `本次试跑 ✦${cost}，记在你的账户`
                : "试跑费用记在操作人账户"}
            </span>
            <span className="flex-1" />
            <Button
              variant="outline"
              disabled={running || !sampleKey || !textModel || !detail}
              onClick={start}
            >
              {running ? <Loader2 className="animate-spin" /> : <Play />}
              {trials.run ? "再跑一次" : "开始试跑"}
            </Button>
            <ReasonTooltip reason={activation.disabled ? activation.reason : null}>
              <Button disabled={activation.disabled} onClick={activate}>
                <Check />
                设为生效
              </Button>
            </ReasonTooltip>
          </>
        )}
      </div>
    </>
  );
}

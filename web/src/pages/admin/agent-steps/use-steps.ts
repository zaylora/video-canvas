import { useCallback, useEffect, useRef, useState } from "react";

import { getStep, getTrial, listFormatters, listSteps, startTrial } from "@/api/admin/steps";
import type {
  FormatterInfo,
  StartTrialInput,
  StepDetail,
  StepSummary,
  TrialCandidate,
  TrialView,
} from "@/api/admin/steps/type";
import { isTrialPending } from "@/utils/admin/steps";

/** 轮询试跑结果的间隔（毫秒） */
const POLL_MS = 1500;

/** 试跑最多等多久（毫秒）：超过就停止轮询，让管理员重新跑 */
const POLL_LIMIT_MS = 5 * 60 * 1000;

/** 列表加载状态 */
type Status = "loading" | "ready" | "error";

/**
 * 环节列表和视频提示词格式化器清单：进页面各拉一次，提供重新拉取（设为生效后刷新）。
 */
export function useSteps() {
  const [steps, setSteps] = useState<StepSummary[]>([]);
  const [formatters, setFormatters] = useState<FormatterInfo[]>([]);
  const [status, setStatus] = useState<Status>("loading");

  const reload = useCallback(async () => {
    try {
      const [list, fmts] = await Promise.all([listSteps(), listFormatters().catch(() => [])]);
      setSteps(list);
      setFormatters(fmts);
      setStatus("ready");
    } catch {
      setStatus((prev) => (prev === "ready" ? "ready" : "error"));
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { steps, formatters, status, reload };
}

/**
 * 一个环节的详情（抽屉打开时拉）：key 变了重新拉，key 为 null 清空。
 * @param key 环节 key
 */
export function useStepDetail(key: string | null) {
  const [detail, setDetail] = useState<StepDetail | null>(null);
  const [status, setStatus] = useState<Status>("loading");

  const reload = useCallback(async () => {
    if (!key) return;
    try {
      setDetail(await getStep(key));
      setStatus("ready");
    } catch {
      setStatus("error");
    }
  }, [key]);

  useEffect(() => {
    setDetail(null);
    setStatus("loading");
    void reload();
  }, [key, reload]);

  return { detail, setDetail, status, reload };
}

/** 一次试跑的结果：候选，以及同时跑的当前绑定（对比） */
export interface TrialRun {
  /** 候选的试跑 */
  cand: TrialView;
  /** 当前绑定的试跑；没勾对比为 null */
  base: TrialView | null;
}

/**
 * 试跑：提交后每 1.5 秒轮询 GET /admin/step-trials/:id 直到成功或失败，两列结果同时更新。
 * 状态放在抽屉层，切标签不会丢；换候选 / 样例 / 模型时调用 reset 清空。
 * @param stepKey 环节 key
 */
export function useTrialRun(stepKey: string) {
  const [run, setRun] = useState<TrialRun | null>(null);
  const [starting, setStarting] = useState(false);
  const generation = useRef(0);
  /** 轮询回调里读最新的试跑状态 */
  const runRef = useRef<TrialRun | null>(null);
  useEffect(() => {
    runRef.current = run;
  }, [run]);

  const reset = useCallback(() => {
    generation.current += 1;
    setRun(null);
  }, []);

  /** 换了环节要清掉上一个环节的结果 */
  useEffect(() => {
    reset();
  }, [stepKey, reset]);

  const start = useCallback(
    async (input: StartTrialInput) => {
      const mine = ++generation.current;
      setStarting(true);
      try {
        const trials = await startTrial(stepKey, input);
        if (mine !== generation.current) return;
        const base = trials.find((item) => item.compare_of !== null) ?? null;
        const cand = trials.find((item) => item !== base) ?? trials[0];
        if (cand) setRun({ cand, base: input.compare ? base : null });
      } catch {
        // 提示由请求层统一弹（该环节已有试跑在进行 68017、技能不可用 68014 等）
      } finally {
        if (mine === generation.current) setStarting(false);
      }
    },
    [stepKey],
  );

  /** 有没有试跑还没出结果：有就继续轮询 */
  const pending = isTrialPending(run?.cand) || isTrialPending(run?.base);
  useEffect(() => {
    if (!pending) return;
    const mine = generation.current;
    const startedAt = Date.now();
    let timer: number;
    const tick = async () => {
      if (mine !== generation.current || Date.now() - startedAt > POLL_LIMIT_MS) return;
      try {
        const current = runRef.current;
        if (!current || mine !== generation.current) return;
        const [cand, base] = await Promise.all([
          isTrialPending(current.cand) ? getTrial(current.cand.id) : current.cand,
          current.base && isTrialPending(current.base) ? getTrial(current.base.id) : current.base,
        ]);
        if (mine !== generation.current) return;
        setRun({ cand, base });
        if (isTrialPending(cand) || isTrialPending(base)) timer = window.setTimeout(tick, POLL_MS);
      } catch {
        if (mine === generation.current) timer = window.setTimeout(tick, POLL_MS * 2);
      }
    };
    timer = window.setTimeout(tick, POLL_MS);
    return () => window.clearTimeout(timer);
  }, [pending, run?.cand.id]);

  return { run, starting, start, reset, pending };
}

/** 候选的试跑请求体 */
export const trialInput = (
  candidate: TrialCandidate,
  sampleKey: string,
  textModel: string,
  compare: boolean,
): StartTrialInput => ({
  candidate,
  sample_key: sampleKey,
  text_model: textModel,
  compare,
});

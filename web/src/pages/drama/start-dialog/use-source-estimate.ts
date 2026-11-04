import { useEffect, useState } from "react";

import { estimateSource } from "@/api/drama";
import { dramaErrorMessage } from "@/api/drama/errors";
import type { SourceEstimate, SourceEstimateInput } from "@/api/drama/type";

/** 预估的加载状态 */
export type SourceEstimateState =
  | { status: "ready"; estimate: SourceEstimate }
  | { status: "loading"; estimate: SourceEstimate | null }
  | { status: "error"; message: string; estimate: SourceEstimate | null };

/** 防抖毫秒数：范围 / 时长 / 模型连续改动时只在停下后请求一次 */
export const ESTIMATE_DEBOUNCE_MS = 300;

const keyOf = (input: SourceEstimateInput) =>
  `${input.sourceKind}|${input.rangeEnd}|${input.episodeSeconds}|${input.textModel}`;

/**
 * 开拍设置的预估：范围、单集时长、模型、类型变化后停 300ms 才调草稿估价接口，旧请求的结果作废。
 * 条件和预检时一致（initialInput）就直接用预检带回的预估，不重复请求。
 * input 为 null（类型还没确认）时不请求，沿用上一次的结果。
 * @param token 草稿 token
 * @param input 当前设置
 * @param initial 预检带回的预估，以及它对应的设置
 */
export function useSourceEstimate(
  token: string,
  input: SourceEstimateInput | null,
  initial: { input: SourceEstimateInput; estimate: SourceEstimate },
): SourceEstimateState {
  const [done, setDone] = useState<{
    key: string;
    result: SourceEstimate | { error: string };
  } | null>(null);
  const [last, setLast] = useState<SourceEstimate | null>(initial.estimate);
  const key = input ? keyOf(input) : "";
  const initialKey = keyOf(initial.input);

  useEffect(() => {
    if (!input || key === initialKey) return;
    let active = true;
    const timer = setTimeout(() => {
      estimateSource(token, input)
        .then((estimate) => {
          if (!active) return;
          setLast(estimate);
          setDone({ key, result: estimate });
        })
        .catch((error: unknown) => {
          if (active) setDone({ key, result: { error: dramaErrorMessage(error, "估算失败") } });
        });
    }, ESTIMATE_DEBOUNCE_MS);
    return () => {
      active = false;
      clearTimeout(timer);
    };
    // input 的内容已编码进 key
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [token, key, initialKey]);

  if (!input) return { status: "ready", estimate: last ?? initial.estimate };
  if (key === initialKey) return { status: "ready", estimate: initial.estimate };
  if (done?.key === key) {
    return "error" in done.result
      ? { status: "error", message: done.result.error, estimate: last }
      : { status: "ready", estimate: done.result };
  }
  return { status: "loading", estimate: last };
}

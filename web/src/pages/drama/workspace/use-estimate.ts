import { useEffect, useState } from "react";

import { estimateJob } from "@/api/drama";
import { dramaErrorMessage } from "@/api/drama/errors";
import type { DramaJobInput } from "@/api/drama/type";

export type Estimate =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready"; credits: number }
  | { status: "error"; message: string };

/**
 * 积分估算（POST /estimate）：输入变化后停 250ms 才请求，旧请求的结果作废。
 * input 为 null 表示不需要估算（如还没选集）。
 * 估算失败（如 67006 前置未完成、67007 没有 Agent 模型）把原因交给界面展示。
 */
export function useEstimate(dramaId: string, input: DramaJobInput | null): Estimate {
  const key = input
    ? `${input.step}|${input.episodes.join(",")}|${input.ratio ?? ""}|${input.maxSeconds ?? ""}`
    : "";
  const [result, setResult] = useState<{ key: string; value: Estimate } | null>(null);

  useEffect(() => {
    if (!input) return;
    let active = true;
    const timer = setTimeout(() => {
      estimateJob(dramaId, input)
        .then((credits) => active && setResult({ key, value: { status: "ready", credits } }))
        .catch(
          (error: unknown) =>
            active &&
            setResult({
              key,
              value: { status: "error", message: dramaErrorMessage(error, "估算失败") },
            }),
        );
    }, 250);
    return () => {
      active = false;
      clearTimeout(timer);
    };
    // input 的内容已编码进 key
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [dramaId, key]);

  if (!input) return { status: "idle" };
  return result?.key === key ? result.value : { status: "loading" };
}

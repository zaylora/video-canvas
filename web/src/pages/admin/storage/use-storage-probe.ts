import { useCallback, useState } from "react";

import type { ProbeResult } from "@/api/admin/storage/type.d";

import { useAliveRef } from "../use-admin";

/** 测试连接的状态 */
export type ProbeState =
  /** 还没测过，或表单改过、旧结果已作废 */
  | { kind: "idle" }
  /** 测试进行中 */
  | { kind: "running" }
  /** 已有结果；source 区分测的是表单里的草稿还是已保存的配置 */
  | { kind: "done"; result: ProbeResult; source: "draft" | "saved" };

/**
 * 弹窗里的测试连接状态机。请求抛错（配置不合法等）时回到 idle，原因由全局 toast 展示；
 * 组件卸载后到达的结果会被丢弃。
 * @returns 当前状态，执行测试的 run，以及让旧结果作废的 reset
 */
export function useStorageProbe() {
  const aliveRef = useAliveRef();
  const [state, setState] = useState<ProbeState>({ kind: "idle" });

  const run = useCallback(
    async (source: "draft" | "saved", request: () => Promise<ProbeResult>) => {
      setState({ kind: "running" });
      try {
        const result = await request();
        if (aliveRef.current) setState({ kind: "done", result, source });
        return result;
      } catch {
        if (aliveRef.current) setState({ kind: "idle" });
        return null;
      }
    },
    [aliveRef],
  );

  const reset = useCallback(() => setState({ kind: "idle" }), []);

  return { state, run, reset };
}

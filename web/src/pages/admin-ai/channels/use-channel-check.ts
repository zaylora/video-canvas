import { useCallback, useState } from "react";

import { checkChannel } from "@/api/admin-ai";
import { classifyCheck, classifyCheckError, type CheckOutcome } from "@/utils/admin/channel-check";

import { useAliveRef } from "../use-admin";

/** 某个渠道的检查状态：进行中，或已有结果 */
export type CheckState = { busy: true } | { busy: false; outcome: CheckOutcome };

/**
 * 渠道连通性检查：结果按渠道 key 记在内存里，列表行与抽屉共用一份。
 * 抛错（409 / 50015、503 / 50021）在这里翻译成分类结果，全局 toast 照常由拦截器弹，不重复。
 */
export function useChannelChecks() {
  const aliveRef = useAliveRef();
  const [checks, setChecks] = useState<Record<string, CheckState>>({});

  const run = useCallback(
    async (key: string) => {
      setChecks((prev) => ({ ...prev, [key]: { busy: true } }));
      let outcome: CheckOutcome;
      try {
        outcome = classifyCheck(await checkChannel(key));
      } catch (error) {
        outcome = classifyCheckError(error);
      }
      if (aliveRef.current) setChecks((prev) => ({ ...prev, [key]: { busy: false, outcome } }));
    },
    [aliveRef],
  );

  return { checks, run };
}

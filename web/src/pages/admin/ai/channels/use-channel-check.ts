import { useCallback, useState } from "react";

import { checkChannelDraft } from "@/api/admin/ai";
import type { ChannelCheckDraftRequest } from "@/api/admin/ai/type.d";
import { classifyCheck, classifyCheckError, type CheckOutcome } from "@/utils/admin/channel-check";

import { useAliveRef } from "../../use-admin";

/** 保存前检查的状态：进行中，或已有结果（带着检查时的配置指纹，配置变了结果就作废） */
export type CheckState =
  | { busy: true }
  | { busy: false; outcome: CheckOutcome; fingerprint: string };

/**
 * 渠道保存前的连通性检查：用表单里还没保存的配置检查，结果只记在弹窗里（不按渠道 key 缓存）。
 * 抛错（409 / 50015、503 / 50021 等）在这里翻译成分类结果，全局 toast 照常由拦截器弹，不重复。
 */
export function useDraftCheck() {
  const aliveRef = useAliveRef();
  const [state, setState] = useState<CheckState | undefined>();

  const run = useCallback(
    async (request: ChannelCheckDraftRequest, fingerprint: string) => {
      setState({ busy: true });
      let outcome: CheckOutcome;
      try {
        outcome = classifyCheck(await checkChannelDraft(request));
      } catch (error) {
        outcome = classifyCheckError(error);
      }
      if (aliveRef.current) setState({ busy: false, outcome, fingerprint });
    },
    [aliveRef],
  );

  return { state, run };
}

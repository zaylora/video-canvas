import { useEffect, useRef } from "react";

import type { AgentApprovalKind } from "@/api/agent/type";
import { usePetStore } from "@/store/pet";
import { approvalBubble, runEndBubble, runEndReaction } from "@/utils/pet/assistant-notice";
import type { AgentRunLive } from "@/utils/agent/session-state";

/**
 * 助手的冒泡（只在浮窗收起时冒；队列本身也会在浮窗打开时丢掉助手类气泡）：
 * - 出现待审批或提问：不自动消失的气泡，点击打开浮窗；审批处理完或浮窗打开时按 id 收起；
 * - 一轮跑完（busy 从 true 变 false）：成功说「好啦，回复写完了」，出错说「卡住了：原因」，用户自己停掉的不冒；
 *   浮窗打开时不冒，改让输入框上沿的小镜播一次庆祝或沮丧。
 */
export function useAssistantBubbles({
  open,
  busy,
  activeRun,
  pending,
  runs,
  openPanel,
}: {
  open: boolean;
  busy: boolean;
  activeRun: string | null;
  pending: { id: string; kind: AgentApprovalKind } | null;
  runs: Record<string, AgentRunLive>;
  openPanel: () => void;
}) {
  const pendingId = pending?.id ?? null;
  const pendingKind = pending?.kind ?? null;

  useEffect(() => {
    if (!pendingId || !pendingKind || open) return;
    const bubble = approvalBubble(pendingId, pendingKind);
    usePetStore.getState().push({ ...bubble, onClick: openPanel });
    return () => usePetStore.getState().dismiss(bubble.id);
  }, [pendingId, pendingKind, open, openPanel]);

  // 记住上一次的运行状态：busy 翻成 false 的那一刻，要用上一次的运行 id 去查结果
  const prev = useRef({ busy, runId: activeRun });
  useEffect(() => {
    const before = prev.current;
    if (before.busy && !busy && before.runId) {
      const run = runs[before.runId];
      if (open) {
        const kind = runEndReaction(run?.status);
        if (kind) usePetStore.getState().react(kind);
      } else {
        const bubble = runEndBubble(before.runId, run?.status, run?.error ?? "");
        if (bubble) usePetStore.getState().push({ ...bubble, onClick: openPanel });
      }
    }
    prev.current = { busy, runId: activeRun ?? (busy ? before.runId : null) };
  }, [busy, activeRun, runs, open, openPanel]);
}

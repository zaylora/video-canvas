import { useEffect, useState } from "react";

import { quoteMarketing } from "@/api/marketing";
import type { Project, Round, Script } from "@/api/marketing/type";
import { useMarketingStore } from "@/store/marketing";
import { useWorkbenchStore } from "@/store/marketing-workbench";
import { useTasksStore } from "@/store/tasks";
import { marketingNodeId } from "@/utils/marketing/task-node";
import { shotIndexAt } from "@/utils/marketing/workbench";
import { isActiveStatus } from "@/utils/tasks/status";

/**
 * 出片进度 0–100：任务库里这个脚本进行中的任务；还没推送时用脚本记录里最近一次的；都没有为 null。
 */
export function useRenderProgress(script: Script | null): number | null {
  const nodeId = script ? marketingNodeId(script.id) : "";
  const live = useTasksStore((state) => {
    if (!nodeId) return null;
    for (const task of Object.values(state.tasks)) {
      if (task.node_id === nodeId && isActiveStatus(task.status)) return task.progress ?? 0;
    }
    return null;
  });
  return live ?? script?.takes.at(-1)?.progress ?? null;
}

/**
 * 当前播到第几镜：没在播且停在开头时为 -1（不高亮任何一镜）。
 * 只在镜头下标变化时触发重绘，播放过程中检查器不会每帧重渲。
 */
export function useCurrentShot(shots: readonly { start: number; end: number }[]): number {
  return useWorkbenchStore((state) =>
    state.playing || state.time > 0 ? shotIndexAt(shots, state.time) : -1,
  );
}

/** 重写跟拍版的积分：拆解有改动时按本轮设置问一次预估，没问到就不显示数字 */
export function useRefollowCost(project: Project, round: Round): number | null {
  const [cost, setCost] = useState<number | null>(null);
  const dirty = round.reverseDirty;
  useEffect(() => {
    if (!dirty) return;
    let alive = true;
    quoteMarketing({ projectId: project.id, hasRef: true, settings: round.settings })
      .then((quote) => {
        if (alive) setCost(quote.steps.find((item) => item.step === "follow")?.credits ?? null);
      })
      .catch(() => alive && setCost(null));
    return () => {
      alive = false;
    };
  }, [dirty, project.id, round.settings]);
  return dirty ? cost : null;
}

/** 当前轮次与选中的脚本（选中原片时 script 为 null） */
export function useWorkbenchTarget(projectId: string) {
  const roundId = useWorkbenchStore((state) => state.roundId);
  const selection = useWorkbenchStore((state) => state.selection);
  const round = useMarketingStore((state) =>
    state.projects[projectId]?.rounds.find((item) => item.id === roundId),
  );
  const script =
    round && selection && selection !== "ref"
      ? (round.scripts.find((item) => item.id === selection) ?? null)
      : null;
  return { round, selection, script };
}

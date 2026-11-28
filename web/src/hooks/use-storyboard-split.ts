import { useCallback, useContext, useEffect, useRef } from "react";
import { useReactFlow, useStore } from "@xyflow/react";
import { toast } from "sonner";

import { getActiveStoryboardSplit, getStoryboardSplitJob, splitStoryboard } from "@/api/storyboard";
import type { StoryboardSplitJob } from "@/api/storyboard/type";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { useCreditsStore } from "@/store/credits";
import {
  IDLE,
  splitDisabledReason,
  useStoryboardSplit,
  type SplitState,
} from "@/store/storyboard-split";
import type { CanvasEdge, CanvasNode } from "@/types";
import { CanvasGenerationContext, useCurrentCanvasId } from "@/utils/canvas/canvas-id";
import { ApiError } from "@/utils/requests/request";
import { emptyStoryboard, normalizeStoryboard } from "@/utils/storyboard/shots";
import { forgetSplit, recallSplit, rememberSplit } from "@/utils/storyboard/split-memory";
import {
  isSplitSettled,
  markSplitSettled,
  reconcileStoryboardSplit,
  setSplitSink,
  type SplitSink,
} from "@/utils/storyboard/split-sync";
import { upstreamScripts, type UpstreamScripts } from "@/utils/storyboard/upstream-scripts";

/** 上游剧本读数只看这三项：值没变就不重渲染，画布拖动时每帧不会带着节点重渲染 */
const sameScripts = (a: UpstreamScripts, b: UpstreamScripts) =>
  a.script === b.script && a.count === b.count && a.hasScript === b.hasScript;

/** 订阅分镜表上游剧本：连线、剧本正文变了才更新（拆分镜按钮是否可点靠它） */
export function useUpstreamScripts(id: string): UpstreamScripts {
  return useStore(
    (state) =>
      upstreamScripts(id, state.nodes as CanvasNode[], state.edges as unknown as CanvasEdge[]),
    sameScripts,
  );
}

/** 读某个节点当前的拆分状态；节点、弹窗、功能区各自订阅 */
export const useSplitStateOf = (id: string): SplitState =>
  useStoryboardSplit((s) => s.states[id] ?? IDLE);

/** 后端「同一张表正在拆」的业务错误码：说明节点上已有任务在跑（刷新后点了按钮、另一个窗口提交过） */
const SPLIT_BUSY = 30014;
/** 后端「任务不存在」的业务错误码 */
const SPLIT_JOB_NOT_FOUND = 30017;

/**
 * 分镜表节点拆分镜的动作（设计 §6.1–6.4）：
 * - run：提交后端任务就返回，模型在后台跑；结果由 useStoryboardSplitSync 收到推送后写回节点。提交被拒（积分不足等）原因进横幅；
 * - trigger：用户点「拆分镜」，表里已有镜头时先弹覆盖 / 追加 / 取消，没有就直接覆盖（空表）；
 * - retry：失败横幅的「重试」，沿用失败那次的模式。
 * 模型从 store 取，三个入口选的是同一份。
 * @param id 分镜表节点 id
 */
export function useStoryboardSplitAction(id: string) {
  const canvasId = useCurrentCanvasId();
  const collaboration = useContext(CanvasGenerationContext);
  const { getNode, getNodes, getEdges } = useReactFlow<CanvasNode, CanvasEdge>();

  const run = useCallback(
    async (mode: "replace" | "append") => {
      const split = useStoryboardSplit.getState();
      if (
        collaboration?.canGenerate === false ||
        !canvasId ||
        split.stateOf(id).phase === "running"
      )
        return;
      if (getNode(id)?.data.storyboardSplitRunning) return;
      const scripts = upstreamScripts(id, getNodes(), getEdges());
      if (!scripts.script) return;
      split.start(id, mode);
      try {
        const revision = await collaboration?.confirmGeneration(id, "storyboard-split", {
          script: scripts.script.trim(),
          mode,
        });
        if (collaboration && !revision) {
          split.fail(id, "画布修改尚未同步，请稍后重试");
          return;
        }
        const job = await splitStoryboard(canvasId, {
          node_id: id,
          script: scripts.script,
          model: split.model,
          ...(collaboration && revision
            ? { mode, collaboration: { epoch: collaboration.epoch, revision } }
            : {}),
        });
        // 推送比提交的响应先到时结果已经写回，不能再回头记一条永远等不到结果的任务
        if (!isSplitSettled(job.id)) rememberSplit(canvasId, id, { jobId: job.id, mode });
      } catch (err) {
        if (err instanceof ApiError && err.code === SPLIT_BUSY) {
          // 节点上已有任务在跑：按节点补查，恢复成它的进行中状态，不当成失败
          await reconcileStoryboardSplit();
          if (useStoryboardSplit.getState().stateOf(id).phase === "running") return;
          useStoryboardSplit.getState().dismiss(id);
          return;
        }
        const reason = err instanceof ApiError ? err.message : "网络异常，请稍后重试";
        useStoryboardSplit.getState().fail(id, reason);
        // 弹窗关着时用户看不到横幅，靠 toast 告知；弹窗开着时横幅已经说明，toast 也无妨
        toast.error(`拆分镜失败：${reason}`);
      }
    },
    [canvasId, collaboration, getEdges, getNode, getNodes, id],
  );

  const trigger = useCallback(() => {
    if (collaboration?.canGenerate === false) return;
    const existing = getNode(id)?.data.storyboard?.shots.length ?? 0;
    if (existing > 0) useStoryboardSplit.getState().askOverwrite(id, existing);
    else void run("replace");
  }, [collaboration, getNode, id, run]);

  const retry = useCallback(() => {
    const state = useStoryboardSplit.getState().stateOf(id);
    if (state.phase === "failed") void run(state.mode);
  }, [id, run]);

  return { run, trigger, retry, collaboration };
}

/** 节点上记着的任务只在 id 对得上时才算「这条任务」，免得旧任务的收尾带走新任务的记录 */
const rememberedFor = (canvasId: string, nodeId: string, jobId: string) => {
  const entry = recallSplit(canvasId, nodeId);
  return entry?.jobId === jobId ? entry : null;
};

/** 分镜表节点 id 拼成一个串：作为订阅的读数，节点增减才变，拖动画布不触发重渲染 */
const storyboardNodeIds = (nodes: CanvasNode[]) =>
  nodes
    .filter((n) => n.data?.kind === "storyboard")
    .map((n) => n.id)
    .join("\n");

/**
 * 画布里接收拆分镜任务结果的一端（挂在画布里，用 StoryboardSplitSync 组件）：
 * - 登记成 split-sync 的接收端：WebSocket 推送过来的任务快照在这里落到界面和节点上；
 * - 画布加载、新增分镜表节点时，按节点补查任务（本机记着任务 id 就按 id 查，没有就查进行中的），
 *   所以刷新页面、关掉弹窗再回来都能接上还在跑的任务，或者拿到离开期间跑完的结果；
 * - 成功：按提交时选的覆盖 / 追加写回节点；失败：显示失败横幅；节点已被删：什么都不留。
 * 同一份终态只处理一次（推送和补查都可能送来）。
 */
export function useStoryboardSplitSync() {
  const canvasId = useCurrentCanvasId();
  const collaboration = useContext(CanvasGenerationContext);
  const { getNode, getNodes, updateNodeData } = useReactFlow<CanvasNode, CanvasEdge>();
  const history = useCanvasHistoryContext();
  const nodeIds = useStore((state) => storyboardNodeIds(state.nodes as CanvasNode[]));

  const apply = (job: StoryboardSplitJob) => {
    if (
      !canvasId ||
      (job.collaboration_epoch ?? 0) !== (collaboration?.epoch ?? 0) ||
      isSplitSettled(job.id)
    )
      return;
    const split = useStoryboardSplit.getState();
    const id = job.node_id;
    const node = getNode(id);
    const mine = rememberedFor(canvasId, id, job.id);
    const running = split.stateOf(id);

    if (job.status === "pending" || job.status === "running") {
      if (node && running.phase !== "running") split.start(id, mine?.mode ?? "append");
      return;
    }

    markSplitSettled(job.id);
    const mode = running.phase === "running" ? running.mode : (mine?.mode ?? "append");
    if (mine) forgetSplit(canvasId, id);
    if (!node) {
      // 拆分期间节点被删了：没有地方可写，也不留下失败横幅
      split.dismiss(id);
      return;
    }
    if (job.status === "failed") {
      const reason = job.error || "拆分镜失败";
      if (running.phase !== "running") split.start(id, mode);
      useStoryboardSplit.getState().fail(id, reason);
      // 弹窗关着时用户看不到横幅，靠 toast 告知；弹窗开着时横幅已经说明，toast 也无妨
      toast.error(`拆分镜失败：${reason}`);
      return;
    }

    if (collaboration) {
      split.dismiss(id);
      void useCreditsStore.getState().refresh();
      return;
    }
    const incoming = normalizeStoryboard({ shots: job.shots ?? [] }).shots;
    const existing = node.data.storyboard ?? emptyStoryboard();
    const shots = mode === "replace" ? incoming : [...existing.shots, ...incoming];
    updateNodeData(id, { storyboard: { shots } });
    split.done(
      id,
      incoming.map((s) => s.id),
    );
    void useCreditsStore.getState().refresh();
    // 覆盖丢掉了原来的镜头：给一个能撤销的出口（画布撤销栈已盯着分镜表的改动）
    if (mode === "replace" && existing.shots.length > 0) {
      toast.success(`已覆盖为 ${incoming.length} 个镜头`, {
        action: { label: "撤销", onClick: () => history?.undo() },
      });
    }
  };

  /** 补查一批节点：查不到的记录忘掉；网络失败保留记录，下次对账再查 */
  const reconcile = async (ids: string[]) => {
    if (!canvasId) return;
    for (const id of ids) {
      const entry = recallSplit(canvasId, id);
      try {
        const job = entry
          ? await getStoryboardSplitJob(canvasId, entry.jobId)
          : await getActiveStoryboardSplit(canvasId, id);
        if (job) apply(job);
      } catch (err) {
        if (entry && err instanceof ApiError && err.code === SPLIT_JOB_NOT_FOUND) {
          forgetSplit(canvasId, id);
          if (useStoryboardSplit.getState().stateOf(id).phase === "running") {
            useStoryboardSplit.getState().dismiss(id);
          }
        }
      }
    }
  };

  // 接收端读最新的 apply / reconcile：每次渲染都换新闭包，登记的对象只在换画布时重建
  const latest = useRef({ apply, reconcile });
  useEffect(() => {
    latest.current = { apply, reconcile };
  });
  const checked = useRef(new Set<string>());

  useEffect(() => {
    if (!canvasId) return;
    checked.current = new Set();
    const sink: SplitSink = {
      canvasId,
      apply: (job) => latest.current.apply(job),
      reconcile: () =>
        latest.current.reconcile(
          getNodes()
            .filter((n) => n.data?.kind === "storyboard")
            .map((n) => n.id),
        ),
    };
    setSplitSink(sink);
    return () => setSplitSink(null, sink);
  }, [canvasId, getNodes]);

  useEffect(() => {
    if (!canvasId || !nodeIds) return;
    const fresh = nodeIds.split("\n").filter((id) => !checked.current.has(id));
    for (const id of fresh) checked.current.add(id);
    if (fresh.length > 0) void latest.current.reconcile(fresh);
  }, [canvasId, nodeIds]);
}

/**
 * 三个入口（弹窗标题栏、节点功能区、空状态）共用的拆分镜状态：按钮是否可点和原因、进行中、触发和重试。
 * @param id 分镜表节点 id
 */
export function useStoryboardSplitUi(id: string) {
  const scripts = useUpstreamScripts(id);
  const state = useSplitStateOf(id);
  const { trigger, run, retry, collaboration } = useStoryboardSplitAction(id);
  const remoteBusy = useStore(
    (store) => store.nodeLookup.get(id)?.data.storyboardSplitRunning === true,
  );
  const busy = state.phase === "running" || remoteBusy;
  const reason =
    collaboration?.canGenerate === false
      ? "没有协作生成权限"
      : splitDisabledReason(scripts.hasScript, !scripts.script, busy ? "running" : state.phase);
  return { state, reason, busy, trigger, run, retry };
}

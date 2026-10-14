import { useCallback } from "react";
import { useReactFlow } from "@xyflow/react";
import { toast } from "sonner";

import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import { buildFrameNodes, frameFileName, type FrameSpec } from "@/utils/canvas/frame";
import { grabFrame } from "@/utils/canvas/frame-run";
import { isGroupNode } from "@/utils/canvas/group";
import { sizeOfNode } from "@/utils/canvas/place-beside";
import { frameEngine, supportsTrim, UNSUPPORTED_TRIM_MESSAGE } from "@/utils/canvas/trim-engine";
import { uploadRunner } from "@/utils/canvas/upload-runner";

/** 正在截帧的视频节点：同一个视频一次只截一帧，重复点不再开第二个 */
const capturing = new Set<string>();

type Plan = ReturnType<typeof buildFrameNodes>;

/**
 * 视频节点「截取帧」的画布侧：把帧变成画布上的图片节点（带普通连线，和修剪的剪辑节点一样）。
 * 一键首帧、尾帧：先建出节点、进度显示在节点里，再取帧和上传；取帧失败就把节点撤掉。
 * 自定义：面板里截好的帧确认后一次建出多个节点。建节点和连线算一步撤销。
 */
export function useFrameNodes() {
  const { getNode, getNodes, setNodes, setEdges, updateNodeData } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const history = useCanvasHistoryContext();

  /** 先在视频右侧建出等着填图的节点（上传型，进度 0）和连线；视频不存在或是分组返回 null */
  const addPending = useCallback(
    (videoId: string, names: string[]): Plan | null => {
      const video = getNode(videoId);
      if (!video || isGroupNode(video) || names.length === 0) return null;
      const size = sizeOfNode(video);
      const plan = buildFrameNodes({
        source: video,
        nodes: getNodes(),
        files: names.map((name) => ({ name })),
        aspect: size.width / size.height,
      });
      history?.record();
      setNodes((nodes) => [...nodes, ...plan.nodes]);
      setEdges((edges) => [...edges, ...plan.edges]);
      // 节点是「上传中」但文件还没截好：先占位，免得被误判成被中断的上传而标红
      for (const node of plan.nodes) uploadRunner.hold(node.id);
      return plan;
    },
    [getNode, getNodes, history, setEdges, setNodes],
  );

  /** 把先建出来的节点撤掉（取帧失败） */
  const discard = useCallback(
    (plan: Plan) => {
      const gone = new Set(plan.nodes.map((node) => node.id));
      for (const id of gone) uploadRunner.cancel(id);
      setNodes((nodes) => nodes.filter((node) => !gone.has(node.id)));
      setEdges((edges) => edges.filter((edge) => !gone.has(edge.source) && !gone.has(edge.target)));
    },
    [setEdges, setNodes],
  );

  /** 截好的文件交给上传流程：进度写进各自的节点；节点已被用户删掉（占位被清）就不传了 */
  const upload = useCallback(
    (plan: Plan, files: File[]) => {
      const patch = (id: string, change: Partial<CanvasNodeData>) => updateNodeData(id, change);
      const started = plan.nodes.flatMap((node, index) =>
        uploadRunner.isActive(node.id) ? [uploadRunner.start(node.id, files[index], patch)] : [],
      );
      void Promise.all(started).then((outcomes) => {
        const failed = outcomes.filter((outcome) => outcome === "failed").length;
        if (failed > 0) {
          toast.error(
            failed === 1
              ? "帧图上传失败，可以在节点上重试"
              : `${failed} 张帧图上传失败，可以在节点上重试`,
          );
        }
      });
    },
    [updateNodeData],
  );

  /**
   * 把截好的帧落到画布（自定义面板确认时用）：每个文件一个图片节点放在视频右侧，然后上传。
   * @returns 实际建了几个节点；视频已经不在了返回 0
   */
  const placeFrames = useCallback(
    (videoId: string, files: File[]): number => {
      const plan = addPending(
        videoId,
        files.map((file) => file.name),
      );
      if (!plan) return 0;
      upload(plan, files);
      return plan.nodes.length;
    },
    [addPending, upload],
  );

  /**
   * 一键截首帧或尾帧：点下去立刻建出节点，进度显示在节点里；取帧、上传在后台做。
   * 取帧失败时撤掉节点并说清原因（多半是存储没开跨域读取）。
   */
  const captureQuick = useCallback(
    async (videoId: string, spec: Exclude<FrameSpec, number>) => {
      const video = getNode(videoId);
      const src = video && !isGroupNode(video) ? video.data.src : undefined;
      if (!video || isGroupNode(video) || !src) return;
      if (!supportsTrim()) {
        toast(UNSUPPORTED_TRIM_MESSAGE);
        return;
      }
      if (capturing.has(videoId)) {
        toast("正在截取，请稍候");
        return;
      }
      const fileName = frameFileName(video.data.label, spec);
      const plan = addPending(videoId, [fileName]);
      if (!plan) return;
      capturing.add(videoId);
      try {
        const file = await grabFrame(frameEngine, { src, spec, fileName });
        upload(plan, [file]);
      } catch (error) {
        discard(plan);
        toast.error(error instanceof Error ? error.message : "截取失败，请稍后重试");
      } finally {
        capturing.delete(videoId);
      }
    },
    [addPending, discard, getNode, upload],
  );

  return { placeFrames, captureQuick };
}

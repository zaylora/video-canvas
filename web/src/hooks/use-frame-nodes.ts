import { useCallback } from "react";
import { useReactFlow } from "@xyflow/react";
import { toast } from "sonner";

import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import {
  FrameCaptureError,
  frameFileName,
  openFrameReader,
  type FrameSpec,
} from "@/utils/canvas/frame-capture";
import { planFrameNodes } from "@/utils/canvas/frame-nodes";
import { isGroupNode } from "@/utils/canvas/group";
import { uploadRunner } from "@/utils/canvas/upload-runner";

/** 正在截帧的视频节点：同一个视频一次只截一帧，重复点不再开第二个 */
const capturing = new Set<string>();

type Plan = ReturnType<typeof planFrameNodes>;

/**
 * 视频节点「截取帧」的画布侧：把帧变成画布上的图片节点（带来源线）。
 * 节点先建出来、进度显示在节点里，再去截取和上传；截取失败就把节点撤掉。
 * 新节点和来源线在同一次更新里加进画布；节点上的上传进度、失败重试都走现有的上传流程。
 */
export function useFrameNodes() {
  const { getNode, getNodes, setNodes, setEdges, updateNodeData } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();

  /** 先在视频右侧建出等着填图的节点（上传型，进度 0）并挂来源线；视频不存在或不是视频返回 null */
  const addPending = useCallback(
    (videoId: string, names: string[]): Plan | null => {
      const video = getNode(videoId);
      if (!video || isGroupNode(video) || names.length === 0) return null;
      const plan = planFrameNodes(
        video,
        getNodes(),
        names.map((name) => ({ name })),
      );
      setNodes((nodes) => [...nodes, ...plan.nodes]);
      setEdges((edges) => [...edges, ...plan.edges]);
      // 节点是「上传中」，但文件还没截好：先在上传流程里占位，免得被误判成被中断的上传、标红「上传失败」
      for (const node of plan.nodes) uploadRunner.hold(node.id);
      return plan;
    },
    [getNode, getNodes, setEdges, setNodes],
  );

  /** 把先建出来的节点撤掉（截取失败） */
  const discard = useCallback(
    (plan: Plan) => {
      const gone = new Set(plan.nodes.map((node) => node.id));
      for (const id of gone) uploadRunner.cancel(id);
      setNodes((nodes) => nodes.filter((node) => !gone.has(node.id)));
      setEdges((edges) => edges.filter((edge) => !gone.has(edge.source) && !gone.has(edge.target)));
    },
    [setEdges, setNodes],
  );

  /** 截好的文件交给上传流程：进度写进各自的节点；节点已被用户删掉就不传了 */
  const upload = useCallback(
    (plan: Plan, files: File[]) => {
      const patch = (id: string, change: Partial<CanvasNodeData>) => updateNodeData(id, change);
      // 占位还在才传：节点建出来时占了位，被用户删掉时会 cancel 清掉占位。
      // 不能用 getNode 判断——刚建出来的节点要等画布刷新后才查得到，会把还在的节点当成已删除
      const started = plan.nodes.flatMap((node, i) =>
        uploadRunner.isActive(node.id) ? [uploadRunner.start(node.id, files[i], patch)] : [],
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
   * 把截好的帧落到画布（自定义面板确认时用）：每个文件一个图片节点放在视频右侧，挂来源线，然后上传。
   * @param videoId 被截帧的视频节点
   * @param files 截好的图片，按截取顺序
   * @returns 实际建了几个节点
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
   * 一键截首帧或尾帧：点下去立刻在画布上建出节点，进度显示在节点里；
   * 读视频、截取、上传都在后台做，没有额外的加载提示。截取失败时撤掉节点，并说清原因
   * （多半是存储没开跨域读取）。
   */
  const captureQuick = useCallback(
    async (videoId: string, spec: Exclude<FrameSpec, number>) => {
      const video = getNode(videoId);
      const src = video && !isGroupNode(video) ? video.data.src : undefined;
      if (!video || isGroupNode(video) || !src) return;
      if (capturing.has(videoId)) {
        toast("正在截取，请稍候");
        return;
      }
      const fileName = frameFileName(video.data.label, spec);
      const plan = addPending(videoId, [fileName]);
      if (!plan) return;
      capturing.add(videoId);
      try {
        const reader = await openFrameReader(src);
        let file: File;
        try {
          file = await reader.grab(spec, { fileName });
        } finally {
          reader.dispose();
        }
        upload(plan, [file]);
      } catch (error) {
        discard(plan);
        toast.error(error instanceof FrameCaptureError ? error.message : "截取失败，请重试");
      } finally {
        capturing.delete(videoId);
      }
    },
    [addPending, discard, getNode, upload],
  );

  return { placeFrames, captureQuick };
}

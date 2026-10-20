import { useCallback } from "react";
import { useReactFlow } from "@xyflow/react";
import { toast } from "sonner";

import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import type { CanvasEdge, CanvasNode } from "@/types";
import { planTiles } from "@/utils/canvas/grid-split";
import { buildGridSplitNodes } from "@/utils/canvas/grid-split-node";
import { gridSplitDeps, runGridSplit } from "@/utils/canvas/grid-split-run";
import { isGroupNode } from "@/utils/canvas/group";
import { sizeOfNode } from "@/utils/canvas/place-beside";
import { uploadRunner } from "@/utils/canvas/upload-runner";

/** 正在切分的图片节点：同一张图一次只切一回，重复点确认不再开第二个 */
const splitting = new Set<string>();

/**
 * 图片节点「宫格切分」的画布侧（设计稿 §4.3）：确认后每一格建成一个上传型图片节点，
 * 都从源节点连线，和视频截取帧、扩图一样。
 * 点确认时先建出节点（进度显示在节点里），再读原图、切图、限并发上传；
 * 读图失败就把节点撤掉并说清原因，单格上传失败的节点留着，可以在节点上重试。
 * 建节点和连线算一步撤销。
 */
export function useGridSplitNodes() {
  const { getNode, getNodes, setNodes, setEdges, updateNodeData } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const history = useCanvasHistoryContext();

  /**
   * 切分一个图片节点。
   * @param nodeId 源图片节点
   * @param rows 行数
   * @param cols 列数
   * @param xs 竖线位置（0 到 1）
   * @param ys 横线位置（0 到 1）
   * @returns 建了几个节点；源节点不在、没有图或正在切分时返回 0
   */
  const split = useCallback(
    async (
      nodeId: string,
      rows: number,
      cols: number,
      xs: number[],
      ys: number[],
    ): Promise<number> => {
      const source = getNode(nodeId);
      if (!source || isGroupNode(source) || !source.data.src) return 0;
      if (splitting.has(nodeId)) {
        toast("正在切分，请稍候");
        return 0;
      }
      const src = source.data.src;

      // 此刻还不知道原图的真实像素：用节点的显示大小估各格画幅，读到原图后再校正
      const shown = sizeOfNode(source);
      const plan = buildGridSplitNodes({
        source,
        nodes: getNodes(),
        tiles: planTiles(xs, ys, shown.width, shown.height),
        rows,
        cols,
      });
      if (plan.nodes.length === 0) return 0;

      history?.record();
      setNodes((nodes) => [...nodes, ...plan.nodes]);
      setEdges((edges) => [...edges, ...plan.edges]);
      // 节点是「上传中」但文件还没切好：先占位，免得被误判成被中断的上传而标红
      for (const node of plan.nodes) uploadRunner.hold(node.id);

      splitting.add(nodeId);
      try {
        const result = await runGridSplit(
          { src, xs, ys, nodeIds: plan.nodes.map((node) => node.id) },
          gridSplitDeps({
            upload: (id, file) =>
              uploadRunner.start(id, file, (target, patch) => updateNodeData(target, patch)),
            isActive: (id) => uploadRunner.isActive(id),
            patch: (id, patch) => updateNodeData(id, patch),
            release: (id) => uploadRunner.cancel(id),
          }),
        );
        if (result.status === "unreadable") {
          const gone = new Set(plan.nodes.map((node) => node.id));
          for (const id of gone) uploadRunner.cancel(id);
          setNodes((nodes) => nodes.filter((node) => !gone.has(node.id)));
          setEdges((edges) =>
            edges.filter((edge) => !gone.has(edge.source) && !gone.has(edge.target)),
          );
          toast.error(result.error.message);
        } else if (result.failed > 0) {
          toast.error(
            result.failed === 1
              ? "有 1 张切图上传失败，可以在节点上重试"
              : `有 ${result.failed} 张切图上传失败，可以在节点上重试`,
          );
        }
      } finally {
        splitting.delete(nodeId);
      }
      return plan.nodes.length;
    },
    [getNode, getNodes, history, setEdges, setNodes, updateNodeData],
  );

  return { split };
}

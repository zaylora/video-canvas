import { useCallback } from "react";
import { useReactFlow } from "@xyflow/react";
import { useParams } from "react-router";
import { toast } from "sonner";

import { uploadAsset } from "@/api/asset";
import type { Capabilities } from "@/api/model/type";
import { REMOTE_KIND_OF_NODE } from "@/constants/canvas";
import type { CanvasEdge, CanvasNode } from "@/types";
import { isGroupNode } from "@/utils/canvas/group";
import {
  buildOutpaintPrompt,
  frameHeight,
  frameWidth,
  ratioText,
  type OutpaintFrame,
} from "@/utils/canvas/outpaint";
import {
  OutpaintError,
  composeOutpaint,
  type OutpaintImage,
} from "@/utils/canvas/outpaint-compose";
import { planOutpaintNode } from "@/utils/canvas/outpaint-node";
import { uploadRunner } from "@/utils/canvas/upload-runner";
import { buildTaskInput } from "@/utils/tasks/capabilities";
import { submitCanvasTasks } from "@/utils/tasks/gateway";
import { buildSubmittedPatch, describeSubmitError } from "@/utils/tasks/submit";

/** 一次扩图要的东西 */
export type OutpaintRequest = {
  /** 被扩图的图片节点 */
  sourceId: string;
  /** 读好的原图 */
  image: OutpaintImage;
  /** 框（原图像素，原图左上角为原点） */
  frame: OutpaintFrame;
  /** 用户写的提示词，可以为空；固定指令在这里加 */
  prompt: string;
  /** 用的图片模型（原图节点当前选中的） */
  modelKey: string;
  /** 该模型的能力，用来组装任务输入 */
  caps: Capabilities | undefined;
};

/**
 * 图片扩图的提交链路（设计稿 6.17）：点发送的同一刻，在原图右侧建出结果节点（进度显示在节点里），
 * 然后在后台依次：把原图按框拼进透明 PNG → 上传 → 用原图当前的模型提交图生图任务，
 * 之后进度、取消、失败重试都是普通生成节点的流程。
 * 任何一步失败（读不出原图、上传失败、积分不足……）都把刚建的节点连同来源线撤掉并说清原因，原图不受影响。
 * 建节点是同步的，调用方可以不等结果就退出扩图界面。
 */
export function useOutpaintSubmit() {
  const { getNode, getNodes, setNodes, setEdges, updateNodeData } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const { id: canvasId } = useParams();

  return useCallback(
    async (request: OutpaintRequest): Promise<boolean> => {
      const { sourceId, image, frame, modelKey, caps } = request;
      const source = getNode(sourceId);
      if (!source || isGroupNode(source)) return false;
      if (!canvasId) {
        toast.error("画布信息缺失，请刷新页面重试");
        return false;
      }

      const prompt = buildOutpaintPrompt(request.prompt, ratioText(image.size, frame));
      const { node, edge } = planOutpaintNode(source, getNodes(), {
        modelKey,
        prompt,
        aspect: frameWidth(frame) / frameHeight(frame),
      });
      const id = node.id;
      setNodes((nodes) => [...nodes, node]);
      setEdges((edges) => [...edges, edge]);
      // 节点是「上传中」，但上传还没开始：先在上传流程里占位，免得被误判成被中断的上传、标红「上传失败」
      uploadRunner.hold(id);
      /** 占位还在才继续：节点被用户删掉时 flow 会 cancel 清掉占位，不依赖画布状态刷没刷新 */
      const alive = () => uploadRunner.isActive(id);
      const discard = (message: string) => {
        setNodes((nodes) => nodes.filter((item) => item.id !== id));
        setEdges((edges) => edges.filter((item) => item.id !== edge.id));
        toast.error(message);
      };

      try {
        const file = await composeOutpaint(image, frame, `${source.data.label}-扩图.png`);
        if (!alive()) return false;

        let asset;
        try {
          asset = await uploadAsset(file, {
            onProgress: (percent) => {
              if (alive()) updateNodeData(id, { uploadProgress: percent });
            },
          });
        } catch {
          if (alive()) discard("拼图上传失败，请重试");
          return false;
        }
        if (!alive()) return false;

        // 拼图当参考图记在节点上：之后重试、重新生成都用同一份，面板里也看得到
        const params = { prompt, op: "i2i", images: [asset.id] };
        updateNodeData(id, {
          params,
          paramAssets: { [asset.id]: { url: asset.url, fileName: file.name } },
        });
        const built = buildTaskInput(caps, params, undefined, "i2i", []);
        const firstError = Object.values(built.errors)[0];
        if (firstError) {
          discard(firstError);
          return false;
        }

        const items = await submitCanvasTasks({
          kind: REMOTE_KIND_OF_NODE.image,
          model_id: modelKey,
          canvas_id: canvasId,
          node_id: id,
          node_ids: [id],
          input: built.input,
        });
        const item = items[0];
        if (!alive()) return false;
        if (!item?.task) {
          discard(describeSubmitError(item?.error ?? null).message);
          return false;
        }
        const taskId = String(item.task.id);
        updateNodeData(id, (current) => ({
          ...buildSubmittedPatch("image", taskId, current.data),
          uploadProgress: undefined,
        }));
        return true;
      } catch (error) {
        if (alive()) {
          discard(
            error instanceof OutpaintError ? error.message : describeSubmitError(error).message,
          );
        }
        return false;
      } finally {
        uploadRunner.cancel(id);
      }
    },
    [canvasId, getNode, getNodes, setEdges, setNodes, updateNodeData],
  );
}

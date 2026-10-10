import type { CanvasEdge, CanvasNode, FlowNode } from "@/types";

import { planFrameNodes } from "./frame-nodes";

/**
 * 规划扩图结果节点（设计稿 docs/design/画布UI设计 6.17）：放在原图右侧、避开已有节点，
 * 挂一根从原图出来的来源线；节点是生成型图片节点，带原图当前的模型和完整提示词，
 * 先显示进度（拼图、上传中），拼图上传完再提交图生图任务。
 * 摆放和来源线复用截取帧的规划（同样是「从一个节点派生出新图片」），这里只换节点里的数据。
 * @param source 被扩图的图片节点
 * @param nodes 画布上现有的全部节点（算避让、避开重名用）
 * @param args.modelKey 用的图片模型（原图节点当前选中的）
 * @param args.prompt 完整提示词（固定指令 + 用户写的），重试时原样再用
 * @param args.aspect 框的宽 / 高：占位框一开始就是对的形状，出图后再换成图片的真实比例
 * @param newId 生成节点 / 连线 id（测试里可以注入确定的 id）
 */
export function planOutpaintNode(
  source: CanvasNode,
  nodes: FlowNode[],
  args: { modelKey: string; prompt: string; aspect: number },
  newId: () => string = () => crypto.randomUUID(),
): { node: CanvasNode; edge: CanvasEdge } {
  const plan = planFrameNodes(source, nodes, [{ name: `${source.data.label}-扩图.png` }], newId);
  const [base] = plan.nodes;
  const [edge] = plan.edges;
  return {
    node: {
      ...base,
      data: {
        kind: "image",
        label: base.data.label,
        model: args.modelKey,
        status: "running",
        uploadProgress: 0,
        params: { prompt: args.prompt, op: "i2i" },
        aspect: args.aspect,
      },
    },
    edge,
  };
}

import type { CanvasEdge, CanvasNode } from "@/types";

/** 节点还没被 xyflow 测量过时按这个尺寸估算，避免副本叠在一起 */
const FALLBACK_SIZE = { width: 280, height: 220 };
/** 副本之间、副本与已有节点之间至少留的空隙 */
const GAP = 40;

const sizeOf = (node: CanvasNode) => ({
  width: node.measured?.width ?? node.width ?? FALLBACK_SIZE.width,
  height: node.measured?.height ?? node.height ?? FALLBACK_SIZE.height,
});

type Rect = { x: number; y: number; width: number; height: number };

const overlaps = (a: Rect, b: Rect) =>
  a.x < b.x + b.width + GAP &&
  b.x < a.x + a.width + GAP &&
  a.y < b.y + b.height + GAP &&
  b.y < a.y + a.height + GAP;

/** 去掉标题末尾的编号：「视频节点 3」→「视频节点」 */
const baseLabel = (label: string) => label.replace(/\s+\d+$/, "");

/** 同一底名下下一个空着的编号，从 2 开始（原节点算 1） */
function nextLabels(label: string, nodes: CanvasNode[], count: number) {
  const base = baseLabel(label);
  const used = new Set(
    nodes
      .map((node) => node.data.label)
      .filter((item) => baseLabel(item) === base)
      .map((item) => Number(/\s(\d+)$/.exec(item)?.[1] ?? 1)),
  );
  const out: string[] = [];
  for (let n = 2; out.length < count; n++) if (!used.has(n)) out.push(`${base} ${n}`);
  return out;
}

/**
 * 像「复制节点」一样复制出 count 个副本：
 * 复制提示词、模型、参数、手动选的参考素材（prompt / model / params / paramAssets），
 * 每个副本都复制一份原节点「进来的连线」（同样的上游、同样的连接口），所以引用的素材和原节点完全一样；
 * 不复制生成结果与任务状态（src / assetId / text / status / error / taskId），也不复制出去的连线。
 * 副本在原节点右侧依次错开摆放，避开已有节点；返回的副本处于选中状态，方便一次拖开。
 * @param newId 生成节点 / 连线 id（测试里可以注入确定的 id）
 */
export function duplicateNode(
  source: CanvasNode,
  nodes: CanvasNode[],
  edges: CanvasEdge[],
  count: number,
  newId: () => string = () => crypto.randomUUID(),
): { nodes: CanvasNode[]; edges: CanvasEdge[] } {
  const size = sizeOf(source);
  const taken: Rect[] = nodes.map((node) => ({ ...node.position, ...sizeOf(node) }));
  const labels = nextLabels(source.data.label, nodes, count);
  const incoming = edges.filter((edge) => edge.target === source.id);
  const outNodes: CanvasNode[] = [];
  const outEdges: CanvasEdge[] = [];

  for (let i = 0; i < count; i++) {
    // 先往右排，撞到别的节点就换下一行
    let position = { x: source.position.x, y: source.position.y };
    for (let step = 1; ; step++) {
      const row = Math.floor((step - 1) / 4);
      const col = ((step - 1) % 4) + 1;
      position = {
        x: source.position.x + col * (size.width + GAP),
        y: source.position.y + row * (size.height + GAP),
      };
      if (!taken.some((rect) => overlaps({ ...position, ...size }, rect))) break;
      if (step > 200) break; // 极端拥挤时不再找，叠放也比卡死好
    }
    taken.push({ ...position, ...size });

    const { data } = source;
    const id = newId();
    outNodes.push({
      id,
      type: source.type,
      position,
      selected: true,
      data: {
        kind: data.kind,
        label: labels[i],
        model: data.model,
        prompt: data.prompt,
        params: data.params ? structuredClone(data.params) : undefined,
        paramAssets: data.paramAssets ? structuredClone(data.paramAssets) : undefined,
      },
    });
    for (const edge of incoming) {
      outEdges.push({ ...edge, id: newId(), target: id, selected: false });
    }
  }
  return { nodes: outNodes, edges: outEdges };
}

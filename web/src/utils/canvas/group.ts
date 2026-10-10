import type { XYPosition } from "@xyflow/react";

import type { CanvasGroupNode, CanvasNode, FlowNode } from "@/types";

import { newNodeLabel } from "./node-label";
import { DEFAULT_NODE_SIZE } from "./placement";

/**
 * 组框比成员包围盒多出的留白（画布单位）。组名行画在框外，但节点自己的标题行画在卡片上方、
 * 不算在节点测量尺寸里，所以上方要多留 32 左右给它，否则标题会贴着组框上沿。
 */
export const GROUP_PADDING = { x: 48, top: 72, bottom: 48 };
/** 组框最小尺寸，缩放和贴合都不会小于它 */
export const GROUP_MIN = { width: 200, height: 140 };

/**
 * 组的 zIndex：垫在所有节点下面。xyflow 选中节点会把 zIndex 再加 1000，
 * 所以取 -2000，选中后仍在普通节点（0）之下，组框不会盖住压在它上面的别的节点。
 */
export const GROUP_Z_INDEX = -2000;

/** 缩小画布时组名最多放大几倍 */
export const GROUP_TITLE_MAX_SCALE = 4;

/** 还没测量过的普通节点按这个尺寸算 */
const FALLBACK_NODE = DEFAULT_NODE_SIZE;

/** 是不是组节点 */
export const isGroupNode = (node: FlowNode): node is CanvasGroupNode => node.type === "group";

const sizeOf = (node: FlowNode) =>
  isGroupNode(node)
    ? {
        width: node.width ?? node.measured?.width ?? GROUP_MIN.width,
        height: node.height ?? node.measured?.height ?? GROUP_MIN.height,
      }
    : {
        width: node.measured?.width ?? node.width ?? FALLBACK_NODE.width,
        height: node.measured?.height ?? node.height ?? FALLBACK_NODE.height,
      };

/** 组排到最前面（xyflow 要求父节点在子节点之前），其余保持相对顺序 */
const groupsFirst = (nodes: FlowNode[]): FlowNode[] => [
  ...nodes.filter(isGroupNode),
  ...nodes.filter((node) => !isGroupNode(node)),
];

/** 节点在画布上的绝对位置：成员的 position 是相对组左上角的，要加上组的位置 */
export function absolutePosition(node: FlowNode, nodes: FlowNode[]): XYPosition {
  if (!node.parentId) return node.position;
  const parent = nodes.find((candidate) => candidate.id === node.parentId);
  if (!parent) return node.position;
  return { x: parent.position.x + node.position.x, y: parent.position.y + node.position.y };
}

/** 一个组的全部成员 */
export const groupMembers = (nodes: FlowNode[], groupId: string): FlowNode[] =>
  nodes.filter((node) => node.parentId === groupId);

/** 成员相对组左上角的包围盒，没有成员返回 null：缩放组框时的下限依据 */
export function membersBounds(nodes: FlowNode[], groupId: string) {
  const members = groupMembers(nodes, groupId);
  if (members.length === 0) return null;
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -Infinity;
  let y1 = -Infinity;
  for (const member of members) {
    const { width, height } = sizeOf(member);
    x0 = Math.min(x0, member.position.x);
    y0 = Math.min(y0, member.position.y);
    x1 = Math.max(x1, member.position.x + width);
    y1 = Math.max(y1, member.position.y + height);
  }
  return { x0, y0, x1, y1 };
}

/** 一组绝对矩形加上留白，得到组框的位置和尺寸（不小于最小尺寸） */
function frameAround(rects: Array<{ x: number; y: number; width: number; height: number }>) {
  const x0 = Math.min(...rects.map((rect) => rect.x));
  const y0 = Math.min(...rects.map((rect) => rect.y));
  const x1 = Math.max(...rects.map((rect) => rect.x + rect.width));
  const y1 = Math.max(...rects.map((rect) => rect.y + rect.height));
  return {
    position: { x: x0 - GROUP_PADDING.x, y: y0 - GROUP_PADDING.top },
    width: Math.max(GROUP_MIN.width, x1 - x0 + GROUP_PADDING.x * 2),
    height: Math.max(GROUP_MIN.height, y1 - y0 + GROUP_PADDING.top + GROUP_PADDING.bottom),
  };
}

/** 把某个节点改挂到 parent 名下（或脱离），绝对位置保持不变 */
function reparent(node: FlowNode, nodes: FlowNode[], parent: FlowNode | undefined): FlowNode {
  const abs = absolutePosition(node, nodes);
  const { parentId: _old, ...rest } = node;
  return {
    ...rest,
    ...(parent ? { parentId: parent.id } : {}),
    position: parent ? { x: abs.x - parent.position.x, y: abs.y - parent.position.y } : abs,
  } as FlowNode;
}

/**
 * 把选中的普通节点打成一个组：组框是它们的包围盒加留白，成员改存相对坐标，视觉位置不变。
 * 只有被选中的节点入组，包围盒里恰好压着的别的节点不会被带进来；已在别的组里的会被移进新组。
 * 选中的组、不存在的 id 一律忽略，不足 2 个普通节点时不建组（group 为 null，nodes 原样返回）。
 * 返回的列表里组排在最前，新组处于选中、其余节点取消选中。
 * @param newId 生成组 id（测试里可以注入）
 */
export function createGroup(
  nodes: FlowNode[],
  ids: string[],
  label: string,
  newId: () => string = () => crypto.randomUUID(),
): { nodes: FlowNode[]; group: CanvasGroupNode | null } {
  const wanted = new Set(ids);
  const picked = nodes.filter((node) => wanted.has(node.id) && !isGroupNode(node));
  if (picked.length < 2) return { nodes, group: null };

  const frame = frameAround(
    picked.map((node) => ({ ...absolutePosition(node, nodes), ...sizeOf(node) })),
  );
  const group: CanvasGroupNode = {
    id: newId(),
    type: "group",
    position: frame.position,
    width: frame.width,
    height: frame.height,
    zIndex: GROUP_Z_INDEX,
    selected: true,
    data: { label },
  };
  const pickedIds = new Set(picked.map((node) => node.id));
  const rest = nodes.map((node) =>
    pickedIds.has(node.id)
      ? { ...reparent(node, nodes, group), selected: false }
      : node.selected
        ? { ...node, selected: false }
        : node,
  ) as FlowNode[];
  return { nodes: groupsFirst([group, ...rest]), group };
}

/**
 * 松手后这个节点该属于哪个组：节点中心点落在组框内就是那个组，没有返回 undefined。
 * 组不嵌套，组本身永远没有父；重叠的组取更靠后（更上层）的；skip 里的组不参与（正被拖动的组）。
 */
export function resolveParent(
  node: FlowNode,
  nodes: FlowNode[],
  skip: ReadonlySet<string> = new Set(),
): string | undefined {
  if (isGroupNode(node)) return undefined;
  const abs = absolutePosition(node, nodes);
  const { width, height } = sizeOf(node);
  const cx = abs.x + width / 2;
  const cy = abs.y + height / 2;
  for (const candidate of [...nodes].reverse()) {
    if (!isGroupNode(candidate) || skip.has(candidate.id)) continue;
    const size = sizeOf(candidate);
    const { x, y } = candidate.position;
    if (cx >= x && cx <= x + size.width && cy >= y && cy <= y + size.height) return candidate.id;
  }
  return undefined;
}

/** 让节点加入某个组（或传 undefined 退出），绝对位置不变；父没变时原样返回同一个数组 */
export function setParent(
  nodes: FlowNode[],
  nodeId: string,
  parentId: string | undefined,
): FlowNode[] {
  const target = nodes.find((node) => node.id === nodeId);
  if (!target || isGroupNode(target) || target.parentId === parentId) return nodes;
  const parent = parentId ? nodes.find((node) => node.id === parentId) : undefined;
  if (parentId && (!parent || !isGroupNode(parent))) return nodes;
  return groupsFirst(nodes.map((node) => (node === target ? reparent(node, nodes, parent) : node)));
}

/** 解组：组消失，成员原地保留（回到绝对坐标） */
export function ungroup(nodes: FlowNode[], groupId: string): FlowNode[] {
  return nodes
    .filter((node) => node.id !== groupId)
    .map((node) => (node.parentId === groupId ? reparent(node, nodes, undefined) : node));
}

/** 删组要一起删掉的节点 id：组本身加全部成员 */
export const removeGroupWithMembers = (nodes: FlowNode[], groupId: string): string[] => [
  groupId,
  ...groupMembers(nodes, groupId).map((node) => node.id),
];

/** 把组框贴合到成员（含留白），成员的视觉位置不变；整理布局之后用 */
export function fitGroupToMembers(nodes: FlowNode[], groupId: string): FlowNode[] {
  const members = groupMembers(nodes, groupId);
  if (members.length === 0) return nodes;
  const frame = frameAround(
    members.map((node) => ({ ...absolutePosition(node, nodes), ...sizeOf(node) })),
  );
  const moved = new Map<string, FlowNode>();
  for (const member of members) {
    const abs = absolutePosition(member, nodes);
    moved.set(member.id, {
      ...member,
      position: { x: abs.x - frame.position.x, y: abs.y - frame.position.y },
    });
  }
  return nodes.map((node) => {
    if (node.id === groupId)
      return { ...node, position: frame.position, width: frame.width, height: frame.height };
    return moved.get(node.id) ?? node;
  }) as FlowNode[];
}

/**
 * 读档时修整节点：父指向不存在的组、或指向的不是组，就清掉 parentId；组排到成员前面。
 * 旧画布没有组，原样通过。
 */
export function normalizeFlowNodes(nodes: FlowNode[]): FlowNode[] {
  const groupIds = new Set(nodes.filter(isGroupNode).map((node) => node.id));
  const fixed = nodes.map((node) => {
    if (isGroupNode(node))
      return node.zIndex === GROUP_Z_INDEX ? node : { ...node, zIndex: GROUP_Z_INDEX };
    if (!node.parentId || groupIds.has(node.parentId)) return node;
    const { parentId: _gone, ...rest } = node;
    return rest as FlowNode;
  });
  return groupsFirst(fixed);
}

/** 新组的默认名：「组」，被占了叫「组 2」「组 3」 */
export function nextGroupLabel(nodes: FlowNode[]): string {
  return newNodeLabel(
    "组",
    nodes.filter(isGroupNode).map((node) => node.data.label),
  );
}

/**
 * 只关心普通节点的代码（任务回填、建节点、上传）改完之后，并回完整节点表：
 * 组原样保留，普通节点换成 next。这样那些代码不用认识组。
 */
export function mergeContentNodes(prev: FlowNode[], next: CanvasNode[]): FlowNode[] {
  return [...prev.filter(isGroupNode), ...next];
}

/**
 * 把「画布绝对坐标」的目标位置换回各节点自己的坐标系：成员是相对组左上角的，其余就是绝对的。
 * 整理布局按绝对位置排好之后，用它得到可以直接写回 position 的值。
 */
export function localizePositions(
  nodes: FlowNode[],
  absoluteTargets: ReadonlyMap<string, XYPosition>,
): Map<string, XYPosition> {
  const result = new Map<string, XYPosition>();
  for (const [id, abs] of absoluteTargets) {
    const node = nodes.find((candidate) => candidate.id === id);
    const parent = node?.parentId
      ? nodes.find((candidate) => candidate.id === node.parentId)
      : undefined;
    result.set(id, parent ? { x: abs.x - parent.position.x, y: abs.y - parent.position.y } : abs);
  }
  return result;
}

/**
 * 组名行跟着画布缩放反向放大的倍数：画布缩小时组名在屏幕上变小、看不清，
 * 反向放大 1/zoom 倍让它的屏幕字号保持不变；放大画布时不缩小（组名本来就是画布的一部分）。
 * 放大有上限，缩放值异常时按不缩放处理。
 */
export function groupTitleScale(zoom: number): number {
  if (!Number.isFinite(zoom) || zoom <= 0) return 1;
  return Math.min(GROUP_TITLE_MAX_SCALE, Math.max(1, 1 / zoom));
}

/** 缩放组框时，边最少要离成员留多远：上方要给节点标题行（画在卡片上方）留位置 */
const RESIZE_MARGIN = { x: 8, top: 40, bottom: 8 };

/**
 * 缩放组框这一步放不放行：每条边都不能向内越过成员（含最小边距）。
 * 已经偏紧的边（比如旧版留白更小的老组）只是不能再往里收，往外拉照常放行，
 * 所以这里**不能**拿默认留白当下限，否则老组从第一步起就缩放不动。
 * @param current 缩放前的组框（画布绝对坐标）
 * @param next 这一步缩放后的组框
 * @param bounds 成员的绝对包围盒，没有成员传 null
 */
export function canResizeGroup(
  current: { x: number; y: number; width: number; height: number },
  next: { x: number; y: number; width: number; height: number },
  bounds: { x0: number; y0: number; x1: number; y1: number } | null,
): boolean {
  if (!bounds) return true;
  const leftLimit = Math.max(current.x, bounds.x0 - RESIZE_MARGIN.x);
  const topLimit = Math.max(current.y, bounds.y0 - RESIZE_MARGIN.top);
  const rightLimit = Math.min(current.x + current.width, bounds.x1 + RESIZE_MARGIN.x);
  const bottomLimit = Math.min(current.y + current.height, bounds.y1 + RESIZE_MARGIN.bottom);
  return (
    next.x <= leftLimit &&
    next.y <= topLimit &&
    next.x + next.width >= rightLimit &&
    next.y + next.height >= bottomLimit
  );
}

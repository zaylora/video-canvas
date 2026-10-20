import type { FlowNode } from "@/types";

import { GROUP_PADDING, groupMembers, isGroupNode, sizeOf } from "./group";

/** 组框被撑大后，被推开的对象与组框之间留的间距 */
export const GROUP_GROW_GAP = 40;

/** 差不到 1 像素的尺寸是测量取整的噪声，不算要撑大，免得来回抖 */
const GROW_EPSILON = 1;

/** 推开最多连锁几层：A 推 B、B 推 C、C 推 D 算三层；再往后不管，避免一次操作牵动整张画布 */
const MAX_PUSH_DEPTH = 3;

type Rect = { x: number; y: number; w: number; h: number };

const hits = (a: Rect, b: Rect) =>
  a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/**
 * 成员实际画出来的尺寸（measured）比组框留的大时，把组框撑到刚好框住，并推开被它压住的相邻对象。
 *
 * 为什么需要它：后端 Agent 排版时按节点参数估尺寸，成片的真实画幅只有前端量得到（比如模型选了 Auto、
 * 出来是竖图），成员会从组框里伸出来、压住旁边的东西。这是前端的兜底，不是用户的编辑：调用方不要把它记进撤销栈。
 *
 * 规则：
 * - 组框只往右、往下撑大，不缩小；位置和成员的相对坐标都不动。
 * - 被撑大的那一圈新占的地方（右侧一条、下侧一条）里的顶层对象（别的组、散落的节点）要让位：
 *   右侧的往右推、下侧的往下推，推到组框外沿再留 GROUP_GROW_GAP；被推开的对象如果又压住了更远的对象，
 *   连锁推，最多 MAX_PUSH_DEPTH 层。没被压住的对象一个都不动。
 * - 成员跟着自己的组走（成员坐标是相对组的，不用改）。
 * @returns 没有任何变化时原样返回传入的数组（同一个引用），否则返回新数组，没变的节点保持原对象
 */
export function growGroupsToFitMembers(nodes: FlowNode[]): FlowNode[] {
  const rects = new Map<string, Rect>();
  for (const node of nodes) {
    if (node.parentId) continue;
    const size = sizeOf(node);
    rects.set(node.id, { x: node.position.x, y: node.position.y, w: size.width, h: size.height });
  }

  // 1. 每个组按成员要撑到多大
  const grown: Array<{ old: Rect; next: Rect }> = [];
  const grownSize = new Map<string, { w: number; h: number }>();
  for (const group of nodes) {
    if (!isGroupNode(group) || group.parentId) continue;
    const members = groupMembers(nodes, group.id);
    if (members.length === 0) continue;
    let right = 0;
    let bottom = 0;
    for (const member of members) {
      const size = sizeOf(member);
      right = Math.max(right, member.position.x + size.width);
      bottom = Math.max(bottom, member.position.y + size.height);
    }
    const old = rects.get(group.id) as Rect;
    const needW = right + GROUP_PADDING.x;
    const needH = bottom + GROUP_PADDING.bottom;
    const w = needW - old.w > GROW_EPSILON ? Math.ceil(needW) : old.w;
    const h = needH - old.h > GROW_EPSILON ? Math.ceil(needH) : old.h;
    if (w === old.w && h === old.h) continue;
    const next = { ...old, w, h };
    rects.set(group.id, next);
    grownSize.set(group.id, { w, h });
    grown.push({ old, next });
  }
  if (grown.length === 0) return nodes;

  // 2. 推开被新占的地方压住的对象，一层层连锁
  const moved = new Map<string, { x: number; y: number }>();
  const settled = new Set(grownSize.keys()); // 撑大的组自己、已经推过一次的对象不再被推
  let wave = grown;
  for (let depth = 0; depth < MAX_PUSH_DEPTH && wave.length > 0; depth++) {
    const nextWave: typeof wave = [];
    for (const { old, next } of wave) {
      // 这一步新占的两条地方：右侧一条（next 比 old 多出来的宽）、下侧一条（多出来的高）
      const rightFrom = Math.max(old.x + old.w, next.x);
      const bottomFrom = Math.max(old.y + old.h, next.y);
      const rightStrip: Rect = {
        x: rightFrom,
        y: next.y,
        w: next.x + next.w - rightFrom,
        h: next.h,
      };
      const bottomStrip: Rect = {
        x: next.x,
        y: bottomFrom,
        w: next.w,
        h: next.y + next.h - bottomFrom,
      };
      for (const [id, rect] of rects) {
        if (settled.has(id)) continue;
        const toRight = rightStrip.w > 0 && hits(rightStrip, rect);
        const toDown = !toRight && bottomStrip.h > 0 && hits(bottomStrip, rect);
        if (!toRight && !toDown) continue;
        const after: Rect = toRight
          ? { ...rect, x: next.x + next.w + GROUP_GROW_GAP }
          : { ...rect, y: next.y + next.h + GROUP_GROW_GAP };
        rects.set(id, after);
        moved.set(id, { x: after.x, y: after.y });
        settled.add(id);
        nextWave.push({ old: rect, next: after });
      }
    }
    wave = nextWave;
  }

  return nodes.map((node) => {
    const size = grownSize.get(node.id);
    const to = moved.get(node.id);
    if (!size && !to) return node;
    return {
      ...node,
      ...(to ? { position: to } : {}),
      ...(size ? { width: size.w, height: size.h } : {}),
    } as FlowNode;
  });
}

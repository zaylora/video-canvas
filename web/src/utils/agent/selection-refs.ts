import type { NodeOption } from "@/pages/canvas/agent/agent-editor";

/** 画布上能被消息引用的东西：普通节点，或者组（输入框里 @ 的候选不含组，这里要含） */
export type RefNode = { id: string; label: string; kind: NodeOption["kind"] | "group" };

/** 一条消息最多直接显示几个引用的节点，超过的折成「+N」 */
export const SELECTION_SHOWN = 3;

/** 节点种类的中文名：节点没有标题时拿它兜底 */
const KIND_NAME: Record<RefNode["kind"], string> = {
  group: "节点组",
  script: "文本节点",
  image: "图片节点",
  video: "视频节点",
  audio: "音频节点",
};

/** 消息里的一个引用：id 已经解析成当前画布上的标题；节点被删了 missing 为 true，不能再点 */
export type SelectionRef = {
  id: string;
  label: string;
  kind: RefNode["kind"] | undefined;
  missing: boolean;
};

/**
 * 把消息发送时选中的节点 id 解析成标题：按选中的顺序、去重；标题取画布上的当前标题（改过名就是新名），
 * 没标题用种类兜底，节点已经不在画布上标成「已删除的节点」。
 */
export function resolveSelection(
  ids: readonly string[],
  nodes: readonly RefNode[],
): SelectionRef[] {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  return [...new Set(ids)].map((id) => {
    const node = byId.get(id);
    if (!node) return { id, label: "已删除的节点", kind: undefined, missing: true };
    return {
      id,
      label: node.label.trim() || KIND_NAME[node.kind],
      kind: node.kind,
      missing: false,
    };
  });
}

/** 折叠：不展开时只显示前 SELECTION_SHOWN 个，hidden 是被折起来的个数（给「+N」用） */
export function splitSelection<T>(
  refs: readonly T[],
  expanded = false,
): { shown: T[]; hidden: number } {
  if (expanded || refs.length <= SELECTION_SHOWN) return { shown: [...refs], hidden: 0 };
  return { shown: refs.slice(0, SELECTION_SHOWN), hidden: refs.length - SELECTION_SHOWN };
}

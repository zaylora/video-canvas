import { chipNode, type DocNode } from "./editor-doc";
import { resolveSelection, type RefNode, type SelectionRef } from "./selection-refs";

/**
 * 输入框里的浅色占位：画布上选中、但还没写进输入框的节点。
 * 按选中顺序、去重；已经是输入框里 chip 的不再出占位（不重复引用）；画布上已经没有的丢掉。
 * @param selected 画布上选中的节点 id（含组）
 * @param nodes 画布上能被引用的节点（含组），用来解析当前标题
 * @param committed 输入框文档里已经有的节点 chip 的 id
 */
export function pendingRefs(
  selected: readonly string[],
  nodes: readonly RefNode[],
  committed: readonly string[],
): SelectionRef[] {
  const done = new Set(committed);
  return resolveSelection(selected, nodes).filter((ref) => !ref.missing && !done.has(ref.id));
}

/**
 * 输入框激活、占位变深时写进文档的内容：每个占位一个节点 chip（组也用节点引用，后端按节点校验），最后带一个空格，和 @ 插入 chip 一致。
 * 没有占位时返回空数组。
 */
export function ghostContent(refs: readonly SelectionRef[]): DocNode[] {
  if (refs.length === 0) return [];
  return [
    ...refs.map((ref): DocNode => {
      const node = chipNode({ type: "node", id: ref.id, name: ref.label });
      // 组和角色卡的图标和普通节点不同：把种类带进 chip，变深后图标才不变
      return ref.kind === "group" || ref.kind === "character"
        ? { ...node, attrs: { ...node.attrs, nodeKind: ref.kind } }
        : node;
    }),
    { type: "text", text: " " },
  ];
}

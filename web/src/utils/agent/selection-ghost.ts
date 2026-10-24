import { chipNode, type DocNode } from "./editor-doc";
import { resolveSelection, type RefNode, type SelectionRef } from "./selection-refs";

/**
 * 输入框里的浅色占位：画布上选中、但还没写进输入框的节点。
 * 按选中顺序、去重；已经是输入框里 chip 的不再出占位（不重复引用）；画布上已经没有的丢掉。
 * @param selected 画布上选中的节点 id（含组）
 * @param nodes 画布上能被引用的节点（含组），用来解析当前标题
 * @param committed 输入框文档里已经有的节点 chip 的 id
 * @param dismissed 用户已经关掉的占位（引用了又删掉、或发送后清空）：这次选中里不再出，见 nextDismissed
 */
export function pendingRefs(
  selected: readonly string[],
  nodes: readonly RefNode[],
  committed: readonly string[],
  dismissed: readonly string[] = [],
): SelectionRef[] {
  const done = new Set([...committed, ...dismissed]);
  return resolveSelection(selected, nodes).filter((ref) => !ref.missing && !done.has(ref.id));
}

/**
 * 算"已关闭的占位"：占位只在节点刚被选中时出现一次，用户把引用删掉（退格、撤销、发送后清空）就说明不需要了，
 * 节点还选着也不能再冒出来，否则删不掉。画布上取消选中后记录清掉，再选中就当作新的选中，重新出一次占位。
 * 输入框里 chip 变化和画布选中变化都走这里：选中变化时 before 和 after 传同一份即可。
 * @param dismissed 之前已经关闭的
 * @param selected 画布上现在选中的节点 id
 * @param before 变化前输入框里的节点 chip id
 * @param after 变化后输入框里的节点 chip id
 */
export function nextDismissed(
  dismissed: readonly string[],
  selected: readonly string[],
  before: readonly string[],
  after: readonly string[],
): string[] {
  const kept = new Set(after);
  const removed = before.filter((id) => !kept.has(id));
  const picked = new Set(selected);
  return [...new Set([...dismissed, ...removed])].filter((id) => picked.has(id));
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

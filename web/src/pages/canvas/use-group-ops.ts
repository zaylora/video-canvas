import { useCallback } from "react";
import { useReactFlow } from "@xyflow/react";
import { toast } from "sonner";

import type { CanvasEdge, CanvasNode, FlowNode } from "@/types";
import { arrangeNodes } from "@/utils/canvas/arrange";
import {
  absolutePosition,
  createGroup,
  fitGroupToMembers,
  groupMembers,
  isGroupNode,
  localizePositions,
  nextGroupLabel,
  ungroup,
} from "@/utils/canvas/group";

import { animatePositions } from "./arrange-animation";
import type { GroupUi } from "./group-ui";

/**
 * 组的操作：打组、解组、整理组内布局。工具条、快捷键、多选工具条共用，
 * 都是直接改节点表，撤销栈会自己发现并各记一步。
 */
export function useGroupOps(ui: Pick<GroupUi, "setMenuId" | "setRenamingId">) {
  const { getNodes, setNodes } = useReactFlow<FlowNode, CanvasEdge>();
  const { setMenuId, setRenamingId } = ui;

  /** 把选中的节点打成组，新组选中、弹出工具条并直接进入改名 */
  const groupSelected = useCallback(() => {
    const all = getNodes();
    const ids = all.filter((node) => node.selected && !isGroupNode(node)).map((node) => node.id);
    const result = createGroup(all, ids, nextGroupLabel(all));
    if (!result.group) {
      toast.info("至少选中 2 个节点才能打组");
      return;
    }
    setNodes(result.nodes);
    setMenuId(result.group.id);
    setRenamingId(result.group.id);
  }, [getNodes, setMenuId, setNodes, setRenamingId]);

  /** 解散一个组：节点原地保留并保持选中 */
  const ungroupById = useCallback(
    (id: string) => {
      const memberIds = new Set(groupMembers(getNodes(), id).map((node) => node.id));
      setNodes((nodes) =>
        ungroup(nodes, id).map((node) =>
          memberIds.has(node.id)
            ? { ...node, selected: true }
            : node.selected
              ? { ...node, selected: false }
              : node,
        ),
      );
      setMenuId(null);
    },
    [getNodes, setMenuId, setNodes],
  );

  /** 把组内节点按网格重排，排完把组框贴合到成员上 */
  const arrangeGroup = useCallback(
    (id: string) => {
      const all = getNodes();
      const members = groupMembers(all, id) as CanvasNode[];
      if (members.length < 2) return;
      // 按绝对位置排，再换回成员的相对坐标
      const flat = members.map((member) => ({
        ...member,
        position: absolutePosition(member, all),
      }));
      const targets = localizePositions(all, arrangeNodes(flat, "grid"));
      animatePositions({ getNodes, setNodes }, targets, (nodes) => fitGroupToMembers(nodes, id));
    },
    [getNodes, setNodes],
  );

  /** 恰好选中了一个组时返回它的 id，快捷键用 */
  const selectedGroupId = useCallback(() => {
    const selected = getNodes().filter((node) => node.selected);
    return selected.length === 1 && isGroupNode(selected[0]) ? selected[0].id : null;
  }, [getNodes]);

  return { groupSelected, ungroupById, arrangeGroup, selectedGroupId };
}

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";

/** 组的界面状态：菜单开着的是哪个组、哪个组正在拖、哪个组正在改名 */
export type GroupUi = {
  /** 工具条已打开的组：只有「点一下」才打开，拖动顺带选中的不打开（同节点浮层，设计稿 6.4 / 6.10） */
  menuId: string | null;
  /** 正在被拖动的组：拖动时工具条收起，松手后（如果之前开着）浮回来 */
  draggingId: string | null;
  /** 正在改名的组 */
  renamingId: string | null;
  setMenuId: Dispatch<SetStateAction<string | null>>;
  setDraggingId: Dispatch<SetStateAction<string | null>>;
  setRenamingId: Dispatch<SetStateAction<string | null>>;
  /** 请求删除这个组：弹确认框，确认后连成员一起删 */
  requestDelete: (id: string) => void;
};

const NOOP = () => undefined;
const GroupUiContext = createContext<GroupUi>({
  menuId: null,
  draggingId: null,
  renamingId: null,
  setMenuId: NOOP,
  setDraggingId: NOOP,
  setRenamingId: NOOP,
  requestDelete: NOOP,
});

export const GroupUiProvider = GroupUiContext.Provider;
export const useGroupUi = () => useContext(GroupUiContext);

/** 组界面状态的持有者，放在画布里；删除确认由调用方接管 */
export function useGroupUiState(requestDelete: (id: string) => void): GroupUi {
  const [menuId, setMenuId] = useState<string | null>(null);
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const stableDelete = useCallback((id: string) => requestDelete(id), [requestDelete]);
  return useMemo(
    () => ({
      menuId,
      draggingId,
      renamingId,
      setMenuId,
      setDraggingId,
      setRenamingId,
      requestDelete: stableDelete,
    }),
    [draggingId, menuId, renamingId, stableDelete],
  );
}

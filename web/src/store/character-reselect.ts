import { create } from "zustand";

type CharacterReselectState = {
  /** 正在「重新选择角色卡」的节点 id；null 表示没有在选 */
  nodeId: string | null;
  /** 已失效的角色卡节点点「重新选择」：由画布页打开单选的选择框 */
  open: (nodeId: string) => void;
  /** 关闭选择框 */
  close: () => void;
};

/**
 * 角色卡节点 → 画布页的「重新选择」请求。选择框挂在画布页（和双击添加共用一套），
 * 节点本身不持有弹框，免得每个节点都挂一份。
 */
export const useCharacterReselect = create<CharacterReselectState>((set) => ({
  nodeId: null,
  open: (nodeId) => set({ nodeId }),
  close: () => set({ nodeId: null }),
}));

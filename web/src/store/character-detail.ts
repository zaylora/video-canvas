import { create } from "zustand";

type CharacterDetailState = {
  /** 被请求打开详情的角色卡节点 id；null 表示没有在看 */
  nodeId: string | null;
  /** 请求打开某个节点的详情（编辑器里双击角色卡节点时发出） */
  open: (nodeId: string) => void;
  /** 关闭详情 */
  close: () => void;
};

/**
 * 画布编辑器 → 角色卡节点的「打开详情」请求。双击由编辑器统一处理（它不认识角色卡的细节），
 * 详情弹框挂在节点自己身上，节点订阅这里看是不是在叫自己，同一时刻只开一个。
 */
export const useCharacterDetail = create<CharacterDetailState>((set) => ({
  nodeId: null,
  open: (nodeId) => set({ nodeId }),
  close: () => set({ nodeId: null }),
}));

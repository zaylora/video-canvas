import { create } from "zustand";

/**
 * 视频节点「截取帧 → 自定义」的界面状态：同一时间只有一个节点在选帧。
 * 面板在节点下方，画面却要显示在节点自己的画幅里，两处隔着组件树，所以靠这份状态牵线：
 * 节点里的预览层把自己的 canvas 登记进来，面板往里画当前帧。
 * 只存界面状态，不进画布数据，也不进撤销栈。
 */
type FramePickState = {
  /** 正在选帧的节点，没有为 null */
  nodeId: string | null;
  /** 该节点画幅里的预览画布；预览层挂载后登记 */
  canvas: HTMLCanvasElement | null;
  /** 进入选帧 */
  begin: (nodeId: string) => void;
  /** 退出选帧；传了 nodeId 时，只有还是这个节点在选才退出，免得误关别人的 */
  end: (nodeId?: string) => void;
  /** 预览层登记 / 注销自己的画布 */
  setCanvas: (canvas: HTMLCanvasElement | null) => void;
};

export const useFramePickStore = create<FramePickState>((set) => ({
  nodeId: null,
  canvas: null,
  begin: (nodeId) => set({ nodeId, canvas: null }),
  end: (nodeId) =>
    set((state) =>
      nodeId === undefined || state.nodeId === nodeId ? { nodeId: null, canvas: null } : state,
    ),
  setCanvas: (canvas) => set({ canvas }),
}));

import { create } from "zustand";

import type { CameraPose, CharacterPose, DirectorProp, DirectorScene } from "@/types";
import {
  moveKey as moveKeyIn,
  removeKey,
  roundKeyTime,
  setKeyEase,
  upsertKey,
} from "@/utils/director/keys";
import { applyPreset as applyPresetTo, type CameraPresetId } from "@/utils/director/preset";
import { sampleCamera, sampleCharacter } from "@/utils/director/sample";
import {
  addCamera,
  addCharacter,
  addProp,
  canAdd,
  limitMessage,
  removeObject as removeFrom,
  setDuration as setSceneDuration,
} from "@/utils/director/scene";

/** 编辑器撤销栈最多记几步 */
export const DIRECTOR_HISTORY_LIMIT = 100;

/** 视口里变换控件的模式 */
export type DirectorTool = "move" | "rotate";

/** 能添加的对象种类 */
export type DirectorAddKind = "character" | "prop" | "camera";

/** 变换控件、属性面板改对象时传的字段；按对象种类取用 */
export type ObjectPatch = Partial<CameraPose & CharacterPose & Pick<DirectorProp, "w" | "h" | "d">>;

type DirectorState = {
  /** 打开着编辑器的导演台节点；只活在内存里 */
  nodeId: string | null;
  /** 正在编辑的场景；改动由编辑器组件防抖写回节点 */
  scene: DirectorScene | null;
  /** 打开后要不要直接弹导出（节点功能区点「导出视频」） */
  exportOnOpen: boolean;
  /** 导出中：锁住关闭 */
  exporting: boolean;
  selectedId: string | null;
  selectedKey: { id: string; t: number } | null;
  /** 播放头（秒） */
  t: number;
  playing: boolean;
  /**
   * 临时姿态：已有关键帧的对象在任意时刻被拖动时，改动先放这里（显示「未记录」），
   * 按 K 才写进轨道；移动播放头、播放、撤销时丢弃。
   */
  pending: Record<string, CharacterPose | CameraPose>;
  /** 编辑器独立的撤销栈，关闭即清空 */
  past: DirectorScene[];
  future: DirectorScene[];
  /** 拖动开始时的场景：松手时和它比，变了才算一步 */
  gestureBase: DirectorScene | null;
  tool: DirectorTool;
  snap: boolean;
  labels: boolean;

  open: (nodeId: string, scene: DirectorScene, opts?: { exportOnOpen?: boolean }) => void;
  /** 关闭编辑器；导出中拒绝，返回 false */
  close: () => boolean;
  takeExportOnOpen: () => boolean;
  setExporting: (exporting: boolean) => void;
  select: (id: string | null) => void;
  setTime: (t: number) => void;
  setPlaying: (playing: boolean) => void;
  setTool: (tool: DirectorTool) => void;
  toggleSnap: () => void;
  toggleLabels: () => void;
  /** 加一个对象并选中；到上限时不改场景，返回提示 */
  addObject: (kind: DirectorAddKind, cameraPose?: CameraPose) => string | undefined;
  removeObject: (id: string) => void;
  /** 改一个对象的名字、姿势、颜色、画幅等非站位字段，算一步 */
  updateObject: (id: string, patch: Record<string, unknown>) => void;
  /** 改站位 / 机位状态：没有关键帧改静态状态，有关键帧放进临时姿态；不入栈（配合手势或 commit） */
  transformObject: (id: string, patch: ObjectPatch) => void;
  startGesture: () => void;
  endGesture: () => void;
  /** 对象在播放头时刻的样子（含临时姿态） */
  poseOf: (id: string) => CharacterPose | CameraPose | undefined;
  /** 在播放头时刻给人物 / 机位记录关键帧；道具返回 false */
  recordKey: (id: string) => boolean;
  selectKey: (key: { id: string; t: number } | null) => void;
  moveKey: (id: string, from: number, to: number) => void;
  /** 关键帧到下一帧的插值在缓入缓出和线性之间切换 */
  toggleKeyEase: (id: string, t: number) => void;
  deleteSelectedKey: () => void;
  applyPreset: (
    cameraId: string,
    preset: CameraPresetId,
    strength?: number,
  ) => { warning?: string };
  setDuration: (duration: number) => void;
  undo: () => void;
  redo: () => void;
  reset: () => void;
};

/** 会话里打开、关闭时要清空的部分；工具、吸附、标签这些偏好跨会话保留 */
type Session = Pick<
  DirectorState,
  | "nodeId"
  | "scene"
  | "exportOnOpen"
  | "exporting"
  | "selectedId"
  | "selectedKey"
  | "t"
  | "playing"
  | "pending"
  | "past"
  | "future"
  | "gestureBase"
>;

const IDLE: Session = {
  nodeId: null,
  scene: null,
  exportOnOpen: false,
  exporting: false,
  selectedId: null,
  selectedKey: null,
  t: 0,
  playing: false,
  pending: {},
  past: [],
  future: [],
  gestureBase: null,
};

const MIN_CAMERA_HEIGHT = 0.1;
const MAX_PITCH = 85;

/** 把改动里的机位高度、俯仰夹到合法范围 */
const clampCamera = <P extends ObjectPatch>(patch: P): P => ({
  ...patch,
  ...(patch.y !== undefined ? { y: Math.max(MIN_CAMERA_HEIGHT, patch.y) } : {}),
  ...(patch.pitch !== undefined
    ? { pitch: Math.min(MAX_PITCH, Math.max(-MAX_PITCH, patch.pitch)) }
    : {}),
});

const pick = <T extends object>(source: object, fields: readonly (keyof T)[]) =>
  Object.fromEntries(
    fields.flatMap((field) =>
      (source as T)[field] !== undefined ? [[field, (source as T)[field]]] : [],
    ),
  ) as Partial<T>;

const CHARACTER_FIELDS = ["x", "z", "yaw"] as const;
const CAMERA_FIELDS = ["x", "y", "z", "yaw", "pitch", "fov"] as const;
const PROP_FIELDS = ["x", "z", "yaw", "w", "h", "d"] as const;

/**
 * 导演台编辑器的会话（设计稿 docs/design/3D导演台 §6.3、§7）：
 * 打开哪个节点、正在编辑的场景、选中、播放头、临时姿态和独立撤销栈。
 * 不进画布撤销栈，关闭即清空；场景由编辑器组件写回节点。
 */
export const useDirectorStore = create<DirectorState>((set, get) => {
  /** 记一步：把改之前的场景压栈、清掉重做 */
  const commit = (next: DirectorScene, extra: Partial<DirectorState> = {}) => {
    const { scene, past } = get();
    if (!scene || next === scene) return;
    set({
      scene: next,
      past: [...past, scene].slice(-DIRECTOR_HISTORY_LIMIT),
      future: [],
      ...extra,
    });
  };

  return {
    ...IDLE,
    tool: "move",
    snap: true,
    labels: true,

    open: (nodeId, scene, opts) =>
      set({ ...IDLE, nodeId, scene, exportOnOpen: !!opts?.exportOnOpen }),
    close: () => {
      if (get().exporting) return false;
      set({ ...IDLE });
      return true;
    },
    takeExportOnOpen: () => {
      const wanted = get().exportOnOpen;
      if (wanted) set({ exportOnOpen: false });
      return wanted;
    },
    setExporting: (exporting) => set({ exporting }),

    select: (id) => {
      const { scene } = get();
      if (!scene) return;
      const isCamera = !!id && scene.cameras.some((c) => c.id === id);
      set({
        selectedId: id,
        selectedKey: null,
        // 选中机位就切成当前机位：只是视图状态，不算一步
        ...(isCamera && scene.activeCameraId !== id
          ? { scene: { ...scene, activeCameraId: id! } }
          : {}),
      });
    },
    setTime: (t) => {
      const { scene } = get();
      if (!scene) return;
      set({ t: Math.min(scene.duration, Math.max(0, t)), pending: {} });
    },
    setPlaying: (playing) => set({ playing, pending: {} }),
    setTool: (tool) => set({ tool }),
    toggleSnap: () => set((state) => ({ snap: !state.snap })),
    toggleLabels: () => set((state) => ({ labels: !state.labels })),

    addObject: (kind, cameraPose) => {
      const { scene } = get();
      if (!scene) return;
      if (!canAdd(scene, kind)) return limitMessage(kind);
      const newId = () => crypto.randomUUID();
      const added =
        kind === "character"
          ? addCharacter(scene, newId)
          : kind === "prop"
            ? addProp(scene, newId)
            : addCamera(scene, newId, cameraPose);
      commit(added.scene, { selectedId: added.id, selectedKey: null });
      return undefined;
    },
    removeObject: (id) => {
      const { scene, selectedId, selectedKey, pending } = get();
      if (!scene) return;
      const { [id]: _dropped, ...rest } = pending;
      commit(removeFrom(scene, id), {
        pending: rest,
        selectedId: selectedId === id ? null : selectedId,
        selectedKey: selectedKey?.id === id ? null : selectedKey,
      });
    },
    updateObject: (id, patch) => {
      const { scene } = get();
      if (!scene) return;
      const apply = <O extends { id: string }>(list: O[]) =>
        list.map((item) => (item.id === id ? { ...item, ...patch } : item));
      commit({
        ...scene,
        characters: apply(scene.characters),
        props: apply(scene.props),
        cameras: apply(scene.cameras),
      });
    },
    transformObject: (id, patch) => {
      const { scene, pending, t } = get();
      if (!scene) return;
      const character = scene.characters.find((c) => c.id === id);
      const camera = scene.cameras.find((c) => c.id === id);
      const prop = scene.props.find((p) => p.id === id);
      if (prop) {
        const next = pick<DirectorProp>(patch, PROP_FIELDS);
        set({
          scene: {
            ...scene,
            props: scene.props.map((p) => (p.id === id ? { ...p, ...next } : p)),
          },
        });
        return;
      }
      if (character) {
        const next = pick<CharacterPose>(patch, CHARACTER_FIELDS);
        if (character.keys.length) {
          const base = pending[id] ?? sampleCharacter(character, t);
          set({ pending: { ...pending, [id]: { ...base, ...next } } });
        } else {
          set({
            scene: {
              ...scene,
              characters: scene.characters.map((c) => (c.id === id ? { ...c, ...next } : c)),
            },
          });
        }
        return;
      }
      if (camera) {
        const next = clampCamera(pick<CameraPose>(patch, CAMERA_FIELDS));
        if (camera.keys.length) {
          const base = pending[id] ?? sampleCamera(camera, t);
          set({ pending: { ...pending, [id]: { ...base, ...next } } });
        } else {
          set({
            scene: {
              ...scene,
              cameras: scene.cameras.map((c) => (c.id === id ? { ...c, ...next } : c)),
            },
          });
        }
      }
    },
    startGesture: () => set({ gestureBase: get().scene }),
    endGesture: () => {
      const { gestureBase, scene, past } = get();
      if (!gestureBase || !scene) return set({ gestureBase: null });
      if (gestureBase === scene) return set({ gestureBase: null });
      set({
        gestureBase: null,
        past: [...past, gestureBase].slice(-DIRECTOR_HISTORY_LIMIT),
        future: [],
      });
    },
    poseOf: (id) => {
      const { scene, pending, t } = get();
      if (!scene) return undefined;
      if (pending[id]) return pending[id];
      const character = scene.characters.find((c) => c.id === id);
      if (character) return sampleCharacter(character, t);
      const camera = scene.cameras.find((c) => c.id === id);
      return camera ? sampleCamera(camera, t) : undefined;
    },

    recordKey: (id) => {
      const { scene, t, pending } = get();
      if (!scene) return false;
      const character = scene.characters.find((c) => c.id === id);
      const camera = scene.cameras.find((c) => c.id === id);
      if (!character && !camera) return false;
      const pose = get().poseOf(id)!;
      const { [id]: _done, ...rest } = pending;
      const at = Math.round(t * 100) / 100;
      if (character) {
        const keys = upsertKey(character.keys, {
          ...(pose as CharacterPose),
          t: at,
          ease: "smooth",
        });
        commit(
          {
            ...scene,
            characters: scene.characters.map((c) => (c.id === id ? { ...c, keys } : c)),
          },
          { pending: rest, selectedKey: { id, t: at } },
        );
      } else {
        const keys = upsertKey(camera!.keys, { ...(pose as CameraPose), t: at, ease: "smooth" });
        commit(
          { ...scene, cameras: scene.cameras.map((c) => (c.id === id ? { ...c, keys } : c)) },
          { pending: rest, selectedKey: { id, t: at } },
        );
      }
      return true;
    },
    selectKey: (key) => set({ selectedKey: key }),
    moveKey: (id, from, to) => {
      const { scene } = get();
      if (!scene) return;
      const target = Math.round(Math.min(scene.duration, Math.max(0, to)) * 100) / 100;
      commit(
        {
          ...scene,
          characters: scene.characters.map((c) =>
            c.id === id ? { ...c, keys: moveKeyIn(c.keys, from, target) } : c,
          ),
          cameras: scene.cameras.map((c) =>
            c.id === id ? { ...c, keys: moveKeyIn(c.keys, from, target) } : c,
          ),
        },
        { selectedKey: { id, t: target } },
      );
    },
    toggleKeyEase: (id, t) => {
      const { scene } = get();
      if (!scene) return;
      const flip = <K extends { t: number; ease: "smooth" | "linear" }>(keys: K[]) => {
        const key = keys.find((k) => roundKeyTime(k.t) === roundKeyTime(t));
        return key ? setKeyEase(keys, t, key.ease === "linear" ? "smooth" : "linear") : keys;
      };
      commit({
        ...scene,
        characters: scene.characters.map((c) => (c.id === id ? { ...c, keys: flip(c.keys) } : c)),
        cameras: scene.cameras.map((c) => (c.id === id ? { ...c, keys: flip(c.keys) } : c)),
      });
    },
    deleteSelectedKey: () => {
      const { scene, selectedKey } = get();
      if (!scene || !selectedKey) return;
      const { id, t } = selectedKey;
      commit(
        {
          ...scene,
          characters: scene.characters.map((c) =>
            c.id === id ? { ...c, keys: removeKey(c.keys, t) } : c,
          ),
          cameras: scene.cameras.map((c) =>
            c.id === id ? { ...c, keys: removeKey(c.keys, t) } : c,
          ),
        },
        { selectedKey: null },
      );
    },
    applyPreset: (cameraId, preset, strength) => {
      const { scene, pending } = get();
      if (!scene) return {};
      const { [cameraId]: _done, ...rest } = pending;
      const result = applyPresetTo(scene, cameraId, preset, strength);
      commit(result.scene, { pending: rest });
      return { warning: result.warning };
    },
    setDuration: (duration) => {
      const { scene, t } = get();
      if (!scene) return;
      const next = setSceneDuration(scene, duration);
      if (next.duration === scene.duration) return;
      commit(next, { t: Math.min(t, next.duration), pending: {} });
    },
    undo: () => {
      const { past, future, scene } = get();
      if (!past.length || !scene) return;
      set({
        scene: past[past.length - 1],
        past: past.slice(0, -1),
        future: [scene, ...future],
        pending: {},
        selectedKey: null,
      });
    },
    redo: () => {
      const { past, future, scene } = get();
      if (!future.length || !scene) return;
      set({
        scene: future[0],
        future: future.slice(1),
        past: [...past, scene],
        pending: {},
        selectedKey: null,
      });
    },
    reset: () => set({ ...IDLE }),
  };
});

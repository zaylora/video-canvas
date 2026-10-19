import { Vector3, type PerspectiveCamera } from "three";

import { useDirectorStore } from "@/store/director";
import type { CameraPose } from "@/types";
import { DEG } from "@/utils/director/sample";
import {
  grabDepth,
  grabLook,
  moveInput,
  normalizeWheel,
  panPosition,
  pixelRay,
  rotateByLook,
  tiltByWheel,
  walk,
  zoomByWheel,
} from "@/utils/director/free-look";

/** 相机的位置、朝向和竖直视场角（偏航 / 俯仰 / 视场角是度） */
export type LookPose = { x: number; y: number; z: number; yaw: number; pitch: number; fov: number };

/**
 * 被操作的「相机」：导演视角里是主视口自己的相机，机位视角里是场景里那台机位。
 * 滚轮、键盘、拖动都只通过它读写，所以两个视角走同一套逻辑。
 */
export type CameraDriver = {
  read: () => LookPose | undefined;
  write: (patch: Partial<LookPose>) => void;
  /** 一次连续操作开始 / 结束（机位视角里对应撤销栈的一步） */
  begin: () => void;
  end: () => void;
};

/** 键盘平移的速度（米 / 秒） */
const WALK_SPEED = 3;
/** 一帧最多按这么久算，免得切回标签页后一步走很远 */
const MAX_STEP_SECONDS = 0.1;
/** 滚轮停下多久算一次操作结束（一连串滚动在撤销栈里只算一步） */
const WHEEL_IDLE_MS = 400;

/** 导演视角：直接操作主视口的相机 */
export function directorDriver(camera: PerspectiveCamera): CameraDriver {
  const read = (): LookPose => {
    const direction = camera.getWorldDirection(new Vector3());
    return {
      x: camera.position.x,
      y: camera.position.y,
      z: camera.position.z,
      yaw: Math.atan2(direction.x, -direction.z) / DEG,
      pitch: Math.asin(Math.min(1, Math.max(-1, direction.y))) / DEG,
      fov: camera.fov,
    };
  };
  return {
    read,
    write: (patch) => {
      const next = { ...read(), ...patch };
      camera.position.set(next.x, next.y, next.z);
      camera.rotation.set(next.pitch * DEG, -next.yaw * DEG, 0, "YXZ");
      camera.fov = next.fov;
      camera.updateProjectionMatrix();
    },
    begin: () => {},
    end: () => {},
  };
}

/** 机位视角：操作场景里的这台机位，改动走编辑器的变换（有关键帧的先是「未记录」），一次连续操作算撤销栈一步 */
export function sceneCameraDriver(cameraId: string): CameraDriver {
  return {
    read: () => {
      const pose = useDirectorStore.getState().poseOf(cameraId) as CameraPose | undefined;
      return (
        pose && { x: pose.x, y: pose.y, z: pose.z, yaw: pose.yaw, pitch: pose.pitch, fov: pose.fov }
      );
    },
    write: (patch) => useDirectorStore.getState().transformObject(cameraId, patch),
    begin: () => {
      const state = useDirectorStore.getState();
      // 操作机位时暂停播放：播放头在走，机位的姿态会被轨道覆盖
      if (state.playing) state.setPlaying(false);
      state.startGesture();
    },
    end: () => useDirectorStore.getState().endGesture(),
  };
}

/** 能当平移键的物理按键（用物理键位，不受输入法和键盘布局影响），换算成 moveInput 认的名字 */
const MOVE_KEYS: Record<string, string> = {
  KeyW: "w",
  KeyA: "a",
  KeyS: "s",
  KeyD: "d",
  ArrowUp: "arrowup",
  ArrowDown: "arrowdown",
  ArrowLeft: "arrowleft",
  ArrowRight: "arrowright",
};

/** 键盘事件落在这些元素上时交给它们自己（打字、选下拉、菜单） */
const isTypingTarget = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  !!target.closest(
    "input:not([type=checkbox]):not([type=radio]), textarea, select, [contenteditable=true], [data-director-menu]",
  );

type Drag =
  | { kind: "look"; start: LookPose; from: Ndc; started: boolean }
  | { kind: "pan"; start: LookPose; pinned: number[]; from: Ndc; started: boolean };
type Ndc = { x: number; y: number };

/**
 * 在视口画布上绑定自由相机的全部操作（导演视角、机位视角共用）：
 * - 左键按住拖动：抓手。按下时鼠标下的那个点，拖到哪画面就跟到哪，精确、没有惯性
 * - 右键 / 中键按住拖动：平移（只在 allowPan 时，导演视角用），同样抓住一个点
 * - 滚轮：视角上下摇；Ctrl / Alt + 滚轮：变焦（改视场角，机位不动）
 * - WASD / 方向键：沿朝向的水平方向前后左右平移，高度不变（按住时每帧走一步，要有人调用 tick）
 * 一次连续操作（拖动、按住键、一串滚动）是 driver 的一次 begin / end。
 * @param options.camera 主视口的相机：抓手的计算要用它此刻实际的视场角和宽高比
 * @param options.gizmoBusy 变换控件正在被拖：这时左键拖动不当抓手
 * @returns tick 每帧调用一次（秒）推进键盘平移；dispose 解绑并收尾
 */
export function attachFreeCamera(options: {
  element: HTMLElement;
  camera: PerspectiveCamera;
  driver: CameraDriver;
  allowPan: boolean;
  gizmoBusy: { current: boolean };
}): { tick: (seconds: number) => void; dispose: () => void } {
  const { element, camera, driver, allowPan, gizmoBusy } = options;

  // 连续操作的记账：任何一种操作在进行就算「进行中」，从无到有 begin、从有到无 end
  const holders = new Set<string>();
  const hold = (reason: string) => {
    if (holders.has(reason)) return;
    if (holders.size === 0) driver.begin();
    holders.add(reason);
  };
  const release = (reason: string) => {
    if (!holders.delete(reason)) return;
    if (holders.size === 0) driver.end();
  };

  const ndcOf = (event: { clientX: number; clientY: number }): Ndc => {
    const rect = element.getBoundingClientRect();
    return {
      x: ((event.clientX - rect.left) / rect.width) * 2 - 1,
      y: 1 - ((event.clientY - rect.top) / rect.height) * 2,
    };
  };

  let drag: Drag | null = null;
  /** @param pointerId 要释放指针捕获的指针；不传就不释放（变换控件也捕获了同一个指针，不能替它放掉） */
  const endDrag = (pointerId?: number) => {
    if (!drag) return;
    release("drag");
    drag = null;
    element.style.cursor = "grab";
    if (pointerId !== undefined && element.hasPointerCapture(pointerId)) {
      element.releasePointerCapture(pointerId);
    }
  };

  const onPointerDown = (event: PointerEvent) => {
    const pan = event.button === 1 || event.button === 2;
    if (event.button !== 0 && !(pan && allowPan)) return;
    const start = driver.read();
    if (!start) return;
    element.setPointerCapture(event.pointerId);
    const from = ndcOf(event);
    if (pan) {
      const direction = rotateByLook(
        start.yaw,
        start.pitch,
        pixelRay(from.x, from.y, camera.fov, camera.aspect),
      );
      const depth = grabDepth(start, direction);
      const pinned = [
        start.x + direction[0] * depth,
        start.y + direction[1] * depth,
        start.z + direction[2] * depth,
      ];
      drag = { kind: "pan", start, pinned, from, started: false };
    } else {
      drag = { kind: "look", start, from, started: false };
    }
  };

  const onPointerMove = (event: PointerEvent) => {
    if (!drag) return;
    // 按在变换控件上的拖动归控件，不当抓手
    if (gizmoBusy.current) {
      endDrag();
      return;
    }
    if (!drag.started) {
      drag.started = true;
      hold("drag");
      element.style.cursor = "grabbing";
    }
    const now = ndcOf(event);
    if (drag.kind === "look") {
      driver.write(grabLook(drag.start, drag.from, now, camera.fov, camera.aspect));
    } else {
      driver.write(panPosition(drag.start, drag.pinned, now, camera.fov, camera.aspect));
    }
  };

  const onPointerUp = (event: PointerEvent) => endDrag(event.pointerId);

  // 滚轮：上下摇 / 变焦。一串滚动合成一次操作，停 WHEEL_IDLE_MS 后收尾
  let wheelTimer: ReturnType<typeof setTimeout> | undefined;
  const onWheel = (event: WheelEvent) => {
    // 不让页面跟着缩放、滚动（Ctrl + 滚轮默认是浏览器缩放）
    event.preventDefault();
    const pose = driver.read();
    const delta = normalizeWheel(event.deltaY, event.deltaMode);
    if (!pose || delta === 0) return;
    hold("wheel");
    if (event.ctrlKey || event.altKey) driver.write({ fov: zoomByWheel(pose.fov, delta) });
    else driver.write({ pitch: tiltByWheel(pose.pitch, delta) });
    clearTimeout(wheelTimer);
    wheelTimer = setTimeout(() => release("wheel"), WHEEL_IDLE_MS);
  };

  // 键盘平移：按住的键在 held 里，tick 每帧走一步
  const held = new Set<string>();
  const onKeyDown = (event: KeyboardEvent) => {
    const name = MOVE_KEYS[event.code];
    if (!name || event.ctrlKey || event.metaKey || event.altKey || isTypingTarget(event.target)) {
      return;
    }
    // 导出进行中不响应：这时画面在逐帧渲染，场景不能动
    if (useDirectorStore.getState().exporting) return;
    // 方向键默认会滚动页面
    event.preventDefault();
    held.add(name);
    hold("keys");
  };
  const onKeyUp = (event: KeyboardEvent) => {
    const name = MOVE_KEYS[event.code];
    if (!name) return;
    held.delete(name);
    if (held.size === 0) release("keys");
  };
  const onBlur = () => {
    held.clear();
    release("keys");
  };

  const onContextMenu = (event: Event) => {
    if (allowPan) event.preventDefault();
  };

  element.style.cursor = "grab";
  element.addEventListener("pointerdown", onPointerDown);
  element.addEventListener("pointermove", onPointerMove);
  element.addEventListener("pointerup", onPointerUp);
  element.addEventListener("pointercancel", onPointerUp);
  element.addEventListener("wheel", onWheel, { passive: false });
  element.addEventListener("contextmenu", onContextMenu);
  // 键盘在窗口的捕获阶段听：编辑器自己的快捷键处理会拦住冒泡
  window.addEventListener("keydown", onKeyDown, true);
  window.addEventListener("keyup", onKeyUp, true);
  window.addEventListener("blur", onBlur);

  return {
    tick: (seconds) => {
      if (held.size === 0) return;
      const input = moveInput(held);
      const pose = driver.read();
      if (!pose || (input.forward === 0 && input.right === 0)) return;
      const next = walk(pose, pose.yaw, input, WALK_SPEED * Math.min(seconds, MAX_STEP_SECONDS));
      driver.write({ x: next.x, z: next.z });
    },
    dispose: () => {
      element.removeEventListener("pointerdown", onPointerDown);
      element.removeEventListener("pointermove", onPointerMove);
      element.removeEventListener("pointerup", onPointerUp);
      element.removeEventListener("pointercancel", onPointerUp);
      element.removeEventListener("wheel", onWheel);
      element.removeEventListener("contextmenu", onContextMenu);
      window.removeEventListener("keydown", onKeyDown, true);
      window.removeEventListener("keyup", onKeyUp, true);
      window.removeEventListener("blur", onBlur);
      clearTimeout(wheelTimer);
      element.style.cursor = "";
      // 操作做到一半被解绑：收尾，不留半截手势
      held.clear();
      drag = null;
      for (const reason of [...holders]) release(reason);
    },
  };
}

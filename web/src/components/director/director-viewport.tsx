import {
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type Ref,
} from "react";
import { Canvas, useFrame, useThree, type ThreeEvent } from "@react-three/fiber";
import { OrbitControls, TransformControls } from "@react-three/drei";
import { Vector3, type Group, type PerspectiveCamera, type Quaternion } from "three";

import { useDirectorStore } from "@/store/director";
import type { CameraPose } from "@/types";
import { activeCameraOf, lookBy, viewFov } from "@/utils/director/camera-view";
import { ASPECT_RATIO } from "@/utils/director/export";
import { FIGURE_TOP } from "@/utils/director/figure";
import { createThreeScene } from "@/utils/director/render";
import { DEG } from "@/utils/director/sample";
import { applyCameraPose, createStage, type DirectorStage } from "@/utils/director/stage";

import { CameraFrame } from "./camera-frame";

/** 视口对外的操作：工具栏、快捷键调用 */
export type DirectorViewportApi = {
  /** 导演视角对准选中的对象 */
  focusSelected: () => void;
  /** 当前导演视角的位置和朝向，「从当前视角新建机位」用 */
  viewPose: () => CameraPose;
};

/** 导演视角控制器里用到的部分（drei 的 OrbitControls 实例） */
type OrbitHandle = { target: Vector3; update: () => void };

/** 点击时指针移动超过这么多像素就算拖动视角，不当点选 */
const CLICK_SLOP = 4;
/** 吸附：移动 0.25 米、旋转 15° 一档 */
const SNAP_MOVE = 0.25;
const SNAP_TURN = 15 * DEG;

/** 导演视角的相机状态：进机位视角前存下，退出时放回去 */
type SavedView = {
  position: Vector3;
  quaternion: Quaternion;
  fov: number;
  target: Vector3 | undefined;
};

const saveView = (camera: PerspectiveCamera, orbit: OrbitHandle | null): SavedView => ({
  position: camera.position.clone(),
  quaternion: camera.quaternion.clone(),
  fov: camera.fov,
  target: orbit?.target.clone(),
});

function restoreView(
  camera: PerspectiveCamera,
  saved: SavedView,
  size: { width: number; height: number },
  orbit: OrbitHandle | null,
) {
  camera.position.copy(saved.position);
  camera.quaternion.copy(saved.quaternion);
  camera.fov = saved.fov;
  camera.aspect = size.width / size.height;
  camera.updateProjectionMatrix();
  if (saved.target && orbit) {
    orbit.target.copy(saved.target);
    orbit.update();
  }
}

/**
 * 让主视口的相机站进机位里看：位置、朝向照机位的，竖直视场角按画幅取景调整（见 viewFov）。
 * @param ratio 机位画幅的宽高比
 * @param aspect 视口的宽高比
 */
function viewThrough(camera: PerspectiveCamera, pose: CameraPose, ratio: number, aspect: number) {
  applyCameraPose(camera, pose, aspect);
  camera.fov = viewFov(pose.fov, ratio, aspect);
  camera.updateProjectionMatrix();
  // 标签投影要用最新的矩阵，不等渲染时才更新
  camera.updateMatrixWorld();
}

/**
 * 在视口画布上绑定「按住左键拖动调整机位朝向」：每次拖动是撤销栈里的一步，拖动时暂停播放。
 * @returns 解绑函数；拖到一半被解绑时把这一步收尾
 */
function attachLook(
  element: HTMLElement,
  camId: string,
  camera: PerspectiveCamera,
  size: { current: { height: number } },
): () => void {
  let last: { x: number; y: number } | null = null;
  const down = (event: PointerEvent) => {
    if (event.button !== 0) return;
    element.setPointerCapture(event.pointerId);
    last = { x: event.clientX, y: event.clientY };
    const state = useDirectorStore.getState();
    if (state.playing) state.setPlaying(false);
    state.startGesture();
    element.style.cursor = "grabbing";
  };
  const move = (event: PointerEvent) => {
    if (!last) return;
    const dx = event.clientX - last.x;
    const dy = event.clientY - last.y;
    last = { x: event.clientX, y: event.clientY };
    const state = useDirectorStore.getState();
    const pose = state.poseOf(camId) as CameraPose | undefined;
    if (!pose) return;
    state.transformObject(camId, lookBy(pose, dx, dy, camera.fov, size.current.height));
  };
  const up = (event: PointerEvent) => {
    if (!last) return;
    last = null;
    if (element.hasPointerCapture(event.pointerId)) element.releasePointerCapture(event.pointerId);
    useDirectorStore.getState().endGesture();
    element.style.cursor = "grab";
  };
  element.style.cursor = "grab";
  element.addEventListener("pointerdown", down);
  element.addEventListener("pointermove", move);
  element.addEventListener("pointerup", up);
  element.addEventListener("pointercancel", up);
  return () => {
    element.removeEventListener("pointerdown", down);
    element.removeEventListener("pointermove", move);
    element.removeEventListener("pointerup", up);
    element.removeEventListener("pointercancel", up);
    element.style.cursor = "";
    if (last) useDirectorStore.getState().endGesture();
  };
}

/**
 * 导演台的 3D 主视口。
 * 导演视角：左键拖空白处旋转、右键平移、滚轮缩放；点选对象后出现变换控件，1 / 2 切换移动 / 旋转。
 * 机位视角：站在当前机位里看，画面按机位的画幅取景（画幅外压暗），按住左键拖动调整机位朝向。
 * 舞台每帧按编辑器状态同步，不走 React 重渲染。
 */
export function DirectorViewport({ ref }: { ref?: Ref<DirectorViewportApi> }) {
  const labelsRef = useRef<HTMLDivElement>(null);
  const stage = useMemo(() => createStage({ helpers: true }), []);
  useEffect(() => () => stage.dispose(), [stage]);
  const threeScene = useMemo(() => createThreeScene(), []);

  return (
    <div className="absolute inset-0">
      <Canvas
        shadows
        scene={threeScene}
        camera={{ position: [6.5, 5, 8.5], fov: 50, near: 0.05, far: 200 }}
        dpr={[1, 2]}
      >
        <StageContent stage={stage} labelsRef={labelsRef} apiRef={ref} />
      </Canvas>
      <div ref={labelsRef} className="pointer-events-none absolute inset-0 overflow-hidden" />
      <CameraFrame />
    </div>
  );
}

function StageContent({
  stage,
  labelsRef,
  apiRef,
}: {
  stage: DirectorStage;
  labelsRef: React.RefObject<HTMLDivElement | null>;
  apiRef?: Ref<DirectorViewportApi>;
}) {
  const camera = useThree((state) => state.camera) as PerspectiveCamera;
  const size = useThree((state) => state.size);
  /** makeDefault 的 OrbitControls 会登记在这里 */
  const controls = useThree((state) => state.controls) as unknown as OrbitHandle | null;
  const selectedId = useDirectorStore((state) => state.selectedId);
  const tool = useDirectorStore((state) => state.tool);
  const snap = useDirectorStore((state) => state.snap);
  /** 机位视角下正在看的机位；导演视角是 null */
  const camViewId = useDirectorStore((state) => activeCameraOf(state.scene, state.viewMode));
  const camView = camViewId !== null;
  const gl = useThree((state) => state.gl);
  /** 事件处理里要读最新的视口大小和控制器，用 ref 免得因为它们变化重新绑定事件 */
  const sizeRef = useRef(size);
  const controlsRef = useRef(controls);
  useEffect(() => {
    sizeRef.current = size;
    controlsRef.current = controls;
  });
  /** 变换控件挂的对象：人物换姿势会重建模型，所以每帧核对一次 */
  const [attached, setAttached] = useState<Group | undefined>();
  const labelHtml = useRef("");

  useFrame(() => {
    const state = useDirectorStore.getState();
    const { scene } = state;
    if (!scene) return;
    stage.sync(scene, state.poseOf, { selectedId: state.selectedId });
    // 机位视角：站进当前机位里看。自己的机位模型和视锥线不画，免得挡在眼前
    for (const item of scene.cameras) {
      const object = stage.objectOf(item.id);
      if (object) object.visible = item.id !== camViewId;
    }
    const viewed = camViewId ? scene.cameras.find((c) => c.id === camViewId) : undefined;
    if (viewed) {
      const pose = (state.poseOf(viewed.id) ?? viewed) as CameraPose;
      viewThrough(camera, pose, ASPECT_RATIO[viewed.aspect], size.width / size.height);
    }
    const target = state.selectedId ? stage.objectOf(state.selectedId) : undefined;
    if (target !== attached) setAttached(target);

    // 人物名、机位名和「未记录」提示：投影到屏幕上，画在叠加层里
    const layer = labelsRef.current;
    if (!layer) return;
    const project = (x: number, y: number, z: number) => {
      const v = new Vector3(x, y, z).project(camera);
      return v.z < 1 ? [((v.x + 1) / 2) * size.width, ((1 - v.y) / 2) * size.height] : null;
    };
    let html = "";
    const tag = (text: string, at: number[] | null, pending = false) => {
      if (!at) return;
      html += `<span class="${
        pending
          ? "text-status-running translate-y-1"
          : "bg-background/70 text-foreground -translate-y-full"
      } absolute -translate-x-1/2 rounded px-1.5 text-[11px] whitespace-nowrap" style="left:${at[0]}px;top:${at[1]}px">${text}</span>`;
    };
    const escape = (text: string) =>
      text.replace(
        /[&<>"]/g,
        (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]!,
      );
    for (const character of scene.characters) {
      if (character.hidden) continue;
      const pose = state.poseOf(character.id) ?? character;
      if (state.labels) {
        tag(escape(character.name), project(pose.x, FIGURE_TOP * (character.scale || 1), pose.z));
      }
      if (state.pending[character.id]) tag("未记录 · 按 K", project(pose.x, 0, pose.z), true);
    }
    for (const item of scene.cameras) {
      const pose = (state.poseOf(item.id) ?? item) as CameraPose;
      const at = project(pose.x, pose.y + 0.3, pose.z);
      if (state.labels) tag(escape(item.name), at);
      if (state.pending[item.id]) tag("未记录 · 按 K", at, true);
    }
    if (html !== labelHtml.current) {
      labelHtml.current = html;
      layer.innerHTML = html;
    }
  });

  useImperativeHandle(
    apiRef,
    () => ({
      focusSelected: () => {
        const id = useDirectorStore.getState().selectedId;
        const object = id ? stage.objectOf(id) : undefined;
        const orbit = controls;
        if (!object || !orbit) return;
        const point = object.position.clone();
        if (point.y < 0.5) point.y = 1;
        const offset = camera.position.clone().sub(orbit.target).setLength(4.5);
        orbit.target.copy(point);
        camera.position.copy(point).add(offset);
        orbit.update();
      },
      viewPose: () => {
        const direction = new Vector3();
        camera.getWorldDirection(direction);
        return {
          x: camera.position.x,
          y: Math.max(0.2, camera.position.y),
          z: camera.position.z,
          yaw: Math.atan2(direction.x, -direction.z) / DEG,
          pitch: Math.asin(Math.min(1, Math.max(-1, direction.y))) / DEG,
          fov: camera.fov,
        };
      },
    }),
    [camera, controls, stage],
  );

  // 进机位视角前先记住导演视角的位置，退出时放回去
  useLayoutEffect(() => {
    if (!camView) return;
    const saved = saveView(camera, controlsRef.current);
    return () => restoreView(camera, saved, sizeRef.current, controlsRef.current);
  }, [camView, camera]);

  // 机位视角：按住左键拖动调整机位朝向，手感和导演视角的拖动一致（画面跟着手走）。
  // 一次拖动算撤销栈里的一步；已有关键帧的机位，改动先是「未记录」，按 K 才写进轨道
  useEffect(() => {
    if (!camViewId) return;
    return attachLook(gl.domElement, camViewId, camera, sizeRef);
  }, [camViewId, camera, gl]);

  /** 点选：点中对象就选它，点地面或空白就取消选中；拖过视角的不算点击；机位视角里左键是调朝向，不点选 */
  const onClick = (event: ThreeEvent<MouseEvent>) => {
    if (camView || event.delta > CLICK_SLOP) return;
    event.stopPropagation();
    useDirectorStore.getState().select(stage.idOf(event.object) ?? null);
  };

  const kind = useDirectorStore((state) => {
    if (!selectedId || !state.scene) return null;
    if (state.scene.cameras.some((c) => c.id === selectedId)) return "camera";
    if (state.scene.characters.some((c) => c.id === selectedId)) return "character";
    return "prop";
  });

  /** 变换控件改了对象：换算回站位 / 机位状态写进编辑器 */
  const onObjectChange = () => {
    if (!attached || !selectedId) return;
    const { position, rotation } = attached;
    useDirectorStore.getState().transformObject(selectedId, {
      x: position.x,
      z: position.z,
      yaw: -rotation.y / DEG,
      ...(kind === "camera" ? { y: position.y, pitch: rotation.x / DEG } : {}),
    });
  };

  const rotating = tool === "rotate";
  return (
    <>
      <primitive object={stage.root} onClick={onClick} />
      <OrbitControls
        makeDefault
        enabled={!camView}
        enableDamping
        target={[0, 1, 0]}
        maxPolarAngle={Math.PI * 0.495}
      />
      {attached && kind && !camView && (
        <TransformControls
          object={attached}
          mode={rotating ? "rotate" : "translate"}
          size={0.85}
          // 人物、道具只贴地移动、只水平转向；机位还能升降和俯仰
          showX={rotating ? kind === "camera" : true}
          showY={rotating ? true : kind === "camera"}
          showZ={!rotating}
          translationSnap={snap ? SNAP_MOVE : null}
          rotationSnap={snap ? SNAP_TURN : null}
          onMouseDown={() => useDirectorStore.getState().startGesture()}
          onMouseUp={() => useDirectorStore.getState().endGesture()}
          onObjectChange={onObjectChange}
        />
      )}
    </>
  );
}

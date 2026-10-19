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
import { TransformControls } from "@react-three/drei";
import { Vector3, type Group, type PerspectiveCamera, type Quaternion } from "three";

import { useDirectorStore } from "@/store/director";
import type { CameraPose } from "@/types";
import { activeCameraOf, viewFov } from "@/utils/director/camera-view";
import { ASPECT_RATIO } from "@/utils/director/export";
import { FIGURE_TOP } from "@/utils/director/figure";
import { createThreeScene } from "@/utils/director/render";
import { DEG } from "@/utils/director/sample";
import { applyCameraPose, createStage, type DirectorStage } from "@/utils/director/stage";

import { CameraFrame } from "./camera-frame";
import { attachFreeCamera, directorDriver, sceneCameraDriver } from "./free-camera";

/** 视口对外的操作：工具栏、快捷键调用 */
export type DirectorViewportApi = {
  /** 导演视角对准选中的对象 */
  focusSelected: () => void;
  /** 当前导演视角的位置和朝向，「从当前视角新建机位」用 */
  viewPose: () => CameraPose;
};

/** 导演视角刚打开时相机的位置，和看向的点 */
const INITIAL_POSITION: [number, number, number] = [6.5, 5, 8.5];
const INITIAL_TARGET: [number, number, number] = [0, 1, 0];
/** 「聚焦选中」时相机退到离对象多远（米），朝向不变 */
const FOCUS_DISTANCE = 4.5;

/** 点击时指针移动超过这么多像素就算拖动视角，不当点选 */
const CLICK_SLOP = 4;
/** 吸附：移动 0.25 米、旋转 15° 一档 */
const SNAP_MOVE = 0.25;
const SNAP_TURN = 15 * DEG;

/** 导演视角的相机状态：进机位视角前存下，退出时放回去 */
type SavedView = { position: Vector3; quaternion: Quaternion; fov: number };

const saveView = (camera: PerspectiveCamera): SavedView => ({
  position: camera.position.clone(),
  quaternion: camera.quaternion.clone(),
  fov: camera.fov,
});

function restoreView(
  camera: PerspectiveCamera,
  saved: SavedView,
  size: { width: number; height: number },
) {
  camera.position.copy(saved.position);
  camera.quaternion.copy(saved.quaternion);
  camera.fov = saved.fov;
  camera.aspect = size.width / size.height;
  camera.updateProjectionMatrix();
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
 * 导演台的 3D 主视口。
 * 两个视角共用一套自由相机操作（见 free-camera）：左键抓手拖动、滚轮上下摇、Ctrl / Alt + 滚轮变焦、
 * WASD / 方向键水平平移；导演视角另有右键 / 中键按住平移。点选对象后出现变换控件，1 / 2 切换移动 / 旋转。
 * 导演视角：自由飞的相机，不再是绕着一个点环绕。
 * 机位视角：站在当前机位里看，画面按机位的画幅取景（画幅外压暗），操作改的是这台机位。
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
        camera={{ position: INITIAL_POSITION, fov: 50, near: 0.05, far: 200 }}
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
  const selectedId = useDirectorStore((state) => state.selectedId);
  const tool = useDirectorStore((state) => state.tool);
  const snap = useDirectorStore((state) => state.snap);
  /** 机位视角下正在看的机位；导演视角是 null */
  const camViewId = useDirectorStore((state) => activeCameraOf(state.scene, state.viewMode));
  const camView = camViewId !== null;
  const gl = useThree((state) => state.gl);
  /** 事件处理里要读最新的视口大小、当前视角，用 ref 免得因为它们变化重新绑定事件 */
  const sizeRef = useRef(size);
  const camViewRef = useRef(camView);
  useEffect(() => {
    sizeRef.current = size;
    camViewRef.current = camView;
  });
  /** 变换控件正在被拖：这时左键拖动归控件，不当抓手 */
  const gizmoBusy = useRef(false);
  /** 当前视角的自由相机控制器，每帧推进键盘平移 */
  const freeCamera = useRef<ReturnType<typeof attachFreeCamera> | null>(null);
  /** 变换控件挂的对象：人物换姿势会重建模型，所以每帧核对一次 */
  const [attached, setAttached] = useState<Group | undefined>();
  const labelHtml = useRef("");

  // 刚打开时看向舞台中心；之后相机是自由飞的，朝向由操作决定
  useLayoutEffect(() => {
    camera.lookAt(...INITIAL_TARGET);
  }, [camera]);

  useFrame((_, delta) => {
    freeCamera.current?.tick(delta);
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
        // 机位视角里相机是机位自己，没有「聚焦」
        if (!object || camViewRef.current) return;
        const point = object.position.clone();
        if (point.y < 0.5) point.y = 1;
        // 朝向不变，退到对象正前方（沿视线反方向）FOCUS_DISTANCE 米，让它落在画面正中
        const forward = camera.getWorldDirection(new Vector3());
        camera.position.copy(point).addScaledVector(forward, -FOCUS_DISTANCE);
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
    [camera, stage],
  );

  // 进机位视角前先记住导演视角的位置，退出时放回去
  useLayoutEffect(() => {
    if (!camView) return;
    const saved = saveView(camera);
    return () => restoreView(camera, saved, sizeRef.current);
  }, [camView, camera]);

  // 自由相机操作：导演视角操作主视口的相机，机位视角操作当前机位（一次连续操作算撤销栈一步）。
  // 已有关键帧的机位，改动先是「未记录」，按 K 才写进轨道
  useEffect(() => {
    const controller = attachFreeCamera({
      element: gl.domElement,
      camera,
      driver: camViewId ? sceneCameraDriver(camViewId) : directorDriver(camera),
      allowPan: !camViewId,
      gizmoBusy,
    });
    freeCamera.current = controller;
    return () => {
      controller.dispose();
      freeCamera.current = null;
    };
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
          onMouseDown={() => {
            gizmoBusy.current = true;
            useDirectorStore.getState().startGesture();
          }}
          onMouseUp={() => {
            gizmoBusy.current = false;
            useDirectorStore.getState().endGesture();
          }}
          onObjectChange={onObjectChange}
        />
      )}
    </>
  );
}

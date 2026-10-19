import { useEffect, useImperativeHandle, useMemo, useRef, useState, type Ref } from "react";
import { Canvas, useFrame, useThree, type ThreeEvent } from "@react-three/fiber";
import { OrbitControls, TransformControls } from "@react-three/drei";
import { Vector3, type Group, type PerspectiveCamera } from "three";

import { useDirectorStore } from "@/store/director";
import type { CameraPose } from "@/types";
import { FIGURE_TOP } from "@/utils/director/figure";
import { createThreeScene } from "@/utils/director/render";
import { DEG } from "@/utils/director/sample";
import { createStage, type DirectorStage } from "@/utils/director/stage";

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

/**
 * 导演台的 3D 主视口（导演视角）：左键拖空白处旋转、右键平移、滚轮缩放；
 * 点选对象后出现变换控件，1 / 2 切换移动 / 旋转。舞台每帧按编辑器状态同步，不走 React 重渲染。
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
  /** 变换控件挂的对象：人物换姿势会重建模型，所以每帧核对一次 */
  const [attached, setAttached] = useState<Group | undefined>();
  const labelHtml = useRef("");

  useFrame(() => {
    const state = useDirectorStore.getState();
    const { scene } = state;
    if (!scene) return;
    stage.sync(scene, state.poseOf, { selectedId: state.selectedId });
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

  /** 点选：点中对象就选它，点地面或空白就取消选中；拖过视角的不算点击 */
  const onClick = (event: ThreeEvent<MouseEvent>) => {
    if (event.delta > CLICK_SLOP) return;
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
      <OrbitControls makeDefault enableDamping target={[0, 1, 0]} maxPolarAngle={Math.PI * 0.495} />
      {attached && kind && (
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

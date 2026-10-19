import { Color, Fog, PerspectiveCamera, Scene, WebGLRenderer } from "three";

import type { CameraPose, DirectorScene } from "@/types";

import { ASPECT_RATIO } from "./export";
import { loadFigureModels } from "./figure";
import { sampleCamera, sampleCharacter } from "./sample";
import { STAGE_BACKGROUND, type PoseLookup, applyCameraPose, createStage } from "./stage";

/** 主视口、小窗、离屏渲染共用的 three 场景底色和雾 */
export function createThreeScene(): Scene {
  const scene = new Scene();
  scene.background = new Color(STAGE_BACKGROUND);
  scene.fog = new Fog(STAGE_BACKGROUND, 20, 45);
  return scene;
}

/**
 * 离屏渲染器：用「干净舞台」（不带网格、机位、标签、控件）从某个机位拍某一时刻。
 * 缩略图、首帧图、导出的每一帧都走这里，画面和小窗里看到的一致。
 */
export function createShotRenderer() {
  const renderer = new WebGLRenderer({ antialias: true, preserveDrawingBuffer: true });
  renderer.setPixelRatio(1);
  renderer.shadowMap.enabled = true;
  const threeScene = createThreeScene();
  const stage = createStage({ helpers: false });
  threeScene.add(stage.root);
  const camera = new PerspectiveCamera(50, 16 / 9, 0.05, 200);

  /** 按给定的站位和机位状态拍一帧（机位小窗用：带着临时姿态和拖动中的机位） */
  const renderPose = (scene: DirectorScene, poseOf: PoseLookup, cameraPose: CameraPose) => {
    stage.sync(scene, poseOf);
    const { width, height } = renderer.domElement;
    applyCameraPose(camera, cameraPose, width / height || 16 / 9);
    renderer.render(threeScene, camera);
  };

  return {
    canvas: renderer.domElement,
    /** 画面尺寸；导出过程中不能变 */
    setSize: (width: number, height: number) => renderer.setSize(width, height, false),
    /**
     * 拍一帧：人物取 t 秒时的站位，机位取 t 秒时的状态。
     * @returns 机位不存在时 false
     */
    render: (scene: DirectorScene, cameraId: string, t: number) => {
      const target = scene.cameras.find((c) => c.id === cameraId);
      if (!target) return false;
      renderPose(
        scene,
        (id) => {
          const character = scene.characters.find((c) => c.id === id);
          return character ? sampleCharacter(character, t) : undefined;
        },
        sampleCamera(target, t),
      );
      return true;
    },
    renderPose,
    toBlob: (type = "image/png", quality?: number) =>
      new Promise<Blob>((resolve, reject) =>
        renderer.domElement.toBlob(
          (blob) => (blob ? resolve(blob) : reject(new Error("画面导出失败"))),
          type,
          quality,
        ),
      ),
    dispose: () => {
      stage.dispose();
      renderer.dispose();
      renderer.forceContextLoss();
    },
  };
}

/** 节点缩略图的长边 */
const THUMB_LONG_SIDE = 480;

/** 用当前机位拍 0 秒的画面当节点缩略图；没有机位返回 null */
export async function renderThumbnail(scene: DirectorScene): Promise<Blob | null> {
  const camera = scene.cameras.find((c) => c.id === scene.activeCameraId) ?? scene.cameras[0];
  if (!camera) return null;
  const ratio = ASPECT_RATIO[camera.aspect];
  // 缩略图里的人物要是人台，不是简化形体：把场景里用到的男、女模型都读好
  await loadFigureModels(scene.characters.map((c) => c.model ?? "male"));
  const shot = createShotRenderer();
  try {
    shot.setSize(
      ratio >= 1 ? THUMB_LONG_SIDE : Math.round(THUMB_LONG_SIDE * ratio),
      ratio >= 1 ? Math.round(THUMB_LONG_SIDE / ratio) : THUMB_LONG_SIDE,
    );
    shot.render(scene, camera.id, 0);
    return await shot.toBlob("image/jpeg", 0.85);
  } finally {
    shot.dispose();
  }
}

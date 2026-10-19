import {
  Box3,
  DirectionalLight,
  Group,
  HemisphereLight,
  MeshStandardMaterial,
  PerspectiveCamera,
  Scene,
  Vector3,
  WebGLRenderer,
} from "three";

import type { FigureModel, PropShape } from "@/types";

import { createFigure, figureModelStatus, loadFigureModel, measureFigure } from "./figure";
import { buildPropObject } from "./prop-object";
import { propSize } from "./prop-presets";
import { createThumbCache } from "./thumbs";

/** 缩略图的边长（像素）：卡片里显示约 96 到 120，留 2 倍清晰度 */
const SIZE = 240;
/** 取景：从人物 / 物体的前方偏右、偏上一点看过去 */
const VIEW_DIRECTION = new Vector3(0.55, 0.32, -1).normalize();
const FOV = 28;
/** 取景留白：包围盒的最大边再放大这么多 */
const MARGIN = 1.22;

/** 缩略图的 key：人物是 character:male，道具是 prop:table */
export const characterThumbKey = (model: FigureModel) => `character:${model}`;
export const propThumbKey = (shape: PropShape) => `prop:${shape}`;

type Shooter = {
  renderer: WebGLRenderer;
  scene: Scene;
  camera: PerspectiveCamera;
};

let shooter: Shooter | null = null;

/** 拍缩略图用的渲染器：整个页面一个，用到才建；背景透明，卡片自己的底色透出来 */
function getShooter(): Shooter {
  if (shooter) return shooter;
  const renderer = new WebGLRenderer({ antialias: true, alpha: true, preserveDrawingBuffer: true });
  renderer.setPixelRatio(1);
  renderer.setSize(SIZE, SIZE, false);
  renderer.setClearColor(0x000000, 0);
  const scene = new Scene();
  scene.add(new HemisphereLight(0xe4e8ff, 0x2a2a30, 1.35));
  const sun = new DirectionalLight(0xffffff, 1.9);
  sun.position.set(3, 6, -4);
  scene.add(sun);
  shooter = { renderer, scene, camera: new PerspectiveCamera(FOV, 1, 0.05, 200) };
  return shooter;
}

/** 释放拍缩略图的渲染器（编辑器关闭时调用）；已拍好的图留在缓存里 */
export function disposeThumbRenderer() {
  if (!shooter) return;
  shooter.renderer.dispose();
  shooter.renderer.forceContextLoss();
  shooter = null;
}

/** 把一个对象放进场景，对准它的包围盒拍一张，再拿掉 */
function shoot(object: Group, box: Box3): string {
  const { renderer, scene, camera } = getShooter();
  scene.add(object);
  try {
    const center = box.getCenter(new Vector3());
    const size = box.getSize(new Vector3());
    const radius = Math.max(size.x, size.y, size.z) * MARGIN;
    const distance = radius / (2 * Math.tan((FOV / 2) * (Math.PI / 180)));
    camera.position.copy(center).addScaledVector(VIEW_DIRECTION, distance);
    camera.lookAt(center);
    camera.updateProjectionMatrix();
    renderer.render(scene, camera);
    return renderer.domElement.toDataURL("image/png");
  } finally {
    scene.remove(object);
    object.traverse((node) => {
      const mesh = node as {
        geometry?: { dispose: () => void };
        material?: { dispose: () => void };
      };
      mesh.geometry?.dispose();
      mesh.material?.dispose();
    });
  }
}

/**
 * 拍一张资产缩略图。
 * @param key characterThumbKey / propThumbKey 生成的 key
 * @returns 图片地址；人物模型还没加载好、不认识的 key 返回 null（不缓存失败，下次还能再拍）
 */
async function renderAssetThumb(key: string): Promise<string | null> {
  const [kind, id] = key.split(":");
  if (kind === "character") {
    const model = id as FigureModel;
    await loadFigureModel(undefined, model);
    // 模型没加载成功时画出来的是简化形体，不当成这个人物的缩略图
    if (figureModelStatus(model) !== "ready") return null;
    const figure = createFigure({ pose: "stand", color: "#d4d4d8", model });
    return shoot(figure.group, measureFigure(figure.group));
  }
  if (kind === "prop") {
    const shape = id as PropShape;
    const material = new MeshStandardMaterial({ color: 0xa9b6c6, roughness: 0.85 });
    const root = new Group();
    const body = buildPropObject(shape, material);
    const [w, h, d] = propSize(shape);
    body.scale.set(w, h, d);
    root.add(body);
    return shoot(root, new Box3().setFromObject(root));
  }
  return null;
}

/** 资产面板的缩略图缓存：整个页面共用，编辑器反复打开不用重拍 */
export const assetThumbs = createThumbCache(renderAssetThumb);

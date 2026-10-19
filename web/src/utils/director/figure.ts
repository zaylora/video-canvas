import {
  Box3,
  CapsuleGeometry,
  Group,
  Mesh,
  MeshStandardMaterial,
  type Object3D,
  type SkinnedMesh,
  SphereGeometry,
  Vector3,
} from "three";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import { clone as cloneSkinned } from "three/examples/jsm/utils/SkeletonUtils.js";

import type { FigureModel, PoseId } from "@/types";

import { applyBoneRotations, captureRest, type RestPose } from "./pose";
import { POSE_ROTATIONS } from "./poses";

/** 人物站直时的身高（米） */
export const FIGURE_HEIGHT: Record<FigureModel, number> = { male: 1.78, female: 1.68 };

/** 人物头顶往上的高度（米，未缩放）：名字标签摆在这上面，留出举手的余地 */
export const FIGURE_TOP = 1.95;

/** 所有人台模型 */
export const FIGURE_MODELS: readonly FigureModel[] = ["male", "female"];

/**
 * 人台模型的地址：Quaternius Universal Base Characters 的 Superhero 男、女，处理成灰色无贴图，
 * 骨骼名称完全相同，所以姿势数据共用；许可证见 public/director/LICENSE.txt。
 */
const modelUrl = (model: FigureModel) => `${import.meta.env?.BASE_URL ?? "/"}director/${model}.glb`;

/** 人台模型的加载状态 */
export type FigureModelStatus = "idle" | "loading" | "ready" | "failed";

/** 模板：加载好的人台，每个人物从它克隆 */
type Template = {
  /** 外壳：把脸朝 +z 的模型转成脸朝 -z（我们的约定） */
  holder: Group;
  /** 静止姿势（T 字形）下的骨骼旋转 */
  rest: RestPose;
  /** 缩放到 FIGURE_HEIGHT 的系数 */
  scale: number;
};

type Slot = {
  template: Template | null;
  status: FigureModelStatus;
  pending: Promise<boolean> | null;
};

const slots: Record<FigureModel, Slot> = {
  male: { template: null, status: "idle", pending: null },
  female: { template: null, status: "idle", pending: null },
};

/** 读模型文件的函数：默认从站点静态资源里取，测试里注入 */
export type ModelLoader = (model: FigureModel) => Promise<ArrayBuffer>;

/** 某个人台模型加载到哪一步了；非 ready 时这类人物用简化形体顶着 */
export const figureModelStatus = (model: FigureModel = "male") => slots[model].status;

/** 清掉已加载的模型，回到没加载过的状态（测试用） */
export function resetFigureModel() {
  for (const model of FIGURE_MODELS) {
    slots[model] = { template: null, status: "idle", pending: null };
  }
}

const fetchModel: ModelLoader = async (model) => {
  const response = await fetch(modelUrl(model));
  if (!response.ok) throw new Error(`人物模型请求失败：${response.status}`);
  return response.arrayBuffer();
};

const findSkinned = (root: Object3D): SkinnedMesh | undefined => {
  let found: SkinnedMesh | undefined;
  root.traverse((node) => {
    if (!found && (node as SkinnedMesh).isSkinnedMesh) found = node as SkinnedMesh;
  });
  return found;
};

/**
 * 量人物在局部坐标里的包围盒：蒙皮网格要按当前骨骼姿势逐顶点算，
 * 不能用几何体自带的包围盒（那是静止姿势下的）。
 */
export function measureFigure(group: Object3D): Box3 {
  group.updateMatrixWorld(true);
  const skinned = findSkinned(group);
  if (!skinned) return new Box3().setFromObject(group);
  const inverse = group.matrixWorld.clone().invert();
  const toGroup = skinned.matrixWorld.clone().premultiply(inverse);
  const box = new Box3();
  const point = new Vector3();
  const count = skinned.geometry.attributes.position.count;
  for (let i = 0; i < count; i++) {
    skinned.getVertexPosition(i, point);
    box.expandByPoint(point.applyMatrix4(toGroup));
  }
  return box;
}

/** 把加载好的 glTF 整理成模板：套转向外壳、记下静止姿势、算出缩放 */
function prepareTemplate(scene: Object3D, model: FigureModel): Template {
  const skinned = findSkinned(scene);
  if (!skinned) throw new Error("人物模型里没有带骨骼的网格");
  const holder = new Group();
  holder.rotation.y = Math.PI;
  holder.add(scene);
  const rest = captureRest(skinned.skeleton);
  const height = measureFigure(holder).getSize(new Vector3()).y;
  if (!(height > 0)) throw new Error("人物模型的尺寸不对");
  return { holder, rest, scale: FIGURE_HEIGHT[model] / height };
}

/**
 * 加载一个人台模型，整个页面每个模型只读一次文件；加载过程中重复调用共用同一个请求，失败后再调用会重试。
 * 加载失败不抛错：这类人物退回简化形体，导出照常可用。
 * @param load 读模型文件的函数，默认从站点静态资源里取；测试里注入
 * @param model 要加载的模型，缺省男性
 * @returns 是否加载成功
 */
export function loadFigureModel(
  load: ModelLoader = fetchModel,
  model: FigureModel = "male",
): Promise<boolean> {
  const slot = slots[model];
  if (slot.status === "ready") return Promise.resolve(true);
  if (slot.pending) return slot.pending;
  slot.status = "loading";
  slot.pending = (async () => {
    try {
      const gltf = await new GLTFLoader().parseAsync(await load(model), "");
      slot.template = prepareTemplate(gltf.scene, model);
      slot.status = "ready";
      return true;
    } catch (error) {
      console.warn(`人物模型（${model}）加载失败，改用简化形体`, error);
      slot.template = null;
      slot.status = "failed";
      return false;
    } finally {
      slot.pending = null;
    }
  })();
  return slot.pending;
}

/**
 * 加载一批人台模型（去重），只读用到的。
 * @returns 全部成功才是 true；有失败的，成功的那些照常可用
 */
export async function loadFigureModels(
  models: readonly FigureModel[],
  load: ModelLoader = fetchModel,
): Promise<boolean> {
  const results = await Promise.all([...new Set(models)].map((m) => loadFigureModel(load, m)));
  return results.every(Boolean);
}

/** 模型没加载好时的简化形体：一根胶囊加一个头，站在地面上 */
function createCapsuleFigure(material: MeshStandardMaterial): Group {
  const group = new Group();
  const body = new Mesh(new CapsuleGeometry(0.24, 1.1, 4, 12), material);
  body.position.y = 0.83;
  const head = new Mesh(new SphereGeometry(0.115, 20, 14), material);
  head.position.y = 1.66;
  for (const mesh of [body, head]) {
    mesh.castShadow = true;
    group.add(mesh);
  }
  return group;
}

/**
 * 建一个人物灰模：面朝本地 -z、脚（或身体）刚好踩在 y = 0。
 * 模型已加载就克隆人台、套姿势预设再按姿势落地；否则是简化形体。
 * 每个人物有自己的材质，着色用人物的颜色。
 * @param options.model 男性或女性，缺省男性
 */
export function createFigure(options: { pose: PoseId; color: string; model?: FigureModel }): {
  group: Group;
  material: MeshStandardMaterial;
} {
  const material = new MeshStandardMaterial({
    color: options.color,
    roughness: 0.7,
    metalness: 0.05,
  });
  const slot = slots[options.model ?? "male"];
  if (!slot.template || slot.status !== "ready") {
    return { group: createCapsuleFigure(material), material };
  }
  const template = slot.template;
  const model = cloneSkinned(template.holder) as Group;
  const skinned = findSkinned(model)!;
  model.traverse((node) => {
    const mesh = node as Mesh;
    if (!mesh.isMesh) return;
    mesh.material = material;
    mesh.castShadow = true;
    // 包围球是静止姿势下算的，躺下、举手后会被误裁掉
    mesh.frustumCulled = false;
  });
  applyBoneRotations(skinned.skeleton, template.rest, POSE_ROTATIONS[options.pose]);

  const body = new Group();
  body.scale.setScalar(template.scale);
  body.add(model);
  const group = new Group();
  group.add(body);
  // 按这个姿势落地：站着是脚踩地，坐着是脚踩地、屁股悬空，躺着是后背贴地
  body.position.y = -measureFigure(group).min.y;
  return { group, material };
}

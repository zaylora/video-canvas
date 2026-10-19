import {
  BoxGeometry,
  BufferAttribute,
  BufferGeometry,
  Color,
  CylinderGeometry,
  DirectionalLight,
  GridHelper,
  Group,
  HemisphereLight,
  Line,
  LineBasicMaterial,
  LineDashedMaterial,
  LineSegments,
  Mesh,
  MeshStandardMaterial,
  type Object3D,
  type PerspectiveCamera,
  PlaneGeometry,
  SphereGeometry,
  Vector3,
} from "three";

import type { CameraPose, CharacterPose, DirectorScene, PropShape } from "@/types";

import { ASPECT_RATIO } from "./export";
import { createFigure, figureModelStatus } from "./figure";
import { DEG, sampleCamera, sampleCharacter } from "./sample";

/** 舞台的底色，主视口、小窗、导出画面共用 */
export const STAGE_BACKGROUND = 0x141417;

/** 选中、当前机位、普通机位的颜色 */
const SELECTED = 0xfbbf24;
const ACTIVE = 0xffffff;
const IDLE = 0x7c7c88;

/** 对象在某一时刻的样子：编辑器传「播放头时刻 + 临时姿态」，导出传逐帧采样 */
export type PoseLookup = (id: string) => CharacterPose | CameraPose | undefined;

type Entry = {
  key: string;
  group: Group;
  materials: MeshStandardMaterial[];
  frustum?: LineSegments;
  body?: Mesh;
};

/** 舞台：按场景数据搭灰模、道具、机位和轨迹，主视口、小窗和离屏导出共用，保证所见即所导 */
export type DirectorStage = {
  /** 挂到 three 场景里的根节点（含灯光、地面） */
  root: Group;
  /** 网格和坐标轴：导出、小窗里隐藏 */
  guides: Group;
  /** 机位模型和轨迹：只在主视口出现 */
  helpers: Group;
  /** 按场景同步舞台；每帧调用也只改变换，姿势、颜色、形状变了才重建 */
  sync: (
    scene: DirectorScene,
    poseOf: PoseLookup,
    options?: { selectedId?: string | null },
  ) => void;
  objectOf: (id: string) => Group | undefined;
  /** 从点中的网格往上找对象 id */
  idOf: (object: Object3D) => string | undefined;
  /** 能点选的对象 */
  pickables: () => Object3D[];
  dispose: () => void;
};

/** 把一棵子树从父节点摘下并释放几何体和材质 */
function disposeTree(object: Object3D) {
  object.removeFromParent();
  object.traverse((node) => {
    const mesh = node as Mesh;
    mesh.geometry?.dispose();
    const material = mesh.material;
    if (material) for (const item of [material].flat()) item.dispose();
  });
}

const PROP_GEOMETRY: Record<PropShape, () => BufferGeometry> = {
  box: () => new BoxGeometry(1, 1, 1),
  plane: () => new BoxGeometry(1, 1, 1),
  cylinder: () => new CylinderGeometry(0.5, 0.5, 1, 32),
  sphere: () => new SphereGeometry(0.5, 32, 20),
};

/** 机位模型：机身 + 镜头 + 视锥线框，前方是本地 -z */
function createCameraModel() {
  const group = new Group();
  group.rotation.order = "YXZ";
  const material = new MeshStandardMaterial({ color: 0xd4d4d8, roughness: 0.5 });
  const body = new Mesh(new BoxGeometry(0.24, 0.17, 0.32), material);
  body.position.z = 0.14;
  const lens = new Mesh(new CylinderGeometry(0.06, 0.075, 0.12, 18), material);
  lens.rotation.x = Math.PI / 2;
  lens.position.z = -0.06;
  const geometry = new BufferGeometry();
  geometry.setAttribute("position", new BufferAttribute(new Float32Array(16 * 3), 3));
  const frustum = new LineSegments(
    geometry,
    new LineBasicMaterial({ color: ACTIVE, transparent: true, opacity: 0.75 }),
  );
  group.add(body, lens, frustum);
  return { group, materials: [material], frustum, body };
}

/** 视锥线框：从镜头到 1.5 米外的取景框 */
function updateFrustum(frustum: LineSegments, fov: number, aspect: number, length = 1.5) {
  const h = Math.tan((fov / 2) * DEG) * length;
  const w = h * aspect;
  const corners = [
    [-w, h, -length],
    [w, h, -length],
    [w, -h, -length],
    [-w, -h, -length],
  ];
  const position = frustum.geometry.attributes.position as BufferAttribute;
  const array = position.array as Float32Array;
  let i = 0;
  const put = (p: number[]) => {
    array[i++] = p[0];
    array[i++] = p[1];
    array[i++] = p[2];
  };
  for (let k = 0; k < 4; k++) {
    put([0, 0, 0]);
    put(corners[k]);
  }
  for (let k = 0; k < 4; k++) {
    put(corners[k]);
    put(corners[(k + 1) % 4]);
  }
  position.needsUpdate = true;
  frustum.geometry.computeBoundingSphere();
}

/** 把机位状态套到 three 相机上 */
export function applyCameraPose(camera: PerspectiveCamera, pose: CameraPose, aspect: number) {
  camera.position.set(pose.x, pose.y, pose.z);
  camera.rotation.set(pose.pitch * DEG, -pose.yaw * DEG, 0, "YXZ");
  camera.fov = pose.fov;
  camera.aspect = aspect;
  camera.updateProjectionMatrix();
}

/**
 * 建一个舞台。
 * @param options.helpers 主视口要机位模型、轨迹和网格；导出、缩略图、小窗用的干净舞台不要
 */
export function createStage(options: { helpers: boolean }): DirectorStage {
  const root = new Group();
  root.add(new HemisphereLight(0xe4e8ff, 0x1c1c20, 1.15));
  const sun = new DirectionalLight(0xffffff, 1.7);
  sun.position.set(5, 10, 6);
  sun.castShadow = true;
  sun.shadow.mapSize.set(1024, 1024);
  Object.assign(sun.shadow.camera, {
    left: -12,
    right: 12,
    top: 12,
    bottom: -12,
    near: 1,
    far: 35,
  });
  root.add(sun);
  const ground = new Mesh(
    new PlaneGeometry(80, 80),
    new MeshStandardMaterial({ color: 0x1b1b20, roughness: 1 }),
  );
  ground.rotation.x = -Math.PI / 2;
  ground.receiveShadow = true;
  root.add(ground);

  const guides = new Group();
  const grid = new GridHelper(24, 24, 0x3b3b46, 0x2a2a31);
  grid.position.y = 0.003;
  guides.add(grid);
  const axis = (to: [number, number, number], color: number) => {
    const line = new Line(
      new BufferGeometry().setFromPoints([
        new Vector3(-to[0], 0, -to[2]),
        new Vector3(to[0], 0, to[2]),
      ]),
      new LineBasicMaterial({ color }),
    );
    line.position.y = 0.006;
    guides.add(line);
  };
  axis([12, 0, 0], 0xc0392b);
  axis([0, 0, 12], 0x3557d6);
  guides.visible = options.helpers;

  const content = new Group();
  const helpers = new Group();
  helpers.visible = options.helpers;
  root.add(guides, content, helpers);

  const entries = new Map<string, Entry>();
  const trails = new Map<string, { key: string; line: Line }>();

  const put = (id: string, key: string, parent: Group, build: () => Omit<Entry, "key">) => {
    let entry = entries.get(id);
    if (!entry || entry.key !== key) {
      if (entry) disposeTree(entry.group);
      entry = { key, ...build() };
      entry.group.userData.directorId = id;
      parent.add(entry.group);
      entries.set(id, entry);
    }
    return entry;
  };

  const syncTrails = (scene: DirectorScene) => {
    const wanted = new Set<string>();
    for (const item of [...scene.cameras, ...scene.characters]) {
      if (item.keys.length < 2) continue;
      wanted.add(item.id);
      const key = JSON.stringify(item.keys) + scene.duration;
      const existing = trails.get(item.id);
      if (existing?.key === key) continue;
      if (existing) disposeTree(existing.line);
      const isCamera = "fov" in item;
      const points: Vector3[] = [];
      for (let i = 0; i <= 60; i++) {
        const t = (scene.duration * i) / 60;
        if (isCamera) {
          const p = sampleCamera(item, t);
          points.push(new Vector3(p.x, p.y, p.z));
        } else {
          const p = sampleCharacter(item, t);
          points.push(new Vector3(p.x, 0.03, p.z));
        }
      }
      const line = new Line(
        new BufferGeometry().setFromPoints(points),
        new LineDashedMaterial({
          color: isCamera ? ACTIVE : new Color(item.color),
          dashSize: 0.18,
          gapSize: 0.1,
          transparent: true,
          opacity: 0.8,
        }),
      );
      line.computeLineDistances();
      helpers.add(line);
      trails.set(item.id, { key, line });
    }
    for (const [id, trail] of trails) {
      if (wanted.has(id)) continue;
      disposeTree(trail.line);
      trails.delete(id);
    }
  };

  const sync: DirectorStage["sync"] = (scene, poseOf, opts = {}) => {
    const seen = new Set<string>();
    for (const character of scene.characters) {
      seen.add(character.id);
      // 人台模型加载好之前画的是简化形体，状态进 key，加载完下一帧就换成人台
      const entry = put(
        character.id,
        `${character.pose}|${character.color}|${figureModelStatus()}`,
        content,
        () => {
          const figure = createFigure({ pose: character.pose, color: character.color });
          figure.group.rotation.order = "YXZ";
          return { group: figure.group, materials: [figure.material] };
        },
      );
      const pose = (poseOf(character.id) ?? character) as CharacterPose;
      entry.group.position.set(pose.x, 0, pose.z);
      entry.group.rotation.set(0, -pose.yaw * DEG, 0);
      entry.group.scale.setScalar(character.scale || 1);
      entry.group.visible = !character.hidden;
    }
    for (const prop of scene.props) {
      seen.add(prop.id);
      const entry = put(prop.id, prop.shape, content, () => {
        const material = new MeshStandardMaterial({ color: 0x8796a8, roughness: 0.85 });
        const mesh = new Mesh(PROP_GEOMETRY[prop.shape](), material);
        mesh.castShadow = true;
        mesh.receiveShadow = true;
        const group = new Group();
        group.rotation.order = "YXZ";
        group.add(mesh);
        return { group, materials: [material], body: mesh };
      });
      entry.group.position.set(prop.x, 0, prop.z);
      entry.group.rotation.set(0, -prop.yaw * DEG, 0);
      entry.body!.scale.set(prop.w, prop.h, prop.d);
      entry.body!.position.y = prop.h / 2;
      entry.group.visible = !prop.hidden;
    }
    if (options.helpers) {
      for (const camera of scene.cameras) {
        seen.add(camera.id);
        const entry = put(camera.id, "camera", helpers, createCameraModel);
        const pose = (poseOf(camera.id) ?? camera) as CameraPose;
        entry.group.position.set(pose.x, pose.y, pose.z);
        entry.group.rotation.set(pose.pitch * DEG, -pose.yaw * DEG, 0);
        updateFrustum(entry.frustum!, pose.fov, ASPECT_RATIO[camera.aspect]);
        const active = camera.id === scene.activeCameraId;
        const color = opts.selectedId === camera.id ? SELECTED : active ? ACTIVE : IDLE;
        (entry.frustum!.material as LineBasicMaterial).color.setHex(color);
        entry.materials[0].color.setHex(active ? 0xe4e4e7 : 0x8a8a94);
      }
      syncTrails(scene);
    }
    for (const [id, entry] of entries) {
      if (seen.has(id)) continue;
      disposeTree(entry.group);
      entries.delete(id);
    }
    for (const [id, entry] of entries) {
      for (const material of entry.materials) {
        material.emissive.setHex(opts.selectedId === id ? 0x5a3a00 : 0x000000);
      }
    }
  };

  return {
    root,
    guides,
    helpers,
    sync,
    objectOf: (id) => entries.get(id)?.group,
    idOf: (object) => {
      for (let node: Object3D | null = object; node; node = node.parent) {
        const id = node.userData.directorId;
        if (typeof id === "string") return id;
      }
      return undefined;
    },
    pickables: () => [...entries.values()].map((entry) => entry.group).filter((g) => g.visible),
    dispose: () => {
      for (const entry of entries.values()) disposeTree(entry.group);
      for (const trail of trails.values()) disposeTree(trail.line);
      entries.clear();
      trails.clear();
      disposeTree(root);
    },
  };
}

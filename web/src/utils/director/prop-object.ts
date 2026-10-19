import {
  BoxGeometry,
  type BufferGeometry,
  CylinderGeometry,
  Group,
  Mesh,
  type MeshStandardMaterial,
  SphereGeometry,
} from "three";

import type { PropShape } from "@/types";

import { type Part, propParts } from "./prop-presets";

/** 一个零件的几何体：单位大小（直径或边长为 1），圆柱按轴向转好 */
function partGeometry(part: Part): BufferGeometry {
  if (part.kind === "box") return new BoxGeometry(1, 1, 1);
  if (part.kind === "sphere") return new SphereGeometry(0.5, 32, 20);
  const cylinder = new CylinderGeometry(0.5, 0.5, 1, 32);
  // 默认沿 y 轴；烘进几何体里，这样后面按 x、y、z 拉伸时三个方向对得上零件的 size
  if (part.axis === "x") cylinder.rotateZ(Math.PI / 2);
  if (part.axis === "z") cylinder.rotateX(Math.PI / 2);
  return cylinder;
}

/**
 * 按形状拼出道具：一个 Group，里面每个零件一个网格，整体占满单位立方体（x、z 居中，y 从地面起）。
 * 道具的宽、高、深由调用方缩放这个 Group 得到。
 * @param material 所有零件共用的材质
 */
export function buildPropObject(shape: PropShape, material: MeshStandardMaterial): Group {
  const root = new Group();
  for (const part of propParts(shape)) {
    const mesh = new Mesh(partGeometry(part), material);
    mesh.position.set(...part.center);
    mesh.scale.set(...part.size);
    mesh.castShadow = true;
    mesh.receiveShadow = true;
    root.add(mesh);
  }
  return root;
}

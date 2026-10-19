import { CapsuleGeometry, Group, Mesh, MeshStandardMaterial, SphereGeometry, Vector3 } from "three";

import type { PoseId } from "@/types";

/**
 * 人物灰模（临时版）：用胶囊体按关节坐标拼出人形。
 * 设计上要换成内置的带骨骼人体模型（Quaternius Universal Base Characters），
 * 换的时候只改这个文件：对外仍是 createFigure 返回一个面朝本地 -z、脚踩 y=0 的 Group。
 */

type Joint = [number, number, number];

/** 一个姿势的关节坐标：本地（右, 上, 前），单位米，身高约 1.75 */
type PoseJoints = {
  hip: Joint;
  neck: Joint;
  head: Joint;
  leftKnee: Joint;
  rightKnee: Joint;
  leftFoot: Joint;
  rightFoot: Joint;
  leftHand: Joint;
  rightHand: Joint;
};

const STAND: PoseJoints = {
  hip: [0, 0.95, 0],
  neck: [0, 1.45, 0],
  head: [0, 1.66, 0],
  leftKnee: [-0.11, 0.5, 0],
  rightKnee: [0.11, 0.5, 0],
  leftFoot: [-0.12, 0.06, 0],
  rightFoot: [0.12, 0.06, 0],
  leftHand: [-0.24, 0.85, 0],
  rightHand: [0.24, 0.85, 0],
};

/** 12 个姿势预设相对站立的差异 */
const POSES: Record<PoseId, Partial<PoseJoints>> = {
  stand: {},
  walk: {
    leftKnee: [-0.1, 0.5, 0.12],
    leftFoot: [-0.1, 0.06, 0.25],
    rightKnee: [0.1, 0.5, -0.08],
    rightFoot: [0.1, 0.08, -0.25],
    leftHand: [-0.24, 0.9, -0.18],
    rightHand: [0.24, 0.9, 0.18],
  },
  run: {
    leftKnee: [-0.1, 0.6, 0.3],
    leftFoot: [-0.1, 0.25, 0.2],
    rightKnee: [0.1, 0.45, -0.15],
    rightFoot: [0.1, 0.12, -0.4],
    leftHand: [-0.24, 1.1, -0.3],
    rightHand: [0.24, 1.0, 0.35],
  },
  sit: {
    hip: [0, 0.5, 0],
    neck: [0, 1.0, 0],
    head: [0, 1.21, 0],
    leftKnee: [-0.12, 0.5, 0.45],
    rightKnee: [0.12, 0.5, 0.45],
    leftFoot: [-0.12, 0.06, 0.45],
    rightFoot: [0.12, 0.06, 0.45],
    leftHand: [-0.22, 0.55, 0.25],
    rightHand: [0.22, 0.55, 0.25],
  },
  crouch: {
    hip: [0, 0.55, -0.1],
    neck: [0, 1.0, 0.05],
    head: [0, 1.21, 0.08],
    leftKnee: [-0.14, 0.5, 0.3],
    rightKnee: [0.14, 0.5, 0.3],
    leftHand: [-0.2, 0.6, 0.3],
    rightHand: [0.2, 0.6, 0.3],
  },
  point: { rightHand: [0.25, 1.4, 0.6] },
  armsCrossed: { leftHand: [0.06, 1.2, 0.15], rightHand: [-0.06, 1.2, 0.15] },
  wave: { rightHand: [0.45, 1.75, 0.05] },
  talk: { rightHand: [0.2, 1.1, 0.3] },
  raiseHand: { rightHand: [0.22, 1.95, 0] },
  lookBack: { head: [0.05, 1.66, -0.04] },
  lying: {
    hip: [0, 0.15, 0],
    neck: [0, 0.15, -0.55],
    head: [0, 0.14, -0.78],
    leftKnee: [-0.12, 0.12, 0.45],
    rightKnee: [0.12, 0.12, 0.45],
    leftFoot: [-0.12, 0.1, 0.9],
    rightFoot: [0.12, 0.1, 0.9],
    leftHand: [-0.4, 0.1, -0.45],
    rightHand: [0.4, 0.1, -0.45],
  },
};

/** 本地坐标（右, 上, 前）换成 three 的坐标：前方是 -z */
const toVector = ([right, up, front]: Joint) => new Vector3(right, up, -front);

const UP = new Vector3(0, 1, 0);

/**
 * 建一个人物灰模。返回的 Group 面朝本地 -z、脚踩 y = 0，材质着色用人物颜色。
 * @param options.capsule 人物模型加载失败时的退路：只画一根胶囊和头
 */
export function createFigure(options: { pose: PoseId; color: string; capsule?: boolean }): {
  group: Group;
  material: MeshStandardMaterial;
} {
  const group = new Group();
  const material = new MeshStandardMaterial({
    color: options.color,
    roughness: 0.7,
    metalness: 0.05,
  });
  const limb = (from: Joint, to: Joint, radius: number) => {
    const a = toVector(from);
    const b = toVector(to);
    const mesh = new Mesh(
      new CapsuleGeometry(radius, Math.max(0.01, a.distanceTo(b)), 4, 10),
      material,
    );
    mesh.position.copy(a).add(b).multiplyScalar(0.5);
    mesh.quaternion.setFromUnitVectors(UP, b.clone().sub(a).normalize());
    mesh.castShadow = true;
    group.add(mesh);
  };
  let head: Joint = [0, 1.66, 0];
  if (options.capsule) {
    limb([0, 0.28, 0], [0, 1.38, 0], 0.24);
  } else {
    const j = { ...STAND, ...POSES[options.pose] };
    const leftShoulder: Joint = [-0.19, j.neck[1] - 0.05, j.neck[2]];
    const rightShoulder: Joint = [0.19, j.neck[1] - 0.05, j.neck[2]];
    const leftHip: Joint = [-0.1, j.hip[1], j.hip[2]];
    const rightHip: Joint = [0.1, j.hip[1], j.hip[2]];
    limb(j.hip, j.neck, 0.15);
    limb(leftShoulder, rightShoulder, 0.07);
    limb(leftHip, rightHip, 0.09);
    limb(leftShoulder, j.leftHand, 0.05);
    limb(rightShoulder, j.rightHand, 0.05);
    limb(leftHip, j.leftKnee, 0.07);
    limb(j.leftKnee, j.leftFoot, 0.06);
    limb(rightHip, j.rightKnee, 0.07);
    limb(j.rightKnee, j.rightFoot, 0.06);
    head = j.head;
  }
  const headMesh = new Mesh(new SphereGeometry(0.115, 20, 14), material);
  headMesh.position.copy(toVector(head));
  headMesh.castShadow = true;
  group.add(headMesh);
  return { group, material };
}

/** 人物头顶的高度（米，未缩放），标签摆在这上面 */
export const FIGURE_TOP = 1.85;

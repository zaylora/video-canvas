import type { GeometryShape, ObjectShape, PropShape } from "@/types";

/** 零件的形状 */
export type PartKind = "box" | "cylinder" | "sphere";

/**
 * 拼成物体的一个零件，坐标在单位立方体里：x、z 以中心为原点（-0.5 到 0.5），y 从地面起（0 到 1）。
 * 整个物体再按道具的宽、高、深拉伸，所以零件的比例跟着物体一起变。
 */
export type Part = {
  kind: PartKind;
  /** 零件中心 */
  center: readonly [number, number, number];
  /** 零件在 x、y、z 三个方向上占的大小（圆柱、球是外接盒） */
  size: readonly [number, number, number];
  /** 圆柱的轴向，缺省沿竖直方向（y）；车轮这类横着的圆柱用 x */
  axis?: "x" | "y" | "z";
};

/** 道具的分类：几何体、物体 */
export type PropCategory = "geometry" | "object";

/** 一种道具形状的静态描述 */
export type PropPreset = {
  label: string;
  category: PropCategory;
  /** 刚放进场景时的宽、高、深（米） */
  size: readonly [number, number, number];
  /** 拼装零件；几何体没有，直接用自己的形状 */
  parts?: readonly Part[];
};

/** 资产面板里道具的分类标签 */
export const PROP_CATEGORIES: readonly { id: PropCategory; label: string }[] = [
  { id: "geometry", label: "几何体" },
  { id: "object", label: "物体" },
];

const box = (center: Part["center"], size: Part["size"]): Part => ({ kind: "box", center, size });

/** 四条腿：对称摆在四角，腿的粗细和高度由调用方给 */
const legs = (x: number, z: number, size: Part["size"], y: number): Part[] =>
  [-1, 1].flatMap((sx) => [-1, 1].map((sz): Part => box([sx * x, y, sz * z], size)));

/** 楼梯：五级台阶，从 +z 端最低一级往 -z 方向逐级升高（人物面朝 -z 往前走就是上楼） */
const STAIR_STEPS = 5;
const stairs = (): Part[] =>
  Array.from({ length: STAIR_STEPS }, (_, i) => {
    const height = (i + 1) / STAIR_STEPS;
    const depth = 1 / STAIR_STEPS;
    return box([0, height / 2, 0.5 - depth / 2 - depth * i], [1, height, depth]);
  });

/**
 * 全部道具预设。物体的朝向约定和人物一致：正面朝本地 -z
 * （椅子、沙发、床的靠背在 +z，汽车车头在 -z，楼梯往 -z 方向上行）。
 */
export const PROP_PRESETS: Record<PropShape, PropPreset> = {
  box: { label: "立方体", category: "geometry", size: [1, 1, 1] },
  cylinder: { label: "圆柱", category: "geometry", size: [0.6, 1, 0.6] },
  sphere: { label: "球体", category: "geometry", size: [0.8, 0.8, 0.8] },
  plane: { label: "平面", category: "geometry", size: [4, 2.5, 0.05] },

  table: {
    label: "桌子",
    category: "object",
    size: [1.4, 0.75, 0.8],
    parts: [box([0, 0.96, 0], [1, 0.08, 1]), ...legs(0.455, 0.4, [0.05, 0.92, 0.09], 0.46)],
  },
  chair: {
    label: "椅子",
    category: "object",
    size: [0.45, 0.9, 0.45],
    parts: [
      box([0, 0.5, 0], [1, 0.06, 1]),
      ...legs(0.4, 0.4, [0.1, 0.47, 0.1], 0.235),
      box([0, 0.76, 0.45], [1, 0.48, 0.1]),
    ],
  },
  sofa: {
    label: "沙发",
    category: "object",
    size: [2, 0.85, 0.9],
    parts: [
      box([0, 0.2, 0], [1, 0.4, 1]),
      box([0, 0.7, 0.375], [1, 0.6, 0.25]),
      box([-0.44, 0.55, -0.125], [0.12, 0.3, 0.75]),
      box([0.44, 0.55, -0.125], [0.12, 0.3, 0.75]),
    ],
  },
  bed: {
    label: "床",
    category: "object",
    size: [1.6, 0.6, 2],
    parts: [
      box([0, 0.175, 0], [1, 0.35, 1]),
      box([0, 0.45, 0], [0.96, 0.2, 0.96]),
      box([0, 0.675, 0.47], [1, 0.65, 0.06]),
      box([-0.24, 0.6, 0.36], [0.35, 0.1, 0.16]),
      box([0.24, 0.6, 0.36], [0.35, 0.1, 0.16]),
    ],
  },
  cabinet: {
    label: "柜子",
    category: "object",
    size: [0.9, 1.8, 0.45],
    parts: [
      box([0, 0.5, 0.02], [1, 1, 0.96]),
      box([0, 0.5, -0.48], [0.02, 0.94, 0.04]),
      box([-0.07, 0.5, -0.48], [0.04, 0.2, 0.04]),
      box([0.07, 0.5, -0.48], [0.04, 0.2, 0.04]),
    ],
  },
  door: {
    label: "门",
    category: "object",
    size: [0.95, 2.1, 0.12],
    parts: [
      box([-0.46, 0.5, 0], [0.08, 1, 1]),
      box([0.46, 0.5, 0], [0.08, 1, 1]),
      box([0, 0.975, 0], [1, 0.05, 1]),
      box([0, 0.475, 0], [0.84, 0.95, 0.4]),
      box([0.3, 0.45, -0.3], [0.06, 0.05, 0.2]),
    ],
  },
  wall: {
    label: "墙",
    category: "object",
    size: [4, 2.6, 0.2],
    parts: [box([0, 0.47, 0], [1, 0.94, 0.8]), box([0, 0.97, 0], [1, 0.06, 1])],
  },
  stairs: { label: "楼梯", category: "object", size: [1.2, 1.8, 3], parts: stairs() },
  car: {
    label: "汽车",
    category: "object",
    size: [1.9, 1.5, 4.4],
    parts: [
      // 车身比车轮窄一点，车轮才露在两侧
      box([0, 0.325, 0], [0.9, 0.35, 1]),
      box([0, 0.75, 0.05], [0.76, 0.5, 0.5]),
      ...[-1, 1].flatMap((sx) =>
        [-1, 1].map((sz): Part => ({
          kind: "cylinder",
          center: [sx * 0.45, 0.15, sz * 0.32],
          size: [0.1, 0.3, 0.3],
          axis: "x",
        })),
      ),
    ],
  },
  tree: {
    label: "树",
    category: "object",
    size: [2, 4, 2],
    parts: [
      { kind: "cylinder", center: [0, 0.2, 0], size: [0.12, 0.4, 0.12] },
      { kind: "sphere", center: [0, 0.65, 0], size: [1, 0.7, 1] },
    ],
  },
};

/** 所有形状，按资产面板里的顺序：先几何体、后物体 */
export const PROP_SHAPES = Object.keys(PROP_PRESETS) as PropShape[];

/** 某个分类下的形状，保持清单顺序 */
export const shapesOf = (category: PropCategory): PropShape[] =>
  PROP_SHAPES.filter((shape) => PROP_PRESETS[shape].category === category);

/** 中文名，默认命名和搜索都用它 */
export const propLabel = (shape: PropShape) => PROP_PRESETS[shape].label;

/** 刚放进场景时的宽、高、深 */
export const propSize = (shape: PropShape) => PROP_PRESETS[shape].size;

/** 几何体对应的零件形状 */
const GEOMETRY_PART: Record<GeometryShape, PartKind> = {
  box: "box",
  plane: "box",
  cylinder: "cylinder",
  sphere: "sphere",
};

/** 物体和几何体统一成零件：几何体就是一个占满单位立方体的零件 */
export function propParts(shape: PropShape): readonly Part[] {
  const preset = PROP_PRESETS[shape];
  if (preset.parts) return preset.parts;
  return [
    {
      kind: GEOMETRY_PART[shape as GeometryShape],
      center: [0, 0.5, 0],
      size: [1, 1, 1],
    },
  ];
}

/** 是不是用零件拼出来的物体（不是几何体） */
export const isObjectShape = (shape: PropShape): shape is ObjectShape =>
  PROP_PRESETS[shape].category === "object";

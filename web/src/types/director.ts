/**
 * 导演台节点的场景数据（设计稿 docs/design/3D导演台）。
 * 存在导演台节点的 data.scene 里，随画布自动保存；坐标单位是米，角度单位是度。
 * 坐标系：y 向上，地面是 y = 0；偏航 yaw 为 0 时朝 -z 看，向 +x 转为正；俯仰 pitch 抬头为正。
 */

/** 机位画幅 */
export type DirectorAspect = "16:9" | "9:16" | "1:1" | "4:3" | "21:9";

/** 关键帧到下一帧的插值：缓入缓出或线性 */
export type KeyEase = "smooth" | "linear";

/** 人物姿势预设 */
export type PoseId =
  | "stand"
  | "walk"
  | "run"
  | "sit"
  | "crouch"
  | "point"
  | "armsCrossed"
  | "wave"
  | "talk"
  | "raiseHand"
  | "lookBack"
  | "lying";

/** 人物在某一时刻的站位 */
export type CharacterPose = { x: number; z: number; yaw: number };

/** 人物关键帧：只记站位和朝向，姿势一段镜头内固定 */
export type CharacterKey = CharacterPose & { t: number; ease: KeyEase };

/** 场景里的人物（灰模） */
export type DirectorCharacter = CharacterPose & {
  id: string;
  name: string;
  /** 灰模着色，用来区分角色，形如 #d4d4d8 */
  color: string;
  pose: PoseId;
  /** 体型缩放，0.8–1.2 */
  scale: number;
  /** 按 t 升序；为空时用上面的静态站位 */
  keys: CharacterKey[];
  hidden?: boolean;
  locked?: boolean;
};

/** 几何体道具的形状 */
export type PropShape = "box" | "cylinder" | "sphere" | "plane";

/** 几何体道具：放在地面上，不打关键帧 */
export type DirectorProp = {
  id: string;
  name: string;
  shape: PropShape;
  x: number;
  z: number;
  yaw: number;
  /** 宽（x）、高（y）、深（z），米 */
  w: number;
  h: number;
  d: number;
  hidden?: boolean;
  locked?: boolean;
};

/** 机位在某一时刻的状态 */
export type CameraPose = {
  x: number;
  y: number;
  z: number;
  yaw: number;
  pitch: number;
  /** 垂直视场角，10–120 */
  fov: number;
};

/** 机位关键帧 */
export type CameraKey = CameraPose & { t: number; ease: KeyEase };

/** 场景里的机位 */
export type DirectorCamera = CameraPose & {
  id: string;
  name: string;
  aspect: DirectorAspect;
  /** 看向的人物 id：运镜预设拿它当目标 */
  lookAt?: string;
  /** 按 t 升序；为空时用上面的静态状态 */
  keys: CameraKey[];
};

/** 导演台场景 */
export type DirectorScene = {
  /** 结构版本，加字段时升级 */
  version: 1;
  /** 镜头时长（秒），1–15，0.5 步进 */
  duration: number;
  /** 小窗和缩略图用的机位；没有机位时为空串 */
  activeCameraId: string;
  characters: DirectorCharacter[];
  props: DirectorProp[];
  cameras: DirectorCamera[];
  /** 节点上显示的缩略图，关闭编辑器时用当前机位渲染并上传 */
  thumb?: { assetId: string; url: string };
};

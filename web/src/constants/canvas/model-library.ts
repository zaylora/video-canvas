import type { NodeKind } from "./node-library";

/** 模型清单里的一条，形状和输入框里的选项一致 */
export type ModelOption = {
  /** 模型标识 */
  id: string;
  /** 模型显示名 */
  label: string;
  /** 单次消耗的积分 */
  credits: number;
  /** 下拉里的一行小字 */
  hint?: string;
  /** 厂商 slug，用来显示 logo；没有时回退首字头像 */
  vendor?: string;
  /** 展示标签 */
  tags?: readonly string[];
};

/**
 * 各种类节点能挑的模型。四种节点的清单都由服务端下发，这里不再放演示数据；
 * 保留这张表是为了给 getModelOptions 等按种类取清单的地方一个统一的兜底。
 * 每种类的第一条就是默认模型。
 */
export const MODEL_LIBRARY = {
  // 文本清单由服务端下发（GET /models?kind=text），见 store/models.ts，这里不放演示数据
  script: [],
  // 图片清单由服务端下发（GET /models?kind=image），见 store/models.ts，这里不放演示数据
  image: [],
  // 视频清单由服务端下发（GET /models?kind=video），见 store/models.ts，这里不放演示数据
  video: [],
  // 音频清单由服务端下发（GET /models?kind=audio），见 store/models.ts，这里不放演示数据
  audio: [],
} as const satisfies Record<NodeKind, readonly ModelOption[]>;

/**
 * 节点种类 -> 后端 kind。模型清单（GET /models?kind=）与任务提交（POST /generation-tasks 的 kind）
 * 都用后端 kind；文本节点的种类名是 script，后端叫 text，这里是唯一的对照表。
 * 表里有的种类由服务端下发清单：不读本地演示清单，也暂不支持自定义模型。
 */
export const REMOTE_KIND_OF_NODE = {
  script: "text",
  image: "image",
  video: "video",
  audio: "audio",
} as const satisfies Partial<Record<NodeKind, string>>;

/** 模型清单由服务端下发的种类 */
export const REMOTE_MODEL_KINDS = Object.keys(REMOTE_KIND_OF_NODE) as NodeKind[];

export const isRemoteModelKind = (kind: string): kind is keyof typeof REMOTE_KIND_OF_NODE =>
  Object.hasOwn(REMOTE_KIND_OF_NODE, kind);

/**
 * 节点种类对应的后端 kind。
 * @param kind 节点种类
 * @returns 后端 kind；该种类不走服务端清单时为 undefined
 */
export const remoteKindOf = (kind: string): string | undefined =>
  isRemoteModelKind(kind) ? REMOTE_KIND_OF_NODE[kind] : undefined;

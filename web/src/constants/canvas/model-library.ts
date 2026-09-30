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
};

/**
 * 各种类节点能挑的模型。视频、文本已经改读接口，其余种类眼下还是写死的演示清单，
 * 接真实服务时把这里换成接口返回的分组，别处不用改。
 * 每种类的第一条就是默认模型。
 */
export const MODEL_LIBRARY = {
  // 文本清单由服务端下发（GET /models?kind=text），见 store/models.ts，这里不放演示数据
  script: [],
  image: [
    { id: "lib-image-2.5", label: "Lib Image 2.5", credits: 2, hint: "标准画质，日常出图" },
    { id: "lib-image-2.5-pro", label: "Lib Image 2.5 Pro", credits: 6, hint: "高清细节，出关键帧" },
    { id: "lib-image-edit", label: "Lib Image Edit", credits: 4, hint: "带参考图改画面" },
  ],
  // 视频清单由服务端下发（GET /models?kind=video），见 store/models.ts，这里不放演示数据
  video: [],
  audio: [
    { id: "lib-voice", label: "Lib Voice", credits: 2, hint: "旁白与角色配音" },
    { id: "lib-music", label: "Lib Music", credits: 8, hint: "BGM 与音效" },
  ],
} as const satisfies Record<NodeKind, readonly ModelOption[]>;

/**
 * 节点种类 -> 后端 kind。模型清单（GET /models?kind=）与任务提交（POST /generation-tasks 的 kind）
 * 都用后端 kind；文本节点的种类名是 script，后端叫 text，这里是唯一的对照表。
 * 表里有的种类由服务端下发清单：不读本地演示清单，也暂不支持自定义模型。
 */
export const REMOTE_KIND_OF_NODE = {
  script: "text",
  video: "video",
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

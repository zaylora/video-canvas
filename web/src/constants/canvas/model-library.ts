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
 * 各种类节点能挑的模型。视频已经改读接口，其余种类眼下还是写死的演示清单，
 * 接真实服务时把这里换成接口返回的分组，别处不用改。
 * 每种类的第一条就是默认模型。
 */
export const MODEL_LIBRARY = {
  script: [
    { id: "gvlm-3.1", label: "GVLM 3.1", credits: 1, hint: "通用文本，写得快" },
    { id: "gvlm-3.1-pro", label: "GVLM 3.1 Pro", credits: 4, hint: "长文与分镜脚本" },
  ],
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

/** 模型清单由服务端下发的种类：不读本地演示清单，也暂不支持自定义模型 */
export const REMOTE_MODEL_KINDS: readonly NodeKind[] = ["video"];

export const isRemoteModelKind = (kind: string): kind is NodeKind =>
  (REMOTE_MODEL_KINDS as readonly string[]).includes(kind);

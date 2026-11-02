import type { ModelInfo } from "@/api/model/type";

/** 拆解时默认的图片模型：项目记下的还在就沿用，否则取第一个可用的 */
export function pickImageModel(
  models: readonly ModelInfo[],
  savedKey: string,
): ModelInfo | undefined {
  return models.find((info) => info.key === savedKey) ?? models[0];
}

import service from "@/utils/requests/service";
import type { ModelInfo } from "./type";

/**
 * 获取模型清单；后端缺失的 capabilities 子项补成空值，下游可以放心遍历
 * @param kind 模型类型
 * @returns 模型列表
 */
export const getModels = async (kind: string): Promise<ModelInfo[]> => {
  const list = await service.get<ModelInfo[] | null>("/models", { kind });
  return (list ?? []).map((item) => ({
    ...item,
    capabilities: {
      ...item.capabilities,
      refs: item.capabilities?.refs ?? {
        image: { on: false, max: 0, max_mb: 0 },
        video: { on: false, max: 0, max_mb: 0 },
        audio: { on: false, max: 0, max_mb: 0 },
      },
      prompt: item.capabilities?.prompt ?? { max_length: 2000 },
    },
  }));
};

import service from "@/utils/requests/service";
import type { ModelInfo } from "./type";

/**
 * 获取模型清单；后端 input_schema 缺失时补成空对象，下游可以放心遍历
 * @param kind 模型类型
 * @returns 模型列表
 */
export const getModels = async (kind: string): Promise<ModelInfo[]> => {
  const list = await service.get<ModelInfo[] | null>("/models", { kind });
  return (list ?? []).map((item) => ({ ...item, input_schema: item.input_schema ?? {} }));
};

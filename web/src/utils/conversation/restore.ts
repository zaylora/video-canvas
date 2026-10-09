import type { GenerateKind, RecordDto } from "@/api/conversation/type";
import type { RefKind } from "@/api/model/type";
import { REF_KEYS, toAssetNumber } from "@/utils/tasks/capabilities";

/** 从记录还原出的输入卡片内容 */
export type RestoredComposer = {
  /** 创作模式 */
  mode: GenerateKind;
  /** 模型 key */
  modelId: string;
  /** 提示词原文 */
  text: string;
  /** 参数取值（含生成方式、生成数量），不含提示词和参考素材 */
  params: Record<string, unknown>;
  /** 参考素材（图片、视频、音频），按种类和输入里的顺序，素材 ID 去重 */
  refs: Array<{ kind: RefKind; id: number }>;
};

/** 不属于「参数」的 input 键：提示词，以及三种参考素材 */
const NON_PARAM_KEYS = new Set(["prompt", ...REF_KEYS.map((ref) => ref.key)]);

/**
 * 从记录的输入快照还原输入卡片：「重新编辑」用它把模式、模型、参数、参考图和提示词填回去。
 * 提示词取记录里保存的原文，不取 input.prompt——后者在提交时可能已把引用展开成了编号。
 * @param record 记录
 * @returns 输入卡片的内容
 */
export function restoreComposer(record: RecordDto): RestoredComposer {
  const params: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(record.input)) {
    if (!NON_PARAM_KEYS.has(key)) params[key] = value;
  }
  const refs = REF_KEYS.flatMap((ref) => {
    const raw = record.input[ref.key];
    if (!Array.isArray(raw)) return [];
    const ids = [...new Set(raw.map(toAssetNumber).filter((id): id is number => id !== null))];
    return ids.map((id) => ({ kind: ref.kind, id }));
  });
  return { mode: record.kind, modelId: record.modelId, text: record.prompt, params, refs };
}

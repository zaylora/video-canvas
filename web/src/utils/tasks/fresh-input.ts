import type { Capabilities } from "@/api/model/type";

import {
  buildTaskInput,
  currentOp,
  hasImageRefs,
  imageLinksOf,
  resolveBindings,
  type BuiltInput,
  type IncomingLink,
} from "./capabilities";

/** 重算出的任务输入，以及绑定阶段给出的非阻断提示 */
export type FreshInput = BuiltInput & {
  /** 声音未生效、参考图被截断之类的提示，不阻断提交 */
  notices: string[];
};

/**
 * 按「有没有图片引用 → 生成方式 → 绑定 → 组装 input」的同一条路径组装提交用的 input。
 * 角色卡的内容就在节点数据里（连线时已经写进 link.character），不需要提交前再读一遍，
 * 所以这里和渲染时是同一份数据；片段工作区提交时也走它。
 * @param caps 所选模型的能力描述
 * @param params 节点里存的参数值
 * @param links 连到目标节点的全部上游连线
 */
export function buildFreshInput(
  caps: Capabilities | undefined,
  params: Record<string, unknown>,
  links: IncomingLink[],
): FreshInput {
  const op = currentOp(caps, params, hasImageRefs(params, { images: imageLinksOf(links) }));
  const bindings = resolveBindings(caps, op, links, params);
  return { ...buildTaskInput(caps, params, bindings, op, links), notices: bindings.notices };
}

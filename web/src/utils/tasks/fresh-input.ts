import type { Capabilities } from "@/api/model/type";
import type { CharacterEntry } from "@/store/characters";
import { applyCharacters } from "@/utils/characters/refs";

import {
  buildTaskInput,
  currentOp,
  hasImageRefs,
  imageLinksOf,
  resolveBindings,
  type BuiltInput,
  type IncomingLink,
} from "./capabilities";

/**
 * 这些上游连线里引用的角色卡 ID（去重，按连线先后）；缺 characterId 的旧数据略过。
 * 提交前据此强制刷新，避免用旧图 / 旧声音生成（设计 §8.2「实时引用的实现要点」）。
 * @param links 连到目标节点的全部上游连线
 */
export function characterIdsOf(links: IncomingLink[]): string[] {
  const ids = links.flatMap((link) =>
    link.sourceKind === "character" && link.characterId ? [link.characterId] : [],
  );
  return [...new Set(ids)];
}

/** 用最新角色卡内容重算出的任务输入，以及绑定阶段给出的非阻断提示 */
export type FreshInput = BuiltInput & {
  /** 声音未生效、参考图被截断之类的提示，不阻断提交 */
  notices: string[];
};

/**
 * 用刚读到的角色卡内容重新组装提交用的 input：先把最新内容写进角色卡连线，
 * 再按「有没有图片引用 → 生成方式 → 绑定 → 组装 input」的同一条路径重算。
 * 提交时的最终 input 以它为准，不用节点渲染时算出的那份。
 * @param caps 所选模型的能力描述
 * @param params 节点里存的参数值
 * @param links 连到目标节点的全部上游连线
 * @param latest 刚强制刷新得到的角色卡缓存项
 */
export function buildFreshInput(
  caps: Capabilities | undefined,
  params: Record<string, unknown>,
  links: IncomingLink[],
  latest: Record<string, CharacterEntry | undefined>,
): FreshInput {
  const fresh = applyCharacters(links, latest);
  const op = currentOp(caps, params, hasImageRefs(params, { images: imageLinksOf(fresh) }));
  const bindings = resolveBindings(caps, op, fresh);
  return { ...buildTaskInput(caps, params, bindings, op, fresh), notices: bindings.notices };
}

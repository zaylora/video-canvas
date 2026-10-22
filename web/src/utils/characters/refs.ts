import type { CharacterEntry } from "@/store/characters";
import type { CharacterRefs, IncomingLink } from "@/utils/tasks/capabilities";

/**
 * 缓存项换成绑定用的素材引用。
 * 失效（角色卡已删除、主图素材已不可用）是 null，节点灰显、连线保留但不参与提交；
 * 还没读到（加载中、没读过、失败且没有旧内容）是 undefined，也不参与提交，但节点显示加载态而不是失效。
 * 读取失败但有旧内容时沿用旧内容，节点保持可用；提交前的强制刷新会另行拦截读取失败。
 * @param entry 角色卡缓存项
 */
export function characterRefsOf(
  entry: CharacterEntry | undefined,
): CharacterRefs | null | undefined {
  if (!entry || entry.status === "loading") return undefined;
  if (entry.status === "missing") return null;
  const card = entry.card;
  if (!card) return undefined;
  if (!card.avatar) return null;
  return {
    id: card.id,
    name: card.name,
    avatarAssetId: card.avatar.id,
    extraAssetIds: card.extras.map((asset) => asset.id),
    voiceAssetId: card.voice?.id ?? null,
  };
}

/**
 * 把缓存里的角色卡内容写进角色卡连线（别的连线原样返回）。
 * @param links 上游连线，角色卡连线带 characterId
 * @param byId 角色卡缓存
 */
export function applyCharacters(
  links: IncomingLink[],
  byId: Record<string, CharacterEntry | undefined>,
): IncomingLink[] {
  return links.map((link) => {
    if (link.sourceKind !== "character") return link;
    return {
      ...link,
      character: link.characterId ? characterRefsOf(byId[link.characterId]) : null,
    };
  });
}

/**
 * 去掉已失效的角色卡节点（@ 候选用）：失效的卡不参与提交，列出来只会让人选了没用。
 * 还在加载（undefined）的先留着，读到之后再判；缺 characterId 的旧数据按失效处理。别的节点原样保留。
 * @param nodes 候选节点
 * @param byId 角色卡缓存
 */
export function withoutDeadCharacters<T extends { data: { kind: string; characterId?: string } }>(
  nodes: readonly T[],
  byId: Record<string, CharacterEntry | undefined>,
): T[] {
  return nodes.filter((node) => {
    if (node.data.kind !== "character") return true;
    const id = node.data.characterId;
    return !!id && characterRefsOf(byId[id]) !== null;
  });
}

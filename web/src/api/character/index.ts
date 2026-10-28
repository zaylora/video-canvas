import { mapAsset } from "@/api/asset";
import service from "@/utils/requests/service";

import type {
  BackendCharacterBody,
  BackendCharacterDto,
  CharacterDto,
  CharacterInput,
  CharacterPatch,
} from "./type";

/** 批量按 ID 读取的上限（契约 §3：≤ 100 个） */
export const CHARACTER_IDS_LIMIT = 100;

/** 后端角色卡转前端结构：素材走 mapAsset，null 列表兜底为空 */
export const mapCharacter = (raw: BackendCharacterDto): CharacterDto => ({
  id: raw.id,
  name: raw.name,
  description: raw.description ?? "",
  avatar: raw.avatar ? mapAsset(raw.avatar) : null,
  extras: (raw.extras ?? []).map(mapAsset),
  voice: raw.voice ? mapAsset(raw.voice) : null,
  dramaId: raw.drama_id ? raw.drama_id : null,
  lookName: raw.look_name ?? "",
  voicePrompt: raw.voice_prompt ?? "",
  createdAt: raw.created_at,
  updatedAt: raw.updated_at,
});

/** 表单内容转后端请求体：素材 id 转数字，补充图与声音始终全量写出（PATCH 里 [] / null 表示清空） */
const toBody = (input: CharacterInput): BackendCharacterBody => ({
  name: input.name,
  description: input.description,
  avatar_asset_id: Number(input.avatarAssetId),
  extra_asset_ids: input.extraAssetIds.map(Number),
  voice_asset_id: input.voiceAssetId === null ? null : Number(input.voiceAssetId),
});

/**
 * 获取我的角色卡列表，按更新时间倒序，不分页（每用户 ≤ 100 张）
 * @param q 名称关键字，不传或空串不过滤
 * @returns 角色卡列表
 */
export const listCharacters = async (q?: string): Promise<CharacterDto[]> => {
  const keyword = q?.trim();
  const page = await service.get<{ items: BackendCharacterDto[] | null }>(
    "/characters",
    keyword ? { q: keyword } : undefined,
  );
  return (page.items ?? []).map(mapCharacter);
};

/**
 * 按 ID 批量读取角色卡（画布节点用）；查不到的缺席，调用方据此判失效
 * @param ids 角色卡 ID，≤ 100 个；空数组不发请求
 * @returns 查到的角色卡，顺序不保证
 */
export const getCharactersByIds = async (ids: string[]): Promise<CharacterDto[]> => {
  if (ids.length === 0) return [];
  const page = await service.get<{ items: BackendCharacterDto[] | null }>(
    "/characters",
    { ids: ids.join(",") },
    { silent: true },
  );
  return (page.items ?? []).map(mapCharacter);
};

/**
 * 新建角色卡；重名、数量上限、素材不可用等业务错误由后端返回，调用方就地展示
 * @param input 表单内容
 * @returns 新角色卡
 */
export const createCharacter = async (input: CharacterInput): Promise<CharacterDto> =>
  mapCharacter(
    await service.post<BackendCharacterDto>("/characters", toBody(input), { silent: true }),
  );

/**
 * 保存角色卡（全量字段）；业务错误同新建，另有角色卡不存在
 * @param id 角色卡 ID
 * @param input 表单内容
 * @returns 保存后的角色卡
 */
export const updateCharacter = async (id: string, input: CharacterInput): Promise<CharacterDto> =>
  mapCharacter(
    await service.patch<BackendCharacterDto>(`/characters/${id}`, toBody(input), {
      silent: true,
    }),
  );

/**
 * 局部更新角色卡（契约 §5）：只传要改的字段。短剧角色生成形象成功后用它把主图写回
 * @param id 角色卡 ID
 * @param patch 要改的字段
 * @returns 保存后的角色卡
 */
export const patchCharacter = async (id: string, patch: CharacterPatch): Promise<CharacterDto> =>
  mapCharacter(
    await service.patch<BackendCharacterDto>(
      `/characters/${id}`,
      {
        ...(patch.avatarAssetId !== undefined
          ? { avatar_asset_id: patch.avatarAssetId === null ? null : Number(patch.avatarAssetId) }
          : {}),
        ...(patch.lookName !== undefined ? { look_name: patch.lookName } : {}),
        ...(patch.voicePrompt !== undefined ? { voice_prompt: patch.voicePrompt } : {}),
      },
      { silent: true },
    ),
  );

/**
 * 删除角色卡，只删卡本身，不删素材、不反查画布引用
 * @param id 角色卡 ID
 */
export const deleteCharacter = async (id: string): Promise<void> => {
  await service.delete(`/characters/${id}`);
};

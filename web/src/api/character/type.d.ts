import type { AssetDto, BackendAssetDto } from "@/api/asset/type";

/** 后端角色卡视图 CharacterView（snake_case，素材 id 是数字） */
export interface BackendCharacterDto {
  /** 角色卡 ID（十六进制串） */
  id: string;
  /** 名称，去首尾空格后 1–64 字，同一用户内忽略大小写唯一 */
  name: string;
  /** 角色设定，≤ 500 字，只展示不注入提示词 */
  description: string;
  /** 主图；素材已不可用时为 null */
  avatar: BackendAssetDto | null;
  /** 补充图，0–4 张，按保存顺序；不可用的素材后端已略过 */
  extras: BackendAssetDto[] | null;
  /** 声音（音频素材）；没绑定或素材不可用时为 null */
  voice?: BackendAssetDto | null;
  /** 创建时间（ISO） */
  created_at: string;
  /** 更新时间（ISO） */
  updated_at: string;
}

/** 前端使用的角色卡 */
export interface CharacterDto {
  /** 角色卡 ID（十六进制串） */
  id: string;
  /** 名称 */
  name: string;
  /** 角色设定，没有时为空串 */
  description: string;
  /** 主图；null 表示主图素材已不可用，等同角色卡失效 */
  avatar: AssetDto | null;
  /** 补充图，0–4 张，主图之后依次带入 */
  extras: AssetDto[];
  /** 声音；null 表示没绑定或素材不可用 */
  voice: AssetDto | null;
  /** 创建时间（ISO） */
  createdAt: string;
  /** 更新时间（ISO） */
  updatedAt: string;
}

/** 新建 / 保存角色卡的表单内容，素材用前端的字符串 id */
export interface CharacterInput {
  /** 名称，调用方先做去空格与 1–64 字校验 */
  name: string;
  /** 角色设定，≤ 500 字 */
  description: string;
  /** 主图素材 ID（必填，image） */
  avatarAssetId: string;
  /** 补充图素材 ID，≤ 4 个，不含主图，按展示顺序 */
  extraAssetIds: string[];
  /** 声音素材 ID（audio）；null 表示没有声音（更新时即清空） */
  voiceAssetId: string | null;
}

/** 后端新建 / 更新请求体 */
export interface BackendCharacterBody {
  /** 名称 */
  name: string;
  /** 角色设定 */
  description: string;
  /** 主图素材 ID（数字） */
  avatar_asset_id: number;
  /** 补充图素材 ID（数字），空数组表示清空 */
  extra_asset_ids: number[];
  /** 声音素材 ID（数字），null 表示清空 */
  voice_asset_id: number | null;
}

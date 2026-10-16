import type { AssetDto, BackendAssetDto } from "@/api/asset/type";

/** 后端文件夹（snake_case） */
export interface BackendFolderDto {
  /** 文件夹 ID（十六进制串） */
  id: string;
  /** 名称，1–30 字 */
  name: string;
  /** 文件夹里的素材数 */
  item_count: number;
  /** 创建时间（ISO） */
  created_at: string;
}

/** 前端使用的文件夹 */
export interface FolderDto {
  /** 文件夹 ID（十六进制串） */
  id: string;
  /** 名称，1–30 字 */
  name: string;
  /** 文件夹里的素材数 */
  itemCount: number;
  /** 创建时间（ISO） */
  createdAt: string;
}

/** 后端资产库条目（snake_case） */
export interface BackendLibraryItemDto {
  /** 归属 ID（十六进制串） */
  id: string;
  /** 素材 */
  asset: BackendAssetDto;
  /** 所在文件夹；null 是根目录 */
  folder_id: string | null;
  /** 加入时间（ISO） */
  created_at: string;
}

/** 前端使用的资产库条目：一个素材在资产库里的归属 */
export interface LibraryItemDto {
  /** 归属 ID（十六进制串），移动、移出都用它 */
  id: string;
  /** 素材 */
  asset: AssetDto;
  /** 所在文件夹；null 是根目录 */
  folderId: string | null;
  /** 加入时间（ISO） */
  createdAt: string;
}

/** 后端生成历史条目里的来源画布 */
export interface BackendGeneratedCanvasDto {
  /** 画布 ID（十六进制串） */
  id: string;
  /** 画布标题；画布已删除时为空串 */
  title: string;
  /** 画布已删除时为 true，其余情况不返回 */
  deleted?: boolean;
}

/** 后端生成历史条目（snake_case） */
export interface BackendGeneratedItemDto {
  /** 素材 */
  asset: BackendAssetDto;
  /** 生成它的提示词（完整，不截断） */
  prompt: string;
  /** 模型展示名 */
  model_label: string;
  /** 来源画布；null 是首页对话 */
  canvas: BackendGeneratedCanvasDto | null;
  /** 素材创建时间（ISO） */
  created_at: string;
  /** 已在资产库时的归属 ID；null 表示还没加入 */
  library_item_id: string | null;
}

/** 生成历史条目的来源画布 */
export interface GeneratedCanvasDto {
  /** 画布 ID（十六进制串） */
  id: string;
  /** 画布标题；已删除时为空串 */
  title: string;
  /** 画布是否已被删除 */
  deleted: boolean;
}

/** 前端使用的生成历史条目：一次生成产出的一个素材 */
export interface GeneratedItemDto {
  /** 素材 */
  asset: AssetDto;
  /** 生成它的提示词 */
  prompt: string;
  /** 模型展示名 */
  modelLabel: string;
  /** 来源画布；null 是首页对话 */
  canvas: GeneratedCanvasDto | null;
  /** 素材创建时间（ISO） */
  createdAt: string;
  /** 已在资产库时的归属 ID；null 表示还没加入 */
  libraryItemId: string | null;
}

/** 游标分页的一页；nextCursor 为空串表示没有更多 */
export interface CursorPageDto<T> {
  /** 本页内容 */
  items: T[];
  /** 下一页游标，空串表示到底了 */
  nextCursor: string;
}

/** 后端游标分页响应 */
export interface BackendCursorPage<T> {
  /** 本页内容，可能为 null */
  items: T[] | null;
  /** 下一页游标，空串表示到底了 */
  next_cursor: string | null;
}

/** 素材类型 */
export type AssetKind = AssetDto["kind"];

/** 来源筛选：全部 / 首页对话 / 某张画布 */
export type GeneratedSource = "all" | "conversation" | "canvas";

/** 生成历史的查询参数 */
export interface GeneratedQueryDto {
  /** 素材类型，必填 */
  kind: AssetKind;
  /** 来源，默认全部 */
  source?: GeneratedSource;
  /** source 为 canvas 时的画布 ID */
  canvasId?: string;
  /** 只要这个时间之后的（RFC3339），由客户端按本地时区算 */
  createdFrom?: string;
  /** 排序：desc 最新优先（默认），asc 最早优先 */
  order?: "desc" | "asc";
  /** 提示词关键字 */
  q?: string;
  /** 下一页游标 */
  cursor?: string;
  /** 每页条数，默认 40，最大 100 */
  limit?: number;
}

/** 资产库条目的查询参数 */
export interface LibraryQueryDto {
  /** 文件夹：root 根目录、具体文件夹 ID、all 跨文件夹（搜索时用） */
  folderId: string;
  /** 素材类型 */
  kind: AssetKind;
  /** 文件名关键字 */
  q?: string;
  /** 下一页游标 */
  cursor?: string;
  /** 每页条数 */
  limit?: number;
}

/** 登录页展示设置（GET / PUT /admin/settings/showcase/settings），沿用后端 snake_case */
export interface ShowcaseSettings {
  /** 每条作品播放多少秒，4 到 15 */
  clip_seconds: number;
  /** 登录页是否播放背景轮播 */
  show_on_login: boolean;
  /** 浏览器开启省流量模式时是否只显示封面 */
  poster_only_on_save_data: boolean;
}

/** 后台看到的一条作品（含禁用的） */
export interface ShowcaseAdminItem {
  /** 作品 ID */
  id: number;
  /** 视频素材 ID */
  asset_id: number;
  /** 封面素材 ID，没有封面为 null */
  poster_asset_id: number | null;
  /** 视频地址 */
  video_url: string;
  /** 封面地址，没有时为空串 */
  poster_url: string;
  /** 生成它的那句话 */
  prompt: string;
  /** 模型标签，可为空串 */
  model_label: string;
  /** 从视频第几秒开始播放 */
  start_sec: number;
  /** 是否参与轮播 */
  enabled: boolean;
  /** 视频宽度（像素），0 表示没有该维度 */
  width: number;
  /** 视频高度（像素），0 表示没有该维度 */
  height: number;
  /** 视频文件大小（字节） */
  byte_size: number;
  /** 视频时长（毫秒），0 表示没有 */
  duration_ms: number;
  /** 视频原始文件名 */
  file_name: string;
  /** 创建时间（ISO 字符串） */
  created_at: string;
}

/** GET /admin/settings/showcase 的响应 */
export interface ShowcaseAdminView {
  /** 全局设置 */
  settings: ShowcaseSettings;
  /** 全部作品，按轮播顺序 */
  items: ShowcaseAdminItem[];
}

/** 新增作品的请求体 */
export interface CreateShowcaseItemBody {
  /** 视频素材 ID（先用 POST /assets 上传） */
  asset_id: number;
  /** 封面素材 ID，不传表示没有封面 */
  poster_asset_id?: number | null;
  /** 生成它的那句话，1 到 80 字 */
  prompt: string;
  /** 模型标签，可选 */
  model_label?: string;
  /** 起始秒，默认 0 */
  start_sec?: number;
  /** 是否启用，默认启用 */
  enabled?: boolean;
}

/** 修改作品的请求体：只传要改的字段；poster_asset_id 传 null 表示清空封面 */
export interface UpdateShowcaseItemBody {
  /** 替换视频：新视频素材 ID（先用 POST /assets 上传），不传不换 */
  asset_id?: number;
  /** 生成它的那句话 */
  prompt?: string;
  /** 模型标签 */
  model_label?: string;
  /** 起始秒 */
  start_sec?: number;
  /** 是否启用 */
  enabled?: boolean;
  /** 封面素材 ID，null 清空 */
  poster_asset_id?: number | null;
}

/** 素材库里的一个平台生成视频（GET /admin/settings/showcase/library） */
export interface ShowcaseLibraryItem {
  /** 视频素材 ID */
  asset_id: number;
  /** 视频地址 */
  video_url: string;
  /** 生成它的提示词（已按展示上限截到 80 字），取不到为空串 */
  prompt: string;
  /** 生成它的模型名称，取不到为空串 */
  model_label: string;
  /** 作者的用户名 */
  owner: string;
  /** 生成时间（ISO 字符串） */
  created_at: string;
  /** 视频宽度（像素），0 表示没有该维度 */
  width: number;
  /** 视频高度（像素），0 表示没有该维度 */
  height: number;
  /** 视频文件大小（字节） */
  byte_size: number;
  /** 视频时长（毫秒），0 表示没有 */
  duration_ms: number;
  /** 是否已在登录页片单里 */
  added: boolean;
}

/** 素材库的一页 */
export interface ShowcaseLibraryPage {
  /** 本页的视频 */
  items: ShowcaseLibraryItem[];
  /** 符合条件的视频总数 */
  total: number;
  /** 页码，从 1 开始 */
  page: number;
  /** 每页条数 */
  page_size: number;
}

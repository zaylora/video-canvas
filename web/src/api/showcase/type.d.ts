/** 登录页轮播的全局设置（前端使用，camelCase） */
export interface ShowcaseSettingsDto {
  /** 每条作品播放多少秒，4 到 15 */
  clipSeconds: number;
  /** 登录页是否播放背景轮播；false 时显示默认渐变背景 */
  showOnLogin: boolean;
  /** 浏览器开启省流量模式时是否只显示封面、不加载视频 */
  posterOnSaveData: boolean;
}

/** 登录页轮播里的一条作品（前端使用，id 为字符串） */
export interface ShowcaseItemDto {
  /** 作品 ID */
  id: string;
  /** 视频地址（稳定的 /files 地址，不需要登录） */
  videoUrl: string;
  /** 封面地址，没有封面时为 null */
  posterUrl: string | null;
  /** 生成它的那句话，显示在画面上，「做同款」会带它进首页输入框 */
  prompt: string;
  /** 模型标签，没有时为空串 */
  modelLabel: string;
  /** 从视频第几秒开始播放 */
  startSec: number;
  /** 视频宽度（像素），没有该维度时为 null */
  width: number | null;
  /** 视频高度（像素），没有该维度时为 null */
  height: number | null;
  /** 视频文件大小（字节），没有时为 null */
  byteSize: number | null;
}

/** 登录页展示：设置 + 启用的作品 */
export interface ShowcaseDto {
  /** 全局设置 */
  settings: ShowcaseSettingsDto;
  /** 启用的作品，按轮播顺序；登录页播放关闭时为空数组 */
  items: ShowcaseItemDto[];
}

/** 后端 GET /showcase 的原始设置（snake_case） */
export interface BackendShowcaseSettingsDto {
  /** 每条播放秒数 */
  clip_seconds: number;
  /** 登录页是否播放 */
  show_on_login: boolean;
  /** 省流量时是否只显示封面 */
  poster_only_on_save_data: boolean;
}

/** 后端 GET /showcase 的原始作品（snake_case，id 为数字） */
export interface BackendShowcaseItemDto {
  /** 作品 ID */
  id: number | string;
  /** 视频地址 */
  video_url: string;
  /** 封面地址，没有时为空串 */
  poster_url?: string | null;
  /** 提示词 */
  prompt: string;
  /** 模型标签 */
  model_label?: string | null;
  /** 起始秒 */
  start_sec?: number | null;
  /** 视频宽度，0 表示没有该维度 */
  width?: number | null;
  /** 视频高度，0 表示没有该维度 */
  height?: number | null;
  /** 文件大小（字节），0 表示没有 */
  byte_size?: number | null;
}

/** 后端 GET /showcase 的原始响应 */
export interface BackendShowcaseDto {
  /** 全局设置 */
  settings: BackendShowcaseSettingsDto;
  /** 启用的作品，null 兜底成空数组 */
  items: BackendShowcaseItemDto[] | null;
}

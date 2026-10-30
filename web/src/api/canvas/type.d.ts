import type { DirectorScene, NodeCharacter, NodeDrama } from "@/types";

/** 持久化到后端的画布节点 */
export interface PersistedCanvasNodeDto {
  /** 节点 ID */
  id: string;
  /** 节点渲染类型，固定为 canvas */
  type: "canvas";
  /** 节点在画布上的坐标 */
  position: { x: number; y: number };
  /** 节点坐标原点 */
  origin?: [number, number];
  /** 所属组的节点 ID；有它时 position 是相对该组左上角的坐标 */
  parentId?: string;
  /** 节点携带的业务数据 */
  data: {
    /** 节点种类 */
    kind: "script" | "image" | "video" | "audio" | "director" | "character";
    /** 节点标题 */
    label: string;
    /** 提示词 */
    prompt?: string;
    /** 选中的模型 key */
    model?: string;
    /** 生成状态 */
    status?: "idle" | "running" | "done" | "error";
    /** 产出或上传素材的地址 */
    src?: string | null;
    /** src 素材的媒体类型 */
    mediaType?: "image" | "video" | "audio";
    /** 服务端素材记录 ID */
    assetId?: string;
    /** 素材是否为本机上传 */
    uploaded?: boolean;
    /** 上传的文件名 */
    fileName?: string | null;
    /** 文本节点生成的正文 */
    text?: string | null;
    /** 生成失败原因 */
    error?: string | null;
    /** 生成失败时的任务编号 */
    errorTaskRef?: string | null;
    /** 生成任务 id（字符串）；running 状态靠它在刷新/重开后对账回填 */
    taskId?: string;
    /** 角色声音生成任务 id；声音独立于主图任务回填 */
    voiceTaskId?: string;
    /** 本节点进行中的任务 id 集合（一次提交多个任务时才有） */
    pendingTaskIds?: string[];
    /** 按模型 capabilities 存的参数值（提示词、生成方式、生成参数、手动添加的参考素材） */
    params?: Record<string, unknown>;
    /** 参数里媒体字段所选素材的展示信息 */
    paramAssets?: Record<
      string,
      { url: string; fileName?: string; mediaType?: "image" | "video" | "audio" }
    >;
    /** 导演台节点的 3D 场景（结构见 types/director.ts） */
    scene?: DirectorScene;
    /** 角色卡节点自带的角色数据（结构见 types/canvas 的 NodeCharacter），主图沿用本节点的 assetId / src */
    character?: NodeCharacter;
    /** @deprecated 旧版角色卡节点引用的全局角色卡 ID，打开画布时规整掉 */
    characterId?: string;
    /** @deprecated 旧版角色卡节点缓存的名称和主图，规整时只取名字 */
    labelCache?: { name: string; coverAssetId?: string };
    /** 短剧资产元数据（仅短剧专属画布的节点），结构见 types/canvas 的 NodeDrama */
    drama?: NodeDrama;
  };
}

/** 持久化到后端的组节点：框住若干节点，成员通过 parentId 指向它 */
export interface PersistedCanvasGroupDto {
  /** 节点 ID */
  id: string;
  /** 节点渲染类型 */
  type: "group";
  /** 组左上角在画布上的坐标 */
  position: { x: number; y: number };
  /** 组框宽度 */
  width: number;
  /** 组框高度 */
  height: number;
  data: {
    /** 组名 */
    label: string;
    /** 背景色 */
    color?: "red" | "orange" | "yellow" | "green" | "cyan" | "blue" | "purple" | "pink";
    /** 组名行颜色 */
    labelColor?: "red" | "orange" | "yellow" | "green" | "cyan" | "blue" | "purple" | "pink";
  };
}

/** 持久化到后端的画布连线 */
export interface PersistedCanvasEdgeDto {
  /** 连线 ID */
  id: string;
  /** 起点节点 ID */
  source: string;
  /** 终点节点 ID */
  target: string;
  /** 起点 handle ID */
  sourceHandle?: string | null;
  /** 终点 handle ID */
  targetHandle?: string | null;
}

/** 画布图谱：节点、连线和视口 */
export interface CanvasGraphDto {
  /** 节点列表 */
  nodes: Array<PersistedCanvasNodeDto | PersistedCanvasGroupDto>;
  /** 连线列表 */
  edges: PersistedCanvasEdgeDto[];
  /** 视口位置与缩放 */
  viewport: { x: number; y: number; zoom: number };
}

/** 列表页使用的展示数据，字段已在 API 层转换成前端命名。 */
export interface CanvasListItemDto {
  /** 画布 ID */
  id: string;
  /** 画布标题 */
  title: string;
  /** 封面地址，暂无封面为 null */
  coverUrl: string | null;
  /** 画布版本号 */
  revision: number;
  /** 最近更新时间 */
  updatedAt: string;
}

/** 后端使用页码分页，参数名与 Go handler 的 query tag 保持一致。 */
export interface CanvasListQueryDto {
  /** 页码，从 1 开始 */
  page?: number;
  /** 每页条数 */
  page_size?: number;
  /** 标题搜索关键字 */
  keyword?: string;
}

/** 前端使用的画布列表响应 */
export interface CanvasListResponseDto {
  /** 当前页的画布列表 */
  items: CanvasListItemDto[];
  /** 总条数 */
  total: number;
  /** 当前页码 */
  page: number;
  /** 每页条数 */
  pageSize: number;
  /** 下一页页码，没有下一页为 null */
  nextCursor: string | null;
}

/** 后端 GET /canvas 列表里的一项原始结构。 */
export interface BackendCanvasListItemDto {
  /** 画布 ID，十六进制串 */
  id: string;
  /** 画布标题 */
  title: string;
  /** 画布版本号 */
  revision: number;
  /** 创建时间 */
  created_at: string;
  /** 最近更新时间 */
  updated_at: string;
  /** 封面地址：画布里第一张生成成功的图；旧画布和没生成过图的画布为空串 */
  cover_url?: string;
}

/** 后端 GET /canvas 的原始响应结构。 */
export interface BackendCanvasListResponseDto {
  /** 当前页列表，无数据时为 null */
  list: BackendCanvasListItemDto[] | null;
  /** 总条数 */
  total: number;
  /** 当前页码 */
  page: number;
  /** 每页条数 */
  page_size: number;
}

/** 后端 POST /canvas 的原始响应结构。 */
export interface BackendCanvasProjectDto {
  /** 画布 ID，十六进制串 */
  id: string;
  /** 画布标题 */
  title: string;
  /** 画布图谱 JSON */
  payload_json: unknown;
  /** 画布版本号，用于乐观锁 */
  revision: number;
  /** 创建时间 */
  created_at: string;
  /** 最近更新时间 */
  updated_at: string;
}

/** 前端使用的画布详情 */
export interface CanvasDetailDto {
  /** 画布 ID */
  id: string;
  /** 画布标题 */
  title: string;
  /** 画布描述，后端暂未提供为 null */
  description: string | null;
  /** 画布版本号 */
  version: number;
  /** 画布图谱 */
  graph: CanvasGraphDto;
  /** 创建时间 */
  createdAt: string;
  /** 最近更新时间 */
  updatedAt: string;
}

/** 创建画布的参数 */
export interface CreateCanvasDto {
  /** 画布标题，缺省为「未命名画布」 */
  title?: string;
  /** 初始图谱，缺省为空画布 */
  graph?: CanvasGraphDto;
}

/** 更新画布的参数 */
export interface UpdateCanvasDto {
  /** 新标题 */
  title?: string;
  /** 新图谱 */
  graph?: CanvasGraphDto;
  /** 当前版本号，用于乐观锁 */
  revision: number;
}

/** 保存画布图谱的参数 */
export interface SaveCanvasGraphDto {
  /** 保存所基于的版本号 */
  baseVersion: number;
  /** 待保存的图谱 */
  graph: CanvasGraphDto;
}

/** 保存画布图谱的结果 */
export interface SaveCanvasGraphResponseDto {
  /** 保存后的版本号 */
  version: number;
  /** 保存后的更新时间 */
  updatedAt: string;
}

/** 导出文档里的一条素材描述，仅用于展示和交叉校验 */
export interface CanvasExportAsset {
  /** 素材类型 */
  kind: "image" | "video" | "audio";
  /** MIME 类型 */
  mimeType: string;
  /** 文件字节数 */
  byteSize: number;
  /** 宽度，像素；0 表示没有该维度 */
  width: number;
  /** 高度，像素；0 表示没有该维度 */
  height: number;
  /** 时长，毫秒；0 表示没有该维度 */
  durationMs: number;
  /** 原文件名 */
  fileName: string;
}

/** 画布导出文档（video-canvas/canvas）；前端只下载和回传，节点与连线不解析 */
export interface CanvasExportDocument {
  /** 固定为 "video-canvas/canvas" */
  format: string;
  /** 文件格式版本 */
  version: number;
  /** 导出时间，RFC 3339 UTC，只用于展示 */
  exportedAt: string;
  /** 导出时的应用版本，只用于排查 */
  appVersion: string;
  /** 画布本体 */
  canvas: {
    /** 画布标题 */
    title: string;
    /** 节点列表，顺序有意义，原样保留 */
    nodes: Array<Record<string, unknown>>;
    /** 连线列表 */
    edges: Array<Record<string, unknown>>;
  };
  /** 用到的素材，键为 SHA-256 十六进制内容哈希（角色卡节点的补充图、音频音色也在其中，角色数据随节点导出） */
  assets: Record<string, CanvasExportAsset>;
}

/** 导出画布的后端响应 */
export interface BackendCanvasExportDto {
  /** 导出文档 */
  document: CanvasExportDocument;
  /** 导出报告 */
  report?: {
    /** 因找不到文件而没有写进文档的素材数 */
    skipped_assets?: number;
  };
}

/** 导出画布的结果 */
export interface CanvasExportResultDto {
  /** 导出文档 */
  document: CanvasExportDocument;
  /** 因找不到文件而被跳过的素材数 */
  skippedAssets: number;
}

/** 导入画布的后端响应 */
export interface BackendCanvasImportDto {
  /** 新建的画布 */
  canvas: {
    /** 画布 ID */
    id: string;
    /** 画布标题 */
    title: string;
    /** 当前版本号 */
    revision: number;
  };
  /** 导入报告 */
  report: {
    /** 素材处理情况 */
    assets: {
      /** 文档里的素材总数 */
      total: number;
      /** 成功复制到当前账号的素材数 */
      copied: number;
      /** 找不到的素材所在节点 */
      missing?: Array<{ node_id: string; file_name: string }> | null;
    };
    /** 因端点节点不存在而丢弃的连线数 */
    edges_dropped: number;
  };
}

/** 导入画布的结果 */
export interface CanvasImportResultDto {
  /** 新建的画布 */
  canvas: {
    /** 画布 ID */
    id: string;
    /** 画布标题 */
    title: string;
    /** 当前版本号 */
    revision: number;
  };
  /** 导入报告 */
  report: {
    /** 素材处理情况 */
    assets: {
      /** 文档里的素材总数 */
      total: number;
      /** 成功复制到当前账号的素材数 */
      copied: number;
      /** 找不到的素材所在节点 */
      missing: { nodeId: string; fileName: string }[];
    };
    /** 因端点节点不存在而丢弃的连线数 */
    edgesDropped: number;
  };
}

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
    kind: "script" | "image" | "video" | "audio";
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
    /** 生成任务 id（字符串）；running 状态靠它在刷新/重开后对账回填 */
    taskId?: string;
    /** 按模型 capabilities 存的参数值（提示词、生成方式、生成参数、手动添加的参考素材） */
    params?: Record<string, unknown>;
    /** 参数里媒体字段所选素材的展示信息 */
    paramAssets?: Record<
      string,
      { url: string; fileName?: string; mediaType?: "image" | "video" | "audio" }
    >;
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

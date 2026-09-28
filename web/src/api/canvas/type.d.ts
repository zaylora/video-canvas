export interface PersistedCanvasNodeDto {
  id: string
  type: 'canvas'
  position: { x: number; y: number }
  origin?: [number, number]
  data: {
    kind: 'script' | 'image' | 'video' | 'audio'
    label: string
    prompt?: string
    model?: string
    status?: 'idle' | 'done' | 'error'
    src?: string | null
    mediaType?: 'image' | 'video'
    assetId?: string
    uploaded?: boolean
    fileName?: string | null
    text?: string | null
    error?: string | null
  }
}
export interface PersistedCanvasEdgeDto {
  id: string
  source: string
  target: string
  sourceHandle?: string | null
  targetHandle?: string | null
}
export interface CanvasGraphDto {
  nodes: PersistedCanvasNodeDto[]
  edges: PersistedCanvasEdgeDto[]
  viewport: { x: number; y: number; zoom: number }
}
/** 列表页使用的展示数据，字段已在 API 层转换成前端命名。 */
export interface CanvasListItemDto {
  id: string;
  title: string;
  coverUrl: string | null;
  revision: number;
  updatedAt: string;
}

/** 后端使用页码分页，参数名与 Go handler 的 query tag 保持一致。 */
export interface CanvasListQueryDto {
  page?: number;
  page_size?: number;
  keyword?: string;
}

export interface CanvasListResponseDto {
  items: CanvasListItemDto[];
  total: number;
  page: number;
  pageSize: number;
  nextCursor: string | null;
}

/** 后端 GET /canvas 的原始响应结构。 */
export interface BackendCanvasListItemDto {
  id: number;
  title: string;
  revision: number;
  created_at: string;
  updated_at: string;
}

export interface BackendCanvasListResponseDto {
  list: BackendCanvasListItemDto[] | null;
  total: number;
  page: number;
  page_size: number;
}

/** 后端 POST /canvas 的原始响应结构。 */
export interface BackendCanvasProjectDto {
  id: number;
  title: string;
  payload_json: unknown;
  revision: number;
  created_at: string;
  updated_at: string;
}
export interface CanvasDetailDto {
  id: string; title: string; description: string | null; version: number
  graph: CanvasGraphDto; createdAt: string; updatedAt: string
}
export interface CreateCanvasDto { title?: string; graph?: CanvasGraphDto }
export interface UpdateCanvasDto {
  title?: string;
  graph?: CanvasGraphDto;
  revision: number;
}
export interface SaveCanvasGraphDto { baseVersion: number; graph: CanvasGraphDto }
export interface SaveCanvasGraphResponseDto { version: number; updatedAt: string }

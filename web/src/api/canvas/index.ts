import service from "@/utils/requests/service";
import type {
  CanvasDetailDto,
  CanvasGraphDto,
  CanvasListQueryDto,
  CanvasListResponseDto,
  BackendCanvasListResponseDto,
  BackendCanvasProjectDto,
  CreateCanvasDto,
  SaveCanvasGraphDto,
  SaveCanvasGraphResponseDto,
  UpdateCanvasDto,
} from "./type";

const EMPTY_CANVAS_GRAPH: CanvasGraphDto = {
  nodes: [],
  edges: [],
  viewport: { x: 0, y: 0, zoom: 1 },
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null;

/** 将后端保存的 payload_json 规范化成画布组件可直接使用的图谱。 */
const normalizeGraph = (value: unknown): CanvasGraphDto => {
  if (!isRecord(value)) return EMPTY_CANVAS_GRAPH;

  const viewport = isRecord(value.viewport) ? value.viewport : {};
  const x = typeof viewport.x === "number" ? viewport.x : 0;
  const y = typeof viewport.y === "number" ? viewport.y : 0;
  const zoom = typeof viewport.zoom === "number" && viewport.zoom > 0
    ? viewport.zoom
    : 1;

  return {
    nodes: Array.isArray(value.nodes) ? value.nodes as CanvasGraphDto["nodes"] : [],
    edges: Array.isArray(value.edges) ? value.edges as CanvasGraphDto["edges"] : [],
    viewport: { x, y, zoom },
  };
};

/** 将后端画布模型转换成页面和 Flow 组件使用的 DTO。 */
const mapCanvasProject = (canvas: BackendCanvasProjectDto): CanvasDetailDto => ({
  id: String(canvas.id),
  title: canvas.title,
  description: null,
  version: canvas.revision,
  graph: normalizeGraph(canvas.payload_json),
  createdAt: canvas.created_at,
  updatedAt: canvas.updated_at,
});

/**
 *  获取画布列表
 * @param params 
 * @returns 
 */
export const getCanvasList = async (
  params?: CanvasListQueryDto,
): Promise<CanvasListResponseDto> => {
  const response = await service.get<BackendCanvasListResponseDto>(
    "/canvas",
    params ? { ...params } : undefined,
  );
  const page = response.page || 1;
  const pageSize = response.page_size || 10;
  const total = response.total || 0;

  return {
    items: (response.list ?? []).map((item) => ({
      id: String(item.id),
      title: item.title,
      coverUrl: null,
      revision: item.revision,
      updatedAt: item.updated_at,
    })),
    total,
    page,
    pageSize,
    nextCursor: page * pageSize < total ? String(page + 1) : null,
  };
};

/**
 *  创建画布
 * @param data 
 * @returns 
 */
export const createCanvas = async (data: CreateCanvasDto = {}) => {
  const payload = {
    title: data.title?.trim() || "未命名画布",
    payload_json: data.graph ?? EMPTY_CANVAS_GRAPH,
  };
  const canvas = await service.post<BackendCanvasProjectDto>("/canvas", payload);
  return mapCanvasProject(canvas);
};

/**
 * 获取画布详情
 * @param id 
 * @returns 
 */
export const getCanvas = (id: string) =>
  service
    .get<BackendCanvasProjectDto>(`/canvas/${id}`)
    .then(mapCanvasProject);

/**
 *  更新画布信息 
 * @param id 
 * @param data 
 * @returns 
 */
export const updateCanvas = (id: string, data: UpdateCanvasDto) =>
  service
    .put<BackendCanvasProjectDto>(`/canvas/${id}`, {
      revision: data.revision,
      ...(data.title !== undefined ? { title: data.title } : {}),
      ...(data.graph ? { payload_json: data.graph } : {}),
    })
    .then(mapCanvasProject);

/**
 * 保存画布图谱
 * @param id 
 * @param data 
 * @returns 
 */
export const saveCanvasGraph = (id: string, data: SaveCanvasGraphDto) =>
  service
    .put<BackendCanvasProjectDto>(
      `/canvas/${id}`,
      {
        revision: data.baseVersion,
        payload_json: data.graph,
      },
      { silent: true },
    )
    .then((canvas): SaveCanvasGraphResponseDto => ({
      version: canvas.revision,
      updatedAt: canvas.updated_at,
    }));


/**
 * 删除画布
 * @param id 
 * @returns 
 */
export const deleteCanvas = (id: string) =>
  service.delete<void>(`/canvas/${id}`);

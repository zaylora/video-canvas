import service from "@/utils/requests/service";
import type { RequestExtraConfig } from "@/utils/requests/service";
import type {
  BackendCanvasExportDto,
  BackendCanvasImportDto,
  CanvasDetailDto,
  CanvasExportResultDto,
  CanvasImportResultDto,
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
  const zoom = typeof viewport.zoom === "number" && viewport.zoom > 0 ? viewport.zoom : 1;

  return {
    nodes: Array.isArray(value.nodes) ? (value.nodes as CanvasGraphDto["nodes"]) : [],
    edges: Array.isArray(value.edges) ? (value.edges as CanvasGraphDto["edges"]) : [],
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
 * 获取画布列表
 * @param params 分页和查询参数
 * @returns 画布列表及分页信息
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
      coverUrl: item.cover_url || null,
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
 * 创建画布
 * @param data 画布标题和初始图谱
 * @returns 新建的画布详情
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
 * @param id 画布 ID
 * @returns 画布详情
 */
export const getCanvas = (id: string) =>
  service.get<BackendCanvasProjectDto>(`/canvas/${id}`).then(mapCanvasProject);

/**
 * 更新画布信息
 * @param id 画布 ID
 * @param data 待更新的标题、图谱及当前 revision
 * @param config 额外的请求配置，如 silent
 * @returns 更新后的画布详情
 */
export const updateCanvas = (id: string, data: UpdateCanvasDto, config?: RequestExtraConfig) =>
  service
    .put<BackendCanvasProjectDto>(
      `/canvas/${id}`,
      {
        revision: data.revision,
        ...(data.title !== undefined ? { title: data.title } : {}),
        ...(data.graph ? { payload_json: data.graph } : {}),
      },
      config,
    )
    .then(mapCanvasProject);

/**
 * 保存画布图谱
 * @param id 画布 ID
 * @param data 图谱数据及基准版本
 * @param config 额外的请求配置，如 silent、页面关闭时的 keepalive
 * @returns 保存后的版本号和更新时间
 */
export const saveCanvasGraph = (
  id: string,
  data: SaveCanvasGraphDto,
  config?: RequestExtraConfig,
) =>
  service
    .put<BackendCanvasProjectDto>(
      `/canvas/${id}`,
      {
        revision: data.baseVersion,
        payload_json: data.graph,
      },
      config,
    )
    .then((canvas): SaveCanvasGraphResponseDto => ({
      version: canvas.revision,
      updatedAt: canvas.updated_at,
    }));

/**
 * 删除画布
 * @param id 画布 ID
 */
export const deleteCanvas = (id: string) => service.delete<void>(`/canvas/${id}`);

/**
 * 导出 / 导入画布的请求超时（毫秒）。
 * 为什么比默认的 2 分钟长：导入要逐个复制最多 1 GB 的素材，导出要为老素材补算哈希；
 * 与后端为这两个接口放宽的 5 分钟读写超时一致。
 */
const CANVAS_TRANSFER_TIMEOUT = 5 * 60 * 1000;

/**
 * 导出画布为文档
 * @param id 画布 ID
 * @param signal 取消信号；取消后请求层原样抛出取消错误、不弹提示
 * @returns 导出文档，以及因找不到文件被跳过的素材数
 */
export const exportCanvas = (id: string, signal?: AbortSignal): Promise<CanvasExportResultDto> =>
  service
    .get<BackendCanvasExportDto>(`/canvas/${id}/export`, undefined, {
      timeout: CANVAS_TRANSFER_TIMEOUT,
      signal,
    })
    .then((data) => ({
      document: data.document,
      skippedAssets: data.report?.skipped_assets ?? 0,
    }));

/**
 * 把导出文档导入为一张新画布
 * @param document 导出文档，前端不解析，原样交给后端校验
 * @param signal 取消信号；取消后请求层原样抛出取消错误、不弹提示
 * @returns 新画布和导入报告
 */
export const importCanvas = (
  document: unknown,
  signal?: AbortSignal,
): Promise<CanvasImportResultDto> =>
  service
    .post<BackendCanvasImportDto>(
      "/canvas/import",
      { document },
      { timeout: CANVAS_TRANSFER_TIMEOUT, signal },
    )
    .then(({ canvas, report }) => ({
      canvas,
      report: {
        assets: {
          total: report.assets.total,
          copied: report.assets.copied,
          missing: (report.assets.missing ?? []).map((item) => ({
            nodeId: item.node_id,
            fileName: item.file_name,
          })),
        },
        characters: {
          created: report.characters.created,
          renamed: report.characters.renamed ?? [],
          invalid: (report.characters.invalid ?? []).map((item) => ({
            nodeId: item.node_id,
            name: item.name,
          })),
        },
        edgesDropped: report.edges_dropped,
      },
    }));

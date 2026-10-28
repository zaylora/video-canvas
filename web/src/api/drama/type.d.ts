import type { AssetDto, BackendAssetDto } from "@/api/asset/type";
import type { TaskStatus, TaskView } from "@/api/generation-task/type";

/**
 * 短剧工作室接口类型（契约 docs/design/短剧工作室/接口契约.md）。
 * Backend* 是后端原样的 snake_case 响应，其余是前端 camelCase 结构。
 */

/** 短剧整体状态 */
export type DramaStatus = "analyzing" | "outlined" | "scripting" | "ready" | "failed";

/** 流水线步骤 */
export type DramaStep =
  | "analyze"
  | "outline"
  | "scripts"
  | "breakdown"
  | "asset_prompts"
  | "storyboard";

/** 任务状态 */
export type DramaJobStatus = "queued" | "running" | "succeeded" | "failed" | "canceled";

/** 一集剧本的状态 */
export type ScriptStatus = "empty" | "generating" | "ready" | "failed";

/** 一集分镜脚本的状态（契约 §9.1） */
export type StoryboardStatus = "empty" | "generating" | "ready" | "failed";

/** 成片比例，空串表示未定 */
export type DramaRatio = "9:16" | "16:9" | "21:9" | "3:4" | "4:3" | "1:1" | "";

/** 原文类型，研读前为空串 */
export type DramaSourceKind = "script" | "novel" | "";

export interface BackendDramaSummary {
  id: string;
  title: string;
  status: DramaStatus;
  episode_count: number;
  locked_count: number;
  cover_url: string;
  updated_at: string;
}

export interface BackendEpisodeSummary {
  no: number;
  title: string;
  synopsis: string;
  script_status: ScriptStatus;
  locked: boolean;
  revision: number;
  updated_at: string;
  /** 二期字段（契约 §9.1）；后端还没带时按空处理 */
  storyboard_status?: StoryboardStatus;
  segment_count?: number;
  segment_seconds?: number;
  done_segment_count?: number;
}

export interface BackendEpisodeView extends BackendEpisodeSummary {
  script_md: string;
}

export interface BackendJobView {
  id: string;
  step: DramaStep;
  /** analyze / outline 为空数组 */
  episodes: number[] | null;
  status: DramaJobStatus;
  progress: number;
  credits: number;
  error_code: number;
  error_message: string;
  created_at: string;
  finished_at: string | null;
}

export interface BackendDramaView {
  id: string;
  title: string;
  canvas_id: string;
  status: DramaStatus;
  source_kind: DramaSourceKind;
  genre: string;
  style_key: string;
  style_custom: string;
  ratio: DramaRatio;
  target_episodes: number;
  episode_seconds: number;
  text_model: string;
  outline_md: string;
  characters_md: string;
  revision: number;
  episodes: BackendEpisodeSummary[] | null;
  active_jobs: BackendJobView[] | null;
  created_at: string;
  updated_at: string;
}

export interface BackendDramaStyles {
  genres: { key: string; name: string }[] | null;
  styles: { key: string; name: string; prompt: string }[] | null;
}

/** 列表项 */
export interface DramaSummary {
  id: string;
  title: string;
  status: DramaStatus;
  episodeCount: number;
  /** 已拆解（锁定）的集数 */
  lockedCount: number;
  /** 一期可为空，前端用渐变占位 */
  coverUrl: string | null;
  updatedAt: string;
}

export interface EpisodeSummary {
  no: number;
  title: string;
  synopsis: string;
  scriptStatus: ScriptStatus;
  /** 已拆解资产，剧本只读 */
  locked: boolean;
  revision: number;
  updatedAt: string;
  /** 分镜脚本状态 */
  storyboardStatus: StoryboardStatus;
  /** 片段数 */
  segmentCount: number;
  /** 各片段时长之和（秒） */
  segmentSeconds: number;
  /** 有「采用」视频的片段数 */
  doneSegmentCount: number;
}

export interface Episode extends EpisodeSummary {
  scriptMd: string;
}

export interface DramaJob {
  id: string;
  step: DramaStep;
  /** 本次范围；analyze / outline 为空 */
  episodes: number[];
  status: DramaJobStatus;
  /** 0–100 */
  progress: number;
  /** 实际扣费，结束后才有值 */
  credits: number;
  errorCode: number;
  errorMessage: string;
  createdAt: string;
  finishedAt: string | null;
}

export interface Drama {
  id: string;
  title: string;
  /** 短剧专属画布 ID，用现有画布接口读写 */
  canvasId: string;
  status: DramaStatus;
  sourceKind: DramaSourceKind;
  genre: string;
  styleKey: string;
  styleCustom: string;
  ratio: DramaRatio;
  /** 0 = 自动 */
  targetEpisodes: number;
  episodeSeconds: number;
  /** 空串 = 跟随后台「文本」类别第一个启用模型 */
  textModel: string;
  outlineMd: string;
  charactersMd: string;
  revision: number;
  episodes: EpisodeSummary[];
  activeJobs: DramaJob[];
  createdAt: string;
  updatedAt: string;
}

export interface DramaStyles {
  genres: { key: string; name: string }[];
  styles: { key: string; name: string; prompt: string }[];
}

/** 分页列表（沿用 OKPage） */
export interface DramaPage {
  items: DramaSummary[];
  total: number;
  page: number;
  pageSize: number;
}

/** 新建短剧的表单：file 与 text 二选一 */
export interface CreateDramaInput {
  file?: File;
  text?: string;
  title?: string;
  genre?: string;
  styleKey: string;
  styleCustom?: string;
  ratio?: DramaRatio;
  /** 0 = 自动 */
  targetEpisodes?: number;
  episodeSeconds?: number;
}

/** PATCH /dramas/:id 可改字段 */
export interface UpdateDramaInput {
  revision: number;
  title?: string;
  genre?: string;
  styleKey?: string;
  styleCustom?: string;
  ratio?: DramaRatio;
  textModel?: string;
  outlineMd?: string;
  charactersMd?: string;
}

/** PATCH /dramas/:id/episodes/:no 可改字段 */
export interface UpdateEpisodeInput {
  revision: number;
  title?: string;
  synopsis?: string;
  scriptMd?: string;
}

/** 提交 / 估价任务的请求 */
export interface DramaJobInput {
  step: DramaStep;
  episodes: number[];
  /** breakdown 且项目未定比例时必填 */
  ratio?: DramaRatio;
  /** storyboard：所选视频模型的单段时长上限（秒），缺省 15，范围 4–60 */
  maxSeconds?: number;
}

/** WebSocket drama.job 事件载荷 */
export interface DramaJobEvent {
  dramaId: string;
  job: DramaJob;
}

/** 片段状态（由最近一条 take 推出） */
export type SegmentStatus = "idle" | "running" | "done" | "error";

export interface BackendShot {
  seconds: number;
  shot_size: string;
  camera: string;
  action: string;
  dialogue: string;
}

export interface BackendSegmentTake {
  task_id: string;
  status: TaskStatus;
  asset: BackendAssetDto | null;
  error_message: string;
  created_at: string;
}

export interface BackendSegmentView {
  id: string;
  episode_no: number;
  seq: number;
  seconds: number;
  prompt: string;
  shots: BackendShot[] | null;
  keyframe: BackendAssetDto | null;
  takes: BackendSegmentTake[] | null;
  chosen_task_id: string;
  status: SegmentStatus;
  revision: number;
  updated_at: string;
}

/** 分镜表的一行（只读，供参考） */
export interface SegmentShot {
  seconds: number;
  shotSize: string;
  camera: string;
  action: string;
  dialogue: string;
}

/** 片段的一次视频生成结果 */
export interface SegmentTake {
  taskId: string;
  status: TaskStatus;
  asset: AssetDto | null;
  errorMessage: string;
  createdAt: string;
}

export interface Segment {
  id: string;
  episodeNo: number;
  /** 从 0 开始，按它排序 */
  seq: number;
  seconds: number;
  /** `@[名](短剧画布节点 id)` 形式的提示词 */
  prompt: string;
  shots: SegmentShot[];
  /** 首帧图；有则生成视频走 i2v */
  keyframe: AssetDto | null;
  /** 按创建时间正序 */
  takes: SegmentTake[];
  /** 采用的那一条；空串 = 没有 */
  chosenTaskId: string;
  status: SegmentStatus;
  revision: number;
  updatedAt: string;
}

/** PATCH /drama-segments/:sid 可改字段 */
export interface UpdateSegmentInput {
  revision: number;
  prompt?: string;
  seconds?: number;
  chosenTaskId?: string;
}

/** POST /dramas/:id/episodes/:no/segments 请求体 */
export interface AddSegmentInput {
  prompt?: string;
  seconds?: number;
  afterSeq?: number;
}

/** generate / keyframe 请求体：input 与 POST /generation-tasks 的 input 同构 */
export interface SegmentGenerateBody {
  model_id: string;
  input: Record<string, unknown>;
}

export type SegmentGenerateResult = { task: TaskView };

/** WebSocket drama.segment 事件载荷（原样 snake_case） */
export interface DramaSegmentEvent {
  drama_id: string;
  episode_no: number;
  segment: BackendSegmentView;
}

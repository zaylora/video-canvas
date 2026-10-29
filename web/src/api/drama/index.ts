import { mapAsset } from "@/api/asset";
import service from "@/utils/requests/service";

import type {
  BackendDramaStyles,
  BackendDramaSummary,
  BackendDramaView,
  BackendEpisodeSummary,
  BackendEpisodeView,
  BackendJobView,
  BackendSegmentView,
  AddSegmentInput,
  CreateDramaInput,
  Drama,
  DramaJob,
  DramaJobInput,
  DramaPage,
  DramaStyles,
  DramaSummary,
  Episode,
  EpisodeSummary,
  Segment,
  SegmentGenerateBody,
  SegmentGenerateResult,
  UpdateDramaInput,
  UpdateEpisodeInput,
  UpdateSegmentInput,
} from "./type";

export const mapDramaSummary = (raw: BackendDramaSummary): DramaSummary => ({
  id: raw.id,
  title: raw.title,
  status: raw.status,
  episodeCount: raw.episode_count,
  lockedCount: raw.locked_count,
  coverUrl: raw.cover_url ? raw.cover_url : null,
  updatedAt: raw.updated_at,
});

export const mapEpisodeSummary = (raw: BackendEpisodeSummary): EpisodeSummary => ({
  no: raw.no,
  title: raw.title,
  synopsis: raw.synopsis ?? "",
  scriptStatus: raw.script_status,
  locked: raw.locked,
  revision: raw.revision,
  updatedAt: raw.updated_at,
  storyboardStatus: raw.storyboard_status ?? "empty",
  segmentCount: raw.segment_count ?? 0,
  segmentSeconds: raw.segment_seconds ?? 0,
  doneSegmentCount: raw.done_segment_count ?? 0,
  final: raw.final ? mapAsset(raw.final) : null,
  finalStale: raw.final_stale ?? false,
});

export const mapEpisode = (raw: BackendEpisodeView): Episode => ({
  ...mapEpisodeSummary(raw),
  scriptMd: raw.script_md ?? "",
});

export const mapJob = (raw: BackendJobView): DramaJob => ({
  id: raw.id,
  step: raw.step,
  episodes: raw.episodes ?? [],
  status: raw.status,
  progress: raw.progress,
  credits: raw.credits,
  errorCode: raw.error_code,
  errorMessage: raw.error_message ?? "",
  createdAt: raw.created_at,
  finishedAt: raw.finished_at ?? null,
  result: raw.result
    ? { asset: mapAsset(raw.result.asset), skippedSeqs: raw.result.skipped_seqs ?? [] }
    : null,
});

export const mapDrama = (raw: BackendDramaView): Drama => ({
  id: raw.id,
  title: raw.title,
  canvasId: raw.canvas_id,
  status: raw.status,
  sourceKind: raw.source_kind,
  genre: raw.genre ?? "",
  styleKey: raw.style_key,
  styleCustom: raw.style_custom ?? "",
  ratio: raw.ratio ?? "",
  targetEpisodes: raw.target_episodes,
  episodeSeconds: raw.episode_seconds,
  textModel: raw.text_model ?? "",
  outlineMd: raw.outline_md ?? "",
  charactersMd: raw.characters_md ?? "",
  revision: raw.revision,
  episodes: (raw.episodes ?? []).map(mapEpisodeSummary),
  activeJobs: (raw.active_jobs ?? []).map(mapJob),
  createdAt: raw.created_at,
  updatedAt: raw.updated_at,
});

export const mapStyles = (raw: BackendDramaStyles): DramaStyles => ({
  genres: raw.genres ?? [],
  styles: raw.styles ?? [],
});

/** 片段（契约 §9.2）：takes 可能为 null，task_id 兼容数字 */
export const mapSegment = (raw: BackendSegmentView): Segment => ({
  id: String(raw.id),
  episodeNo: raw.episode_no,
  seq: raw.seq,
  seconds: raw.seconds,
  prompt: raw.prompt ?? "",
  takes: (raw.takes ?? []).map((take) => ({
    taskId: String(take.task_id),
    status: take.status,
    asset: take.asset ? mapAsset(take.asset) : null,
    errorMessage: take.error_message ?? "",
    createdAt: take.created_at,
  })),
  chosenTaskId: raw.chosen_task_id ? String(raw.chosen_task_id) : "",
  status: raw.status,
  revision: raw.revision,
  updatedAt: raw.updated_at,
});

/** 去掉值为 undefined 的键，PATCH 只发要改的字段 */
const compact = <T extends Record<string, unknown>>(body: T): Partial<T> =>
  Object.fromEntries(Object.entries(body).filter(([, value]) => value !== undefined)) as Partial<T>;

/** 新建短剧的 multipart 表单：file 与 text 二选一，空字段不发 */
export function buildCreateForm(input: CreateDramaInput): FormData {
  const form = new FormData();
  if (input.file) form.append("file", input.file);
  else form.append("text", input.text ?? "");
  const fields: Record<string, string | undefined> = {
    title: input.title?.trim() || undefined,
    genre: input.genre || undefined,
    style_key: input.styleKey,
    style_custom: input.styleCustom || undefined,
    ratio: input.ratio || undefined,
    target_episodes: input.targetEpisodes ? String(input.targetEpisodes) : undefined,
    episode_seconds: input.episodeSeconds ? String(input.episodeSeconds) : undefined,
  };
  for (const [key, value] of Object.entries(fields)) {
    if (value !== undefined) form.append(key, value);
  }
  return form;
}

/** PATCH /dramas/:id 请求体（snake_case，只含传了的字段） */
export const toUpdateDramaBody = (input: UpdateDramaInput) =>
  compact({
    revision: input.revision,
    title: input.title,
    genre: input.genre,
    style_key: input.styleKey,
    style_custom: input.styleCustom,
    ratio: input.ratio,
    text_model: input.textModel,
    outline_md: input.outlineMd,
    characters_md: input.charactersMd,
  });

/** PATCH /dramas/:id/episodes/:no 请求体 */
export const toUpdateEpisodeBody = (input: UpdateEpisodeInput) =>
  compact({
    revision: input.revision,
    title: input.title,
    synopsis: input.synopsis,
    script_md: input.scriptMd,
  });

/** 估价 / 提交任务的请求体 */
export const toJobBody = (input: DramaJobInput) =>
  compact({
    step: input.step,
    episodes: input.episodes,
    ratio: input.ratio || undefined,
    max_seconds: input.step === "storyboard" ? input.maxSeconds : undefined,
  });

/** PATCH /drama-segments/:sid 请求体 */
export const toSegmentUpdateBody = (input: UpdateSegmentInput) =>
  compact({
    revision: input.revision,
    prompt: input.prompt,
    seconds: input.seconds,
    chosen_task_id: input.chosenTaskId,
  });

/** 获取题材与画风清单 */
export const getDramaStyles = async (): Promise<DramaStyles> =>
  mapStyles(await service.get<BackendDramaStyles>("/drama-styles"));

/**
 * 我的短剧列表，updated_at 倒序
 * @param params 关键字与分页
 */
export const listDramas = async (
  params: { keyword?: string; page?: number; pageSize?: number } = {},
): Promise<DramaPage> => {
  const res = await service.get<{
    list: BackendDramaSummary[] | null;
    total: number;
    page: number;
    page_size: number;
  }>("/dramas", {
    keyword: params.keyword?.trim() || undefined,
    page: params.page,
    page_size: params.pageSize,
  });
  return {
    items: (res.list ?? []).map(mapDramaSummary),
    total: res.total ?? 0,
    page: res.page ?? 1,
    pageSize: res.page_size ?? params.pageSize ?? 20,
  };
};

/**
 * 新建短剧并排队研读；67002 / 67003 由调用方就地展示，不弹全局提示
 * @param input 原文（文件或文本）与创作设置
 */
export const createDrama = async (input: CreateDramaInput): Promise<Drama> =>
  mapDrama(
    await service.upload<BackendDramaView>("/dramas", buildCreateForm(input), { silent: true }),
  );

/** 短剧详情 */
export const getDrama = async (id: string, silent = false): Promise<Drama> =>
  mapDrama(await service.get<BackendDramaView>(`/dramas/${id}`, undefined, { silent }));

/** 改短剧；revision 冲突（409）由调用方处理，不弹全局提示 */
export const updateDrama = async (id: string, input: UpdateDramaInput): Promise<Drama> =>
  mapDrama(
    await service.patch<BackendDramaView>(`/dramas/${id}`, toUpdateDramaBody(input), {
      silent: true,
    }),
  );

/** 删除短剧（连带删画布与它的角色卡） */
export const deleteDrama = async (id: string): Promise<void> => {
  await service.delete(`/dramas/${id}`);
};

/** 读单集（含剧本正文） */
export const getEpisode = async (id: string, no: number): Promise<Episode> =>
  mapEpisode(await service.get<BackendEpisodeView>(`/dramas/${id}/episodes/${no}`));

/** 在末尾追加一集 */
export const addEpisode = async (
  id: string,
  body: { title?: string; synopsis?: string } = {},
): Promise<Episode> =>
  mapEpisode(await service.post<BackendEpisodeView>(`/dramas/${id}/episodes`, body));

/** 改单集；锁定集改正文 67005、revision 冲突 409 都由调用方处理 */
export const updateEpisode = async (
  id: string,
  no: number,
  input: UpdateEpisodeInput,
): Promise<Episode> =>
  mapEpisode(
    await service.patch<BackendEpisodeView>(
      `/dramas/${id}/episodes/${no}`,
      toUpdateEpisodeBody(input),
      { silent: true },
    ),
  );

/** 估算一次任务要多少积分 */
export const estimateJob = async (id: string, input: DramaJobInput): Promise<number> => {
  const res = await service.post<{ credits: number }>(`/dramas/${id}/estimate`, toJobBody(input), {
    silent: true,
  });
  return res.credits;
};

/** 提交任务（202）；业务错误由调用方展示 */
export const submitJob = async (id: string, input: DramaJobInput): Promise<DramaJob> =>
  mapJob(
    await service.post<BackendJobView>(`/dramas/${id}/jobs`, toJobBody(input), { silent: true }),
  );

/** 进行中（queued / running）的任务，挂载与重连时对账用 */
export const listActiveJobs = async (id: string): Promise<DramaJob[]> => {
  const res = await service.get<{ items: BackendJobView[] | null }>(
    `/dramas/${id}/jobs`,
    { status: "active" },
    { silent: true },
  );
  return (res.items ?? []).map(mapJob);
};

/** 取消任务 */
export const cancelJob = async (id: string, jobId: string): Promise<DramaJob> =>
  mapJob(await service.post<BackendJobView>(`/dramas/${id}/jobs/${jobId}/cancel`));

/** 一集的片段，按 seq 排序 */
export const listSegments = async (id: string, no: number, silent = true): Promise<Segment[]> => {
  const res = await service.get<{ items: BackendSegmentView[] | null }>(
    `/dramas/${id}/episodes/${no}/segments`,
    undefined,
    { silent },
  );
  return (res.items ?? []).map(mapSegment);
};

/** 新增片段（不传 afterSeq 追加到末尾） */
export const addSegment = async (
  id: string,
  no: number,
  body: AddSegmentInput = {},
): Promise<Segment> =>
  mapSegment(
    await service.post<BackendSegmentView>(`/dramas/${id}/episodes/${no}/segments`, {
      prompt: body.prompt,
      seconds: body.seconds,
      after_seq: body.afterSeq,
    }),
  );

/** 改片段；revision 冲突（409）、67011 由调用方处理，不弹全局提示 */
export const updateSegment = async (sid: string, input: UpdateSegmentInput): Promise<Segment> =>
  mapSegment(
    await service.patch<BackendSegmentView>(`/drama-segments/${sid}`, toSegmentUpdateBody(input), {
      silent: true,
    }),
  );

/** 删除片段（不删生成任务和素材） */
export const deleteSegment = async (sid: string): Promise<void> => {
  await service.delete(`/drama-segments/${sid}`, { silent: true });
};

/** 重排：ids 必须恰好是该集全部片段（否则 67012），返回新顺序的片段 */
export const reorderSegments = async (
  id: string,
  no: number,
  ids: string[],
): Promise<Segment[]> => {
  const res = await service.put<{ items: BackendSegmentView[] | null }>(
    `/dramas/${id}/episodes/${no}/segments/order`,
    { ids },
    { silent: true },
  );
  return (res.items ?? []).map(mapSegment);
};

/** 提交片段的视频生成（202）；统一经 utils/tasks/gateway.ts 调用，那里管幂等键与重试 */
export const generateSegment = (
  sid: string,
  body: SegmentGenerateBody,
  idempotencyKey: string,
): Promise<SegmentGenerateResult> =>
  service.post<SegmentGenerateResult>(`/drama-segments/${sid}/generate`, body, {
    headers: { "Idempotency-Key": idempotencyKey },
  });

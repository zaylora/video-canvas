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
  AdaptFormat,
  AdaptSegmentInput,
  AddSegmentInput,
  BackendSourcePreflight,
  CreateDramaInput,
  Drama,
  DramaJob,
  DramaJobInput,
  DramaPage,
  DramaSkill,
  DramaStyles,
  DramaSummary,
  DramaUnit,
  Episode,
  EpisodeSummary,
  JobEstimate,
  MyPrompt,
  MyPromptInput,
  PreflightInput,
  PromptSettings,
  PromptStep,
  PromptTemplate,
  Segment,
  SegmentGenerateBody,
  SegmentGenerateResult,
  SourceEstimate,
  SourceEstimateInput,
  SourcePreflight,
  UpdateDramaInput,
  UpdateEpisodeInput,
  UpdateSegmentInput,
} from "./type";

export const mapDramaSummary = (raw: BackendDramaSummary): DramaSummary => ({
  id: raw.id,
  title: raw.title,
  status: raw.status,
  phase: raw.phase ?? "done",
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
  hook: raw.hook ?? "",
  cliffhanger: raw.cliffhanger ?? "",
  unitFrom: raw.unit_from ? raw.unit_from : null,
  unitTo: raw.unit_to ? raw.unit_to : null,
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
  lane: raw.lane ?? "user",
  batchFrom: raw.batch_from ? raw.batch_from : null,
  batchTo: raw.batch_to ? raw.batch_to : null,
});

/** 没有提示词设置（老项目）时四个环节都是内置 */
const BUILTIN_SETTING = { source: "builtin", label: "内置" } as const;

/** 四个环节的提示词设置：缺的环节补成内置 */
export const mapPromptSettings = (raw: Partial<PromptSettings> | undefined): PromptSettings => ({
  adapt: raw?.adapt ?? BUILTIN_SETTING,
  assets: raw?.assets ?? BUILTIN_SETTING,
  asset_prompts: raw?.asset_prompts ?? BUILTIN_SETTING,
  video: raw?.video ?? BUILTIN_SETTING,
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
  videoModel: raw.video_model ?? "",
  imageModels: {
    character: raw.image_models?.character ?? "",
    scene: raw.image_models?.scene ?? "",
    prop: raw.image_models?.prop ?? "",
  },
  outlineMd: raw.outline_md ?? "",
  charactersMd: raw.characters_md ?? "",
  revision: raw.revision,
  episodes: (raw.episodes ?? []).map(mapEpisodeSummary),
  activeJobs: (raw.active_jobs ?? []).map(mapJob),
  createdAt: raw.created_at,
  updatedAt: raw.updated_at,
  sourceChars: raw.source_chars ?? 0,
  unitsTotal: raw.units_total ?? 0,
  unitKind: raw.unit_kind ?? "episode",
  phase: raw.phase ?? "done",
  planCursor: raw.plan_cursor ?? 0,
  rangeEnd: raw.range_end ?? 0,
  pauseReason: raw.pause_reason ?? "",
  pauseCode: raw.pause_code ?? "",
  promptSettings: mapPromptSettings(raw.prompt_settings),
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
  adaptations: mapAdaptations(raw.adaptations),
});

/** 转写稿 snake→camel；缺省为空对象 */
const mapAdaptations = (raw: BackendSegmentView["adaptations"]): Segment["adaptations"] => {
  const out: Segment["adaptations"] = {};
  for (const [format, item] of Object.entries(raw ?? {})) {
    if (!item) continue;
    out[format as AdaptFormat] = {
      prompt: item.prompt ?? "",
      sourceHash: item.source_hash ?? "",
      edited: !!item.edited,
      updatedAt: item.updated_at ?? "",
    };
  }
  return out;
};

/** 去掉值为 undefined 的键，PATCH 只发要改的字段 */
const compact = <T extends Record<string, unknown>>(body: T): Partial<T> =>
  Object.fromEntries(Object.entries(body).filter(([, value]) => value !== undefined)) as Partial<T>;

/** 图片模型三项里只发有值的；一个都没有整个字段不发 */
const imageModelsBody = (models: CreateDramaInput["imageModels"]) => {
  const picked = Object.fromEntries(Object.entries(models ?? {}).filter(([, key]) => !!key));
  return Object.keys(picked).length > 0 ? picked : undefined;
};

/**
 * POST /dramas 请求体（契约 §2）：只带草稿 token 与开拍设置；空的可选字段不发，
 * 不再有 target_episodes。
 */
export const toCreateDramaBody = (input: CreateDramaInput) =>
  compact({
    source_token: input.sourceToken,
    source_kind: input.sourceKind,
    range_end: input.rangeEnd,
    title: input.title?.trim() || undefined,
    ratio: input.ratio,
    genre: input.genre || undefined,
    style_key: input.styleKey,
    style_custom: input.styleCustom || undefined,
    episode_seconds: input.episodeSeconds,
    text_model: input.textModel ?? "",
    video_model: input.videoModel || undefined,
    image_models: imageModelsBody(input.imageModels),
    prompts: input.prompts,
  });

/** 预检的 multipart 表单：file 与 text 二选一，空的可选字段不发 */
export function buildPreflightForm(input: PreflightInput): FormData {
  const form = new FormData();
  if (input.file) form.append("file", input.file);
  else form.append("text", input.text ?? "");
  if (input.sourceKind) form.append("source_kind", input.sourceKind);
  if (input.textModel) form.append("text_model", input.textModel);
  if (input.episodeSeconds) form.append("episode_seconds", String(input.episodeSeconds));
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
    prompts: input.prompts,
  });

/** PATCH /dramas/:id/episodes/:no 请求体 */
export const toUpdateEpisodeBody = (input: UpdateEpisodeInput) =>
  compact({
    revision: input.revision,
    title: input.title,
    synopsis: input.synopsis,
    script_md: input.scriptMd,
    hook: input.hook,
    cliffhanger: input.cliffhanger,
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
    adaptation: input.adaptation,
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

/** 预检返回的预估：snake_case 转 camelCase */
const mapEstimate = (raw: {
  episodes_min: number;
  episodes_max: number;
  episodes_exact: boolean;
  credits: number;
  minutes: number;
}): SourceEstimate => ({
  episodesMin: raw.episodes_min,
  episodesMax: raw.episodes_max,
  episodesExact: raw.episodes_exact,
  credits: raw.credits,
  minutes: raw.minutes,
});

/** 预检结果：units / issues 可能是 null，统一补成空数组 */
const mapPreflight = (raw: BackendSourcePreflight): SourcePreflight => ({
  sourceToken: raw.source_token,
  filename: raw.filename,
  chars: raw.chars,
  kindGuess: raw.kind_guess,
  kindSignals: raw.kind_signals ?? "",
  unitKind: raw.unit_kind,
  units: raw.units ?? [],
  unitsTotal: raw.units_total,
  issues: raw.issues ?? [],
  estimate: mapEstimate(raw.estimate),
});

/**
 * 预检原文：解析、去噪、切单元、识别类型并估价，不调用模型；67002 / 67003 由调用方就地展示，不弹全局提示。
 * @param input 原文（文件或粘贴文本）与可选的类型、模型、单集时长
 * @param onProgress 上传进度（0 到 1），大文件时用来给按钮画进度环
 * @returns 预检结果（含草稿 token）
 */
export const preflightSource = async (
  input: PreflightInput,
  onProgress?: (ratio: number) => void,
): Promise<SourcePreflight> =>
  mapPreflight(
    await service.upload<BackendSourcePreflight>(
      "/drama-sources/preflight",
      buildPreflightForm(input),
      {
        silent: true,
        timeout: 5 * 60 * 1000,
        onUploadProgress: (event) => {
          if (event.total) onProgress?.(event.loaded / event.total);
        },
      },
    ),
  );

/**
 * 草稿预估：改范围 / 时长 / 模型 / 类型时重算自动部分（研读 + 规划）的集数、积分、耗时；草稿过期为 67014
 * @param token 草稿 token
 * @param input 用户选的类型、范围、单集时长、模型
 * @returns 预估
 */
export const estimateSource = async (
  token: string,
  input: SourceEstimateInput,
): Promise<SourceEstimate> =>
  mapEstimate(
    await service.post(
      `/drama-sources/${token}/estimate`,
      {
        source_kind: input.sourceKind,
        range_end: input.rangeEnd,
        episode_seconds: input.episodeSeconds,
        text_model: input.textModel,
      },
      { silent: true },
    ),
  );

/**
 * 按开拍设置新建短剧并自动入队研读；67014 / 67016 / 67018 / 67022 等由调用方就地展示，不弹全局提示
 * @param input 草稿 token 与开拍设置
 * @returns 新建的短剧
 */
export const createDrama = async (input: CreateDramaInput): Promise<Drama> =>
  mapDrama(
    await service.post<BackendDramaView>("/dramas", toCreateDramaBody(input), { silent: true }),
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

/** 删一集：后面的集号往前补位，资产的出现集数由后端一起改；有任务在跑 67004，由调用方提示 */
export const deleteEpisode = async (id: string, no: number): Promise<void> => {
  await service.delete(`/dramas/${id}/episodes/${no}`, { silent: true });
};

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

/**
 * 估算一次任务：积分，以及 scripts 时「上一集没有剧本」的衔接缺口
 * @param id 短剧 id
 * @param input 任务步骤与选中的集
 * @returns 积分与缺口集号
 */
export const estimateJobDetail = async (id: string, input: DramaJobInput): Promise<JobEstimate> => {
  const res = await service.post<{ credits: number; gap_episodes?: number[] | null }>(
    `/dramas/${id}/estimate`,
    toJobBody(input),
    { silent: true },
  );
  return { credits: res.credits, gapEpisodes: res.gap_episodes ?? [] };
};

/** 估算一次任务要多少积分 */
export const estimateJob = async (id: string, input: DramaJobInput): Promise<number> =>
  (await estimateJobDetail(id, input)).credits;

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

/**
 * 把原稿转写成某个平台格式（同步，可能几十秒）；返回带新转写稿的片段。
 * 静默：失败原因由片场就地展示
 */
export const adaptSegment = async (sid: string, input: AdaptSegmentInput): Promise<Segment> =>
  mapSegment(
    await service.post<BackendSegmentView>(`/drama-segments/${sid}/adapt`, input, {
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

/** 后端的「我的提示词」项 */
type BackendMyPrompt = Omit<MyPrompt, "updatedAt"> & { updated_at: string };

const mapMyPrompt = (raw: BackendMyPrompt): MyPrompt => ({
  id: raw.id,
  step: raw.step,
  name: raw.name,
  bodies: raw.bodies ?? {},
  revision: raw.revision,
  updatedAt: raw.updated_at,
});

/** 内置提示词正文（只含可替换的正文，不含输入变量和输出结构） */
export const listPromptTemplates = async (): Promise<PromptTemplate[]> => {
  const res = await service.get<{ items: PromptTemplate[] | null }>("/drama-prompt-templates");
  return res.items ?? [];
};

/**
 * 我的提示词列表，updated_at 倒序
 * @param step 环节；省略返回全部
 */
export const listMyPrompts = async (step?: PromptStep): Promise<MyPrompt[]> => {
  const res = await service.get<{ items: BackendMyPrompt[] | null }>("/drama-prompts", {
    step,
  });
  return (res.items ?? []).map(mapMyPrompt);
};

/**
 * 新建我的提示词；正文为空 / 超长 / 重名（67017）由编辑器就地展示，不弹全局提示
 * @param input 环节、名称与各页正文
 */
export const createMyPrompt = async (input: MyPromptInput): Promise<MyPrompt> =>
  mapMyPrompt(
    await service.post<BackendMyPrompt>(
      "/drama-prompts",
      { step: input.step, name: input.name, bodies: input.bodies },
      { silent: true },
    ),
  );

/**
 * 修改我的提示词（带 revision 乐观锁）；67016 / 67017 / 409 由调用方处理，不弹全局提示
 * @param id 提示词 id
 * @param input 环节、名称、各页正文与 revision
 */
export const updateMyPrompt = async (id: string, input: MyPromptInput): Promise<MyPrompt> =>
  mapMyPrompt(
    await service.put<BackendMyPrompt>(
      `/drama-prompts/${id}`,
      { step: input.step, name: input.name, bodies: input.bodies, revision: input.revision },
      { silent: true },
    ),
  );

/** 删除我的提示词；已建的短剧不受影响（快照） */
export const deleteMyPrompt = async (id: string): Promise<void> => {
  await service.delete(`/drama-prompts/${id}`, { silent: true });
};

/**
 * 某个环节可用的技能：后台已启用且标了该环节的
 * @param step 环节
 */
export const listDramaSkills = async (step: PromptStep): Promise<DramaSkill[]> => {
  const res = await service.get<{ items: DramaSkill[] | null }>("/drama-skills", { step });
  return res.items ?? [];
};

/** 暂停自动规划：在途的那一批跑完后停，不中途取消；状态不对 67019 */
export const pausePlanning = async (id: string): Promise<void> => {
  await service.post(`/dramas/${id}/planning/pause`, undefined, { silent: true });
};

/** 继续自动规划：失败的批会重试；仍不足积分则保持暂停；状态不对 67019 */
export const resumePlanning = async (id: string): Promise<void> => {
  await service.post(`/dramas/${id}/planning/resume`, undefined, { silent: true });
};

/**
 * 规划完成后扩大范围，接着规划（只能变大）；状态不对 67019
 * @param id 短剧 id
 * @param rangeEnd 新的终点单元号，0 = 全部
 */
export const setPlanningRange = async (id: string, rangeEnd: number): Promise<void> => {
  await service.post(`/dramas/${id}/planning/range`, { range_end: rangeEnd }, { silent: true });
};

/**
 * 「继续规划」确认框的预估
 * @param id 短剧 id
 * @param rangeEnd 终点单元号，0 = 全部
 */
export const estimatePlanning = async (id: string, rangeEnd: number): Promise<SourceEstimate> =>
  mapEstimate(
    await service.post(
      `/dramas/${id}/planning/estimate`,
      { range_end: rangeEnd },
      { silent: true },
    ),
  );

/**
 * 把该环节改回内置后继续规划（自定义提示词导致失败时用）
 * @param id 短剧 id
 * @param step 要改回内置的环节
 */
export const retryBuiltin = async (id: string, step: PromptStep): Promise<void> => {
  await service.post(`/dramas/${id}/planning/retry-builtin`, { step }, { silent: true });
};

/**
 * 原文单元目录（「对应原文」查看用）
 * @param id 短剧 id
 * @param from 起始单元号
 * @param to 结束单元号
 */
export const listUnits = async (id: string, from: number, to: number): Promise<DramaUnit[]> => {
  const res = await service.get<{ items: DramaUnit[] | null }>(
    `/dramas/${id}/units`,
    { from, to },
    { silent: true },
  );
  return res.items ?? [];
};

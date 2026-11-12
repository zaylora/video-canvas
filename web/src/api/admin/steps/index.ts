import service from "@/utils/requests/service";

import type {
  BindInput,
  FormatterInfo,
  StartTrialInput,
  StepDetail,
  StepSummary,
  TrialView,
} from "./type";

const seg = (value: string) => encodeURIComponent(value);

/** 后端可能把空数组序列化成 null：详情里的列表字段补成 [] */
export const normalizeStepDetail = (raw: StepDetail): StepDetail => ({
  ...raw,
  inputs: raw.inputs ?? [],
  rules: { hard: raw.rules?.hard ?? [], soft: raw.rules?.soft ?? [] },
  samples: raw.samples ?? [],
  history: raw.history ?? [],
  candidates: (raw.candidates ?? []).map((item) =>
    item.source === "skill" ? { ...item, versions: item.versions ?? [] } : item,
  ),
});

/** 试跑结果里的列表字段补成 [] */
export const normalizeTrial = (raw: TrialView): TrialView => ({
  ...raw,
  errors: raw.errors ?? [],
  flags: raw.flags ?? [],
});

/**
 * 全部环节（营销工作室四行；短剧待迁移，后端不返回）。
 * @returns 环节列表
 */
export const listSteps = async (): Promise<StepSummary[]> =>
  (await service.get<StepSummary[] | null>("/admin/steps")) ?? [];

/**
 * 环节详情：协议、候选方法、绑定历史与试跑样例。
 * @param key 环节 key，如 marketing.reverse；不存在返回 68012
 * @returns 环节详情
 */
export const getStep = async (key: string): Promise<StepDetail> =>
  normalizeStepDetail(await service.get<StepDetail>(`/admin/steps/${seg(key)}`));

/**
 * 对一个候选方法发起试跑；compare 为 true 时多跑一条当前绑定做对比（返回 1 或 2 条，queued）。
 * 该环节已有试跑在进行返回 68017。
 * @param key 环节 key
 * @param input 候选、样例、文本模型与是否对比
 * @returns 新建的试跑
 */
export const startTrial = async (key: string, input: StartTrialInput): Promise<TrialView[]> =>
  (
    (await service.post<{ trials: TrialView[] | null }>(`/admin/steps/${seg(key)}/trials`, input))
      .trials ?? []
  ).map(normalizeTrial);

/**
 * 查询一次试跑，用于轮询到 succeeded / failed。
 * @param id 试跑 ID
 * @returns 试跑当前状态与结果
 */
export const getTrial = async (id: string): Promise<TrialView> =>
  normalizeTrial(await service.get<TrialView>(`/admin/step-trials/${seg(id)}`));

/**
 * 把候选设为该环节的生效方法；必须带通过格式校验的试跑 ID（68013）。
 * @param key 环节 key
 * @param input 候选与试跑 ID
 * @returns 更新后的环节详情
 */
export const bindStep = async (key: string, input: BindInput): Promise<StepDetail> =>
  normalizeStepDetail(await service.put<StepDetail>(`/admin/steps/${seg(key)}/binding`, input));

/**
 * 视频提示词格式化器清单（代码维护，只读预览）。
 * @returns 格式化器列表
 */
export const listFormatters = async (): Promise<FormatterInfo[]> =>
  (await service.get<FormatterInfo[] | null>("/admin/formatters")) ?? [];

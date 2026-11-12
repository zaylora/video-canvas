import { describe, expect, test } from "bun:test";

import type { StepCandidateInfo, StepSummary, TrialView } from "@/api/admin/steps/type";
import {
  candidateOfHistory,
  sampleAllowed,
  stepNeedsRefSample,
  activateState,
  bindingLabel,
  candidateBadges,
  candidateLabel,
  fallbackSteps,
  isTrialPending,
  sameCandidate,
  trialModelAllowed,
} from "@/utils/admin/steps";

const step = (patch: Partial<StepSummary> = {}): StepSummary => ({
  key: "marketing.reverse",
  title: "拆解参考片",
  studio: "marketing",
  media: "video",
  binding: { source: "skill", skill_name: "daihuo-fanpai", skill_title: "带货翻拍", version: 2 },
  effective: { source: "skill", skill_name: "daihuo-fanpai", version: 2 },
  newer_version: null,
  last_trial: null,
  fail_rate_7d: 0,
  ...patch,
});

const trial = (patch: Partial<TrialView> = {}): TrialView => ({
  id: "t1",
  step_key: "marketing.reverse",
  candidate: { source: "skill", skill_name: "tvc-director", version: 1 },
  sample_key: "perfume",
  text_model: "m",
  compare_of: null,
  status: "succeeded",
  passed: true,
  errors: [],
  flags: [],
  preview: "",
  output: {},
  duration_ms: 1000,
  input_tokens: 1,
  output_tokens: 1,
  credits: 2,
  created_at: "2026-11-11T10:00:00Z",
  ...patch,
});

const cand = { source: "skill" as const, skill_name: "tvc-director", version: 1 };

describe("候选与标签", () => {
  test("候选相同：都是内置，或技能名与版本都相同", () => {
    expect(sameCandidate({ source: "builtin" }, { source: "builtin" })).toBe(true);
    expect(sameCandidate(cand, { ...cand })).toBe(true);
    expect(sameCandidate(cand, { ...cand, version: 2 })).toBe(false);
    expect(sameCandidate(cand, { source: "builtin" })).toBe(false);
    expect(sameCandidate(null, cand)).toBe(false);
  });

  test("标签：内置方法 / 技能名 vN", () => {
    expect(candidateLabel({ source: "builtin" })).toBe("内置方法");
    expect(candidateLabel(cand)).toBe("tvc-director v1");
    expect(bindingLabel(step().binding)).toBe("daihuo-fanpai v2");
    expect(bindingLabel(null)).toBe("内置方法");
  });

  test("候选角标：仅用正文、会截断（超过 1.6 万字）、已停用", () => {
    const info = (patch: Partial<Extract<StepCandidateInfo, { source: "skill" }>>) =>
      ({
        source: "skill",
        skill_name: "s",
        skill_title: "S",
        versions: [1],
        active_version: 1,
        has_scripts: false,
        chars: 1000,
        enabled: true,
        ...patch,
      }) as StepCandidateInfo;
    expect(candidateBadges(info({}))).toEqual([]);
    expect(candidateBadges(info({ has_scripts: true }))).toEqual(["仅用正文"]);
    expect(candidateBadges(info({ chars: 16001 }))).toEqual(["会截断"]);
    expect(candidateBadges(info({ chars: 16000 }))).toEqual([]);
    expect(candidateBadges(info({ enabled: false, has_scripts: true, chars: 38000 }))).toEqual([
      "仅用正文",
      "会截断",
      "已停用",
    ]);
    expect(candidateBadges({ source: "builtin" })).toEqual([]);
  });
});

describe("退回内置提示", () => {
  test("只列 effective.fallback 的环节", () => {
    const steps = [
      step(),
      step({
        key: "marketing.follow",
        title: "跟拍改写",
        effective: { source: "builtin", fallback: true },
      }),
    ];
    expect(fallbackSteps(steps).map((item) => item.key)).toEqual(["marketing.follow"]);
    expect(fallbackSteps([step()])).toEqual([]);
  });
});

describe("设为生效是否可点", () => {
  const binding = step().binding;
  const state = (patch: Record<string, unknown> = {}) =>
    activateState({
      candidate: cand,
      binding,
      fallback: false,
      trial: trial(),
      ...patch,
    } as never);

  test("候选试跑通过、且不是当前绑定：可点", () => {
    expect(state()).toEqual({ disabled: false, reason: "" });
  });

  test("还没试跑 / 试跑的是别的候选：提示先试跑", () => {
    expect(state({ trial: null })).toEqual({ disabled: true, reason: "先试跑这个候选" });
    expect(state({ trial: trial({ candidate: { source: "builtin" } }) }).reason).toBe(
      "先试跑这个候选",
    );
  });

  test("试跑进行中、调用出错、格式校验没通过：禁用并写原因", () => {
    expect(state({ trial: trial({ status: "running" }) }).reason).toBe("试跑进行中");
    expect(state({ trial: trial({ status: "queued" }) }).reason).toBe("试跑进行中");
    expect(state({ trial: trial({ status: "failed", passed: false }) }).reason).toBe(
      "试跑调用失败，没有结果",
    );
    expect(state({ trial: trial({ passed: false }) }).reason).toBe("候选没有通过格式校验");
  });

  test("候选就是当前绑定：禁用；但绑定已退回内置时，选回原绑定可以重新生效", () => {
    expect(state({ candidate: binding, trial: trial({ candidate: binding }) }).reason).toBe(
      "候选就是当前绑定",
    );
    expect(
      state({ candidate: binding, trial: trial({ candidate: binding }), fallback: true }).disabled,
    ).toBe(false);
  });

  test("试跑是否进行中", () => {
    expect(isTrialPending(trial({ status: "queued" }))).toBe(true);
    expect(isTrialPending(trial({ status: "running" }))).toBe(true);
    expect(isTrialPending(trial())).toBe(false);
    expect(isTrialPending(null)).toBe(false);
  });
});

describe("试跑选项", () => {
  test("看图 / 看视频的环节只能选能看图的文本模型；纯文本环节都能选", () => {
    expect(trialModelAllowed("images", { vision: false })).toBe(false);
    expect(trialModelAllowed("images", { vision: true })).toBe(true);
    expect(trialModelAllowed("video", { vision: true })).toBe(true);
    expect(trialModelAllowed("none", { vision: false })).toBe(true);
  });
});

describe("试跑样例与恢复", () => {
  test("拆解和跟拍只能选带参考片的样例，事实卡和创意都能选", () => {
    expect(stepNeedsRefSample("marketing.reverse")).toBe(true);
    expect(stepNeedsRefSample("marketing.follow")).toBe(true);
    expect(stepNeedsRefSample("marketing.facts")).toBe(false);
    expect(sampleAllowed("marketing.reverse", { needs_ref: false })).toBe(false);
    expect(sampleAllowed("marketing.reverse", { needs_ref: true })).toBe(true);
    expect(sampleAllowed("marketing.create", { needs_ref: false })).toBe(true);
  });

  test("绑定历史恢复：对象、内置方法、技能名 vN 都能转成候选；认不出返回 null", () => {
    expect(candidateOfHistory(null)).toEqual({ source: "builtin" });
    expect(candidateOfHistory("内置方法")).toEqual({ source: "builtin" });
    expect(candidateOfHistory("daihuo-fanpai v2")).toEqual({
      source: "skill",
      skill_name: "daihuo-fanpai",
      version: 2,
    });
    expect(candidateOfHistory({ source: "skill", skill_name: "s", version: 1 })).toEqual({
      source: "skill",
      skill_name: "s",
      version: 1,
    });
    expect(candidateOfHistory("乱写")).toBeNull();
  });
});

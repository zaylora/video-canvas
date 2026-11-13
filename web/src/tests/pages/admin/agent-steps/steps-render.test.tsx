import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { StepDetail, TrialView } from "@/api/admin/steps/type";
import { ContractTab, MethodTab } from "@/pages/admin/agent-steps/step-tabs";
import { TrialTab } from "@/pages/admin/agent-steps/trial-tab";

const detail: StepDetail = {
  key: "marketing.reverse",
  title: "拆解参考片",
  studio: "marketing",
  media: "video",
  binding: { source: "skill", skill_name: "daihuo-fanpai", skill_title: "带货翻拍", version: 2 },
  effective: { source: "skill", skill_name: "daihuo-fanpai", version: 2 },
  newer_version: 3,
  last_trial: null,
  fail_rate_7d: 0.018,
  duty: "把参考视频拆成逐镜头的分镜表",
  inputs: [{ name: "video", type: "视频 ≤15s", desc: "参考视频" }],
  schema: '{ "overall": {} }',
  rules: { hard: ["镜头首尾相接"], soft: ["动作偏概括"] },
  samples: [
    { key: "perfume", name: "晨雾香水 · 无参考", needs_ref: false },
    { key: "cream", name: "护手霜 · 带参考片", needs_ref: true },
  ],
  history: [
    {
      id: "h1",
      from: { source: "builtin" },
      to: { source: "skill", skill_name: "daihuo-fanpai", version: 2 },
      actor: "admin",
      trial_id: "t1",
      created_at: "2026-11-08T09:30:00Z",
    },
  ],
  candidates: [
    { source: "builtin" },
    {
      source: "skill",
      skill_name: "daihuo-fanpai",
      skill_title: "带货翻拍",
      versions: [1, 2, 3],
      active_version: 3,
      has_scripts: true,
      chars: 38000,
      enabled: true,
    },
    {
      source: "skill",
      skill_name: "old-ad-writer",
      skill_title: "老版广告文案",
      versions: [1],
      active_version: 1,
      has_scripts: false,
      chars: 3100,
      enabled: false,
    },
  ],
};

const trial = (patch: Partial<TrialView> = {}): TrialView => ({
  id: "t1",
  step_key: "marketing.reverse",
  candidate: { source: "skill", skill_name: "daihuo-fanpai", version: 2 },
  sample_key: "cream",
  text_model: "m",
  compare_of: null,
  status: "succeeded",
  passed: true,
  errors: [],
  flags: ["2 个镜头未标关键颜色"],
  preview: "4 个镜头 · 10s",
  output: {},
  duration_ms: 9200,
  input_tokens: 3100,
  output_tokens: 800,
  credits: 4,
  created_at: "2026-11-11T10:00:00Z",
  ...patch,
});

describe("后台环节技能：抽屉标签渲染", () => {
  test("协议标签：三层说明、职责、输入表、Schema、硬错误与软问题、拼接顺序", () => {
    const out = renderToStaticMarkup(<ContractTab detail={detail} />);
    expect(out).toContain("协议层");
    expect(out).toContain("方法层");
    expect(out).toContain("格式化层");
    expect(out).toContain("daihuo-fanpai v2");
    expect(out).toContain("把参考视频拆成逐镜头的分镜表");
    expect(out).toContain("参考视频");
    expect(out).toContain("镜头首尾相接");
    expect(out).toContain("动作偏概括");
    expect(out).toContain("提示词拼接顺序");
  });

  test("方法标签：当前绑定、候选角标（仅用正文 / 会截断 / 已停用）、绑定历史与恢复", () => {
    const out = renderToStaticMarkup(
      <MethodTab
        detail={detail}
        candidate={{ source: "skill", skill_name: "daihuo-fanpai", version: 3 }}
        onCandidate={() => undefined}
        onRestore={() => undefined}
      />,
    );
    expect(out).toContain("生效中");
    expect(out).toContain("已钉死版本 v2");
    expect(out).toContain("仅用正文");
    expect(out).toContain("会截断");
    expect(out).toContain("已停用");
    expect(out).toContain("v3（最新）");
    expect(out).toContain("内置方法 → <b");
    expect(out).toContain("恢复");
  });

  test("绑定已退回内置：当前生效显示已退回内置", () => {
    const out = renderToStaticMarkup(
      <MethodTab
        detail={{ ...detail, effective: { source: "builtin", fallback: true } }}
        candidate={{ source: "builtin" }}
        onCandidate={() => undefined}
        onRestore={() => undefined}
      />,
    );
    expect(out).toContain("已退回内置");
    expect(out).toContain("技能已停用，自动退回");
  });

  test("试跑标签：带参考片的环节禁用无参考样例；两列结果与软问题", () => {
    const out = renderToStaticMarkup(
      <TrialTab
        detail={detail}
        candidate={{ source: "skill", skill_name: "daihuo-fanpai", version: 3 }}
        sampleKey="cream"
        textModel="m"
        compare
        textModels={[]}
        textLoading={false}
        run={{
          cand: trial(),
          base: trial({
            id: "t2",
            compare_of: "t1",
            passed: false,
            errors: ["缺少字段 shots"],
            flags: [],
          }),
        }}
        candidateIsCurrent={false}
        onChange={() => undefined}
        onChangeCandidate={() => undefined}
      />,
    );
    expect(out).toMatch(/disabled=""[^>]*title="这个环节需要带参考片的样例"/);
    expect(out).toContain("格式校验通过");
    expect(out).toContain("格式校验失败");
    expect(out).toContain("缺少字段 shots");
    expect(out).toContain("软问题：2 个镜头未标关键颜色");
    expect(out).toContain("9.2s");
    expect(out).toContain("✦4");
  });

  test("试跑进行中显示转圈；还没试跑显示空状态", () => {
    const base = {
      detail,
      candidate: { source: "builtin" as const },
      sampleKey: "cream",
      textModel: "m",
      compare: false,
      textModels: [],
      textLoading: false,
      candidateIsCurrent: false,
      onChange: () => undefined,
      onChangeCandidate: () => undefined,
    };
    expect(
      renderToStaticMarkup(
        <TrialTab {...base} run={{ cand: trial({ status: "running" }), base: null }} />,
      ),
    ).toContain("试跑中…");
    expect(renderToStaticMarkup(<TrialTab {...base} run={null} />)).toContain("还没有试跑结果");
  });
});

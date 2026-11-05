import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { AdminStats } from "@/api/admin/ai/type";
import { OnboardingSteps, type OnboardingStep } from "@/pages/admin/ai/overview/onboarding-steps";
import { ShareRanking } from "@/pages/admin/ai/overview/share-ranking";

const step = (title: string, ok: boolean): OnboardingStep => ({
  title,
  ok,
  stat: `${title} 统计`,
  short: title,
  action: null,
});

const onboarding = (steps: OnboardingStep[]) =>
  renderToStaticMarkup(<OnboardingSteps steps={steps} ready onNavigate={() => {}} />);

describe("总览 · 上线引导", () => {
  test("三步都完成后整块不显示，不再占一行", () => {
    expect(
      onboarding([step("接入平台", true), step("配置渠道", true), step("上线模型", true)]),
    ).toBe("");
  });

  test("有一步没完成时显示三步大卡", () => {
    const out = onboarding([
      step("接入平台", true),
      step("配置渠道", false),
      step("上线模型", false),
    ]);
    expect(out).toContain("上线进度");
    expect(out).toContain("配置渠道 统计");
  });
});

const stats: AdminStats = {
  days: 7,
  daily: [],
  by_model: [
    { model: "a", label: "gpt-image-2.5", count: 75 },
    { model: "b", label: "seed-audio-1.0", count: 25 },
  ],
  by_kind: [{ kind: "image", count: 100 }],
};

const ranking = (s: AdminStats) =>
  renderToStaticMarkup(<ShareRanking stats={s} status="ready" onRetry={() => {}} />);

describe("总览 · 调用排行", () => {
  test("头部给出总调用次数，每行给出次数与占比", () => {
    const out = ranking(stats);
    expect(out).toContain("100");
    expect(out).toContain("gpt-image-2.5");
    expect(out).toContain("75.0%");
    expect(out).toContain("25.0%");
  });

  test("用横向占比条展示，不再画环形图", () => {
    const out = ranking(stats);
    expect(out.match(/data-slot="share-bar"/g)?.length).toBe(2);
    expect(out).not.toContain("recharts-pie");
  });

  test("没有任务时只显示空状态", () => {
    const out = ranking({ ...stats, by_model: [], by_kind: [] });
    expect(out).toContain("这段时间还没有任务");
    expect(out).not.toContain('data-slot="share-bar"');
  });
});

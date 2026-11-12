import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router";

import type { Project, Round, Script, StepRun } from "@/api/marketing/type";
import { MarketingComposer } from "@/components/marketing/marketing-composer";
import { RoundSection } from "@/pages/marketing/detail/round-section";
import type { MarketingActions } from "@/pages/marketing/detail/use-actions";
import { ProjectCard } from "@/pages/marketing/project-grid";
import { DEFAULT_ANGLES, DEFAULT_PLATFORMS } from "@/utils/marketing/options";

const step = (key: StepRun["step"], status: StepRun["status"], error = ""): StepRun => ({
  id: `s-${key}`,
  step: key,
  status,
  method: {
    source: "skill",
    skillName: "daihuo-fanpai",
    skillTitle: "带货翻拍",
    version: 2,
    fallback: false,
  },
  textModel: "m",
  error,
  attempts: 1,
  charged: 3,
});

const script = (patch: Partial<Script> = {}): Script => ({
  id: "x1",
  roundId: "r1",
  kind: "follow",
  angle: "follow",
  title: "跟拍 · 早上喷的香，中午就散了？",
  hook: "",
  shots: [
    {
      start: 0,
      end: 3,
      purpose: "",
      shot: "特写",
      camera: "手持推近",
      scene: "左手手腕朝上",
      narration: "早上喷的香，中午就散了？",
      refShot: 1,
    },
  ],
  music: "轻快鼓点",
  voice: "年轻女声",
  flags: [],
  status: "draft",
  edited: true,
  method: {
    source: "skill",
    skillName: "daihuo-fanpai",
    skillTitle: "带货翻拍",
    version: 2,
    fallback: false,
  },
  takes: [],
  renderCredits: 120,
  error: "",
  revision: 1,
  ...patch,
});

const round = (patch: Partial<Round> = {}): Round => ({
  id: "r1",
  no: 2,
  status: "ready",
  stage: "正在写跟拍版和改编版",
  error: "",
  warnings: [],
  settings: {
    platform: "douyin",
    language: "zh",
    angles: [],
    autoCount: 2,
    videoModel: "seedance",
    textModel: "",
    params: { aspect_ratio: "9:16", resolution: "720p", duration: 15 },
    description: "",
  },
  refVideo: { id: "103", url: "https://x/ref.mp4", duration: 10 },
  steps: [step("facts", "succeeded"), step("reverse", "succeeded"), step("follow", "succeeded")],
  planCount: 2,
  reverse: {
    overall: {
      style: "UGC",
      narrativeArc: "钩子",
      whyViral: "前 2 秒手背干纹特写",
      transcript: "",
      category: "",
    },
    shots: [
      {
        id: 1,
        start: 0,
        end: 2,
        isOpening3s: true,
        shotSize: "特写",
        camera: "手持",
        subject: "",
        action: "划过干纹",
        scene: "",
        lighting: "",
        person: "",
        productInFrame: "不出现",
        productRole: "none",
        dialogue: "冬天手干？",
        onscreenText: "",
        keyColors: [],
      },
    ],
    brands: ["某品牌"],
    claims: ["全网销量第一"],
  },
  reverseRevision: 1,
  reverseEdited: false,
  reverseDirty: true,
  scripts: [
    script(),
    script({ id: "x2", kind: "create", angle: "pain", title: "改编", status: "done" }),
  ],
  frozen: 0,
  charged: 0,
  createdAt: "2026-11-11T10:00:00Z",
  ...patch,
});

const project: Project = {
  id: "p1",
  title: "晨雾香水 50ml",
  product: null,
  productAssets: [{ id: "101", url: "https://x/a.jpg" }],
  modelAsset: null,
  revision: 1,
  rounds: [],
  createdAt: "2026-11-11T09:00:00Z",
  updatedAt: "2026-11-11T10:00:00Z",
};

const actions = { busy: {} } as unknown as MarketingActions;

const html = (r: Round, available: number | null = 1000) =>
  renderToStaticMarkup(
    <MemoryRouter>
      <RoundSection
        project={project}
        round={r}
        angles={DEFAULT_ANGLES}
        platforms={DEFAULT_PLATFORMS}
        videoModelLabel="Seedance 2.5"
        available={available}
        actions={actions}
      />
    </MemoryRouter>,
  );

describe("详情页：轮次渲染", () => {
  test("写好的轮次：轮次头设置标签、环节小标签、拆解卡摘要、跟拍与改编脚本卡、全部出片", () => {
    const out = html(round());
    expect(out).toContain("第 2 轮");
    expect(out).toContain("抖音");
    expect(out).toContain("Seedance 2.5");
    expect(out).toContain("9:16 · 720P · 15s");
    expect(out).toContain("中文旁白");
    expect(out).toContain("参考 10s");
    expect(out).toContain("全部出片");
    expect(out).toContain("事实卡");
    expect(out).toContain("参考拆解");
    expect(out).toContain("1 个镜头");
    expect(out).toContain("前 2 秒手背干纹特写");
    expect(out).toContain("第三方品牌 1");
    expect(out).toContain("原片背书 1");
    expect(out).toContain("跟拍");
    expect(out).toContain("参考 1");
    expect(out).toContain("已修改");
    expect(out).toContain("方法：daihuo-fanpai v2");
    expect(out).toContain("✦120");
    expect(out).toContain("已出片");
  });

  test("写脚本中：环节进度条、阶段说明、取消、按计划份数画骨架", () => {
    const out = html(round({ status: "scripting", scripts: [], planCount: 3, reverse: null }));
    expect(out).toContain("取消");
    expect(out).toContain("正在写跟拍版和改编版");
    expect(out.split('data-slot="skeleton"').length - 1).toBeGreaterThanOrEqual(3);
  });

  test("失败的轮次：原因、积分已退回、重试；取消的轮次写已取消", () => {
    const failed = html(
      round({
        status: "failed",
        error: "商品事实卡生成失败",
        steps: [step("facts", "failed", "文本模型两次输出都不合格")],
        scripts: [],
      }),
    );
    expect(failed).toContain("商品事实卡生成失败");
    expect(failed).toContain("积分已退回");
    expect(failed).toContain("重试");
    expect(html(round({ status: "canceled", scripts: [] }))).toContain("已取消，积分已退回");
  });

  test("拆解失败：拆解卡位置显示原因和重试拆解，跟拍卡不出现，改编版照常", () => {
    const out = html(
      round({
        reverse: null,
        steps: [
          step("facts", "succeeded"),
          step("reverse", "failed", "看视频模型返回的分镜不完整"),
          step("follow", "skipped"),
        ],
        scripts: [script({ id: "x2", kind: "create", angle: "pain", title: "改编" })],
      }),
    );
    expect(out).toContain("参考片拆解失败：看视频模型返回的分镜不完整");
    expect(out).toContain("重试拆解");
    expect(out).not.toContain("跟拍 · 早上喷的香");
  });

  test("跟拍失败卡与警告行", () => {
    const out = html(
      round({
        warnings: ["拆解未包含声音，台词按画面推断"],
        steps: [
          step("facts", "succeeded"),
          step("reverse", "succeeded"),
          step("follow", "failed", "校验不过"),
        ],
        scripts: [],
      }),
    );
    expect(out).toContain("拆解未包含声音，台词按画面推断");
    expect(out).toContain("跟拍版生成失败：校验不过");
  });

  test("旁白有问题的脚本：问题标签、出片禁用并写原因", () => {
    const out = html(
      round({
        reverse: null,
        refVideo: null,
        steps: [step("facts", "succeeded"), step("create", "succeeded")],
        scripts: [
          script({
            kind: "create",
            angle: "pain",
            flags: [{ shot: 0, type: "claim", text: "全网销量第一" }],
          }),
        ],
      }),
    );
    expect(out).toContain("背书无依据");
    expect(out).toContain("旁白有问题：背书无依据「全网销量第一」");
    expect(out).toMatch(/<button[^>]*disabled[^>]*>[^]*?出片/);
  });
});

describe("营销生成框与列表卡渲染", () => {
  test("列表页生成框：三个上传块、平台、角度、模型、参数、写脚本键", () => {
    const out = renderToStaticMarkup(
      <MemoryRouter>
        <MarketingComposer />
      </MemoryRouter>,
    );
    expect(out).toContain("商品");
    expect(out).toContain("模特");
    expect(out).toContain("参考");
    expect(out).toContain("抖音");
    expect(out).toContain("自动 ×2");
    expect(out).toContain("9:16 · 720P · 15s · 中");
    expect(out).toContain('aria-label="写脚本"');
    expect(out).toContain("商品名、卖点、想要的感觉，也可以不写");
  });

  test("详情页 dock：商品图固定、占位文案不同", () => {
    const out = renderToStaticMarkup(
      <MemoryRouter>
        <MarketingComposer project={project} />
      </MemoryRouter>,
    );
    expect(out).toContain("换个说法、补充卖点，再写一轮");
    expect(out).toContain('data-placement="dock"');
  });

  test("列表卡：标题、状态行、状态条", () => {
    const out = renderToStaticMarkup(
      <MemoryRouter>
        <ProjectCard
          project={{
            id: "a",
            title: "便携榨汁杯",
            coverUrl: "",
            scripting: false,
            rendering: 1,
            lastRoundFailed: false,
            scriptCounts: { draft: 1, rendering: 1, done: 2, failed: 0 },
            bars: ["done", "rendering", "draft"],
            updatedAt: "2026-11-10T10:00:00Z",
          }}
          index={0}
          deleting={false}
          onDelete={() => undefined}
        />
      </MemoryRouter>,
    );
    expect(out).toContain("便携榨汁杯");
    expect(out).toContain("出片中");
    expect(out.split("h-[3px]").length - 1).toBe(3);
    expect(out).toContain('href="/marketing/a"');
  });
});

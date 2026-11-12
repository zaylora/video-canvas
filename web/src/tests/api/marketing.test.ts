import { afterEach, beforeEach, describe, expect, spyOn, test, type Mock } from "bun:test";

import {
  createMarketingProject,
  getMarketingOptions,
  getMarketingPrompt,
  listMarketingProjects,
  mapProject,
  mapProjectSummary,
  mapRound,
  mapScript,
  quoteMarketing,
  renderMarketingRound,
  renderMarketingScript,
  saveMarketingReverse,
  saveMarketingScript,
  sendMarketingScriptToCanvas,
  toBackendReverse,
  toBackendSettings,
} from "@/api/marketing";
import { isMarketingConflict, MARKETING_ERR } from "@/api/marketing/errors";
import type {
  BackendProject,
  BackendRound,
  BackendScript,
  RoundSettings,
} from "@/api/marketing/type";
import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";

const rawScript = (patch: Partial<BackendScript> = {}): BackendScript => ({
  id: "x1",
  round_id: "r1",
  kind: "follow",
  angle: "follow",
  title: "跟拍 · 早上喷的香，中午就散了？",
  shots: [
    {
      start: 0,
      end: 3,
      purpose: "hook",
      shot: "特写",
      camera: "手持推近",
      scene: "左手手腕朝上",
      narration: "早上喷的香，中午就散了？",
      ref_shot: 1,
    },
    { start: 3, end: 6, shot: "中景", camera: "固定", scene: "喷雾", narration: "" },
  ],
  flags: [{ shot: 0, type: "claim", text: "全网销量第一" }],
  status: "draft",
  edited: true,
  method: { source: "skill", skill_name: "daihuo-fanpai", skill_title: "带货翻拍", version: 2 },
  takes: [
    {
      id: 77,
      task_ref: "4d",
      status: "succeeded",
      progress: 100,
      outputs: [{ url: "https://x/v.mp4", poster_url: "https://x/p.jpg" }, {}],
      error_message: null,
    },
  ],
  render_credits: 120,
  revision: 4,
  ...patch,
});

const rawSettings = {
  platform: "douyin" as const,
  language: "zh" as const,
  angles: null,
  auto_count: 2,
  video_model: "seedance-2-5",
  text_model: "",
  params: { aspect_ratio: "9:16", resolution: "720p", duration: 15 },
  description: "木质调",
};

const rawRound = (patch: Partial<BackendRound> = {}): BackendRound => ({
  id: "r1",
  no: 2,
  status: "ready",
  stage: "",
  warnings: null,
  settings: rawSettings,
  ref_video: { id: 103, url: "https://x/ref.mp4", duration: 10 },
  steps: [
    {
      id: "s1",
      step: "reverse",
      status: "succeeded",
      method: { source: "skill", skill_name: "daihuo-fanpai", skill_title: "带货翻拍", version: 2 },
      text_model: "gemini-3-pro",
      attempts: 1,
      charged: 6,
    },
  ],
  plan_count: 2,
  reverse: {
    overall: {
      style: "UGC",
      narrative_arc: "钩子",
      why_viral: "前 2 秒",
      transcript: "",
      category: "护手霜",
    },
    shots: [
      {
        id: 1,
        start: 0,
        end: 2,
        is_opening_3s: true,
        shot_size: "特写",
        camera: "手持",
        subject: "",
        action: "划过干纹",
        scene: "",
        lighting: "",
        person: "",
        product_in_frame: "不出现",
        product_role: "none",
        dialogue: "冬天手干？",
        onscreen_text: "",
        key_colors: null,
      },
    ],
    brands: null,
    claims: ["全网销量第一"],
  },
  reverse_revision: 1,
  reverse_edited: true,
  reverse_dirty: true,
  scripts: [rawScript()],
  frozen: 12,
  charged: 10,
  created_at: "2026-11-11T10:00:00Z",
  ...patch,
});

const rawProject = (patch: Partial<BackendProject> = {}): BackendProject => ({
  id: "a1b2",
  title: "晨雾香水 50ml",
  product: {
    name: "晨雾香水 50ml",
    category: "香水",
    anchors: ["方形磨砂玻璃瓶身"],
    selling_points: [{ text: "留香 8 小时", proof: "时间跳切", source: "description" }],
    prohibited: null,
    audience: "通勤女性",
    cta: "点击下方链接",
  },
  product_assets: [{ id: 101, url: "https://x/a.jpg" }],
  model_asset: null,
  revision: 3,
  rounds: [rawRound()],
  created_at: "2026-11-11T09:00:00Z",
  updated_at: "2026-11-11T10:00:00Z",
  ...patch,
});

describe("api/marketing：mapper", () => {
  test("项目详情：素材 id 转字符串，事实卡与轮次 snake→camel，null 数组兜底", () => {
    const project = mapProject(rawProject());
    expect(project.productAssets).toEqual([{ id: "101", url: "https://x/a.jpg" }]);
    expect(project.modelAsset).toBeNull();
    expect(project.product?.sellingPoints[0]).toEqual({
      text: "留香 8 小时",
      proof: "时间跳切",
      source: "description",
    });
    expect(project.product?.prohibited).toEqual([]);
    expect(project.rounds[0].no).toBe(2);
    expect(mapProject(rawProject({ rounds: null, product_assets: null })).rounds).toEqual([]);
  });

  test("轮次：设置、参考视频、拆解、环节与警告", () => {
    const round = mapRound(rawRound());
    expect(round.settings).toMatchObject({
      platform: "douyin",
      autoCount: 2,
      videoModel: "seedance-2-5",
      textModel: "",
      angles: [],
    });
    expect(round.settings.params).toEqual({
      aspect_ratio: "9:16",
      resolution: "720p",
      duration: 15,
    });
    expect(round.refVideo).toEqual({ id: "103", url: "https://x/ref.mp4", duration: 10 });
    expect(round.warnings).toEqual([]);
    expect(round.reverseDirty).toBe(true);
    expect(round.reverseEdited).toBe(true);
    expect(round.reverse?.shots[0]).toMatchObject({
      isOpening3s: true,
      shotSize: "特写",
      productRole: "none",
      keyColors: [],
    });
    expect(round.reverse?.overall.whyViral).toBe("前 2 秒");
    expect(round.reverse?.brands).toEqual([]);
    expect(round.steps[0]).toMatchObject({
      step: "reverse",
      textModel: "gemini-3-pro",
      charged: 6,
      error: "",
    });
    expect(round.steps[0].method).toEqual({
      source: "skill",
      skillName: "daihuo-fanpai",
      skillTitle: "带货翻拍",
      version: 2,
      fallback: false,
    });
    expect(mapRound(rawRound({ ref_video: null })).refVideo).toBeNull();
  });

  test("脚本：镜头旁白、参考镜头、问题与出片历史（无地址的产出丢掉）", () => {
    const script = mapScript(rawScript());
    expect(script.shots[0]).toMatchObject({ narration: "早上喷的香，中午就散了？", refShot: 1 });
    expect(script.shots[1]).toMatchObject({ refShot: null, purpose: "" });
    expect(script.flags).toEqual([{ shot: 0, type: "claim", text: "全网销量第一" }]);
    expect(script.renderCredits).toBe(120);
    expect(script.takes).toHaveLength(1);
    expect(script.takes[0]).toMatchObject({ id: "77", taskRef: "4d", progress: 100 });
    expect(script.takes[0].outputs).toEqual([
      { url: "https://x/v.mp4", posterUrl: "https://x/p.jpg" },
    ]);
    expect(mapScript(rawScript({ takes: null, flags: null, method: null })).takes).toEqual([]);
  });

  test("列表项：状态条与计数兜底", () => {
    const item = mapProjectSummary({
      id: "a",
      title: "t",
      scripting: false,
      rendering: 1,
      last_round_failed: false,
      script_counts: { done: 2 },
      bars: ["done", "rendering"],
      updated_at: "2026-11-11T10:00:00Z",
    });
    expect(item.coverUrl).toBe("");
    expect(item.scriptCounts).toEqual({ draft: 0, rendering: 0, done: 2, failed: 0 });
    expect(item.bars).toEqual(["done", "rendering"]);
  });
});

describe("api/marketing：请求体", () => {
  test("设置转后端：camel→snake", () => {
    const settings: RoundSettings = {
      platform: "tiktok",
      language: "en",
      angles: ["pain"],
      autoCount: 1,
      videoModel: "m",
      textModel: "t",
      params: { duration: 10 },
      description: "d",
    };
    expect(toBackendSettings(settings)).toEqual({
      platform: "tiktok",
      language: "en",
      angles: ["pain"],
      auto_count: 1,
      video_model: "m",
      text_model: "t",
      params: { duration: 10 },
      description: "d",
    });
  });

  test("拆解往返：改后的镜头字段回到 snake", () => {
    const reverse = mapRound(rawRound()).reverse!;
    reverse.shots[0].action = "新动作";
    reverse.shots[0].keyColors = ["白色"];
    const body = toBackendReverse(reverse);
    expect(body.shots?.[0]).toMatchObject({
      id: 1,
      is_opening_3s: true,
      shot_size: "特写",
      action: "新动作",
      key_colors: ["白色"],
      product_role: "none",
    });
    expect(body.overall.why_viral).toBe("前 2 秒");
  });
});

describe("api/marketing：请求", () => {
  let get: Mock<typeof service.get>;
  let post: Mock<typeof service.post>;
  let put: Mock<typeof service.put>;
  beforeEach(() => {
    get = spyOn(service, "get");
    post = spyOn(service, "post");
    put = spyOn(service, "put");
  });
  afterEach(() => {
    get.mockRestore();
    post.mockRestore();
    put.mockRestore();
  });

  test("预设：snake→camel，数组 null 兜底", async () => {
    get.mockResolvedValue({
      platforms: [{ key: "douyin", name: "抖音", aspect_ratio: "9:16", language: "zh", style: "" }],
      angles: null,
    });
    const options = await getMarketingOptions();
    expect(get.mock.calls[0][0]).toBe("/marketing/options");
    expect(options.platforms[0]).toMatchObject({
      key: "douyin",
      aspectRatio: "9:16",
      language: "zh",
    });
    expect(options.angles).toEqual([]);
  });

  test("预估：路径、请求体，失败静默", async () => {
    post.mockResolvedValue({ credits: 11, steps: [{ step: "facts", credits: 3 }] });
    const settings: RoundSettings = {
      platform: "douyin",
      language: "zh",
      angles: [],
      autoCount: 2,
      videoModel: "m",
      textModel: "",
      params: {},
      description: "",
    };
    const quote = await quoteMarketing({ projectId: "p1", hasRef: true, settings });
    expect(quote.credits).toBe(11);
    expect(post.mock.calls[0][0]).toBe("/marketing/quote");
    expect(post.mock.calls[0][1]).toMatchObject({ project_id: "p1", has_ref: true });
    expect(post.mock.calls[0][2]).toMatchObject({ silent: true });
  });

  test("列表：page_size 映射，list 为 null 兜底", async () => {
    get.mockResolvedValue({ list: null, total: 0, page: 1, page_size: 20 });
    expect(await listMarketingProjects({ page: 2, pageSize: 20 })).toEqual({ items: [], total: 0 });
    expect(get.mock.calls[0][0]).toBe("/marketing-projects");
    expect(get.mock.calls[0][1]).toEqual({ page: 2, page_size: 20 });
  });

  test("创建项目：素材 id 与设置按契约字段发送", async () => {
    post.mockResolvedValue(rawProject());
    const settings = mapRound(rawRound()).settings;
    await createMarketingProject({
      productAssetIds: [101, 102],
      modelAssetId: 5,
      refVideoAssetId: 6,
      settings,
    });
    expect(post.mock.calls[0][0]).toBe("/marketing-projects");
    expect(post.mock.calls[0][1]).toMatchObject({
      product_asset_ids: [101, 102],
      model_asset_id: 5,
      ref_video_asset_id: 6,
    });
  });

  test("保存拆解 / 旁白：带 revision", async () => {
    put.mockResolvedValueOnce(rawRound()).mockResolvedValueOnce(rawScript());
    await saveMarketingReverse("r1", mapRound(rawRound()).reverse!, 1);
    expect(put.mock.calls[0][0]).toBe("/marketing-rounds/r1/reverse");
    expect(put.mock.calls[0][1]).toMatchObject({ reverse_revision: 1 });
    await saveMarketingScript("x1", ["a", "b"], 4);
    expect(put.mock.calls[1][0]).toBe("/marketing-scripts/x1");
    expect(put.mock.calls[1][1]).toEqual({ narrations: ["a", "b"], revision: 4 });
  });

  test("出片：幂等键放请求体；全部出片返回跳过项", async () => {
    post.mockResolvedValueOnce(rawScript({ status: "rendering" }));
    const script = await renderMarketingScript("x1", "key-1");
    expect(post.mock.calls[0][0]).toBe("/marketing-scripts/x1/render");
    expect(post.mock.calls[0][1]).toEqual({ idempotency_key: "key-1" });
    expect(script.status).toBe("rendering");

    post.mockResolvedValueOnce({
      round: rawRound(),
      skipped: [{ script_id: "x2", reason: "旁白有问题" }],
    });
    const result = await renderMarketingRound("r1", "key-2");
    expect(post.mock.calls[1][0]).toBe("/marketing-rounds/r1/render");
    expect(result.skipped).toEqual([{ scriptId: "x2", reason: "旁白有问题" }]);
    expect(result.round.no).toBe(2);
  });

  test("提示词与发到画布", async () => {
    get.mockResolvedValue({ format: "seedance", prompt: "[基本参数]" });
    expect((await getMarketingPrompt("x1", "m")).format).toBe("seedance");
    expect(get.mock.calls[0][0]).toBe("/marketing-scripts/x1/prompt");
    expect(get.mock.calls[0][1]).toEqual({ video_model: "m" });
    post.mockResolvedValue({ canvas_id: "c9" });
    expect(await sendMarketingScriptToCanvas("x1")).toBe("c9");
    expect(post.mock.calls[0][0]).toBe("/marketing-scripts/x1/to-canvas");
  });
});

describe("api/marketing：错误码", () => {
  test("68009 认作编辑冲突", () => {
    expect(isMarketingConflict(new ApiError("x", MARKETING_ERR.conflict, 409))).toBe(true);
    expect(isMarketingConflict(new ApiError("x", MARKETING_ERR.scriptBusy, 409))).toBe(false);
    expect(isMarketingConflict(new Error("x"))).toBe(false);
  });
});

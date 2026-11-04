import { afterEach, beforeEach, describe, expect, spyOn, test, type Mock } from "bun:test";

import {
  buildPreflightForm,
  createDrama,
  createMyPrompt,
  deleteMyPrompt,
  estimateJob,
  estimatePlanning,
  estimateSource,
  listDramaSkills,
  listMyPrompts,
  listPromptTemplates,
  mapDrama,
  mapJob,
  pausePlanning,
  preflightSource,
  resumePlanning,
  retryBuiltin,
  setPlanningRange,
  toCreateDramaBody,
  updateMyPrompt,
} from "@/api/drama";
import { DRAMA_ERR, dramaErrorMessage } from "@/api/drama/errors";
import type { BackendDramaView, BackendJobView } from "@/api/drama/type";
import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";

const rawPreflight = {
  source_token: "9f2c",
  filename: "霜刃.docx",
  chars: 4860000,
  kind_guess: "novel",
  kind_signals: "找到 1,215 个“第N章”",
  unit_kind: "chapter",
  units: [{ no: 1, title: "第1章 雪夜入门", chars: 3980 }],
  units_total: 1215,
  issues: [{ kind: "noise_removed", count: 312, message: "清掉 312 行网文站插入语" }],
  estimate: {
    episodes_min: 900,
    episodes_max: 1400,
    episodes_exact: false,
    credits: 3200,
    minutes: 40,
  },
};

describe("api/drama 开拍设置：预检", () => {
  let post: Mock<typeof service.post>;
  let upload: Mock<typeof service.upload>;
  let get: Mock<typeof service.get>;
  beforeEach(() => {
    post = spyOn(service, "post");
    upload = spyOn(service, "upload");
    get = spyOn(service, "get");
  });
  afterEach(() => {
    post.mockRestore();
    upload.mockRestore();
    get.mockRestore();
  });

  test("预检表单：文件走 file，粘贴走 text，可选字段为空不发", () => {
    const file = new File(["x"], "a.txt");
    const form = buildPreflightForm({ file });
    expect(form.get("file")).toBeInstanceOf(File);
    expect(form.has("text")).toBe(false);
    expect(form.has("source_kind")).toBe(false);
    const text = buildPreflightForm({
      text: "正文",
      sourceKind: "script",
      textModel: "m1",
      episodeSeconds: 90,
    });
    expect(text.get("text")).toBe("正文");
    expect(text.has("file")).toBe(false);
    expect(text.get("source_kind")).toBe("script");
    expect(text.get("text_model")).toBe("m1");
    expect(text.get("episode_seconds")).toBe("90");
  });

  test("预检：POST /drama-sources/preflight，结果转 camelCase，错误就地展示不弹全局提示", async () => {
    upload.mockResolvedValue(rawPreflight);
    const progress: number[] = [];
    const result = await preflightSource({ text: "正文" }, (ratio) => progress.push(ratio));
    expect(upload.mock.calls[0][0]).toBe("/drama-sources/preflight");
    const config = upload.mock.calls[0][2] as {
      silent?: boolean;
      onUploadProgress?: (e: { loaded: number; total?: number }) => void;
    };
    expect(config.silent).toBe(true);
    config.onUploadProgress?.({ loaded: 50, total: 100 });
    expect(progress).toEqual([0.5]);
    expect(result).toMatchObject({
      sourceToken: "9f2c",
      kindGuess: "novel",
      unitKind: "chapter",
      unitsTotal: 1215,
      units: [{ no: 1, title: "第1章 雪夜入门", chars: 3980 }],
      estimate: { episodesMin: 900, episodesMax: 1400, episodesExact: false, credits: 3200 },
    });
    expect(result.issues[0].message).toContain("312");
  });

  test("预检：units / issues 为 null 时兜底为空数组", async () => {
    upload.mockResolvedValue({ ...rawPreflight, units: null, issues: null });
    const result = await preflightSource({ text: "x" });
    expect(result.units).toEqual([]);
    expect(result.issues).toEqual([]);
  });

  test("草稿估价：POST /drama-sources/:token/estimate，请求体 snake_case", async () => {
    post.mockResolvedValue(rawPreflight.estimate);
    const est = await estimateSource("9f2c", {
      sourceKind: "novel",
      rangeEnd: 120,
      episodeSeconds: 60,
      textModel: "",
    });
    expect(post.mock.calls[0][0]).toBe("/drama-sources/9f2c/estimate");
    expect(post.mock.calls[0][1]).toEqual({
      source_kind: "novel",
      range_end: 120,
      episode_seconds: 60,
      text_model: "",
    });
    expect(est.credits).toBe(3200);
  });
});

describe("api/drama 开拍设置：建剧", () => {
  let post: Mock<typeof service.post>;
  beforeEach(() => {
    post = spyOn(service, "post");
  });
  afterEach(() => post.mockRestore());

  test("建剧请求体：JSON，带 source_token、画幅与四个环节提示词，空字段不发", () => {
    const body = toCreateDramaBody({
      sourceToken: "9f2c",
      sourceKind: "novel",
      rangeEnd: 0,
      ratio: "9:16",
      styleKey: "xianxia",
      genre: "",
      episodeSeconds: 60,
      textModel: "",
      prompts: {
        adapt: { source: "builtin" },
        assets: { source: "skill", skill: "cross" },
        asset_prompts: { source: "mine", id: "1f" },
      },
    });
    expect(body).toEqual({
      source_token: "9f2c",
      source_kind: "novel",
      range_end: 0,
      ratio: "9:16",
      style_key: "xianxia",
      episode_seconds: 60,
      text_model: "",
      prompts: {
        adapt: { source: "builtin" },
        assets: { source: "skill", skill: "cross" },
        asset_prompts: { source: "mine", id: "1f" },
      },
    });
    expect("target_episodes" in body).toBe(false);
  });

  test("建剧请求体：视频模型与图片模型用 snake_case，空的不发", () => {
    const base = {
      sourceToken: "t",
      sourceKind: "novel" as const,
      rangeEnd: 0,
      ratio: "9:16" as const,
      styleKey: "xianxia",
      episodeSeconds: 60,
    };
    const body = toCreateDramaBody({
      ...base,
      videoModel: "v1",
      imageModels: { character: "c1", scene: "", prop: "p1" },
    });
    expect(body.video_model).toBe("v1");
    expect(body.image_models).toEqual({ character: "c1", prop: "p1" });
    const bare = toCreateDramaBody({ ...base, videoModel: "", imageModels: {} });
    expect("video_model" in bare).toBe(false);
    expect("image_models" in bare).toBe(false);
  });

  test("建剧：POST /dramas（JSON），67002 等错误由调用方展示", async () => {
    post.mockResolvedValue({ id: "d1", title: "霜刃", episodes: null, active_jobs: null });
    const drama = await createDrama({
      sourceToken: "t",
      sourceKind: "script",
      rangeEnd: 0,
      ratio: "16:9",
      styleKey: "k",
      episodeSeconds: 60,
    });
    expect(post.mock.calls[0][0]).toBe("/dramas");
    expect((post.mock.calls[0][2] as { silent?: boolean }).silent).toBe(true);
    expect(drama.id).toBe("d1");
  });
});

describe("api/drama 开拍设置：提示词与技能", () => {
  let get: Mock<typeof service.get>;
  let post: Mock<typeof service.post>;
  let put: Mock<typeof service.put>;
  let del: Mock<typeof service.delete>;
  beforeEach(() => {
    get = spyOn(service, "get");
    post = spyOn(service, "post");
    put = spyOn(service, "put");
    del = spyOn(service, "delete");
  });
  afterEach(() => {
    get.mockRestore();
    post.mockRestore();
    put.mockRestore();
    del.mockRestore();
  });

  test("内置正文：GET /drama-prompt-templates", async () => {
    get.mockResolvedValue({
      items: [{ step: "adapt", pages: [{ key: "plan", label: "分集规划", body: "正文" }] }],
    });
    const items = await listPromptTemplates();
    expect(get.mock.calls[0][0]).toBe("/drama-prompt-templates");
    expect(items[0].pages[0].body).toBe("正文");
  });

  test("我的提示词：列表带 step，updated_at 转 camelCase，items 为 null 兜底", async () => {
    get.mockResolvedValue({
      items: [
        {
          id: "1f",
          step: "adapt",
          name: "我的改编",
          bodies: { plan: "p" },
          revision: 1,
          updated_at: "t",
        },
      ],
    });
    const items = await listMyPrompts("adapt");
    expect(get.mock.calls[0][0]).toBe("/drama-prompts");
    expect(get.mock.calls[0][1]).toEqual({ step: "adapt" });
    expect(items[0]).toMatchObject({ id: "1f", updatedAt: "t", revision: 1 });
    get.mockResolvedValue({ items: null });
    expect(await listMyPrompts()).toEqual([]);
  });

  test("我的提示词：新建 POST、修改 PUT 带 revision、删除 DELETE；错误就地处理", async () => {
    post.mockResolvedValue({
      id: "1f",
      step: "video",
      name: "n",
      bodies: { storyboard: "b" },
      revision: 1,
      updated_at: "t",
    });
    const created = await createMyPrompt({
      step: "video",
      name: "n",
      bodies: { storyboard: "b" },
    });
    expect(post.mock.calls[0][0]).toBe("/drama-prompts");
    expect(post.mock.calls[0][1]).toEqual({
      step: "video",
      name: "n",
      bodies: { storyboard: "b" },
    });
    expect((post.mock.calls[0][2] as { silent?: boolean }).silent).toBe(true);
    expect(created.id).toBe("1f");

    put.mockResolvedValue({
      id: "1f",
      step: "video",
      name: "n2",
      bodies: { storyboard: "b" },
      revision: 2,
      updated_at: "t",
    });
    await updateMyPrompt("1f", {
      step: "video",
      name: "n2",
      bodies: { storyboard: "b" },
      revision: 1,
    });
    expect(put.mock.calls[0][0]).toBe("/drama-prompts/1f");
    expect((put.mock.calls[0][1] as { revision: number }).revision).toBe(1);

    del.mockResolvedValue(undefined);
    await deleteMyPrompt("1f");
    expect(del.mock.calls[0][0]).toBe("/drama-prompts/1f");
  });

  test("可用技能：GET /drama-skills?step=", async () => {
    get.mockResolvedValue({ items: [{ name: "a", title: "A", description: "d" }] });
    const items = await listDramaSkills("assets");
    expect(get.mock.calls[0][0]).toBe("/drama-skills");
    expect(get.mock.calls[0][1]).toEqual({ step: "assets" });
    expect(items).toEqual([{ name: "a", title: "A", description: "d" }]);
  });
});

describe("api/drama 开拍设置：自动规划", () => {
  let post: Mock<typeof service.post>;
  beforeEach(() => {
    post = spyOn(service, "post");
  });
  afterEach(() => post.mockRestore());

  test("暂停 / 继续 / 扩大范围 / 预估 / 改用内置重试的路径与请求体", async () => {
    post.mockResolvedValue({});
    await pausePlanning("d1");
    expect(post.mock.calls[0][0]).toBe("/dramas/d1/planning/pause");
    await resumePlanning("d1");
    expect(post.mock.calls[1][0]).toBe("/dramas/d1/planning/resume");
    await setPlanningRange("d1", 240);
    expect(post.mock.calls[2][0]).toBe("/dramas/d1/planning/range");
    expect(post.mock.calls[2][1]).toEqual({ range_end: 240 });
    post.mockResolvedValue({
      episodes_min: 1,
      episodes_max: 2,
      episodes_exact: false,
      credits: 9,
      minutes: 3,
    });
    const est = await estimatePlanning("d1", 0);
    expect(post.mock.calls[3][0]).toBe("/dramas/d1/planning/estimate");
    expect(post.mock.calls[3][1]).toEqual({ range_end: 0 });
    expect(est).toMatchObject({ credits: 9, minutes: 3, episodesMax: 2 });
    post.mockResolvedValue({});
    await retryBuiltin("d1", "adapt");
    expect(post.mock.calls[4][0]).toBe("/dramas/d1/planning/retry-builtin");
    expect(post.mock.calls[4][1]).toEqual({ step: "adapt" });
  });

  test("任务估价：返回积分与衔接缺口 gap_episodes", async () => {
    post.mockResolvedValue({ credits: 66, gap_episodes: [8] });
    const est = await (
      await import("@/api/drama")
    ).estimateJobDetail("d1", {
      step: "scripts",
      episodes: [9],
    });
    expect(est).toEqual({ credits: 66, gapEpisodes: [8] });
    post.mockResolvedValue({ credits: 5 });
    expect(await estimateJob("d1", { step: "breakdown", episodes: [1] })).toBe(5);
  });
});

describe("api/drama 开拍设置：视图新字段映射", () => {
  const raw = {
    id: "1",
    title: "霜刃",
    canvas_id: "c",
    status: "outlined",
    source_kind: "novel",
    genre: "",
    style_key: "k",
    style_custom: "",
    ratio: "9:16",
    target_episodes: 0,
    episode_seconds: 60,
    text_model: "",
    outline_md: "",
    characters_md: "",
    revision: 1,
    episodes: [
      {
        no: 1,
        title: "雪夜",
        synopsis: "s",
        script_status: "empty",
        locked: false,
        revision: 1,
        updated_at: "t",
        hook: "钩",
        cliffhanger: "悬",
        unit_from: 3,
        unit_to: 5,
      },
      {
        no: 2,
        title: "老集",
        synopsis: "",
        script_status: "ready",
        locked: false,
        revision: 1,
        updated_at: "t",
        unit_from: 0,
        unit_to: 0,
      },
    ],
    active_jobs: null,
    created_at: "t",
    updated_at: "t",
    units_total: 1215,
    unit_kind: "chapter",
    source_chars: 4860000,
    phase: "paused",
    plan_cursor: 44,
    range_end: 1215,
    pause_reason: "规划失败",
    pause_code: "custom_prompt",
    prompt_settings: {
      adapt: { source: "builtin", label: "内置" },
      assets: { source: "skill", skill: "x", label: "技能 · X", missing: true },
      asset_prompts: { source: "builtin", label: "内置" },
      video: { source: "builtin", label: "内置" },
    },
  } as unknown as BackendDramaView;

  test("详情：phase、规划游标、暂停原因、提示词设置与集的钩子 / 悬念 / 对应原文", () => {
    const drama = mapDrama(raw);
    expect(drama).toMatchObject({
      phase: "paused",
      planCursor: 44,
      rangeEnd: 1215,
      pauseReason: "规划失败",
      pauseCode: "custom_prompt",
      unitsTotal: 1215,
      unitKind: "chapter",
      sourceChars: 4860000,
    });
    expect(drama.promptSettings.assets.missing).toBe(true);
    expect(drama.episodes[0]).toMatchObject({
      hook: "钩",
      cliffhanger: "悬",
      unitFrom: 3,
      unitTo: 5,
    });
    // 0 = 未关联单元，前端统一成 null
    expect(drama.episodes[1]).toMatchObject({ hook: "", cliffhanger: "", unitFrom: null });
  });

  test("详情：video_model / image_models 映射为 videoModel / imageModels，老数据为空串", () => {
    const withModels = mapDrama({
      ...raw,
      video_model: "v1",
      image_models: { character: "c1", scene: "s1" },
    } as unknown as BackendDramaView);
    expect(withModels.videoModel).toBe("v1");
    expect(withModels.imageModels).toEqual({ character: "c1", scene: "s1", prop: "" });
    const old = mapDrama(raw);
    expect(old.videoModel).toBe("");
    expect(old.imageModels).toEqual({ character: "", scene: "", prop: "" });
  });

  test("老后端没有新字段：phase 按 done，提示词全是内置，任务线按用户线", () => {
    const old = mapDrama({
      ...raw,
      phase: undefined,
      prompt_settings: undefined,
      unit_kind: undefined,
      pause_code: undefined,
    } as unknown as BackendDramaView);
    expect(old.phase).toBe("done");
    expect(old.pauseCode).toBe("");
    expect(old.promptSettings.adapt).toMatchObject({ source: "builtin" });
    expect(old.unitKind).toBe("episode");
    const job = mapJob({
      id: "j",
      step: "plan",
      episodes: null,
      status: "running",
      progress: 1,
      credits: 0,
      error_code: 0,
      error_message: "",
      created_at: "t",
      finished_at: null,
      lane: "pipeline",
      batch_from: 8,
      batch_to: 14,
    } as BackendJobView);
    expect(job).toMatchObject({ lane: "pipeline", batchFrom: 8, batchTo: 14 });
    expect(
      mapJob({ ...(job as unknown as BackendJobView), lane: undefined, step: "scripts" } as never)
        .lane,
    ).toBe("user");
  });
});

describe("api/drama 开拍设置：错误码 67014–67022", () => {
  test("兜底文案", () => {
    const err = (code: number) => new ApiError("", code, 400);
    expect(dramaErrorMessage(err(DRAMA_ERR.sourceExpired))).toContain("重新上传");
    expect(dramaErrorMessage(err(DRAMA_ERR.promptNotFound))).toContain("不存在");
    expect(dramaErrorMessage(err(DRAMA_ERR.promptInvalid))).toBeTruthy();
    expect(dramaErrorMessage(err(DRAMA_ERR.skillUnavailable))).toContain("技能");
    expect(dramaErrorMessage(err(DRAMA_ERR.phaseNotAllowed))).toBeTruthy();
    expect(dramaErrorMessage(err(DRAMA_ERR.contextTooSmall))).toContain("128k");
    expect(DRAMA_ERR.sourceExpired).toBe(67014);
    expect(DRAMA_ERR.contextTooSmall).toBe(67022);
  });

  test("67017 后端 Msg 优先（写了具体哪里不对）", () => {
    expect(dramaErrorMessage(new ApiError("名称重复", DRAMA_ERR.promptInvalid, 400))).toBe(
      "名称重复",
    );
  });
});

export type { BackendDramaView };

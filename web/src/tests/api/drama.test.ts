import { afterEach, beforeEach, describe, expect, spyOn, test, type Mock } from "bun:test";

import {
  buildCreateForm,
  createDrama,
  estimateJob,
  listActiveJobs,
  listDramas,
  mapDrama,
  mapJob,
  submitJob,
  toJobBody,
  toUpdateDramaBody,
  toUpdateEpisodeBody,
  updateDrama,
  updateEpisode,
} from "@/api/drama";
import type { BackendDramaView, BackendJobView } from "@/api/drama/type";
import service from "@/utils/requests/service";

const rawJob = (patch: Partial<BackendJobView> = {}): BackendJobView => ({
  id: "7f3e",
  step: "breakdown",
  episodes: [2, 3],
  status: "running",
  progress: 40,
  credits: 0,
  error_code: 0,
  error_message: "",
  created_at: "2026-10-28T10:00:00Z",
  finished_at: null,
  ...patch,
});

const rawDrama = (patch: Partial<BackendDramaView> = {}): BackendDramaView => ({
  id: "1a2b",
  title: "双鱼玉佩",
  canvas_id: "c9f0",
  status: "ready",
  source_kind: "script",
  genre: "悬疑推理",
  style_key: "real_city_film",
  style_custom: "",
  ratio: "9:16",
  target_episodes: 0,
  episode_seconds: 60,
  text_model: "",
  outline_md: "大纲",
  characters_md: "人物",
  revision: 3,
  episodes: [
    {
      no: 1,
      title: "村里的女人",
      synopsis: "",
      script_status: "ready",
      locked: true,
      revision: 2,
      updated_at: "2026-10-28T10:00:00Z",
    },
  ],
  active_jobs: [rawJob()],
  created_at: "2026-10-28T09:00:00Z",
  updated_at: "2026-10-28T10:00:00Z",
  ...patch,
});

describe("api/drama：mapper", () => {
  test("详情 snake→camel，任务与集一并映射", () => {
    const drama = mapDrama(rawDrama());
    expect(drama.canvasId).toBe("c9f0");
    expect(drama.styleKey).toBe("real_city_film");
    expect(drama.episodeSeconds).toBe(60);
    expect(drama.outlineMd).toBe("大纲");
    expect(drama.episodes[0]).toMatchObject({ no: 1, scriptStatus: "ready", locked: true });
    expect(drama.activeJobs[0]).toMatchObject({ id: "7f3e", step: "breakdown", progress: 40 });
  });

  test("episodes / active_jobs 为 null 时兜底为空数组", () => {
    const drama = mapDrama(rawDrama({ episodes: null, active_jobs: null }));
    expect(drama.episodes).toEqual([]);
    expect(drama.activeJobs).toEqual([]);
  });

  test("任务：episodes 为 null 兜底空数组，finished_at 保持 null", () => {
    const job = mapJob(rawJob({ episodes: null }));
    expect(job.episodes).toEqual([]);
    expect(job.finishedAt).toBeNull();
    expect(mapJob(rawJob({ status: "failed", error_code: 67008 })).errorCode).toBe(67008);
  });
});

describe("api/drama：请求体", () => {
  test("新建（文件）：file 与 style_key 必带，空字段不发", () => {
    const file = new File(["第1集"], "剧本.txt", { type: "text/plain" });
    const form = buildCreateForm({ file, styleKey: "real_city_film", genre: "", ratio: "" });
    expect(form.get("file")).toBeInstanceOf(File);
    expect(form.get("text")).toBeNull();
    expect(form.get("style_key")).toBe("real_city_film");
    expect(form.has("genre")).toBe(false);
    expect(form.has("ratio")).toBe(false);
    expect(form.has("target_episodes")).toBe(false);
    // 没选模型：不传，后端跟随后台第一个
    expect(form.has("text_model")).toBe(false);
  });

  test("新建（文本）：text 与创作设置转 snake_case 字符串", () => {
    const form = buildCreateForm({
      text: "正文",
      title: " 我的短剧 ",
      styleKey: "custom",
      styleCustom: "赛博",
      genre: "悬疑推理",
      ratio: "16:9",
      targetEpisodes: 10,
      episodeSeconds: 90,
      textModel: "pro",
    });
    expect(form.get("text")).toBe("正文");
    expect(form.get("file")).toBeNull();
    expect(form.get("title")).toBe("我的短剧");
    expect(form.get("style_custom")).toBe("赛博");
    expect(form.get("ratio")).toBe("16:9");
    expect(form.get("target_episodes")).toBe("10");
    expect(form.get("episode_seconds")).toBe("90");
    expect(form.get("text_model")).toBe("pro");
  });

  test("改短剧 / 改集：只带传了的字段", () => {
    expect(toUpdateDramaBody({ revision: 3, title: "新名" })).toEqual({
      revision: 3,
      title: "新名",
    });
    expect(toUpdateDramaBody({ revision: 3, textModel: "", outlineMd: "x" })).toEqual({
      revision: 3,
      text_model: "",
      outline_md: "x",
    });
    expect(toUpdateEpisodeBody({ revision: 2, scriptMd: "正文" })).toEqual({
      revision: 2,
      script_md: "正文",
    });
  });

  test("任务请求体：比例为空串不发", () => {
    expect(toJobBody({ step: "breakdown", episodes: [1], ratio: "" })).toEqual({
      step: "breakdown",
      episodes: [1],
    });
    expect(toJobBody({ step: "breakdown", episodes: [1], ratio: "9:16" })).toEqual({
      step: "breakdown",
      episodes: [1],
      ratio: "9:16",
    });
  });
});

describe("api/drama：请求", () => {
  let get: Mock<typeof service.get>;
  let post: Mock<typeof service.post>;
  let patch: Mock<typeof service.patch>;
  let upload: Mock<typeof service.upload>;
  beforeEach(() => {
    get = spyOn(service, "get");
    post = spyOn(service, "post");
    patch = spyOn(service, "patch");
    upload = spyOn(service, "upload");
  });
  afterEach(() => {
    get.mockRestore();
    post.mockRestore();
    patch.mockRestore();
    upload.mockRestore();
  });

  test("列表：list/page_size 映射，list 为 null 兜底", async () => {
    get.mockResolvedValue({ list: null, total: 0, page: 1, page_size: 20 });
    const page = await listDramas({ keyword: " 双 ", page: 2 });
    expect(page).toEqual({ items: [], total: 0, page: 1, pageSize: 20 });
    expect(get.mock.calls[0][0]).toBe("/dramas");
    expect(get.mock.calls[0][1]).toMatchObject({ keyword: "双", page: 2 });
  });

  test("列表项：封面空串变 null", async () => {
    get.mockResolvedValue({
      list: [
        {
          id: "1",
          title: "t",
          status: "ready",
          episode_count: 6,
          locked_count: 1,
          cover_url: "",
          updated_at: "x",
        },
      ],
      total: 1,
      page: 1,
      page_size: 20,
    });
    const page = await listDramas();
    expect(page.items[0]).toMatchObject({ episodeCount: 6, lockedCount: 1, coverUrl: null });
  });

  test("新建走 multipart 上传且静默（67002 / 67003 就地提示）", async () => {
    upload.mockResolvedValue(rawDrama());
    const drama = await createDrama({ text: "正文", styleKey: "real_city_film" });
    expect(upload.mock.calls[0][0]).toBe("/dramas");
    expect(upload.mock.calls[0][1]).toBeInstanceOf(FormData);
    expect(upload.mock.calls[0][2]).toMatchObject({ silent: true });
    expect(drama.id).toBe("1a2b");
  });

  test("改短剧 / 改集静默（409 由调用方弹冲突提示）", async () => {
    patch.mockResolvedValue(rawDrama());
    await updateDrama("1a2b", { revision: 3, title: "x" });
    expect(patch.mock.calls[0][0]).toBe("/dramas/1a2b");
    expect(patch.mock.calls[0][2]).toMatchObject({ silent: true });
    patch.mockResolvedValue({
      ...rawDrama().episodes![0],
      script_md: "正文",
    });
    const episode = await updateEpisode("1a2b", 1, { revision: 2, scriptMd: "正文" });
    expect(patch.mock.calls[1][0]).toBe("/dramas/1a2b/episodes/1");
    expect(episode.scriptMd).toBe("正文");
  });

  test("估价取 credits；提交返回任务", async () => {
    post.mockResolvedValue({ credits: 12 });
    expect(await estimateJob("1a2b", { step: "scripts", episodes: [3, 4] })).toBe(12);
    expect(post.mock.calls[0][0]).toBe("/dramas/1a2b/estimate");
    post.mockResolvedValue(rawJob());
    const job = await submitJob("1a2b", { step: "breakdown", episodes: [2], ratio: "9:16" });
    expect(post.mock.calls[1][0]).toBe("/dramas/1a2b/jobs");
    expect(job.status).toBe("running");
  });

  test("对账：status=active，items 为 null 兜底", async () => {
    get.mockResolvedValue({ items: null });
    expect(await listActiveJobs("1a2b")).toEqual([]);
    expect(get.mock.calls[0][0]).toBe("/dramas/1a2b/jobs");
    expect(get.mock.calls[0][1]).toEqual({ status: "active" });
  });
});

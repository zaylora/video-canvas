import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  spyOn,
  test,
} from "bun:test";

import type { CreateDramaInput, SourcePreflight } from "@/api/drama/type";
import type { PromptLibrary } from "@/components/drama/prompts/use-prompt-library";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import service from "@/utils/requests/service";
import type { StartBody as StartBodyType } from "@/pages/drama/start-dialog";
import type { useModelsStore as useModelsStoreType } from "@/store/models";

/**
 * 开拍设置弹窗主体：用 jsdom 挂载真实组件，检查来源行、规则（剧本置灰 / 时长锁定 / 上下文 < 128k 禁用 /
 * 识别不出禁用开始）以及点「开始」时提交的建剧请求体。接口用 mock。
 */

let dom: DomEnv;
let StartBody: typeof StartBodyType;
let useModelsStore: typeof useModelsStoreType;
beforeAll(async () => {
  dom = await installDom();
  StartBody = (await import("@/pages/drama/start-dialog")).StartBody;
  useModelsStore = (await import("@/store/models")).useModelsStore;
});
afterAll(() => {
  // models store 是全局单例：清掉本文件灌进去的模型，免得后面的测试（如连线规则）读到残留
  useModelsStore.setState({ byKind: {} });
  dom.restore();
});

const pre = (patch: Partial<SourcePreflight> = {}): SourcePreflight => ({
  sourceToken: "tok",
  filename: "霜刃.docx",
  chars: 4860000,
  kindGuess: "novel",
  kindSignals: "找到 1,215 个“第N章”",
  unitKind: "chapter",
  units: [],
  unitsTotal: 1215,
  issues: [{ kind: "noise_removed", count: 312, message: "清掉 312 行网文站插入语" }],
  estimate: {
    episodesMin: 900,
    episodesMax: 1400,
    episodesExact: false,
    credits: 3200,
    minutes: 40,
  },
  ...patch,
});

const library: PromptLibrary = {
  ready: true,
  templates: [],
  mine: { adapt: [], assets: [], asset_prompts: [], video: [] },
  skills: { adapt: [], assets: [], asset_prompts: [], video: [] },
  save: async () => {
    throw new Error("not used");
  },
  remove: async () => {},
};

const model = (key: string, window: number) =>
  ({
    key,
    kind: "agent",
    label: key,
    pricing: {},
    capabilities: { context: { window, output: 8000 } },
  }) as never;

let get: ReturnType<typeof spyOn>;
beforeEach(() => {
  get = spyOn(service, "get").mockResolvedValue({
    genres: [{ key: "xianxia", name: "玄幻修仙" }],
    styles: [{ key: "real_city_film", name: "都市电影", prompt: "" }],
  } as never);
  useModelsStore.setState({
    byKind: {
      agent: {
        status: "ready",
        loadedAt: Date.now(),
        models: [model("big", 200_000), model("small", 64_000)],
      },
    },
  });
  dom.win.localStorage.clear();
});
afterEach(() => get.mockRestore());

const mountBody = async (
  preflight: SourcePreflight,
  onSubmit: (input: CreateDramaInput) => Promise<void> = async () => {},
) => {
  const m = await dom.mount(
    <StartBody pre={preflight} library={library} onCancel={() => {}} onSubmit={onSubmit} />,
  );
  await dom.flush();
  return m;
};

const buttons = (container: HTMLElement) => [...container.querySelectorAll("button")];
const byText = (container: HTMLElement, text: string) =>
  buttons(container).find((b) => (b.textContent ?? "").trim() === text);
const startButton = (container: HTMLElement) => byText(container, "开始") as HTMLButtonElement;

describe("开拍设置：来源与基本规则", () => {
  test("来源行：文件名、字数、单元数、识别结果、依据与处理记录", async () => {
    const m = await mountBody(pre());
    const text = m.container.textContent ?? "";
    expect(text).toContain("霜刃.docx");
    expect(text).toContain("486 万字");
    expect(text).toContain("1,215 章");
    expect(text).toContain("小说");
    expect(text).toContain("依据：找到 1,215 个“第N章”");
    expect(text).toContain("已处理 1 处");
    expect(text).toContain("清掉 312 行网文站插入语");
    await m.unmount();
  });

  test("小说：规划范围显示预估（集数、积分、耗时），画幅默认 9:16，只有三档", async () => {
    const m = await mountBody(pre());
    const text = m.container.textContent ?? "";
    expect(text).toContain("预计约 900–1,400 集");
    expect(text).toContain("3,200");
    expect(text).toContain("约 40 分钟");
    const ratios = buttons(m.container).filter((b) => b.hasAttribute("aria-pressed"));
    expect(ratios.map((b) => b.textContent)).toEqual(["9:16", "16:9", "4:3"]);
    expect(ratios[0].getAttribute("aria-pressed")).toBe("true");
    await m.unmount();
  });

  test("识别不出：胶囊显示「请选择」，开始禁用并写明原因", async () => {
    const m = await mountBody(pre({ kindGuess: "unknown" }));
    expect(byText(m.container, "请选择")).toBeTruthy();
    const start = startButton(m.container);
    expect(start.disabled).toBe(true);
    expect(start.title).toBe("先确认是剧本还是小说");
    await m.unmount();
  });

  test("剧本带「第N集」：剧本改编行置灰、单集时长锁成「按原分集 · 80 集」、规划范围一次切完", async () => {
    const m = await mountBody(
      pre({
        kindGuess: "script",
        unitKind: "episode",
        unitsTotal: 80,
        estimate: {
          episodesMin: 80,
          episodesMax: 80,
          episodesExact: true,
          credits: 40,
          minutes: 2,
        },
      }),
    );
    const adapt = byText(m.container, "剧本直接导入") as HTMLButtonElement;
    expect(adapt.disabled).toBe(true);
    expect(adapt.title).toBe("剧本直接导入，不改编");
    const seconds = byText(m.container, "按原分集 · 80 集") as HTMLButtonElement;
    expect(seconds.disabled).toBe(true);
    expect(m.container.textContent).toContain("剧本一次切完 · 80 集");
    expect(m.container.textContent).not.toContain("规划到第");
    await m.unmount();
  });

  test("剧本没有分集标记：时长可选，只作参考；其他环节的提示词不置灰", async () => {
    const m = await mountBody(pre({ kindGuess: "script", unitKind: "scene", unitsTotal: 30 }));
    const seconds = byText(m.container, "60s/集（参考）") as HTMLButtonElement;
    expect(seconds).toBeTruthy();
    expect(seconds.disabled).toBe(false);
    const rowButton = (name: string) =>
      [...m.container.querySelectorAll("span")]
        .find((el) => el.textContent === name)
        ?.parentElement?.querySelector("button") as HTMLButtonElement;
    expect(rowButton("资产拆解").disabled).toBe(false);
    expect(rowButton("剧本改编").disabled).toBe(true);
    await m.unmount();
  });
});

describe("开拍设置：文本模型", () => {
  test("记住的模型上下文低于 128k：写出原因，开始禁用", async () => {
    dom.win.localStorage.setItem("drama-start-prefs", JSON.stringify({ textModel: "small" }));
    const m = await mountBody(pre());
    expect(m.container.textContent).toContain("上下文低于 128k，装不下一批完整原文");
    expect(startButton(m.container).disabled).toBe(true);
    await m.unmount();
  });

  test("模型下拉里 < 128k 的项禁用并带原因，≥ 128k 的可选", async () => {
    const m = await mountBody(pre());
    await dom.click(byText(m.container, "big"));
    await dom.flush();
    const items = [...dom.win.document.body.querySelectorAll("button")].filter((b) =>
      ["big", "small"].some(
        (name) => (b.textContent ?? "").startsWith(name) && b.className.includes("min-h-8"),
      ),
    );
    const small = items.find((b) => (b.textContent ?? "").startsWith("small")) as HTMLButtonElement;
    const big = items.find((b) => (b.textContent ?? "").startsWith("big")) as HTMLButtonElement;
    expect(small.disabled).toBe(true);
    expect(small.title).toBe("上下文低于 128k，装不下一批完整原文");
    expect(big.disabled).toBe(false);
    await m.unmount();
  });
});

describe("开拍设置：点「开始」建剧", () => {
  test("小说：请求体带 token、类型、范围（全部 = 0）、画幅、画风、时长、四环节提示词", async () => {
    const calls: CreateDramaInput[] = [];
    const m = await mountBody(pre(), async (input) => {
      calls.push(input);
    });
    await dom.click(byText(m.container, "16:9"));
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({
      sourceToken: "tok",
      sourceKind: "novel",
      rangeEnd: 0,
      ratio: "16:9",
      styleKey: "real_city_film",
      episodeSeconds: 60,
      textModel: "",
      prompts: {
        adapt: { source: "builtin" },
        assets: { source: "builtin" },
        asset_prompts: { source: "builtin" },
        video: { source: "builtin" },
      },
    });
    await m.unmount();
  });

  test("建剧成功后记住画幅；下次弹窗预填", async () => {
    const m = await mountBody(pre());
    await dom.click(byText(m.container, "4:3"));
    await dom.click(startButton(m.container));
    await dom.flush();
    await m.unmount();
    const again = await mountBody(pre());
    const ratios = buttons(again.container).filter((b) => b.hasAttribute("aria-pressed"));
    expect(ratios.find((b) => b.textContent === "4:3")?.getAttribute("aria-pressed")).toBe("true");
    await again.unmount();
  });

  test("建剧失败：错误就地显示在弹窗里，开始恢复可点", async () => {
    const { ApiError } = await import("@/utils/requests/request");
    const m = await mountBody(pre(), async () => {
      throw new ApiError("", 67022, 400);
    });
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(m.container.querySelector('[role="alert"]')?.textContent).toContain("128k");
    expect(startButton(m.container).disabled).toBe(false);
    await m.unmount();
  });

  test("剧本型：提交时剧本改编强制内置，规划范围恒为全部", async () => {
    const calls: CreateDramaInput[] = [];
    dom.win.localStorage.setItem(
      "drama-start-prefs",
      JSON.stringify({ prompts: { adapt: { source: "mine", id: "1f" } } }),
    );
    const m = await mountBody(
      pre({ kindGuess: "script", unitKind: "episode", unitsTotal: 80 }),
      async (input) => {
        calls.push(input);
      },
    );
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls[0].sourceKind).toBe("script");
    expect(calls[0].rangeEnd).toBe(0);
    expect(calls[0].prompts?.adapt).toEqual({ source: "builtin" });
    await m.unmount();
  });
});

describe("开拍设置：视频 / 图片模型", () => {
  const videoModel = (key: string, withImageRef: boolean) =>
    ({
      key,
      kind: "video",
      label: key,
      pricing: {},
      capabilities: {
        ops: withImageRef ? ["t2v", "i2v"] : ["t2v"],
        refs: { image: { on: withImageRef, max: withImageRef ? 9 : 0, max_mb: 10 } },
        params: { duration: { type: "number", label: "时长", open: true, min: 4, max: 15 } },
      },
    }) as never;
  const imageModel = (key: string) =>
    ({ key, kind: "image", label: key, pricing: {}, capabilities: {} }) as never;

  beforeEach(() => {
    const byKind = useModelsStore.getState().byKind;
    useModelsStore.setState({
      byKind: {
        ...byKind,
        video: {
          status: "ready",
          loadedAt: Date.now(),
          models: [videoModel("vid-a", true), videoModel("vid-text", false)],
        },
        image: {
          status: "ready",
          loadedAt: Date.now(),
          models: [imageModel("img-a"), imageModel("img-b")],
        },
      },
    });
  });

  const menuRows = () =>
    [...dom.win.document.body.querySelectorAll("button")].filter((b) =>
      b.className.includes("min-h-10"),
    ) as HTMLButtonElement[];

  const pickFromPopover = async (trigger: HTMLElement | undefined, name: string) => {
    await dom.click(trigger);
    await dom.flush();
    await dom.click(menuRows().find((b) => (b.textContent ?? "").startsWith(name)));
    await dom.flush();
  };

  test("文本模型下面依次是视频模型、资产图片模型（人物 / 场景 / 物品）", async () => {
    const m = await mountBody(pre());
    const text = m.container.textContent ?? "";
    expect(text.indexOf("文本模型")).toBeLessThan(text.indexOf("视频模型"));
    expect(text.indexOf("视频模型")).toBeLessThan(text.indexOf("资产图片模型"));
    expect(byText(m.container, "人物 · 跟随默认")).toBeTruthy();
    expect(byText(m.container, "场景 · 跟随默认")).toBeTruthy();
    expect(byText(m.container, "物品 · 跟随默认")).toBeTruthy();
    await m.unmount();
  });

  test("选视频模型和图片模型后点开始：请求体带上它们", async () => {
    const calls: CreateDramaInput[] = [];
    dom.win.localStorage.setItem(
      "drama-start-prefs",
      JSON.stringify({ styleKey: "real_city_film" }),
    );
    const m = await mountBody(pre(), async (input) => {
      calls.push(input);
    });
    await pickFromPopover(byText(m.container, "跟随默认"), "vid-a");
    await pickFromPopover(byText(m.container, "人物 · 跟随默认"), "img-a");
    await pickFromPopover(byText(m.container, "物品 · 跟随默认"), "img-b");
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls[0].videoModel).toBe("vid-a");
    expect(calls[0].imageModels).toMatchObject({ character: "img-a", prop: "img-b" });
    expect(calls[0].imageModels?.scene).toBeUndefined();
    await m.unmount();
  });

  test("不支持参考图的视频模型置灰并写原因，可用的副文案带时长上限", async () => {
    const m = await mountBody(pre());
    await dom.click(byText(m.container, "跟随默认"));
    await dom.flush();
    const rows = menuRows();
    const ok = rows.find((b) => (b.textContent ?? "").startsWith("vid-a"))!;
    const bad = rows.find((b) => (b.textContent ?? "").startsWith("vid-text"))!;
    expect(ok.disabled).toBe(false);
    expect(ok.textContent).toContain("≤15s");
    expect(bad.disabled).toBe(true);
    expect(bad.textContent).toContain("不支持参考图");
    expect(bad.title).toContain("不支持参考图");
    await m.unmount();
  });

  test("记住的模型已下线：读取时回落为不指定，提交不带它", async () => {
    dom.win.localStorage.setItem(
      "drama-start-prefs",
      JSON.stringify({
        styleKey: "real_city_film",
        videoModel: "gone",
        imageModels: { character: "gone", scene: "img-a" },
      }),
    );
    const calls: CreateDramaInput[] = [];
    const m = await mountBody(pre(), async (input) => {
      calls.push(input);
    });
    expect(byText(m.container, "人物 · 跟随默认")).toBeTruthy();
    expect(byText(m.container, "场景 · img-a")).toBeTruthy();
    await dom.click(startButton(m.container));
    await dom.flush();
    expect(calls[0].videoModel).toBeUndefined();
    expect(calls[0].imageModels?.character).toBeUndefined();
    expect(calls[0].imageModels?.scene).toBe("img-a");
    await m.unmount();
  });
});

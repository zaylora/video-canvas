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

import { installDom, type DomEnv } from "@/tests/helpers/dom";
import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";
import type { UploadCard as UploadCardType } from "@/pages/drama/upload-card";
import type {
  MemoryRouter as MemoryRouterType,
  Routes as RoutesType,
  Route as RouteType,
} from "react-router";
import type { useModelsStore as useModelsStoreType } from "@/store/models";

/**
 * 首页创作卡 → 预检 → 开拍设置弹窗 → 建剧：用 jsdom 挂载真实组件走一遍，接口全部 mock。
 */

let dom: DomEnv;
let UploadCard: typeof UploadCardType;
let MemoryRouter: typeof MemoryRouterType;
let Routes: typeof RoutesType;
let Route: typeof RouteType;
let useModelsStore: typeof useModelsStoreType;
beforeAll(async () => {
  dom = await installDom();
  UploadCard = (await import("@/pages/drama/upload-card")).UploadCard;
  const router = await import("react-router");
  MemoryRouter = router.MemoryRouter;
  Routes = router.Routes;
  Route = router.Route;
  useModelsStore = (await import("@/store/models")).useModelsStore;
});
afterAll(() => dom.restore());

const rawPreflight = {
  source_token: "tok1",
  filename: "粘贴文本.txt",
  chars: 12000,
  kind_guess: "novel",
  kind_signals: "找到 3 个“第N章”",
  unit_kind: "chapter",
  units: [{ no: 1, title: "第1章", chars: 4000 }],
  units_total: 3,
  issues: null,
  estimate: { episodes_min: 6, episodes_max: 9, episodes_exact: false, credits: 80, minutes: 3 },
};

let get: ReturnType<typeof spyOn>;
let upload: ReturnType<typeof spyOn>;
let post: ReturnType<typeof spyOn>;
beforeEach(() => {
  get = spyOn(service, "get").mockImplementation((async (url: string) => {
    if (url === "/drama-styles") {
      return {
        genres: [],
        styles: [{ key: "real_city_film", name: "都市电影", prompt: "" }],
      };
    }
    return { items: [] };
  }) as never);
  upload = spyOn(service, "upload");
  post = spyOn(service, "post");
  useModelsStore.setState({
    byKind: { agent: { status: "ready", loadedAt: Date.now(), models: [] } },
  });
  dom.win.localStorage.clear();
});
afterEach(() => {
  get.mockRestore();
  upload.mockRestore();
  post.mockRestore();
});

const mountCard = async () => {
  const m = await dom.mount(
    <MemoryRouter>
      <Routes>
        <Route path="/" element={<UploadCard />} />
        <Route path="/drama/:id" element={<div data-testid="workspace">工作台</div>} />
      </Routes>
    </MemoryRouter>,
  );
  return m;
};
const body = () => dom.win.document.body;
const sendButton = (c: HTMLElement) =>
  c.querySelector('button[aria-label="开始"]') as HTMLButtonElement;
const dialogStart = () =>
  [...body().querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === "开始") as
    | HTMLButtonElement
    | undefined;
const pasteText = async (c: HTMLElement, text: string) => {
  await dom.click(
    [...c.querySelectorAll('[role="tab"]')].find((t) => t.textContent === "粘贴正文"),
  );
  await dom.type(c.querySelector("textarea"), text);
};

describe("创作卡：底栏只剩发送键", () => {
  test("不再有题材 / 画风 / 比例 / 集数 / 模型胶囊，字数提示改成「500 万字以内」", async () => {
    const m = await mountCard();
    const buttons = [...m.container.querySelectorAll("button")].map((b) => b.textContent?.trim());
    for (const gone of ["题材", "都市电影", "9:16", "自动集数", "Agent 模型"]) {
      expect(buttons.some((t) => t?.includes(gone))).toBe(false);
    }
    expect(m.container.textContent).toContain("500 万字以内");
    expect(m.container.querySelectorAll("button[aria-label]")).toBeTruthy();
    await m.unmount();
  });

  test("粘贴计数上限 5,000,000", async () => {
    const m = await mountCard();
    await pasteText(m.container, "正文");
    expect(m.container.textContent).toContain("2 / 5,000,000 字");
    await m.unmount();
  });
});

describe("上传 → 预检 → 开拍设置 → 建剧", () => {
  test("点 ↑ 调预检（粘贴走 text），成功后弹「开拍设置」，在弹窗里点开始才建剧并进工作台", async () => {
    upload.mockResolvedValue(rawPreflight as never);
    post.mockResolvedValue({
      id: "d9",
      title: "t",
      episodes: null,
      active_jobs: null,
    } as never);
    const m = await mountCard();
    await pasteText(m.container, "第1章 雪夜\n正文");
    await dom.click(sendButton(m.container));
    await dom.flush();

    expect(upload.mock.calls[0][0]).toBe("/drama-sources/preflight");
    const form = upload.mock.calls[0][1] as FormData;
    expect(form.get("text")).toBe("第1章 雪夜\n正文");
    expect(post).not.toHaveBeenCalled();
    expect(body().textContent).toContain("开拍设置");
    expect(body().textContent).toContain("预计约 6–9 集");

    await dom.click(dialogStart());
    await dom.flush();
    expect(post.mock.calls[0][0]).toBe("/dramas");
    expect(post.mock.calls[0][1]).toMatchObject({
      source_token: "tok1",
      source_kind: "novel",
      range_end: 0,
      ratio: "9:16",
      style_key: "real_city_film",
      episode_seconds: 60,
      prompts: {
        adapt: { source: "builtin" },
        assets: { source: "builtin" },
        asset_prompts: { source: "builtin" },
        video: { source: "builtin" },
      },
    });
    expect(body().querySelector('[data-testid="workspace"]')).toBeTruthy();
    await m.unmount();
  });

  test("取消弹窗：卡片里的文本保留；再点 ↑ 直接重开弹窗，不重复上传", async () => {
    upload.mockResolvedValue(rawPreflight as never);
    const m = await mountCard();
    await pasteText(m.container, "第1章 雪夜\n正文");
    await dom.click(sendButton(m.container));
    await dom.flush();
    const cancel = [...body().querySelectorAll("button")].find((b) => b.textContent === "取消");
    await dom.click(cancel);
    await dom.flush(300);
    expect((m.container.querySelector("textarea") as HTMLTextAreaElement).value).toBe(
      "第1章 雪夜\n正文",
    );
    await dom.click(sendButton(m.container));
    await dom.flush();
    expect(upload).toHaveBeenCalledTimes(1);
    expect(body().textContent).toContain("开拍设置");
    await m.unmount();
  });

  test("67003 超长：就地显示在卡片里，不弹窗", async () => {
    upload.mockRejectedValue(new ApiError("", 67003, 400));
    const m = await mountCard();
    await pasteText(m.container, "正文");
    await dom.click(sendButton(m.container));
    await dom.flush();
    expect(m.container.querySelector('[role="alert"]')?.textContent).toContain("500 万字");
    expect(body().textContent).not.toContain("开拍设置");
    await m.unmount();
  });

  test("67002 格式不支持 / 内容为空：显示后端给的原因", async () => {
    upload.mockRejectedValue(new ApiError("内容为空", 67002, 400));
    const m = await mountCard();
    await pasteText(m.container, "正文");
    await dom.click(sendButton(m.container));
    await dom.flush();
    expect(m.container.querySelector('[role="alert"]')?.textContent).toBe("内容为空");
    expect(body().textContent).not.toContain("开拍设置");
    await m.unmount();
  });

  test("建剧时草稿已过期（67014）：回到卡片提示重新上传", async () => {
    upload.mockResolvedValue(rawPreflight as never);
    post.mockRejectedValue(new ApiError("", 67014, 400));
    const m = await mountCard();
    await pasteText(m.container, "第1章 雪夜\n正文");
    await dom.click(sendButton(m.container));
    await dom.flush();
    await dom.click(dialogStart());
    await dom.flush(300);
    expect(m.container.querySelector('[role="alert"]')?.textContent).toContain("重新上传");
    expect(body().textContent).not.toContain("开拍设置");
    await m.unmount();
  });
});

import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { MemoryRouter } from "react-router";

import type { Project, Round, Script } from "@/api/marketing/type";
import type { MarketingActions } from "@/pages/marketing/detail/use-actions";
import { installDom, type DomEnv } from "../../helpers/dom";
import { project, round, script, step } from "./fixtures";

/**
 * 营销创作台（/marketing/:id）各栏的渲染与联动：
 * 顶栏的主题切换在模块加载时就读 window，所以先装 jsdom 再动态引入组件。
 */

let env: DomEnv;
let TopBar: typeof import("@/pages/marketing/detail/top-bar").TopBar;
let AssetList: typeof import("@/pages/marketing/detail/asset-list").AssetList;
let ReverseInspector: typeof import("@/pages/marketing/detail/inspector").ReverseInspector;
let ScriptInspector: typeof import("@/pages/marketing/detail/inspector").ScriptInspector;
let Stage: typeof import("@/pages/marketing/detail/stage").Stage;
let useMarketingStore: typeof import("@/store/marketing").useMarketingStore;
let useWorkbenchStore: typeof import("@/store/marketing-workbench").useWorkbenchStore;

beforeAll(async () => {
  env = await installDom();
  ({ TopBar } = await import("@/pages/marketing/detail/top-bar"));
  ({ AssetList } = await import("@/pages/marketing/detail/asset-list"));
  ({ ReverseInspector, ScriptInspector } = await import("@/pages/marketing/detail/inspector"));
  ({ Stage } = await import("@/pages/marketing/detail/stage"));
  ({ useMarketingStore } = await import("@/store/marketing"));
  ({ useWorkbenchStore } = await import("@/store/marketing-workbench"));
});

afterAll(() => env.restore());

const actions = { busy: {} } as unknown as MarketingActions;

/** 把项目放进缓存、设好选中项：创作台各栏都从 store 读当前轮次 */
const prepare = (r: Round, selection: string | null): Project => {
  const p = { ...project, rounds: [r] };
  useMarketingStore.getState().setProject(p);
  useWorkbenchStore.getState().reset();
  useWorkbenchStore.setState({ roundId: r.id, selection });
  return p;
};
/** 真实挂载后取 HTML：zustand 在服务端渲染时只读初始状态，所以不用 renderToStaticMarkup */
const mount = async (
  r: Round,
  selection: string | null,
  node: (p: Project) => React.ReactElement,
) => {
  const { container, unmount } = await env.mount(
    <MemoryRouter>{node(prepare(r, selection))}</MemoryRouter>,
  );
  const html = container.innerHTML;
  await unmount();
  return html;
};

const topBar = (r: Round, available: number | null = 1000) =>
  mount(r, null, (p) => (
    <TopBar
      project={p}
      round={r}
      available={available}
      actions={actions}
      composerOpen={false}
      onRound={() => undefined}
      onEditFacts={() => undefined}
      onComposer={() => undefined}
    />
  ));
const assetList = (r: Round, selection: string | null = null) =>
  mount(r, selection, (p) => (
    <AssetList project={p} round={r} actions={actions} onEditFacts={() => undefined} />
  ));
const reverseInspector = (r: Round) =>
  mount(r, "ref", (p) => (
    <ReverseInspector project={p} round={r} available={1000} actions={actions} />
  ));
const scriptInspector = (r: Round, s: Script) =>
  mount(r, s.id, (p) => (
    <ScriptInspector project={p} round={r} script={s} available={1000} actions={actions} />
  ));
const stage = (r: Round, selection: string | null) =>
  mount(r, selection, (p) => <Stage project={p} available={1000} actions={actions} />);

describe("创作台：顶栏", () => {
  test("轮次切换、本轮参数、环节进度、全部出片、再写一轮", async () => {
    const out = await topBar(round());
    expect(out).toContain("第 2 轮");
    expect(out).toContain("抖音");
    expect(out).toContain("9:16 · 720P · 15s");
    expect(out).toContain("中文旁白");
    expect(out).toContain("参考 10s");
    expect(out).toContain("事实卡");
    expect(out).toContain("全部出片");
    expect(out).toContain("再写一轮");
  });

  test("写脚本中：阶段说明与取消", async () => {
    const out = await topBar(round({ status: "scripting", scripts: [], reverse: null }));
    expect(out).toContain("正在写跟拍版和改编版");
    expect(out).toContain("取消");
  });
});

describe("创作台：左栏", () => {
  test("原片 + 每份脚本一行，选中项标记；商品信息区", async () => {
    const out = await assetList(round(), "x1");
    expect(out).toContain("原片");
    expect(out).toContain("参考拆解 · 1 个镜头 · 10s");
    expect(out).toContain("跟拍 · 早上喷的香，中午就散了？");
    expect(out).toContain('aria-current="true"');
    expect(out).toContain("商品信息");
  });

  test("写脚本中按计划份数画骨架", async () => {
    const out = await assetList(
      round({ status: "scripting", scripts: [], planCount: 3, reverse: null }),
    );
    expect(out.split("animate-pulse").length - 1).toBeGreaterThanOrEqual(3);
  });

  test("没写出来的环节显示虚线行和重试；轮次警告", async () => {
    const out = await assetList(
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
    expect(out).toContain("跟拍版没写出来");
    expect(out).toContain("校验不过");
    expect(out).toContain('aria-label="重试"');
    expect(out).toContain("拆解未包含声音，台词按画面推断");
  });
});

describe("创作台：检查器", () => {
  test("原片：参考拆解全部字段与重写跟拍版", async () => {
    const out = await reverseInspector(round());
    expect(out).toContain("参考拆解");
    expect(out).toContain("1 个镜头");
    expect(out).toContain("方法：daihuo-fanpai v2");
    expect(out).toContain("前 2 秒手背干纹特写");
    expect(out).toContain("第三方品牌 1");
    expect(out).toContain("原片背书 1");
    expect(out).toContain("前 3 秒");
    expect(out).toContain("主体 + 动作");
    expect(out).toContain("台词");
    expect(out).toContain("商品怎么出现");
    expect(out).toContain("原片背书：全网销量第一");
    expect(out).toContain("按拆解重写跟拍版");
  });

  test("拆解失败：原因和重试拆解", async () => {
    const out = await reverseInspector(
      round({
        reverse: null,
        steps: [
          step("facts", "succeeded"),
          step("reverse", "failed", "看视频模型返回的分镜不完整"),
        ],
      }),
    );
    expect(out).toContain("参考片拆解失败：看视频模型返回的分镜不完整");
    expect(out).toContain("重试拆解");
  });

  test("脚本：脚本 / 提示词两个标签页、镜头（参考 n）、已修改、方法、配乐音色、出片积分", async () => {
    const r = round();
    const out = await scriptInspector(r, r.scripts[0]);
    expect(out).toContain("提示词");
    expect(out).not.toContain("镜头对照");
    expect(out).toContain("参考 1");
    expect(out).toContain("已修改");
    expect(out).toContain("方法：daihuo-fanpai v2");
    expect(out).toContain("轻快鼓点 · 年轻女声");
    expect(out).toContain("✦120");
  });

  test("旁白有问题：问题标签、原因、出片禁用", async () => {
    const s = script({
      kind: "create",
      angle: "pain",
      flags: [{ shot: 0, type: "claim", text: "全网销量第一" }],
    });
    const r = round({ refVideo: null, reverse: null, scripts: [s] });
    const out = await scriptInspector(r, s);
    expect(out).toContain("背书无依据「全网销量第一」");
    expect(out).toMatch(/<button[^>]*disabled[^>]*title="旁白有问题[^"]*"[^>]*>[^]*?出片/);
  });
});

describe("创作台：舞台", () => {
  test("未出片的脚本：标题和出片按钮", async () => {
    const out = await stage(round(), "x1");
    expect(out).toContain("跟拍 · 早上喷的香，中午就散了？");
    expect(out).toContain("✦120");
  });

  test("出片中：按画幅画生成中的画框（扫光），还没拿到进度时进度环转圈", async () => {
    const r = round({ scripts: [script({ status: "rendering" })] });
    const out = await stage(r, "x1");
    expect(out).toContain('data-slot="marketing-rendering"');
    expect(out).toContain("marketing-shimmer");
    expect(out).toContain("animate-spin");
    expect(out).toContain("出片中 0%");
  });

  test("失败的轮次：原因、积分已退回、重试；取消的轮次写已取消", async () => {
    const failed = await stage(
      round({
        status: "failed",
        error: "商品事实卡生成失败",
        steps: [step("facts", "failed", "文本模型两次输出都不合格")],
        scripts: [],
      }),
      null,
    );
    expect(failed).toContain("商品事实卡生成失败");
    expect(failed).toContain("积分已退回");
    expect(failed).toContain("重试");
    expect(await stage(round({ status: "canceled", scripts: [] }), null)).toContain("已取消");
  });

  test("选中原片：可播放；不再有单看 / 对比原片切换", async () => {
    const out = await stage(round(), "ref");
    expect(out).toContain('aria-label="播放视频"');
    expect(out).not.toContain("对比原片");
  });
});

describe("创作台：播放器", () => {
  const done = () =>
    script({
      status: "done",
      shots: [
        { ...script().shots[0], start: 0, end: 4, refShot: 1, narration: "第一句旁白" },
        { ...script().shots[0], start: 4, end: 10, refShot: 1, narration: "第二句旁白" },
      ],
      takes: [
        {
          id: "t1",
          taskRef: "T1",
          status: "succeeded",
          progress: 100,
          outputs: [{ url: "https://x/out.mp4", posterUrl: "" }],
          errorMessage: "",
        },
      ],
    });

  const renderStage = async () => {
    const s = done();
    const r = round({ scripts: [s] });
    const p = prepare(r, s.id);
    const { container, unmount } = await env.mount(
      <MemoryRouter>
        <Stage project={p} available={1000} actions={actions} />
      </MemoryRouter>,
    );
    const html = container.innerHTML;
    await unmount();
    return html;
  };

  test("已出片：用共享的视频播放器（停在封面、带播放键）；下面不再有时间轴", async () => {
    const html = await renderStage();
    expect(html.split('aria-label="播放视频"').length - 1).toBe(1);
    // 自写的走带条已去掉，播放 / 进度 / 静音都在共享播放器里
    expect(html).not.toContain("上一镜");
    expect(html).not.toContain('data-slot="marketing-timeline"');
    expect(html).not.toContain("镜 1 · 原片 1");
    expect(html).not.toContain("第二句旁白");
  });
});

describe("创作台：联动", () => {
  test("点左栏一项：选中项切过去", async () => {
    const r = round();
    const p = prepare(r, "x1");
    const { container, unmount } = await env.mount(
      <MemoryRouter>
        <AssetList project={p} round={r} actions={actions} onEditFacts={() => undefined} />
      </MemoryRouter>,
    );
    const buttons = [...container.querySelectorAll("button")];
    await env.click(buttons.find((b) => b.textContent?.includes("原片")));
    expect(useWorkbenchStore.getState().selection).toBe("ref");
    await unmount();
  });

  test("点检查器里的镜头：通过舞台注册的控制跳到那一镜；点在输入框里不跳", async () => {
    const done = script({
      id: "x2",
      kind: "create",
      angle: "pain",
      status: "done",
      shots: [
        { ...script().shots[0], start: 0, end: 3 },
        { ...script().shots[0], start: 3, end: 8, narration: "第二镜" },
      ],
    });
    const r = round({ scripts: [done] });
    const p = prepare(r, "x2");
    const seeks: number[] = [];
    useWorkbenchStore.getState().setControls({
      seek: (t) => seeks.push(t),
      toggle: () => undefined,
      pause: () => undefined,
      fullscreen: () => undefined,
    });
    const { container, unmount } = await env.mount(
      <MemoryRouter>
        <ScriptInspector project={p} round={r} script={done} available={1000} actions={actions} />
      </MemoryRouter>,
    );
    await env.click(container.querySelector('[data-shot="1"]'));
    expect(seeks).toEqual([3]);
    await env.click(container.querySelector('[data-shot="0"] input'));
    expect(seeks).toEqual([3]);
    await unmount();
  });

  test("播放到第二镜：检查器高亮第二镜", async () => {
    const r = round();
    const s = script({
      shots: [
        { ...script().shots[0], start: 0, end: 3 },
        { ...script().shots[0], start: 3, end: 8 },
      ],
    });
    const p = prepare({ ...r, scripts: [s] }, s.id);
    const { container, unmount } = await env.mount(
      <MemoryRouter>
        <ScriptInspector project={p} round={r} script={s} available={1000} actions={actions} />
      </MemoryRouter>,
    );
    await env.act(async () => useWorkbenchStore.getState().setClock(4, true));
    expect(container.querySelector('[data-shot="1"]')?.hasAttribute("data-current")).toBe(true);
    expect(container.querySelector('[data-shot="0"]')?.hasAttribute("data-current")).toBe(false);
    await unmount();
  });
});

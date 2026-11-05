import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import type { MyPrompt, PromptChoice, PromptStep } from "@/api/drama/type";
import type { PromptLibrary } from "@/components/drama/prompts/use-prompt-library";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import { builtinPrompts } from "@/utils/drama/start-settings";
import type { PromptEditorPane as PromptEditorPaneType } from "@/components/drama/prompts/prompt-editor";
import type { PromptRows as PromptRowsType } from "@/components/drama/prompts/prompt-rows";

/**
 * 我的提示词：编辑器（新建 / 编辑 / 恢复内置 / 校验）与四行下拉（选择、新建、编辑、删除、剧本型置灰）。
 */

let dom: DomEnv;
let PromptEditorPane: typeof PromptEditorPaneType;
let PromptRows: typeof PromptRowsType;
beforeAll(async () => {
  dom = await installDom();
  PromptEditorPane = (await import("@/components/drama/prompts/prompt-editor")).PromptEditorPane;
  PromptRows = (await import("@/components/drama/prompts/prompt-rows")).PromptRows;
});
afterAll(() => dom.restore());

const mineItem = (patch: Partial<MyPrompt> = {}): MyPrompt => ({
  id: "1f",
  step: "adapt",
  name: "我的改编",
  bodies: { plan: "改过的规划", scripts: "改过的剧本" },
  revision: 3,
  updatedAt: "t",
  ...patch,
});

const makeLibrary = (items: MyPrompt[] = []) => {
  const saved: Parameters<PromptLibrary["save"]>[0][] = [];
  const removed: string[] = [];
  const library: PromptLibrary = {
    ready: true,
    templates: [
      {
        step: "adapt",
        pages: [
          { key: "plan", label: "分集规划", body: "内置规划" },
          { key: "scripts", label: "分集剧本", body: "内置剧本" },
        ],
      },
      { step: "video", pages: [{ key: "storyboard", label: "视频提示词", body: "内置视频" }] },
    ],
    mine: {
      adapt: items.filter((i) => i.step === "adapt"),
      assets: [],
      asset_prompts: [],
      video: items.filter((i) => i.step === "video"),
    },
    skills: {
      adapt: [],
      assets: [{ name: "cross", title: "跨集连续性", description: "保持角色一致" }],
      asset_prompts: [],
      video: [],
    },
    save: async (draft) => {
      saved.push(draft);
      return {
        id: draft.id ?? "new1",
        step: draft.step,
        name: draft.name.trim(),
        bodies: draft.bodies,
        revision: (draft.revision ?? 0) + 1,
        updatedAt: "t",
      };
    },
    remove: async (item) => {
      removed.push(item.id);
    },
  };
  return { library, saved, removed };
};

const textarea = (c: HTMLElement) => c.querySelector("textarea") as HTMLTextAreaElement;
const nameInput = (c: HTMLElement) =>
  c.querySelector('input[aria-label="名称"]') as HTMLInputElement;
const byText = (c: HTMLElement, text: string) =>
  [...c.querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === text);

describe("提示词编辑器", () => {
  test("新建：正文预填内置，不展示「输入变量」「输出结构」，右下角有字数", async () => {
    const { library } = makeLibrary();
    const m = await dom.mount(
      <PromptEditorPane step="video" item={null} library={library} onClose={() => {}} />,
    );
    expect(textarea(m.container).value).toBe("内置视频");
    const text = m.container.textContent ?? "";
    expect(text).toContain("视频提示词 · 新建");
    expect(text).not.toContain("输入变量");
    expect(text).not.toContain("输出结构");
    expect(text).toContain("4 / 20,000");
    await m.unmount();
  });

  test("剧本改编分「分集规划 / 分集剧本」两页，切页各改各的", async () => {
    const { library, saved } = makeLibrary();
    const m = await dom.mount(
      <PromptEditorPane step="adapt" item={null} library={library} onClose={() => {}} />,
    );
    const tabs = [...m.container.querySelectorAll('[role="tab"]')];
    expect(tabs.map((t) => t.textContent)).toEqual(["分集规划", "分集剧本"]);
    expect(textarea(m.container).value).toBe("内置规划");
    await dom.type(textarea(m.container), "我的规划");
    await dom.click(tabs[1]);
    expect(textarea(m.container).value).toBe("内置剧本");
    await dom.type(nameInput(m.container), "我的改编");
    await dom.click(byText(m.container, "保存"));
    expect(saved[0].bodies).toEqual({ plan: "我的规划", scripts: "内置剧本" });
    await m.unmount();
  });

  test("保存：名称为空就地报错，不调接口；填好后保存并把条目交给调用方", async () => {
    const { library, saved } = makeLibrary();
    const closed: (MyPrompt | null)[] = [];
    const m = await dom.mount(
      <PromptEditorPane
        step="video"
        item={null}
        library={library}
        onClose={(x) => closed.push(x)}
      />,
    );
    await dom.click(byText(m.container, "保存"));
    expect(m.container.querySelector('[role="alert"]')?.textContent).toBe("请填写名称");
    expect(saved).toHaveLength(0);
    await dom.type(nameInput(m.container), " 古风视频 ");
    await dom.click(byText(m.container, "保存"));
    await dom.flush();
    expect(saved).toHaveLength(1);
    expect(closed[0]).toMatchObject({ id: "new1", name: "古风视频", step: "video" });
    await m.unmount();
  });

  test("正文清空或同名：校验报错；返回按钮传 null", async () => {
    const { library } = makeLibrary([mineItem({ id: "9", step: "video", name: "已有" })]);
    const closed: (MyPrompt | null)[] = [];
    const m = await dom.mount(
      <PromptEditorPane
        step="video"
        item={null}
        library={library}
        onClose={(x) => closed.push(x)}
      />,
    );
    await dom.type(nameInput(m.container), "已有");
    await dom.click(byText(m.container, "保存"));
    expect(m.container.querySelector('[role="alert"]')?.textContent).toBe(
      "这个环节里已有同名提示词",
    );
    await dom.type(nameInput(m.container), "新的");
    await dom.type(textarea(m.container), "   ");
    await dom.click(byText(m.container, "保存"));
    expect(m.container.querySelector('[role="alert"]')?.textContent).toBe("正文不能为空");
    await dom.click(m.container.querySelector('[aria-label="返回"]'));
    expect(closed).toEqual([null]);
    await m.unmount();
  });

  test("编辑：预填名称与正文；恢复内置只恢复当前页；保存带 id 与 revision", async () => {
    const item = mineItem();
    const { library, saved } = makeLibrary([item]);
    const m = await dom.mount(
      <PromptEditorPane step="adapt" item={item} library={library} onClose={() => {}} />,
    );
    expect(m.container.textContent).toContain("剧本改编 · 编辑");
    expect(nameInput(m.container).value).toBe("我的改编");
    expect(textarea(m.container).value).toBe("改过的规划");
    await dom.click(byText(m.container, "恢复内置"));
    expect(textarea(m.container).value).toBe("内置规划");
    await dom.click(byText(m.container, "保存"));
    await dom.flush();
    expect(saved[0]).toMatchObject({
      id: "1f",
      revision: 3,
      bodies: { plan: "内置规划", scripts: "改过的剧本" },
    });
    await m.unmount();
  });
});

describe("四行提示词下拉", () => {
  const mount = async (
    items: MyPrompt[],
    over: {
      kind?: "script" | "novel" | null;
      value?: Record<PromptStep, PromptChoice>;
    } = {},
  ) => {
    const lib = makeLibrary(items);
    const calls = {
      change: [] as [PromptStep, PromptChoice][],
      edit: [] as [PromptStep, MyPrompt | null][],
    };
    const m = await dom.mount(
      <PromptRows
        kind={over.kind ?? "novel"}
        value={over.value ?? builtinPrompts()}
        library={lib.library}
        onChange={(step, choice) => calls.change.push([step, choice])}
        onEdit={(step, item) => calls.edit.push([step, item])}
      />,
    );
    return { ...lib, ...calls, m };
  };
  const rowChip = (c: HTMLElement, name: string) =>
    [...c.querySelectorAll("span")]
      .find((el) => el.textContent === name)
      ?.parentElement?.querySelector("button") as HTMLButtonElement;
  const popupButton = (text: string) =>
    [...dom.win.document.body.querySelectorAll("button")].find((b) =>
      (b.textContent ?? "").includes(text),
    ) as HTMLButtonElement | undefined;

  test("四行：剧本改编 / 资产拆解 / 资产图提示词 / 视频提示词，默认都是「内置」", async () => {
    const { m } = await mount([]);
    const names = [...m.container.querySelectorAll('[data-slot="prompt-rows"] > div > span')].map(
      (el) => el.textContent,
    );
    expect(names).toEqual(["剧本改编", "资产拆解", "资产图提示词", "视频提示词"]);
    expect(rowChip(m.container, "视频提示词").textContent).toBe("内置");
    await m.unmount();
  });

  test("剧本型输入：剧本改编置灰并写原因", async () => {
    const { m } = await mount([], { kind: "script" });
    const chip = rowChip(m.container, "剧本改编");
    expect(chip.disabled).toBe(true);
    expect(chip.title).toBe("剧本直接导入，不改编");
    expect(chip.textContent).toBe("剧本直接导入");
    await m.unmount();
  });

  test("下拉：内置（推荐）、我的提示词（含新建）、技能（没有时写「暂无适用技能」）", async () => {
    const { m, change, edit } = await mount([
      mineItem({ id: "7", step: "video", name: "古风视频" }),
    ]);
    await dom.click(rowChip(m.container, "视频提示词"));
    await dom.flush();
    const body = dom.win.document.body.textContent ?? "";
    expect(body).toContain("推荐");
    expect(body).toContain("古风视频");
    expect(body).toContain("暂无适用技能");
    await dom.click(popupButton("古风视频"));
    expect(change).toEqual([["video", { source: "mine", id: "7" }]]);
    await dom.click(rowChip(m.container, "视频提示词"));
    await dom.flush();
    await dom.click(popupButton("新建"));
    expect(edit).toEqual([["video", null]]);
    await m.unmount();
  });

  test("下拉：编辑进入编辑器；有技能的环节列出技能并可选", async () => {
    const item = mineItem({ id: "7", step: "video", name: "古风视频" });
    const { m, change, edit } = await mount([item]);
    await dom.click(rowChip(m.container, "视频提示词"));
    await dom.flush();
    await dom.click(dom.win.document.body.querySelector('[aria-label="编辑 古风视频"]'));
    expect(edit).toEqual([["video", item]]);
    await dom.click(rowChip(m.container, "资产拆解"));
    await dom.flush();
    expect(dom.win.document.body.textContent).toContain("保持角色一致");
    await dom.click(popupButton("跨集连续性"));
    expect(change.at(-1)).toEqual(["assets", { source: "skill", skill: "cross" }]);
    await m.unmount();
  });

  test("删除我的提示词：调库删除；正在用它的行回落为内置", async () => {
    const item = mineItem({ id: "7", step: "video", name: "古风视频" });
    const { m, removed, change } = await mount([item], {
      value: { ...builtinPrompts(), video: { source: "mine", id: "7" } },
    });
    expect(rowChip(m.container, "视频提示词").textContent).toBe("我的 · 古风视频");
    await dom.click(rowChip(m.container, "视频提示词"));
    await dom.flush();
    await dom.click(dom.win.document.body.querySelector('[aria-label="删除 古风视频"]'));
    await dom.flush();
    expect(removed).toEqual(["7"]);
    expect(change).toEqual([["video", { source: "builtin" }]]);
    await m.unmount();
  });
});

import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import type { Drama, PromptSettings, UpdateDramaInput } from "@/api/drama/type";
import type { PromptLibrary } from "@/components/drama/prompts/use-prompt-library";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type {
  PromptSettingsBody as PromptSettingsBodyType,
  changedSteps as changedStepsType,
  settingsToPrompts as settingsToPromptsType,
} from "@/pages/drama/workspace/prompt-settings-sheet";
import type { CommandPalette as CommandPaletteType } from "@/pages/drama/workspace/command-palette";
import type { DramaWorkspaceContext as DramaWorkspaceContextType } from "@/pages/drama/workspace/context";
import type { Sheet as SheetType } from "@/components/ui/sheet";

/**
 * 工作台「提示词设置」：四行（同开拍设置）、提示「修改只影响之后发起的任务」、
 * 点完成保存（PATCH prompts）；入口在 ⌘K 命令面板。
 */

let dom: DomEnv;
let PromptSettingsBody: typeof PromptSettingsBodyType;
let changedSteps: typeof changedStepsType;
let settingsToPrompts: typeof settingsToPromptsType;
let CommandPalette: typeof CommandPaletteType;
let DramaWorkspaceContext: typeof DramaWorkspaceContextType;
let Sheet: typeof SheetType;
beforeAll(async () => {
  dom = await installDom();
  const sheet = await import("@/pages/drama/workspace/prompt-settings-sheet");
  PromptSettingsBody = sheet.PromptSettingsBody;
  changedSteps = sheet.changedSteps;
  settingsToPrompts = sheet.settingsToPrompts;
  CommandPalette = (await import("@/pages/drama/workspace/command-palette")).CommandPalette;
  DramaWorkspaceContext = (await import("@/pages/drama/workspace/context")).DramaWorkspaceContext;
  Sheet = (await import("@/components/ui/sheet")).Sheet;
});
afterAll(() => dom.restore());

const settings = (patch: Partial<PromptSettings> = {}): PromptSettings => ({
  adapt: { source: "builtin", label: "内置" },
  assets: { source: "skill", skill: "cross", label: "技能 · 跨集连续性", missing: true },
  asset_prompts: { source: "mine", id: "1f", label: "我的 · 古风资产" },
  video: { source: "builtin", label: "内置" },
  ...patch,
});

const library: PromptLibrary = {
  ready: true,
  templates: [],
  mine: {
    adapt: [],
    assets: [],
    asset_prompts: [
      {
        id: "1f",
        step: "asset_prompts",
        name: "古风资产",
        bodies: {},
        revision: 1,
        updatedAt: "t",
      },
    ],
    video: [],
  },
  skills: {
    adapt: [],
    assets: [{ name: "cross", title: "跨集连续性", description: "d" }],
    asset_prompts: [],
    video: [{ name: "tone", title: "镜头语气", description: "d" }],
  },
  save: async () => {
    throw new Error("not used");
  },
  remove: async () => {},
};

const dramaOf = (patch: Partial<Drama> = {}) =>
  ({ id: "d1", sourceKind: "novel", promptSettings: settings(), episodes: [], ...patch }) as never;

const mountBody = (
  drama: Drama,
  patches: Omit<UpdateDramaInput, "revision">[],
  onClose: () => void = () => {},
) =>
  dom.mount(
    <DramaWorkspaceContext.Provider
      value={
        {
          drama,
          jobs: [],
          patch: async (fields: Omit<UpdateDramaInput, "revision">) => {
            patches.push(fields);
            return 2;
          },
        } as never
      }
    >
      <Sheet open>
        <PromptSettingsBody library={library} onClose={onClose} />
      </Sheet>
    </DramaWorkspaceContext.Provider>,
  );
const chip = (c: HTMLElement, name: string) =>
  [...c.querySelectorAll("span")]
    .find((el) => el.textContent === name)
    ?.parentElement?.querySelector("button") as HTMLButtonElement;
const byText = (c: HTMLElement, text: string) =>
  [...c.querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === text);

describe("提示词设置：纯逻辑", () => {
  test("把已建剧的设置转成可编辑的选择；比较出改了哪些环节", () => {
    const prompts = settingsToPrompts(settings());
    expect(prompts).toEqual({
      adapt: { source: "builtin" },
      assets: { source: "skill", skill: "cross" },
      asset_prompts: { source: "mine", id: "1f" },
      video: { source: "builtin" },
    });
    expect(changedSteps(settings(), prompts)).toEqual([]);
    expect(
      changedSteps(settings(), { ...prompts, video: { source: "skill", skill: "tone" } }),
    ).toEqual(["video"]);
  });
});

describe("提示词设置：Sheet 内容", () => {
  test("提示「修改只影响之后发起的任务」，四行显示已建剧时的设置，来源丢失的行标红提示", async () => {
    const m = await mountBody(dramaOf(), []);
    const text = m.container.textContent ?? "";
    expect(text).toContain("提示词设置");
    expect(text).toContain("修改只影响之后发起的任务");
    expect(chip(m.container, "资产拆解").textContent).toBe("技能 · 跨集连续性");
    expect(chip(m.container, "资产拆解").title).toContain("已被删除或停用");
    expect(chip(m.container, "资产拆解").className).toContain("text-status-warning");
    expect(chip(m.container, "资产图提示词").textContent).toBe("我的 · 古风资产");
    expect(chip(m.container, "视频提示词").textContent).toBe("内置");
    await m.unmount();
  });

  test("剧本型项目：剧本改编置灰", async () => {
    const m = await mountBody(dramaOf({ sourceKind: "script" }), []);
    expect(chip(m.container, "剧本改编").disabled).toBe(true);
    await m.unmount();
  });

  test("没改动点「完成」直接关闭，不发请求", async () => {
    const patches: Omit<UpdateDramaInput, "revision">[] = [];
    let closed = 0;
    const m = await mountBody(dramaOf(), patches, () => (closed += 1));
    await dom.click(byText(m.container, "完成"));
    await dom.flush();
    expect(patches).toEqual([]);
    expect(closed).toBe(1);
    await m.unmount();
  });

  test("改了视频提示词的技能，点「完成」带上四个环节的选择一起保存", async () => {
    const patches: Omit<UpdateDramaInput, "revision">[] = [];
    let closed = 0;
    const m = await mountBody(dramaOf(), patches, () => (closed += 1));
    await dom.click(chip(m.container, "视频提示词"));
    await dom.flush();
    const pick = [...dom.win.document.body.querySelectorAll("button")].find((b) =>
      (b.textContent ?? "").includes("镜头语气"),
    );
    await dom.click(pick);
    expect(chip(m.container, "视频提示词").textContent).toBe("技能 · 镜头语气");
    await dom.click(byText(m.container, "完成"));
    await dom.flush();
    expect(patches).toEqual([
      {
        prompts: {
          adapt: { source: "builtin" },
          assets: { source: "skill", skill: "cross" },
          asset_prompts: { source: "mine", id: "1f" },
          video: { source: "skill", skill: "tone" },
        },
      },
    ]);
    expect(closed).toBe(1);
    await m.unmount();
  });
});

describe("提示词设置：命令面板入口", () => {
  test("⌘K 里有「提示词设置」，选中后打开", async () => {
    const calls: string[] = [];
    const m = await dom.mount(
      <DramaWorkspaceContext.Provider
        value={{ drama: { id: "d1", episodes: [] }, jobs: [] } as never}
      >
        <CommandPalette
          open
          onOpenChange={() => calls.push("close")}
          onGo={() => {}}
          onBreakdownOpen={() => calls.push("breakdown")}
          onPromptsOpen={() => calls.push("prompts")}
        />
      </DramaWorkspaceContext.Provider>,
    );
    await dom.flush(50);
    const item = [...dom.win.document.body.querySelectorAll('[cmdk-item], [role="option"]')].find(
      (el) => (el.textContent ?? "").includes("提示词设置"),
    );
    expect(item).toBeTruthy();
    await dom.click(item);
    expect(calls).toContain("prompts");
    await m.unmount();
  });
});

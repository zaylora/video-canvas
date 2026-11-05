import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import {
  EmptyScript,
  type MetaFields as MetaFieldsType,
} from "@/pages/drama/workspace/episode-meta";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import { showEmptyScript } from "@/utils/drama/episodes";

/**
 * 没有剧本的集：空状态只留「生成剧本」（不再有「手写」）；钩子 / 悬念可以直接改，梗概只读。
 * 手动新增、没有对应原文的集不显示空状态，直接给空白稿纸（和改动前一样能手写）。
 */

describe("空状态：只有「生成剧本」", () => {
  test("不再有「手写」按钮", () => {
    const html = renderToStaticMarkup(
      <EmptyScript
        episode={{ synopsis: "s", hook: "h", cliffhanger: "c" }}
        onGenerate={() => {}}
      />,
    );
    expect(html).toContain("生成剧本");
    expect(html).not.toContain("手写");
  });
});

describe("什么时候显示空状态", () => {
  const ep = (patch: Record<string, unknown>) =>
    ({ scriptStatus: "empty", unitFrom: 3, unitTo: 3, ...patch }) as never;

  test("有对应原文、还没有剧本：显示空状态", () => {
    expect(showEmptyScript(ep({}), "")).toBe(true);
    expect(showEmptyScript(ep({ scriptStatus: "failed" }), "  ")).toBe(true);
  });

  test("手动新增、没有对应原文的集：直接给稿纸手写", () => {
    expect(showEmptyScript(ep({ unitFrom: null, unitTo: null }), "")).toBe(false);
    expect(showEmptyScript(ep({ unitFrom: 0, unitTo: 0 }), "")).toBe(false);
  });

  test("已有正文、生成中、已就绪：不显示空状态", () => {
    expect(showEmptyScript(ep({}), "正文")).toBe(false);
    expect(showEmptyScript(ep({ scriptStatus: "generating" }), "")).toBe(false);
    expect(showEmptyScript(ep({ scriptStatus: "ready" }), "")).toBe(false);
  });
});

describe("钩子 / 悬念：点开直接改", () => {
  let dom: DomEnv;
  let MetaFields: typeof MetaFieldsType;
  beforeAll(async () => {
    dom = await installDom();
    MetaFields = (await import("@/pages/drama/workspace/episode-meta")).MetaFields;
  });
  afterAll(() => dom.restore());

  const edit = (c: HTMLElement, label: string) =>
    c.querySelector(`button[aria-label="编辑${label}"]`) as HTMLButtonElement | null;
  const area = (c: HTMLElement) => c.querySelector("textarea") as HTMLTextAreaElement | null;
  const key = async (el: Element | null, k: string, shiftKey = false) =>
    dom.act(async () => {
      el?.dispatchEvent(new dom.win.KeyboardEvent("keydown", { key: k, shiftKey, bubbles: true }));
    });
  const blur = async (el: Element | null) =>
    dom.act(async () => {
      el?.dispatchEvent(new dom.win.FocusEvent("focusout", { bubbles: true }));
      el?.dispatchEvent(new dom.win.FocusEvent("blur"));
    });

  test("梗概只读；钩子、悬念有编辑入口，空值也能填", async () => {
    const m = await dom.mount(
      <MetaFields
        episode={{ synopsis: "沈砚上山", hook: "", cliffhanger: "师兄认出了剑" }}
        onSave={async () => {}}
      />,
    );
    expect(m.container.textContent).toContain("沈砚上山");
    expect(edit(m.container, "梗概")).toBeNull();
    expect(edit(m.container, "开场钩子")).not.toBeNull();
    expect(edit(m.container, "结尾悬念")?.textContent).toContain("师兄认出了剑");
    await m.unmount();
  });

  test("回车保存，Shift+回车换行不保存；保存时带字段名和新值", async () => {
    const saved: [string, string][] = [];
    const m = await dom.mount(
      <MetaFields
        episode={{ synopsis: "", hook: "旧钩子", cliffhanger: "" }}
        onSave={async (field, value) => {
          saved.push([field, value]);
        }}
      />,
    );
    await dom.click(edit(m.container, "开场钩子"));
    await dom.type(area(m.container), "新钩子");
    await key(area(m.container), "Enter", true);
    expect(saved).toEqual([]);
    await key(area(m.container), "Enter");
    await dom.flush();
    expect(saved).toEqual([["hook", "新钩子"]]);
    expect(area(m.container)).toBeNull();
    await m.unmount();
  });

  test("失焦保存；没改动不保存；Esc 取消", async () => {
    const saved: [string, string][] = [];
    const m = await dom.mount(
      <MetaFields
        episode={{ synopsis: "", hook: "", cliffhanger: "旧悬念" }}
        onSave={async (field, value) => {
          saved.push([field, value]);
        }}
      />,
    );
    await dom.click(edit(m.container, "结尾悬念"));
    await blur(area(m.container));
    await dom.flush();
    expect(saved).toEqual([]);

    await dom.click(edit(m.container, "结尾悬念"));
    await dom.type(area(m.container), "改了又反悔");
    await key(area(m.container), "Escape");
    expect(area(m.container)).toBeNull();
    expect(saved).toEqual([]);

    await dom.click(edit(m.container, "结尾悬念"));
    await dom.type(area(m.container), "新悬念");
    await blur(area(m.container));
    await dom.flush();
    expect(saved).toEqual([["cliffhanger", "新悬念"]]);
    await m.unmount();
  });

  test("没给 onSave（只读场景）时只列有值的行，没有编辑入口", () => {
    const html = renderToStaticMarkup(
      <MetaFields episode={{ synopsis: "梗", hook: "钩", cliffhanger: "" }} />,
    );
    expect(html).toContain("钩");
    expect(html).not.toContain("结尾悬念");
    expect(html).not.toContain('aria-label="编辑');
  });
});

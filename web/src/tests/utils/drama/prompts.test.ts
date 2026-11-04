import { describe, expect, test } from "bun:test";

import type { PromptTemplate } from "@/api/drama/type";
import {
  PROMPT_BODY_MAX,
  PROMPT_NAME_MAX,
  PROMPT_STEPS,
  adaptLockedReason,
  choiceLabel,
  draftFromMine,
  draftFromTemplate,
  promptRowState,
  restoreBuiltin,
  sameChoice,
  validatePromptDraft,
} from "@/utils/drama/prompts";

const adapt: PromptTemplate = {
  step: "adapt",
  pages: [
    { key: "plan", label: "分集规划", body: "内置规划" },
    { key: "scripts", label: "分集剧本", body: "内置剧本" },
  ],
};
const video: PromptTemplate = {
  step: "video",
  pages: [{ key: "storyboard", label: "视频提示词", body: "内置视频" }],
};

describe("提示词环节", () => {
  test("四个环节：剧本改编 / 资产拆解 / 资产图提示词 / 视频提示词", () => {
    expect(PROMPT_STEPS.map((s) => [s.key, s.name])).toEqual([
      ["adapt", "剧本改编"],
      ["assets", "资产拆解"],
      ["asset_prompts", "资产图提示词"],
      ["video", "视频提示词"],
    ]);
  });

  test("剧本型输入：剧本改编行置灰，原因是「剧本直接导入，不改编」", () => {
    expect(adaptLockedReason("script")).toBe("剧本直接导入，不改编");
    expect(adaptLockedReason("novel")).toBeNull();
    expect(adaptLockedReason(null)).toBeNull();
    expect(promptRowState("adapt", "script", { source: "mine", id: "1" }, [], [])).toEqual({
      label: "剧本直接导入",
      disabledReason: "剧本直接导入，不改编",
      missing: false,
    });
    expect(promptRowState("assets", "script", { source: "builtin" }, [], []).disabledReason).toBe(
      null,
    );
  });

  test("当前选择的展示名：内置 / 我的 · 名称 / 技能 · 标题，来源丢失时标「已删除 / 已停用」", () => {
    const mine = [{ id: "1f", name: "古风资产" }];
    const skills = [{ name: "cross", title: "跨集连续性" }];
    expect(choiceLabel({ source: "builtin" }, mine, skills)).toEqual({
      text: "内置",
      missing: false,
    });
    expect(choiceLabel({ source: "mine", id: "1f" }, mine, skills).text).toBe("我的 · 古风资产");
    expect(choiceLabel({ source: "skill", skill: "cross" }, mine, skills).text).toBe(
      "技能 · 跨集连续性",
    );
    expect(choiceLabel({ source: "mine", id: "x" }, mine, skills)).toEqual({
      text: "我的 · 已删除",
      missing: true,
    });
    expect(choiceLabel({ source: "skill", skill: "x" }, mine, skills)).toEqual({
      text: "技能 · 已停用",
      missing: true,
    });
  });

  test("sameChoice：来源 + id / 技能名相同才算同一个", () => {
    expect(sameChoice({ source: "builtin" }, { source: "builtin" })).toBe(true);
    expect(sameChoice({ source: "mine", id: "1" }, { source: "mine", id: "1" })).toBe(true);
    expect(sameChoice({ source: "mine", id: "1" }, { source: "mine", id: "2" })).toBe(false);
    expect(sameChoice({ source: "skill", skill: "a" }, { source: "builtin" })).toBe(false);
  });
});

describe("提示词编辑器草稿", () => {
  test("新建：名称为空，正文预填内置；剧本改编有「分集规划 / 分集剧本」两页", () => {
    const draft = draftFromTemplate(adapt);
    expect(draft).toMatchObject({
      step: "adapt",
      id: null,
      name: "",
      page: "plan",
      bodies: { plan: "内置规划", scripts: "内置剧本" },
    });
    expect(draft.pages.map((p) => p.label)).toEqual(["分集规划", "分集剧本"]);
    expect(draftFromTemplate(video).page).toBe("storyboard");
  });

  test("编辑：用条目的名称和正文，缺的页用内置补齐", () => {
    const draft = draftFromMine(adapt, {
      id: "1f",
      step: "adapt",
      name: "我的改编",
      bodies: { plan: "改过的规划" },
      revision: 3,
      updatedAt: "t",
    });
    expect(draft).toMatchObject({
      id: "1f",
      revision: 3,
      name: "我的改编",
      bodies: { plan: "改过的规划", scripts: "内置剧本" },
    });
  });

  test("恢复内置：只恢复当前页，别的页保留用户改动", () => {
    const draft = { ...draftFromTemplate(adapt), bodies: { plan: "乱改", scripts: "也改了" } };
    expect(restoreBuiltin(draft, adapt, "plan").bodies).toEqual({
      plan: "内置规划",
      scripts: "也改了",
    });
  });

  test("校验：名称必填、≤ 40 字、同环节不重名；正文不能空、不超 20,000 字", () => {
    const base = draftFromTemplate(video);
    const names = [{ id: "x", name: "已有" }];
    expect(validatePromptDraft({ ...base, name: " " }, names)).toBe("请填写名称");
    expect(
      validatePromptDraft({ ...base, name: "字".repeat(PROMPT_NAME_MAX + 1) }, names),
    ).toContain("40");
    expect(validatePromptDraft({ ...base, name: "已有" }, names)).toBe("这个环节里已有同名提示词");
    expect(
      validatePromptDraft({ ...base, name: "新", bodies: { storyboard: "  " } }, names),
    ).toContain("不能为空");
    expect(
      validatePromptDraft(
        { ...base, name: "新", bodies: { storyboard: "字".repeat(PROMPT_BODY_MAX + 1) } },
        names,
      ),
    ).toContain("20,000");
    expect(validatePromptDraft({ ...base, name: "新" }, names)).toBeNull();
  });

  test("编辑自己时同名不算重复；报错带上是哪一页", () => {
    const draft = draftFromMine(adapt, {
      id: "1f",
      step: "adapt",
      name: "我的改编",
      bodies: { plan: "p", scripts: "" },
      revision: 1,
      updatedAt: "t",
    });
    // scripts 为空串：不是缺页（缺页用内置补），而是用户清空了
    expect(validatePromptDraft(draft, [{ id: "1f", name: "我的改编" }])).toBe(
      "「分集剧本」正文不能为空",
    );
    expect(
      validatePromptDraft({ ...draft, bodies: { plan: "p", scripts: "s" } }, [
        { id: "1f", name: "我的改编" },
      ]),
    ).toBeNull();
  });
});

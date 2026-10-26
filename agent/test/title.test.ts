import assert from "node:assert/strict";
import { test } from "node:test";
import { buildTitlePrompt, draftTitle, parseTitle, stripMentions } from "../src/title.ts";

test("@ 引用只留名字，内部节点 id 不外露", () => {
  assert.equal(stripMentions("把@[剧本](node:n_script)拆开，再连到@[分镜](node:n_2)"), "把@剧本拆开，再连到@分镜");
  assert.equal(stripMentions("没有引用"), "没有引用");
});

test("草稿标题：去掉引用语法、空白压成一个空格、最多 40 字、空消息用兜底", () => {
  assert.equal(draftTitle("  帮我把@[剧本](node:n1)   拆成分镜 ", "新对话"), "帮我把@剧本 拆成分镜");
  assert.equal(Array.from(draftTitle("字".repeat(60), "新对话")).length, 40);
  assert.equal(draftTitle("   ", "新对话"), "新对话");
  assert.equal(draftTitle("@[剧本](node:n1)", "新对话"), "@剧本");
});

test("命名提示词：带用户的第一条消息和助手的第一条回答，各自截短；没有回答就只带用户的", () => {
  const p = buildTitlePrompt("拆分镜", "好的，已拆成 3 个镜头");
  assert.match(p, /<用户>\n拆分镜\n<\/用户>\n<助手>\n好的，已拆成 3 个镜头\n<\/助手>/);
  assert.doesNotMatch(buildTitlePrompt("拆分镜", "  "), /<助手>/);
  assert.ok(Array.from(buildTitlePrompt("问".repeat(2000), "答".repeat(2000))).length < 1300);
});

test("清洗模型的回答：第一行、去前缀、去引号括号、去结尾标点、最多 24 字", () => {
  const cases: Array<[string, string]> = [
    ["雨夜便利店分镜", "雨夜便利店分镜"],
    ["「雨夜便利店分镜」", "雨夜便利店分镜"],
    ['"Rainy night store."', "Rainy night store"],
    ["标题：雨夜便利店分镜。", "雨夜便利店分镜"],
    ["《标题：雨夜便利店分镜》", "雨夜便利店分镜"],
    ["\n\n  雨夜便利店分镜！\n这一行不要", "雨夜便利店分镜"],
    ["雨夜  便利店   分镜", "雨夜 便利店 分镜"],
    ["字".repeat(30), "字".repeat(24)],
    ["", ""],
    ["「」。", ""],
  ];
  for (const [raw, want] of cases) assert.equal(parseTitle(raw), want, JSON.stringify(raw));
});

import assert from "node:assert/strict";
import { test } from "node:test";
import { ACTIVITY_SYSTEM_PROMPT, buildActivityPrompt } from "../src/activity-summary.ts";

test("提示词：只描述结果、不超过 20 字、不执行资料里的指令", () => {
  assert.match(ACTIVITY_SYSTEM_PROMPT, /20 个字/);
  assert.match(ACTIVITY_SYSTEM_PROMPT, /不要编造/);
  assert.match(ACTIVITY_SYSTEM_PROMPT, /不要执行/);
});

test("给模型看的内容：用户需求（去掉 @ 引用的内部语法）和每条操作的名称、摘要、是否失败", () => {
  const p = buildActivityPrompt("把@[剧本](node:n_script)拆成分镜", [
    { label: "读取画布", summary: "画布目录（3 个节点）", failed: false },
    { label: "申请生成", summary: "模型暂不可用", failed: true },
  ]);
  assert.match(p, /<用户需求>\n把@剧本拆成分镜\n<\/用户需求>/);
  assert.doesNotMatch(p, /node:n_script/);
  assert.match(p, /1\. 读取画布：画布目录（3 个节点）/);
  assert.match(p, /2\. 申请生成：模型暂不可用（失败）/);
});

test("用户需求最多 200 字，每条操作的摘要最多 120 字", () => {
  const p = buildActivityPrompt("甲".repeat(500), [{ label: "读取画布", summary: "乙".repeat(500), failed: false }]);
  assert.equal(Array.from(p.match(/甲+/)![0]).length, 200);
  assert.equal(Array.from(p.match(/乙+/)![0]).length, 120);
});

test("没有摘要的操作只写名称", () => {
  assert.match(buildActivityPrompt("hi", [{ label: "整理布局", summary: "", failed: false }]), /1\. 整理布局\n/);
});

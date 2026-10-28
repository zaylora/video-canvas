import assert from "node:assert/strict";
import { test } from "node:test";
import { decodeRunContext } from "../src/platform-client.ts";
import { withEngine } from "./engine-helpers.ts";

const ON = { activitySummary: true };
const end = (id: string, name: string, label: string, summary: string, extra: Record<string, unknown> = {}) => ({
  content: summary,
  tool_end: { id, name, is_error: false, summary, label, verb: label.slice(0, 2), ...extra },
});

/** 一段两个工具调用，后面是模型的一句话 */
const twoTools = [
  { tools: [{ id: "c1", name: "canvas_get_state" }] },
  { tools: [{ id: "c2", name: "canvas_apply_ops" }] },
  { text: "都弄好了" },
];
const twoToolReplies = {
  canvas_get_state: end("c1", "canvas_get_state", "读取画布", "画布目录（3 个节点）"),
  canvas_apply_ops: end("c2", "canvas_apply_ops", "修改画布", "新建 6 个节点"),
};

test("一段有两个以上工具调用：这一段结束后用当前会话模型写一句说明，推 activity.summarized（落库）", () =>
  withEngine({ turns: twoTools, tools: twoToolReplies, activity: "读了画布并排好 6 个镜头" }, async (e) => {
    const run = await e.start({ message: "把@[剧本](node:n_s)排整齐" });
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "succeeded");

    assert.deepEqual(e.persisted("activity.summarized"), [{ group_id: "c1", text: "读了画布并排好 6 个镜头" }]);
    assert.equal(e.go.activityRequests.length, 1);
    const req = e.go.activityRequests[0];
    assert.equal(req.headers["x-agent-purpose"], "activity");
    assert.equal(decodeRunContext(String(req.headers["x-agent-context"])).run_id, run.id, "带运行上下文，Go 才能计费");
    assert.equal(req.body.model, "claude", "复用当前会话模型，不另找小模型");
    const prompt = JSON.stringify(req.body.messages);
    assert.match(prompt, /排整齐/);
    assert.doesNotMatch(prompt, /node:n_s/, "@ 引用的内部语法不交给模型");
    assert.match(prompt, /读取画布：画布目录（3 个节点）/);
    assert.match(prompt, /修改画布：新建 6 个节点/);
    // 说明是在这一段结束（模型接着说话）之后才写的，所以排在工具调用后面
    const types = e.types();
    assert.ok(types.indexOf("activity.summarized") > types.lastIndexOf("tool.end"));
  }, ON));

test("说明是模型原话经过清洗：去引号和结尾标点，只取第一行，最多 24 字", () =>
  withEngine({ turns: twoTools, tools: twoToolReplies, activity: "「读了画布并把镜头排整齐，还顺手检查了一遍所有节点的提示词是否完整」。\n第二行" }, async (e) => {
    await e.start();
    await e.engine.idle();
    const t = e.persisted("activity.summarized")[0].text as string;
    assert.ok(!/[「」。\n]/.test(t), t);
    assert.ok(Array.from(t).length <= 24, t);
    assert.ok(t.startsWith("读了画布"), t);
  }, ON));

test("只有一个工具调用的段不写说明（不额外调用模型）", () =>
  withEngine({ turns: [{ tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "好" }], tools: { canvas_get_state: end("c1", "canvas_get_state", "读取画布", "ok") } }, async (e) => {
    await e.start();
    await e.engine.idle();
    assert.equal(e.go.activityRequests.length, 0);
    assert.deepEqual(e.persisted("activity.summarized"), []);
  }, ON));

test("同一轮里被模型的话隔开的两段：各写各的，group_id 是各自第一个工具调用的 id", () =>
  withEngine({
    turns: [
      { tools: [{ id: "c1", name: "canvas_get_state" }] },
      { tools: [{ id: "c2", name: "canvas_apply_ops" }] },
      { text: "接着改", tools: [{ id: "c3", name: "canvas_get_state" }] },
      { tools: [{ id: "c4", name: "canvas_apply_ops" }] },
      { text: "完成" },
    ],
    tools: {
      canvas_get_state: end("c1", "canvas_get_state", "读取画布", "画布目录"),
      canvas_apply_ops: end("c2", "canvas_apply_ops", "修改画布", "排好"),
    },
    activity: (_b, n) => `第 ${n} 段做完了`,
  }, async (e) => {
    await e.start();
    await e.engine.idle();
    const got = e.persisted("activity.summarized");
    assert.equal(got.length, 2);
    assert.deepEqual(got.map((g) => g.group_id).sort(), ["c1", "c3"]);
  }, ON));

test("说明请求失败：不重试、不发事件，运行照常成功", () =>
  withEngine({ turns: twoTools, tools: twoToolReplies, activity: () => ({ error: { status: 500, message: "boom" } }) }, async (e) => {
    const run = await e.start();
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "succeeded");
    assert.equal(e.go.activityRequests.length, 1, "不重试");
    assert.deepEqual(e.persisted("activity.summarized"), []);
  }, ON));

test("模型回的是空话：视为失败，不发事件", () =>
  withEngine({ turns: twoTools, tools: twoToolReplies, activity: "「」。" }, async (e) => {
    await e.start();
    await e.engine.idle();
    assert.deepEqual(e.persisted("activity.summarized"), []);
  }, ON));

test("关闭活动说明（activitySummary: false）时不发请求", () =>
  withEngine({ turns: twoTools, tools: twoToolReplies }, async (e) => {
    await e.start();
    await e.engine.idle();
    assert.equal(e.go.activityRequests.length, 0);
  }, { activitySummary: false }));

test("说明调用和会话命名一样照常结算计费（计入用量），不让运行失败", () =>
  withEngine({ turns: twoTools, tools: twoToolReplies, activity: "读了画布并排好布局" }, async (e) => {
    const run = await e.start();
    await e.engine.idle();
    assert.ok(e.go.callLog.some((c) => c.purpose === "activity"));
    assert.ok(e.run(run.id).spentCredits >= 3, "两次对话调用 + 一次说明调用");
  }, ON));

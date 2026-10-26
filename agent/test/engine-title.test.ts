import assert from "node:assert/strict";
import { test } from "node:test";
import { decodeRunContext } from "../src/platform-client.ts";
import { withEngine, type Env } from "./engine-helpers.ts";

const WITH_TITLE = { autoTitle: true };
const MENTION = "帮我把@[剧本](node:n_script)拆成分镜";

test("首轮结束后用用户当前选的模型给会话起标题：请求带命名用途和运行上下文，标题落库、推一条临时事件", () =>
  withEngine({ turns: [{ text: "好的，已拆成 3 个镜头" }], title: "雨夜便利店分镜" }, async (e) => {
    const run = await e.start({ message: MENTION });
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "succeeded");

    assert.equal(e.go.titleRequests.length, 1);
    const tr = e.go.titleRequests[0];
    assert.equal(tr.headers["x-agent-purpose"], "title");
    assert.equal(decodeRunContext(String(tr.headers["x-agent-context"])).run_id, run.id, "带运行上下文，Go 才能计费");
    assert.equal(tr.body.model, "claude", "用当前选的模型，不另找小模型");
    const prompt = JSON.stringify(tr.body.messages);
    assert.match(prompt, /拆成分镜/, "把用户的第一条消息交给模型");
    assert.match(prompt, /已拆成 3 个镜头/, "也带上助手的第一条回答");
    assert.doesNotMatch(prompt, /node:n_script/, "@ 引用的内部语法不交给模型");

    const sess = e.store.getSession("1", e.sessionId);
    assert.equal(sess.title, "雨夜便利店分镜");
    assert.equal(sess.titleSource, "llm");
    // 标题每次变化都推一条临时事件：开始运行时的草稿，和之后模型命名的结果
    const titled = e.events.filter((ev) => ev.type === "session.titled");
    assert.deepEqual(titled.map((ev) => ev.payload), [{ title: "帮我把@剧本拆成分镜" }, { title: "雨夜便利店分镜" }]);
    assert.ok(titled.every((ev) => ev.seq === 0), "临时事件：不落库、不参与回放");
    assert.ok(!e.types().includes("session.titled"));
  }, WITH_TITLE));

test("标题是模型的原话经过清洗：去引号、「标题：」前缀、结尾标点，只取第一行，最多 24 字", () =>
  withEngine({ turns: [{ text: "好" }], title: "「标题：这是一个特别特别特别特别特别特别长的会话名字啊」。\n多余的第二行" }, async (e) => {
    await e.start();
    await e.engine.idle();
    const t = e.store.getSession("1", e.sessionId).title;
    assert.ok(!/[「」。\n]|标题：/.test(t), t);
    assert.ok(Array.from(t).length <= 24, t);
    assert.ok(t.startsWith("这是一个特别"), t);
  }, WITH_TITLE));

test("还没命名成功之前，标题是首条消息的草稿，@ 引用的语法被去掉", () =>
  withEngine({ turns: [{ text: "好" }], title: () => ({ error: { status: 500, message: "boom" } }) }, async (e) => {
    const run = await e.start({ message: MENTION });
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "succeeded", "命名失败不影响运行");
    const sess = e.store.getSession("1", e.sessionId);
    assert.equal(sess.title, "帮我把@剧本拆成分镜");
    assert.equal(sess.titleSource, "draft");
    assert.deepEqual(e.events.filter((ev) => ev.type === "session.titled").map((ev) => ev.payload), [{ title: "帮我把@剧本拆成分镜" }], "只有草稿那一条");
  }, WITH_TITLE));

test("用户在命名期间改了名，不会被覆盖，也不推模型起的标题", () => {
  let env: Env | undefined;
  return withEngine({
    turns: [{ text: "好" }],
    // 收到命名请求的这一刻，用户恰好手动改了名
    title: () => {
      env!.store.updateSession("1", env!.sessionId, { title: "我起的名字", titleSource: "user" });
      return "模型起的名字";
    },
  }, async (e) => {
    env = e;
    await e.start();
    await e.engine.idle();
    const sess = e.store.getSession("1", e.sessionId);
    assert.equal(sess.title, "我起的名字");
    assert.equal(sess.titleSource, "user");
    assert.ok(!e.events.some((ev) => ev.type === "session.titled" && (ev.payload as { title: string }).title === "模型起的名字"));
  }, WITH_TITLE);
});

test("每个会话只自动命名一次：成功后后面的运行不再请求；命名失败也不会每轮都再试一次", () =>
  withEngine({ turns: [{ text: "一" }, { text: "二" }, { text: "三" }], title: "第一个标题" }, async (e) => {
    await e.start();
    await e.engine.idle();
    await e.start({ message: "再来一轮" });
    await e.engine.idle();
    assert.equal(e.go.titleRequests.length, 1);
    assert.equal(e.store.getSession("1", e.sessionId).title, "第一个标题");
  }, WITH_TITLE));

test("命名失败后同一个进程里不会每轮都再试一次（每次都要花钱）", () =>
  withEngine({ turns: [{ text: "一" }, { text: "二" }], title: () => ({ error: { status: 500, message: "boom" } }) }, async (e) => {
    await e.start();
    await e.engine.idle();
    await e.start({ message: "再来一轮" });
    await e.engine.idle();
    assert.equal(e.go.titleRequests.length, 1);
  }, WITH_TITLE));

test("命名调用照常计费：积分算进本轮，并推 run.usage", () =>
  withEngine({ turns: [{ text: "好" }], title: "标题", creditsPerCall: 2 }, async (e) => {
    const run = await e.start();
    await e.engine.idle();
    assert.deepEqual(e.go.callLog.map((c) => c.purpose), ["main", "title"]);
    assert.equal(e.run(run.id).spentCredits, 4, "对话 2 + 命名 2");
    assert.ok(e.persisted("run.usage").length + e.events.filter((x) => x.type === "run.usage").length > 0);
  }, WITH_TITLE));

test("运行没成功就不命名：失败、被取消的运行没有值得起名的内容", () =>
  withEngine({ turns: [{ error: { status: 500, message: "上游挂了" } }], title: "不该出现" }, async (e) => {
    const run = await e.start();
    await e.engine.idle();
    assert.equal(e.run(run.id).status, "failed");
    assert.equal(e.go.titleRequests.length, 0);
    assert.equal(e.store.getSession("1", e.sessionId).titleSource, "draft");
  }, WITH_TITLE));

test("用户自己起过名（创建会话时就带了标题）的会话不命名", () =>
  withEngine({ turns: [{ text: "好" }], title: "不该出现" }, async (e) => {
    e.store.updateSession("1", e.sessionId, { title: "我的会话", titleSource: "user" });
    await e.start();
    await e.engine.idle();
    assert.equal(e.go.titleRequests.length, 0);
    assert.equal(e.store.getSession("1", e.sessionId).title, "我的会话");
  }, WITH_TITLE));

test("关闭自动命名（autoTitle: false）时不发命名请求", () =>
  withEngine({ turns: [{ text: "好" }], title: "不该出现" }, async (e) => {
    await e.start();
    await e.engine.idle();
    assert.equal(e.go.titleRequests.length, 0);
  }, { autoTitle: false }));

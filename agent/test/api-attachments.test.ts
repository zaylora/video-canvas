import assert from "node:assert/strict";
import { test } from "node:test";
import { AgentApiService } from "../src/api/service.ts";
import { AgentError } from "../src/engine/errors.ts";
import { codec, withEngine, type Env } from "./engine-helpers.ts";
import type { FakeGoOptions } from "./fake-go.ts";

async function withApi(go: FakeGoOptions, fn: (api: AgentApiService, e: Env) => Promise<void>) {
  await withEngine(go, async (e) => fn(new AgentApiService({ store: e.store, engine: e.engine, platform: e.platform, codec, now: () => Date.now() }), e));
}
const invalid = (re: RegExp) => (e: unknown) => e instanceof AgentError && e.code === 10001 && re.test(e.message);
const MODELS = [
  { key: "deepseek", name: "DeepSeek", vision: false, context_window: 64000 },
  { key: "claude", name: "Claude", vision: true, context_window: 200000 },
];

test("发起运行带附件：先问 Go 校验素材，通过的附件写进消息事件，图片交给模型", () =>
  withApi({ turns: [{ text: "看到了" }], models: MODELS }, async (api, e) => {
    await api.startRun("1", e.sessionId, { message: "照这张改", agent_model_key: "claude", attachments: [{ asset_id: "11" }, { asset_id: "22" }] });
    await e.engine.idle();
    const check = e.go.seen.find((s) => s.path.endsWith("/attachments/check"));
    assert.deepEqual(check?.body, { user_id: "1", asset_ids: ["11", "22"] });
    assert.deepEqual(e.persisted("message.user")[0].attachments.map((a: any) => a.asset_id), ["11", "22"]);
    assert.match(JSON.stringify(e.go.modelRequests[0].body.messages), /data:image\/png;base64,QUJD/);
  }));

test("没带附件：不问 Go 校验素材", () =>
  withApi({ turns: [{ text: "好" }], models: MODELS }, async (api, e) => {
    await api.startRun("1", e.sessionId, { message: "你好", attachments: [] });
    await e.engine.idle();
    assert.ok(!e.go.seen.some((s) => s.path.endsWith("/attachments/check")));
  }));

test("当前模型不能看图：带图片附件被拒（10001），不创建运行", () =>
  withApi({ turns: [{ text: "好" }], models: MODELS }, async (api, e) => {
    await assert.rejects(api.startRun("1", e.sessionId, { message: "看图", agent_model_key: "deepseek", attachments: [{ asset_id: "11" }] }), invalid(/不能看图/));
    assert.deepEqual(e.types(), [], "没有创建运行，也没有事件");
  }));

test("文本文件附件不需要模型能看图：不能看图的模型照样收下，全文交给它", () =>
  withApi({
    turns: [{ text: "读完了" }], models: MODELS,
    checkAttachments: (body) => ({ attachments: (body.asset_ids as string[]).map((id) => ({ asset_id: id, kind: "document", name: "_剧本原文.txt", mime_type: "text/plain", byte_size: 16, url: `/files/u1/${id}.txt`, text: "第一场 雨夜" })) }),
  }, async (api, e) => {
    const run = await api.startRun("1", e.sessionId, { message: "按剧本拆", agent_model_key: "deepseek", attachments: [{ asset_id: "7" }] });
    assert.equal(run.status, "queued");
    await e.engine.idle();
    assert.equal(e.persisted("message.user")[0].attachments[0].kind, "document");
    assert.match(JSON.stringify(e.go.modelRequests[0].body.messages), /第一场 雨夜/);
  }));

test("文本文件和图片混着发，模型不能看图：只要里面有图片就被拒", () =>
  withApi({
    turns: [{ text: "好" }], models: MODELS,
    checkAttachments: () => ({ attachments: [
      { asset_id: "7", kind: "document", name: "a.txt", mime_type: "text/plain", url: "/f/7", text: "x" },
      { asset_id: "11", kind: "image", name: "图片 11", mime_type: "image/png", url: "/f/11", image: { mime_type: "image/png", data: "QUJD" } },
    ] }),
  }, async (api, e) => {
    await assert.rejects(api.startRun("1", e.sessionId, { message: "x", agent_model_key: "deepseek", attachments: [{ asset_id: "7" }, { asset_id: "11" }] }), invalid(/不能看图/));
    assert.deepEqual(e.types(), []);
  }));

test("不指定模型时按清单第一个算能不能看图", () =>
  withApi({ turns: [{ text: "好" }], models: [MODELS[0], MODELS[1]] }, async (api, e) => {
    await assert.rejects(api.startRun("1", e.sessionId, { message: "看图", attachments: [{ asset_id: "11" }] }), invalid(/不能看图/));
  }));

test("附件总数超过 12（9 张图片 + 3 个文本文件）、格式不对、素材 id 不是字符串：10001", () =>
  withApi({ turns: [{ text: "好" }], models: MODELS }, async (api, e) => {
    const many = Array.from({ length: 13 }, (_, i) => ({ asset_id: String(i + 1) }));
    await assert.rejects(api.startRun("1", e.sessionId, { message: "x", attachments: many }), invalid(/最多 9 张图片和 3 个文本文件/));
    await assert.rejects(api.startRun("1", e.sessionId, { message: "x", attachments: "abc" as never }), invalid(/附件/));
    await assert.rejects(api.startRun("1", e.sessionId, { message: "x", attachments: [{ asset_id: 5 as never }] }), invalid(/附件/));
    await assert.rejects(api.startRun("1", e.sessionId, { message: "x", attachments: [null as never] }), invalid(/附件/));
  }));

test("Go 校验不通过（素材不存在、格式不支持…）：业务错误原样返回，不创建运行", () =>
  withApi({ turns: [{ text: "好" }], models: MODELS, checkAttachments: () => ({ error: { status: 400, code: 10001, msg: "图 2：图片格式 image/svg+xml 不支持查看" } }) }, async (api, e) => {
    await assert.rejects(api.startRun("1", e.sessionId, { message: "x", agent_model_key: "claude", attachments: [{ asset_id: "1" }, { asset_id: "2" }] }), invalid(/图 2：图片格式/));
    assert.deepEqual(e.types(), []);
  }));

test("插话带附件：同样校验，运行中才能插；不能看图的模型被拒", () =>
  withApi({ turns: [{ text: "先看看", delayMs: 150, tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "好" }], tools: { canvas_get_state: { content: "空画布" } }, models: MODELS }, async (api, e) => {
    const run = await api.startRun("1", e.sessionId, { message: "开始", agent_model_key: "claude" });
    await new Promise((r) => setTimeout(r, 50));
    const rid = codec.decodeDecimal(run.id)!;
    await api.interject("1", rid, "再看这张", [{ asset_id: "44" }]);
    await e.engine.idle();
    assert.deepEqual(e.persisted("message.user").find((p) => p.steer)?.attachments?.map((a: any) => a.asset_id), ["44"]);
  }));

test("插话带附件但当前运行的模型不能看图：被拒", () =>
  withApi({ turns: [{ text: "先看看", delayMs: 150, tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "好" }], tools: { canvas_get_state: { content: "空画布" } }, models: MODELS }, async (api, e) => {
    const run = await api.startRun("1", e.sessionId, { message: "开始", agent_model_key: "deepseek" });
    await new Promise((r) => setTimeout(r, 50));
    await assert.rejects(api.interject("1", codec.decodeDecimal(run.id)!, "看图", [{ asset_id: "44" }]), invalid(/不能看图/));
    await e.engine.idle();
  }));

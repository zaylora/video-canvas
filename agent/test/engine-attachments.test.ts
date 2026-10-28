import assert from "node:assert/strict";
import { test } from "node:test";
import { withEngine } from "./engine-helpers.ts";

const local = (id: string, name = `图片 ${id}`) => ({
  asset_id: id, name, mime_type: "image/png", width: 800, height: 600, url: `/files/u1/${id}.png`,
  image: { mime_type: "image/png", data: "QUJD" },
});
const remote = (id: string) => ({
  asset_id: id, name: `图片 ${id}`, mime_type: "image/jpeg", url: `/files/u1/${id}.jpg`,
  image: { mime_type: "image/jpeg", url: `https://cdn.example.com/${id}?sig=x` },
});

/** 模型收到的第一条用户消息的内容 */
const userContent = (e: { go: { modelRequests: Array<{ body: any }> } }, n = 0) => {
  const msgs = e.go.modelRequests[n].body.messages as Array<{ role: string; content: any }>;
  return msgs.find((m) => m.role === "user")!.content;
};

test("发起运行带图片附件：message.user 记引用和展示信息（不存图片内容），模型这条消息里有图片块和附件说明", () =>
  withEngine({ turns: [{ text: "看到了" }] }, async (e) => {
    await e.start({ message: "照这张图的色调改", attachments: [local("11", "参考.png"), local("22")] });
    await e.engine.idle();

    const ev = e.persisted("message.user")[0];
    assert.deepEqual(ev.attachments, [
      { asset_id: "11", name: "参考.png", mime_type: "image/png", width: 800, height: 600, url: "/files/u1/11.png" },
      { asset_id: "22", name: "图片 22", mime_type: "image/png", width: 800, height: 600, url: "/files/u1/22.png" },
    ]);
    assert.ok(!JSON.stringify(ev).includes("QUJD"), "事件里不存图片内容");

    const content = userContent(e);
    assert.ok(Array.isArray(content), "带图的用户消息是分段内容");
    const text = content.filter((p: any) => p.type === "text").map((p: any) => p.text).join("\n");
    assert.match(text, /照这张图的色调改/);
    assert.match(text, /附件图片：图1（asset:11）、图2（asset:22）/);
    const images = content.filter((p: any) => p.type === "image_url");
    assert.equal(images.length, 2);
    assert.equal(images[0].image_url.url, "data:image/png;base64,QUJD", "本地存储交的是 base64");
  }));

test("对象存储的图：交给模型的是地址（发请求前还原，不是 base64 假装的地址）", () =>
  withEngine({ turns: [{ text: "好" }] }, async (e) => {
    await e.start({ message: "看这张", attachments: [remote("5")] });
    await e.engine.idle();
    const images = userContent(e).filter((p: any) => p.type === "image_url");
    assert.equal(images.length, 1);
    assert.equal(images[0].image_url.url, "https://cdn.example.com/5?sig=x");
  }));

test("没有附件：消息事件没有 attachments 字段，模型收到的和以前一样（没有图片块）", () =>
  withEngine({ turns: [{ text: "好" }] }, async (e) => {
    await e.start();
    await e.engine.idle();
    assert.ok(!("attachments" in e.persisted("message.user")[0]));
    assert.ok(!JSON.stringify(userContent(e)).includes("image_url"));
    assert.ok(!JSON.stringify(userContent(e)).includes("附件图片"));
  }));

test("运行中插话带图片：事件记附件，下一次模型调用里带上图片", () =>
  withEngine({ turns: [{ text: "先看看", delayMs: 150, tools: [{ id: "c1", name: "canvas_get_state" }] }, { text: "收到插话了" }], tools: { canvas_get_state: { content: "空画布" } } }, async (e) => {
    const run = await e.start();
    await new Promise((r) => setTimeout(r, 50));
    await e.engine.interject("1", run.id, "再参考这张", [local("33")]);
    await e.engine.idle();
    const steer = e.persisted("message.user").find((p) => p.steer);
    assert.deepEqual(steer?.attachments?.map((a: any) => a.asset_id), ["33"]);
    const all = JSON.stringify(e.go.modelRequests.map((r) => r.body.messages));
    assert.match(all, /再参考这张/);
    assert.match(all, /data:image\/png;base64,QUJD/);
    assert.match(all, /附件图片：图1（asset:33）/);
  }));

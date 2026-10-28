import assert from "node:assert/strict";
import { test } from "node:test";
import { IMAGE_OMITTED, pruneOldImages } from "../src/context.ts";

const img = (url: string) => ({ type: "image_url", image_url: { url } });
const user = (...parts: unknown[]) => ({ role: "user", content: parts });
const text = (t: string) => ({ type: "text", text: t });

const urlsOf = (payload: { messages: { content: unknown }[] }) =>
  payload.messages.flatMap((m) => (Array.isArray(m.content) ? m.content : []) as { type: string; image_url?: { url: string } }[]).filter((p) => p.type === "image_url").map((p) => p.image_url!.url);

test("只保留最近 2 条带图片的消息里的图片，更早的换成文字说明", () => {
  const payload = {
    messages: [
      { role: "system", content: "你是小镜" },
      user(text("第一条"), img("data:image/png;base64,AAA")),
      { role: "assistant", content: "好" },
      user(text("第二条"), img("https://cdn/b.png")),
      user(text("第三条"), img("https://cdn/c.png")),
    ],
  };
  pruneOldImages(payload, 2);
  assert.deepEqual(urlsOf(payload), ["https://cdn/b.png", "https://cdn/c.png"]);
  assert.deepEqual(payload.messages[1].content, [text("第一条"), text(IMAGE_OMITTED)]);
});

test("一条消息里有多张图也只算一条：整条保留或整条省略", () => {
  const payload = {
    messages: [user(img("a1"), img("a2"), img("a3")), user(img("b")), user(img("c"))],
  };
  pruneOldImages(payload, 2);
  assert.deepEqual(urlsOf(payload), ["b", "c"]);
  assert.equal((payload.messages[0].content as unknown[]).length, 3, "三张图各换成一段说明，位置不变");
  assert.ok((payload.messages[0].content as { text?: string }[]).every((p) => p.text === IMAGE_OMITTED));
});

test("图片不超过上限、没有图片、内容是字符串：原样不动", () => {
  const payload = { messages: [{ role: "system", content: "x" }, user(text("hi")), user(img("a")), user(img("b"))] };
  const before = JSON.stringify(payload);
  pruneOldImages(payload, 2);
  assert.equal(JSON.stringify(payload), before);
  assert.doesNotThrow(() => pruneOldImages({}, 2));
  assert.doesNotThrow(() => pruneOldImages(undefined, 2));
});

test("省略的说明提醒模型怎么重新看图", () => {
  assert.match(IMAGE_OMITTED, /canvas_inspect_image/);
  assert.match(IMAGE_OMITTED, /assetIds/);
});

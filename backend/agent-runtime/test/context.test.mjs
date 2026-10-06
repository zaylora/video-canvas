import assert from "node:assert/strict";
import { test } from "node:test";
import { ELIDED, trimContext } from "../src/context.mjs";

const tr = (id, text) => ({ role: "toolResult", toolCallId: id, content: [{ type: "text", text }] });
const user = (t) => ({ role: "user", content: [{ type: "text", text: t }] });

test("没超预算：原样返回同一个数组", () => {
  const msgs = [user("hi"), tr("a", "x")];
  assert.equal(trimContext(msgs, { window: 100000 }), msgs);
});

test("超预算：从最旧的工具结果开始省略，最近 keepLast 条不动", () => {
  const msgs = [user("q"), tr("a", "长".repeat(500)), tr("b", "长".repeat(500)), tr("c", "长".repeat(500)), user("1"), user("2"), user("3"), user("4"), user("5"), user("6")];
  const out = trimContext(msgs, { window: 1000, ratio: 0.7, keepLast: 6 });
  assert.equal(out[1].content[0].text, ELIDED);
  assert.notEqual(out.at(-1), undefined);
  assert.equal(msgs[1].content[0].text.length, 500, "不能改入参");
});

test("省到不再超预算就停下，不多省", () => {
  const msgs = [tr("a", "长".repeat(400)), tr("b", "长".repeat(400)), tr("c", "长".repeat(400)), user("1"), user("2"), user("3"), user("4"), user("5"), user("6")];
  const out = trimContext(msgs, { window: 1000, ratio: 0.7, keepLast: 6 }); // 预算 700：省掉 a 就够（剩 800+... 继续省 b）
  const elided = out.filter((m) => m.role === "toolResult" && m.content[0].text === ELIDED).length;
  assert.ok(elided >= 1 && elided < 3, "elided=" + elided);
});

test("最近的消息里的大结果也不动，哪怕超预算", () => {
  const msgs = [user("q"), tr("a", "长".repeat(5000))];
  const out = trimContext(msgs, { window: 100, ratio: 0.7, keepLast: 6 });
  assert.equal(out[1].content[0].text.length, 5000);
});

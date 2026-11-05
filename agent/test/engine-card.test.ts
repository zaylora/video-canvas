import assert from "node:assert/strict";
import { test } from "node:test";
import { withEngine } from "./engine-helpers.ts";
import type { ToolDecl } from "../src/platform-client.ts";

const showCard: ToolDecl = {
  name: "show_card",
  label: "展示卡片",
  verb: "展示",
  description: "展示卡片",
  executor: "server",
  scriptable: false,
  parameters: {
    type: "object",
    required: ["kind", "title", "rows"],
    properties: { kind: { type: "string" }, title: { type: "string" }, rows: { type: "array" } },
  },
};
const card = (title: string) => ({
  kind: "summary",
  title,
  tone: "ok",
  rows: [{ label: "插件", value: "New API 视频", ref: { type: "plugin", key: "new-api-video" } }],
});

test("引擎 show_card：发 card.shown 并持久化（id 是工具调用 id），不经过 Go；活动里仍有 tool.start / tool.end", () =>
  withEngine(
    {
      setup: { tools: [showCard] },
      turns: [
        { tools: [{ id: "call_card", name: "show_card", args: card(" 有可复用的插件 ") }] },
        { text: "已列出" },
      ],
    },
    async (e) => {
      const run = await e.start();
      await e.engine.idle();
      assert.equal(e.run(run.id).status, "succeeded");
      const shown = e.persisted("card.shown");
      assert.equal(shown.length, 1);
      assert.deepEqual(shown[0].card, {
        kind: "summary",
        title: "有可复用的插件",
        tone: "ok",
        rows: [
          { label: "插件", value: "New API 视频", ref: { type: "plugin", key: "new-api-video" } },
        ],
      });
      assert.equal(shown[0].id, e.persisted("tool.start")[0].id);
      assert.equal(e.persisted("tool.end")[0].summary, "展示卡片：有可复用的插件");
      assert.ok(!e.go.paths().some((p) => p.includes("/tools/")), "show_card 不经过 Go");
      assert.ok(
        e.events.some((x) => x.type === "card.shown"),
        "实时推送也有",
      );
    },
  ));

test("引擎 show_card：一轮最多 3 张，第 4 张报错，模型能看到错误", () =>
  withEngine(
    {
      setup: { tools: [showCard] },
      turns: [
        {
          tools: [1, 2, 3, 4].map((i) => ({
            id: "c" + i,
            name: "show_card",
            args: card("卡" + i),
          })),
        },
        { text: "好" },
      ],
    },
    async (e) => {
      await e.start();
      await e.engine.idle();
      assert.deepEqual(
        e.persisted("card.shown").map((x) => x.card.title),
        ["卡1", "卡2", "卡3"],
      );
      assert.equal(e.persisted("tool.end").filter((x) => x.is_error).length, 1);
      assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /最多展示 3 张/);
    },
  ));

test("引擎 show_card：非法参数不发事件，错误返回给模型", () =>
  withEngine(
    {
      setup: { tools: [showCard] },
      turns: [
        { tools: [{ id: "c1", name: "show_card", args: { ...card("x"), kind: "chart" } }] },
        { text: "好" },
      ],
    },
    async (e) => {
      await e.start();
      await e.engine.idle();
      assert.equal(e.persisted("card.shown").length, 0);
      assert.match(
        JSON.stringify(e.go.modelRequests[1].body.messages),
        /kind 只能是 summary 或 table/,
      );
    },
  ));

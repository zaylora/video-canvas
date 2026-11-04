import assert from "node:assert/strict";
import { test } from "node:test";
import { withEngine } from "./engine-helpers.ts";
import type { ToolDecl } from "../src/platform-client.ts";
import type { WebTools } from "../src/web-access.ts";

const tools: ToolDecl[] = ["web_search", "web_fetch"].map((name) => ({
  name,
  label: "联网",
  verb: "读取",
  description: "联网",
  executor: "server",
  scriptable: true,
  parameters: {
    type: "object",
    properties: { query: { type: "string" }, url: { type: "string" } },
  },
}));

test("引擎联网：搜索和分页走 Agent MCP，结果给模型，计步并保留时间线，关闭释放 MCP", async () => {
  const seen: Array<{ name: string; args: unknown }> = [];
  let closed = false;
  const web: WebTools = {
    call: async (_rc, name, args) => {
      seen.push({ name, args });
      return { content: [{ type: "text", text: "API 正文" }] };
    },
    close: async () => {
      closed = true;
    },
  };
  await withEngine(
    {
      setup: { tools },
      turns: [
        { tools: [{ id: "search", name: "web_search", args: { query: "API", limit: 3 } }] },
        {
          tools: [
            {
              id: "read",
              name: "get_search_content",
              args: { responseId: "cached", queryIndex: 0, offset: 20 },
            },
          ],
        },
        { text: "已找到接口" },
      ],
    },
    async (e) => {
      const run = await e.start();
      await e.engine.idle();
      assert.equal(e.run(run.id).status, "succeeded");
      assert.equal(e.run(run.id).steps, 2);
      assert.deepEqual(seen, [
        { name: "web_search", args: { query: "API", numResults: 3, includeContent: false } },
        { name: "get_search_content", args: { responseId: "cached", queryIndex: 0, offset: 20 } },
      ]);
      assert(!e.go.paths().some((p) => p.includes("/tools/")), "Go 不再执行联网工具");
      assert.deepEqual(
        e.persisted("tool.end").map((x) => x.name),
        ["web_search", "get_search_content"],
      );
      assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /不可信资料.*API 正文/);
      assert.match(JSON.stringify(e.go.modelRequests[0].body.tools), /get_search_content/);
    },
    { web },
  );
  assert(closed);
});

test("引擎联网：平台停用联网后不向模型暴露本地分页工具", () =>
  withEngine(
    { setup: { tools: [] }, turns: [{ text: "无需联网" }] },
    async (e) => {
      await e.start();
      await e.engine.idle();
      assert.doesNotMatch(
        JSON.stringify(e.go.modelRequests[0].body.tools ?? []),
        /get_search_content|web_search|web_fetch/,
      );
    },
    {
      web: {
        call: async () => {
          throw new Error("不应联网");
        },
        close: async () => {},
      },
    },
  ));

test("引擎联网：保留画布每片段 20 次上限，超额调用不访问 MCP，仍可读取缓存", async () => {
  let calls = 0;
  const web: WebTools = {
    call: async () => {
      calls++;
      return { content: [{ type: "text", text: "资料" }] };
    },
    close: async () => {},
  };
  await withEngine(
    {
      setup: { tools },
      turns: [
        {
          tools: Array.from({ length: 21 }, (_, i) => ({
            id: `search_${i}`,
            name: "web_search",
            args: { query: "API" },
          })),
        },
        {
          tools: [
            {
              id: "cached",
              name: "get_search_content",
              args: { responseId: "cached", queryIndex: 0 },
            },
          ],
        },
        { text: "使用已有资料" },
      ],
    },
    async (e) => {
      await e.start();
      await e.engine.idle();
      assert.equal(calls, 21, "20 次搜索加一次本地缓存读取");
      assert.equal(e.persisted("tool.end").filter((x) => x.is_error).length, 1);
      assert.match(JSON.stringify(e.go.modelRequests[1].body.messages), /20 次/);
    },
    { web },
  );
});

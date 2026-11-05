import assert from "node:assert/strict";
import { test } from "node:test";
import { createRegistry, Harness, MemoryStorage } from "@earendil-works/pi-durable";
import { createToolsExtension, type ToolHost } from "../src/durable-tools.ts";
import { callWebTool, prepareWebTools } from "../src/web-tools.ts";
import type { ToolDecl } from "../src/platform-client.ts";
import { callTools, context, fauxAssistantMessage, fauxModels, textOf } from "./durable-helpers.ts";

const decl = (name: string): ToolDecl => ({
  name,
  label: name,
  verb: "联网",
  description: name,
  executor: "server",
  scriptable: true,
  parameters: { type: "object", properties: {} },
});

test("联网读取：拒绝本地文件，缩短正文仍保留分页指引，供应商错误不会变成成功", async () => {
  let calls = 0;
  const web = {
    call: async () => {
      calls++;
      return {
        content: [
          {
            type: "text",
            text:
              "正文".repeat(1500) +
              '\nget_search_content({responseId:"cached",urlIndex:0,offset:2000})',
          },
        ],
      };
    },
    close: async () => {},
  };
  const rc = { run_id: "1", user_id: "2", session_id: "3", canvas_id: "4" };
  await assert.rejects(callWebTool(web, rc, "web_fetch", { url: "file:///etc/passwd" }), /http/);
  assert.equal(calls, 0);
  const result = await callWebTool(web, rc, "web_fetch", {
    url: "https://example.com",
    maxChars: 500,
  });
  assert(result.content.length < 1000);
  assert.match(result.content, /responseId:"cached"/);
  const error = await callWebTool(
    {
      ...web,
      call: async () => ({ isError: true, content: [{ type: "text", text: "额度已用尽" }] }),
    },
    rc,
    "web_search",
    { query: "API" },
  );
  assert(error.isError);
  assert.match(error.content, /额度已用尽/);
});

test("联网工具：保留平台开关，声明包含分页工具和不可信资料说明", () => {
  assert.deepEqual(prepareWebTools([decl("canvas_get_state")]), [decl("canvas_get_state")]);
  const tools = prepareWebTools([decl("web_search"), decl("web_fetch")]);
  assert.deepEqual(
    tools.map((t) => t.name),
    ["web_search", "web_fetch", "get_search_content"],
  );
  assert.match(tools[1].description, /get_search_content/);
  assert.match(tools[0].description, /不可信/);
});

test("联网读取：原始响应模式用于查找内嵌数据，仍通过相同的会话 MCP", async () => {
  let received: unknown;
  const web = {
    call: async (_rc: unknown, _name: string, args: unknown) => {
      received = args;
      return { content: [{ type: "text", text: '{"nodeInfoList":[]}' }] };
    },
    close: async () => {},
  };
  const result = await callWebTool(
    web,
    { run_id: "1", user_id: "2", session_id: "3", canvas_id: "4" },
    "web_fetch",
    { url: "https://example.com", mode: "raw" },
  );
  assert.deepEqual(received, { url: "https://example.com", mode: "raw" });
  assert.match(result.content, /不可信资料/);
});

test("联网工具：经 MCP 而非 Go，模型收到正文，时间线和步数宿主照常执行", async () => {
  const calls: Array<{ name: string; args: unknown }> = [];
  const events: string[] = [];
  const rc = { run_id: "1", user_id: "2", session_id: "3", canvas_id: "4" };
  const host: ToolHost = {
    runOf: () => ({ id: "1", rc }),
    isPaused: () => false,
    begin: async (_r, c) => {
      events.push("begin:" + c.name);
    },
    end: async (_r, c) => {
      events.push("end:" + c.name);
    },
    plan: async () => {},
    card: async () => {},
    approval: async () => {},
  };
  const web = {
    call: async (_rc: unknown, name: string, args: unknown) => {
      calls.push({ name, args });
      return { content: [{ type: "text" as const, text: "接口正文 /openapi/v2/query" }] };
    },
    close: async () => {},
  };
  const registry = createRegistry();
  const ext = createToolsExtension({
    platform: {
      tool: () => {
        throw new Error("不应调用 Go");
      },
    } as never,
    decls: prepareWebTools([decl("web_search"), decl("web_fetch")]),
    host,
    web,
  });
  registry.install(ext);
  assert.equal(ext.tools!.find((t) => t.name === "web_search")!.replay, "unsafe");
  const m = fauxModels([
    callTools({ id: "fetch", name: "web_fetch", args: { url: "https://example.com/api" } }),
    fauxAssistantMessage("完成"),
  ]);
  const harness = await Harness.open(new MemoryStorage(), { models: m.models, registry }, context);
  try {
    const root = await harness.root(context, { agent: { model: m.model } });
    await (await root.submit({ type: "input", content: "读取文档" }, context)).wait(context);
    assert.deepEqual(calls, [
      { name: "fetch_content", args: { url: "https://example.com/api", mode: "readable" } },
    ]);
    assert.deepEqual(events, ["begin:web_fetch", "end:web_fetch"]);
    const result = m.seen[1].find((msg) => msg.role === "toolResult")!;
    assert.match(textOf(result), /不可信资料[\s\S]*接口正文/);
  } finally {
    await harness.close(context);
  }
});

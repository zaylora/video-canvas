import fs from "node:fs";
import http, { type IncomingHttpHeaders } from "node:http";
import type { AddressInfo } from "node:net";
import type { Msg } from "../src/context.ts";
import type { RunInput, ToolDecl, ToolEvent, ToolEventReply, ToolReply } from "../src/types.ts";

// 测试里收到的请求体（OpenAI 兼容的聊天请求、保存的对话历史）只是被断言的数据，不逐个声明形状，用 any

/** 模型回合的脚本：文字、工具调用，或者延迟、报错 */
export interface ModelTurn {
  text?: string;
  /** 响应头回来之前等多久（模拟排队的慢模型） */
  delay?: number;
  /** 每输出两个字等多久（模拟慢输出） */
  slow?: number;
  /** 不为 0 就直接回这个 HTTP 状态和错误 */
  status?: number;
  msg?: string;
  tools?: { id: string; name: string; args?: unknown }[];
}

/** /tool 收到的请求 */
export interface ToolRequest {
  tool_call_id: string;
  name: string;
  args: any;
  parent_tool_call_id?: string;
}

export interface FakeBridgeOptions {
  /** 模型回合的脚本，按请求顺序用 */
  models?: ModelTurn[];
  /** 摘要请求（请求头 X-Agent-Purpose: summary）的脚本，用完后回默认摘要 */
  summaries?: ModelTurn[];
  /** 工具回调的处理函数 */
  tool?: (req: ToolRequest) => Promise<Partial<ToolReply>>;
  toolEvent?: (ev: ToolEvent) => Promise<ToolEventReply>;
  /** /state 回这个状态码，非 200 时带「令牌无效」 */
  stateStatus?: number;
}

/** 假桥记录下来的所有请求 */
export interface BridgeRecord {
  chats: any[];
  summaries: any[];
  headers: IncomingHttpHeaders[];
  tools: ToolRequest[];
  toolEvents: ToolEvent[];
  states: any[][];
  finishes: { status: string; message: string }[];
  auths: (string | undefined)[];
  serviceTokens: (string | string[] | undefined)[];
  closed: number;
}

export interface FakeBridge {
  rec: BridgeRecord;
  url: string;
  close: () => Promise<void>;
}

/**
 * 假的 Go 桥：一个 OpenAI 兼容的流式端点，加 /tool、/state、/finish。
 * models 是模型回合的脚本；tool 是工具回调的处理函数；记录收到的所有请求。
 */
export async function fakeBridge({
  models = [],
  summaries = [],
  tool = async () => ({ content: "ok" }),
  toolEvent = async () => ({ allowed: true }),
  stateStatus = 200,
}: FakeBridgeOptions = {}): Promise<FakeBridge> {
  const rec: BridgeRecord = { chats: [], summaries: [], headers: [], tools: [], toolEvents: [], states: [], finishes: [], auths: [], serviceTokens: [], closed: 0 };
  let turn = 0;
  let summaryTurn = 0;
  const srv = http.createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", async () => {
      rec.auths.push(req.headers.authorization);
      rec.serviceTokens.push(req.headers["x-agent-service-token"]);
      const json = body ? JSON.parse(body) : {};
      const ok = (data: unknown) => { res.writeHead(200, { "content-type": "application/json" }); res.end(JSON.stringify({ code: 0, msg: "success", data })); };

      if (req.url === "/internal/agent/bridge/v1/chat/completions") {
        rec.headers.push(req.headers);
        // 摘要请求（请求头 X-Agent-Purpose: summary）单独记，用 summaries 的脚本回复，不占用 models 的回合
        const isSummary = req.headers["x-agent-purpose"] === "summary";
        (isSummary ? rec.summaries : rec.chats).push(json);
        const t: ModelTurn = isSummary ? (summaries[summaryTurn++] ?? { text: "【测试摘要】目标：拆分镜" }) : (models[turn++] ?? { text: "（脚本用完）" });
        req.on("close", () => rec.closed++);
        if (t.delay) await sleep(t.delay); // 模拟慢模型：连响应头都要等这么久才回来（pi 要等响应头到了才开始算「正在输出」）
        if (t.status) { res.writeHead(t.status, { "content-type": "application/json" }); return res.end(JSON.stringify({ error: { message: t.msg ?? "boom", type: "agent_error" } })); }
        res.writeHead(200, { "content-type": "text/event-stream" });
        const send = (o: unknown) => res.write("data: " + JSON.stringify(o) + "\n\n");
        const delta = (d: unknown, fin: string | null = null) => ({ id: "c", object: "chat.completion.chunk", choices: [{ index: 0, delta: d, finish_reason: fin }] });
        send(delta({ role: "assistant", content: "" }));
        for (const ch of (t.text ?? "").match(/.{1,2}/gs) ?? []) { send(delta({ content: ch })); if (t.slow) await new Promise((r) => setTimeout(r, t.slow)); }
        (t.tools ?? []).forEach((tc, i) => {
          send(delta({ tool_calls: [{ index: i, id: tc.id, type: "function", function: { name: tc.name, arguments: "" } }] }));
          const a = JSON.stringify(tc.args ?? {});
          send(delta({ tool_calls: [{ index: i, function: { arguments: a.slice(0, 4) } }] }));
          send(delta({ tool_calls: [{ index: i, function: { arguments: a.slice(4) } }] }));
        });
        send(delta({}, t.tools ? "tool_calls" : "stop"));
        send({ id: "c", choices: [], usage: { prompt_tokens: 100, completion_tokens: 20 } });
        res.write("data: [DONE]\n\n");
        return res.end();
      }
      if (req.url === "/internal/agent/bridge/tool") {
        rec.tools.push(json);
        try { return ok(await tool(json)); } catch (e) { res.writeHead(500, { "content-type": "application/json" }); return res.end(JSON.stringify({ code: 10000, msg: String((e as Error).message) })); }
      }
      if (req.url === "/internal/agent/bridge/tool-event") {
        rec.toolEvents.push(json);
        return ok(await toolEvent(json));
      }
      if (req.url === "/internal/agent/bridge/state") {
        rec.states.push(json.messages);
        if (stateStatus !== 200) { res.writeHead(stateStatus, { "content-type": "application/json" }); return res.end(JSON.stringify({ code: 10003, msg: "令牌无效" })); }
        return ok(null);
      }
      if (req.url === "/internal/agent/bridge/finish") { rec.finishes.push(json); return ok(null); }
      res.writeHead(404); res.end();
    });
  });
  await new Promise<void>((r) => srv.listen(0, "127.0.0.1", r));
  return { rec, url: `http://127.0.0.1:${(srv.address() as AddressInfo).port}`, close: () => new Promise<void>((r) => { srv.closeAllConnections?.(); srv.close(() => r()); }) };
}

/** 起一个假桥，跑完 fn 就关掉 */
export async function withBridge(opts: FakeBridgeOptions, fn: (b: FakeBridge) => Promise<void> | void): Promise<void> {
  const b = await fakeBridge(opts);
  try {
    await fn(b);
  } finally {
    await b.close();
  }
}

/** Go 下发的工具声明：取自迁移前导出的基准（12 个业务工具，全部由 Go 执行） */
export const TOOL_DECLS: ToolDecl[] = JSON.parse(fs.readFileSync(new URL("./fixtures/tool-defs.baseline.json", import.meta.url), "utf8")).map((d: ToolDecl) => ({
  ...d, label: d.name, executor: "server", scriptable: true,
}));

/** 启动参数的默认值 */
export function baseInput(bridgeUrl: string, over: Partial<RunInput> = {}): RunInput {
  return {
    bridge_url: bridgeUrl, token: "tok-123", system_prompt: "你是画布助手",
    model: { name: "Claude", context_window: 200000, max_tokens: 8192, vision: true },
    mode: "start", prompt: { text: "拆分镜" }, tools: TOOL_DECLS, ...over,
  };
}

export const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/** 一条消息的文字 */
export const text = (m: Msg): string => (typeof m.content === "string" ? m.content : (m.content ?? []).map((p) => p.text ?? "").join(""));

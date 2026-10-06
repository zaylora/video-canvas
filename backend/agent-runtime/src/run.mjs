import { Agent } from "@earendil-works/pi-agent-core";
import { createModels, createProvider } from "@earendil-works/pi-ai";
import { openAICompletionsApi } from "@earendil-works/pi-ai/api/openai-completions.lazy";
import { createBridge } from "./bridge-client.mjs";
import { trimContext } from "./context.mjs";
import { buildTools } from "./tools.mjs";

const CONTINUE_TEXT = "请继续刚才的任务。";
const MAX_ERROR_CHARS = 300;

/**
 * 跑一个运行片段：开始、审批后续跑、或「继续」。Go 起一个进程调用它一次，片段结束进程就退出。
 *
 * 模型：pi 自带的 openai-completions 提供方，baseUrl 指向 Go 的桥（一个 OpenAI 兼容端点），apiKey 是一次性桥令牌；
 * 渠道密钥、计费、重试策略都在 Go，这里没有。模型上的 compat 把请求收成老的 OpenAI 兼容网关也认的形状。
 * 工具：每个工具的 execute 回调 Go；执行方式必须是 sequential，否则同一条消息里的多个画布写入会并发。
 *
 * @param {object} input 启动参数，见 docs/design 的 13.7
 * @param {{ control?: import("node:events").EventEmitter, fetchImpl?: typeof fetch, log?: (msg: string, extra?: object) => void }} [io]
 * @returns {Promise<{ status: "done" | "error", message: string }>}
 */
export async function runAgent(input, io = {}) {
  const log = io.log ?? (() => {});
  const bridge = createBridge({ baseUrl: input.bridge_url, token: input.token, fetchImpl: io.fetchImpl });

  const agent = buildAgent(input, bridge);
  agent.subscribe(async (e) => {
    if (e.type === "turn_end") await saveState(agent, bridge, log);
  });
  io.control?.on("steer", (text) => agent.steer({ role: "user", content: [{ type: "text", text }], timestamp: Date.now() }));
  // 用户中止是正常结束：运行的状态由 Go 在处理「停止」时设好，这里不能把它报成出错
  let aborted = false;
  io.control?.on("abort", () => {
    aborted = true;
    agent.abort();
  });

  let result;
  try {
    await start(agent, input);
    await agent.waitForIdle();
    const err = agent.state.errorMessage;
    result = err && !aborted ? { status: "error", message: String(err).slice(0, MAX_ERROR_CHARS) } : { status: "done", message: "" };
  } catch (e) {
    log("运行异常", { error: String(e?.message ?? e) });
    result = { status: "error", message: String(e?.message ?? e).slice(0, MAX_ERROR_CHARS) };
  }
  await saveState(agent, bridge, log);
  await bridge.finish(result.status, result.message);
  return result;
}

/** 构造 Agent：模型走桥，工具走桥，上下文发给模型前裁剪 */
function buildAgent(input, bridge) {
  const m = input.model;
  const baseUrl = input.bridge_url.replace(/\/+$/, "") + "/internal/agent/bridge/v1";
  const model = {
    id: "agent", name: m.name ?? "agent", api: "openai-completions", provider: "bridge", baseUrl,
    reasoning: false, input: m.vision ? ["text", "image"] : ["text"],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }, // 计费在 Go，这里不算钱
    contextWindow: m.context_window, maxTokens: m.max_tokens,
    // 去掉 store 和 max_completion_tokens 等较新的字段，老的 OpenAI 兼容网关不一定认
    compat: { maxTokensField: "max_tokens", supportsStore: false, supportsDeveloperRole: false, supportsReasoningEffort: false },
  };
  const provider = createProvider({
    id: "bridge", name: "bridge", baseUrl,
    auth: { apiKey: { name: "bridge", resolve: async () => ({ auth: { apiKey: input.token } }) } },
    models: [model], api: openAICompletionsApi(),
  });
  const models = createModels();
  models.setProvider(provider);

  const agent = new Agent({
    // 历史通过 initialState 交给 pi：它会用这次的系统提示和工具声明重新生成开头的 system 消息；
    // 直接给 state.messages 赋值会丢掉或沿用旧的系统提示
    initialState: {
      systemPrompt: input.system_prompt, model: models.getModel("bridge", "agent"), tools: buildTools(bridge, input.allowed_tools),
      messages: stripSystem(input.messages ?? []),
    },
    streamFn: models.streamSimple.bind(models),
    transformContext: async (messages) => trimContext(messages, { window: m.context_window }),
    toolExecution: "sequential",
  });
  return agent;
}

/** 按启动方式驱动 Agent：start 发新消息；continue 把审批后的真实结果放回工具结果再继续；resume 接着中断的地方 */
async function start(agent, input) {
  switch (input.mode) {
    case "start": {
      const images = (input.prompt.images ?? []).map((i) => ({ type: "image", data: i.data, mimeType: i.mime_type }));
      return agent.prompt(input.prompt.text, images);
    }
    case "continue": {
      agent.state.messages = withToolResult(agent.state.messages, input.tool_result);
      return agent.continue();
    }
    case "resume": {
      const msgs = stripFailedTail(agent.state.messages);
      agent.state.messages = msgs;
      const last = msgs.at(-1);
      return last && (last.role === "user" || last.role === "toolResult") ? agent.continue() : agent.prompt(CONTINUE_TEXT);
    }
    default:
      throw new Error("未知的启动方式：" + input.mode);
  }
}

/** 把最后一条匹配的工具结果换成真实结果：审批等待期间它只是「已提交确认」的占位 */
export function withToolResult(messages, tr) {
  const idx = messages.findLastIndex((m) => m.role === "toolResult" && m.toolCallId === tr.tool_call_id);
  if (idx < 0) throw new Error("历史里找不到要续跑的工具调用：" + tr.tool_call_id);
  const out = messages.slice();
  out[idx] = { ...out[idx], isError: false, content: [{ type: "text", text: tr.content }] };
  return out;
}

/** 去掉结尾因出错或中止而不完整的 assistant 消息，让历史能从最后一条用户消息或工具结果接着走 */
export function stripFailedTail(messages) {
  const out = messages.slice();
  while (out.length && out.at(-1).role === "assistant" && (out.at(-1).stopReason === "error" || out.at(-1).stopReason === "aborted")) out.pop();
  return out;
}

/**
 * 去掉开头的 system 消息。系统提示和工具声明每次启动都由 Go 重新给（任务模式、可用工具都可能变），
 * 存进历史会沿用旧的，还白占几 KB。
 */
export function stripSystem(messages) {
  const i = messages.findIndex((m) => m.role !== "system");
  return i < 0 ? [] : messages.slice(i);
}

/** 把对话历史（不含系统提示）交回 Go；失败只记日志（下个回合会再交一次），不中断运行 */
async function saveState(agent, bridge, log) {
  try {
    await bridge.state(stripSystem(JSON.parse(JSON.stringify(agent.state.messages))));
  } catch (e) {
    log("保存对话历史失败", { error: String(e?.message ?? e) });
  }
}

import type { EventEmitter } from "node:events";
import { Agent, type AgentMessage } from "@earendil-works/pi-agent-core";
import { createModels, createProvider, type Api, type ImageContent, type Model } from "@earendil-works/pi-ai";
import { openAICompletionsApi } from "@earendil-works/pi-ai/api/openai-completions.lazy";
import { createBridge, serviceHeaders, type Bridge } from "./bridge-client.ts";
import { SUMMARY_SYSTEM_PROMPT, createCompactor, summaryMaxTokens } from "./compaction.ts";
import { dropOldImages, restoreImageUrls, trimContext, type Msg, type Part } from "./context.ts";
import { createSteering } from "./steer.ts";
import type { Log, RunInput, RunResult } from "./types.ts";
import { buildAllTools } from "./agent-tools/index.ts";

const CONTINUE_TEXT = "请继续刚才的任务。";
const MAX_ERROR_CHARS = 300;
const SKIPPED_TEXT = "本轮已暂停等待用户，这条调用没有执行；用户回应后如仍需要请重新调用。";
const LOST_RESULT_TEXT = "这次调用没有拿到结果（运行中断了），它可能已经执行。请先重新读取画布或任务确认现状，再决定要不要重做。";

type Steering = ReturnType<typeof createSteering>;
type Models = ReturnType<typeof createModels>;

/** runAgent 的外部依赖：控制消息通道（插话、中止）、fetch（测试里替换）、日志 */
export interface RunIO {
  control?: EventEmitter;
  fetchImpl?: typeof fetch;
  log?: Log;
}

/**
 * 跑一个运行片段：开始、审批后续跑、或「继续」。Go 起一个进程调用它一次，片段结束进程就退出。
 *
 * 模型：pi 自带的 openai-completions 提供方，baseUrl 指向 Go 的桥（一个 OpenAI 兼容端点），apiKey 是一次性桥令牌；
 * 渠道密钥、计费、重试策略都在 Go，这里没有。模型上的 compat 把请求收成老的 OpenAI 兼容网关也认的形状。
 * 工具：每个工具的 execute 回调 Go；执行方式必须是 sequential，否则同一条消息里的多个画布写入会并发。
 *
 * input 是启动参数，见 docs/design 的 13.7。
 */
export async function runAgent(input: RunInput, io: RunIO = {}): Promise<RunResult> {
  const log = io.log ?? (() => {});
  const bridge = createBridge({ baseUrl: input.bridge_url, token: input.token, serviceToken: input.service_token, fetchImpl: io.fetchImpl });

  // 因工具要求终止（等审批、等回答、步数用尽）而停下，要和「真的跑完了」区分开：
  // 前者报 paused，Go 只收回令牌、不改运行状态，否则用户批准得快时，旧进程的收尾会误伤已经续跑的新片段
  let paused = false;
  const { agent, steering } = buildAgent(input, bridge, () => (paused = true), () => paused, log);
  // 每个工具结果出来、每个回合结束都交回一次历史：Go 已经执行了的工具，崩溃后续跑时模型看得到，不会当作没做过重来一遍。
  // 内容没变（回合的最后一个工具结果刚存过）就不重复发
  let saved: { length: number; last: AgentMessage | undefined } = { length: -1, last: undefined };
  agent.subscribe(async (e) => {
    const toolResult = e.type === "message_end" && e.message.role === "toolResult";
    if (e.type !== "turn_end" && !toolResult) return;
    const msgs = agent.state.messages;
    if (msgs.length === saved.length && msgs.at(-1) === saved.last) return;
    saved = { length: msgs.length, last: msgs.at(-1) };
    await saveState(agent, bridge, log);
  });
  io.control?.on("steer", (text: string) => steering.push(text));
  // 用户中止是正常结束：运行的状态由 Go 在处理「停止」时设好，这里不能把它报成出错
  let aborted = false;
  io.control?.on("abort", () => {
    aborted = true;
    steering.dispose();
    agent.abort();
  });

  let result: RunResult;
  try {
    await start(agent, input);
    await settle(agent, steering, { isPaused: () => paused, isAborted: () => aborted });
    const err = agent.state.errorMessage;
    if (err && !aborted) result = { status: "error", message: String(err).slice(0, MAX_ERROR_CHARS) };
    else result = { status: paused && !aborted ? "paused" : "done", message: "" };
  } catch (e) {
    const reason = String((e as Error)?.message ?? e);
    log("运行异常", { error: reason });
    result = { status: "error", message: reason.slice(0, MAX_ERROR_CHARS) };
  }
  steering.dispose();
  await saveState(agent, bridge, log);
  await bridge.finish(result.status, result.message);
  return result;
}

/**
 * 等运行停下，并处理停下时还没处理的插话：
 * - 被插话中止的：去掉半截回答，接着跑，循环一开始就会把队列里的插话和原请求一起交给模型；
 * - 正常停下但队列里还有插话：没暂停就接着跑，免得插话随进程退出而丢；
 * - 暂停等审批或回答时队列里还有插话：追加进历史（工具结果之后），续跑时模型看得到；
 * - 用户点了停止：队列作废。
 */
async function settle(agent: Agent, steering: Steering, { isPaused, isAborted }: { isPaused: () => boolean; isAborted: () => boolean }) {
  for (;;) {
    await agent.waitForIdle();
    if (isAborted()) return agent.clearAllQueues();
    if (steering.consumeInterrupt() && (agent.state.messages.at(-1) as Msg | undefined)?.stopReason === "aborted") {
      agent.state.messages = stripFailedTail(agent.state.messages);
      await agent.continue();
      continue;
    }
    if (!agent.hasQueuedMessages()) return;
    if (!isPaused()) {
      await agent.continue();
      continue;
    }
    const left = agent.peekQueuedMessages();
    agent.clearAllQueues();
    agent.state.messages = [...agent.state.messages, ...left];
    return;
  }
}

/** 构造 Agent 和它的插话控制器：模型走桥，工具走桥，上下文发给模型前裁剪 */
function buildAgent(input: RunInput, bridge: Bridge, onTerminate: () => void, isPaused: () => boolean, log: Log) {
  const m = input.model;
  const baseUrl = input.bridge_url.replace(/\/+$/, "") + "/internal/agent/bridge/v1";
  const model: Model<"openai-completions"> = {
    id: "agent", name: m.name ?? "agent", api: "openai-completions", provider: "bridge", baseUrl,
    reasoning: false, input: m.vision ? ["text", "image"] : ["text"],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }, // 计费在 Go，这里不算钱
    contextWindow: m.context_window, maxTokens: m.max_tokens,
    // 去掉 store 和 max_completion_tokens 等较新的字段，老的 OpenAI 兼容网关不一定认
    compat: { maxTokensField: "max_tokens", supportsStore: false, supportsDeveloperRole: false, supportsReasoningEffort: false },
  };
  const provider = createProvider({
    id: "bridge", name: "bridge", baseUrl,
    // 模型请求（含摘要压缩）也走桥，同样要带服务令牌；pi 取的是认证结果里的请求头
    auth: { apiKey: { name: "bridge", resolve: async () => ({ auth: { apiKey: input.token, ...(input.service_token && { headers: serviceHeaders(input.service_token) }) } }) } },
    models: [model], api: openAICompletionsApi(),
  });
  const models = createModels();
  models.setProvider(provider);
  const piModel = models.getModel("bridge", "agent");
  if (!piModel) throw new Error("桥模型没有注册成功");

  // 摘要压缩：历史超过触发线时把较早的消息写成摘要。摘要请求也走桥（Go 计费），请求头标明是内部调用，Go 不把它当助手的话推给前端
  const compactor = createCompactor<AgentMessage>({
    window: m.context_window,
    overhead: (input.system_prompt?.length ?? 0) + (input.tools ?? []).reduce((n, d) => n + d.description.length + JSON.stringify(d.parameters).length, 0),
    view: (msgs) => trimContext(dropOldImages(msgs, KEEP_IMAGES), { window: m.context_window }),
    summarize: (prompt, signal) => summarize(models, piModel, prompt, signal, m.context_window),
    onError: (e) => log("摘要压缩失败，只做裁剪", { error: String((e as Error)?.message ?? e) }),
  });

  const agent = new Agent({
    // 历史通过 initialState 交给 pi：它会用这次的系统提示和工具声明重新生成开头的 system 消息；
    // 直接给 state.messages 赋值会丢掉或沿用旧的系统提示
    initialState: {
      systemPrompt: input.system_prompt, model: piModel, tools: buildAllTools({ bridge, input, onTerminate, log }),
      messages: closeDanglingToolCalls(stripSystem(input.messages ?? [])),
    },
    streamFn: models.streamSimple.bind(models),
    transformContext: async (messages) => trimContext(dropOldImages(messages, KEEP_IMAGES), { window: m.context_window }),
    // 每次请求模型前看一眼历史：超过触发线就压缩，并把压缩后的消息同步回 agent.state，保存的历史就是压缩后的、下次不用重做
    prepareRequest: async ({ context }, signal) => {
      const lead = Math.max(0, context.messages.findIndex((x) => x.role !== "system")); // 开头的 system 消息原样留着，不参与压缩
      const next = await compactor.compact(context.messages.slice(lead), signal);
      // 压缩做完了，模型请求马上发出：从这一刻起插话可以中止它（慢模型要等响应头才开始输出，这段等待也算）
      steering.requestStarted();
      if (!next) return undefined;
      const messages = [...context.messages.slice(0, lead), ...next];
      agent.state.messages = messages.slice();
      return { context: { ...context, messages } };
    },
    toolExecution: "sequential",
    // 多条插话一次取出，在同一次回复里一起回应，而不是每条各答一遍
    steeringMode: "all",
    onPayload: async (payload) => restoreImageUrls(payload),
    // pi 只在整批调用都要求终止时才停。模型把提问或审批和别的工具放在同一条消息里时，
    // 必须在暂停那一刻停下：后面的调用不执行（告诉模型稍后重调），这一轮结束后不再请求模型，
    // 历史才会停在工具结果上，续跑时 continue 能接上
    beforeToolCall: async () => (isPaused() ? { block: true, reason: SKIPPED_TEXT, terminate: true } : undefined),
    finishTurn: async () => (isPaused() ? { action: "end" } : undefined),
  });
  const steering = createSteering(agent);
  return { agent, steering };
}

/** 让模型把一段对话写成摘要：走桥，请求头标明是内部调用；出错或被中止都抛出，由压缩器退回不压缩 */
async function summarize(models: Models, model: Model<Api>, prompt: string, signal: AbortSignal | undefined, window: number): Promise<string> {
  const msg = await models.completeSimple(
    model,
    { systemPrompt: SUMMARY_SYSTEM_PROMPT, messages: [{ role: "user", content: [{ type: "text", text: prompt }], timestamp: Date.now() }] },
    { signal, maxTokens: summaryMaxTokens(window), headers: { "X-Agent-Purpose": "summary" } },
  );
  if (msg.stopReason === "error" || msg.stopReason === "aborted") throw new Error(msg.errorMessage || "摘要请求失败");
  return msg.content.map((p) => (p.type === "text" ? p.text : "")).join("");
}

/** 按启动方式驱动 Agent：start 发新消息；continue 把审批后的真实结果放回工具结果再继续；resume 接着中断的地方 */
async function start(agent: Agent, input: RunInput) {
  switch (input.mode) {
    case "start": {
      if (!input.prompt) throw new Error("start 模式缺少 prompt");
      const images = (input.prompt.images ?? []).map((i): ImageContent => ({ type: "image", data: i.data, mimeType: i.mime_type }));
      return agent.prompt(input.prompt.text, images);
    }
    case "continue": {
      if (!input.tool_result) throw new Error("continue 模式缺少 tool_result");
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

/**
 * 给没有结果的工具调用补上结果。崩溃时一批工具可能只跑了一半，保存的历史里 assistant 的调用比工具结果多。
 * pi-ai 发请求时也会补一句英文的「No result provided」让接口不报错，但模型会因此以为这些调用没做过；
 * 这里补成明确的中文提示：可能已经执行，先确认现状。被出错或中止截断的 assistant 没有真正执行过工具，不补，由 stripFailedTail 整条去掉。
 * 不修改入参；历史完整时原样返回。
 */
export function closeDanglingToolCalls<M extends Msg>(messages: M[]): M[] {
  const out: M[] = [];
  let changed = false;
  for (let i = 0; i < messages.length; i++) {
    const m = messages[i];
    out.push(m);
    if (m.role !== "assistant" || m.stopReason === "error" || m.stopReason === "aborted") continue;
    const calls = (Array.isArray(m.content) ? m.content : []).filter((p: Part) => p.type === "toolCall");
    if (!calls.length) continue;
    // 紧跟在这条 assistant 后面的工具结果
    let j = i + 1;
    while (j < messages.length && messages[j].role === "toolResult") out.push(messages[j++]);
    const answered = new Set(messages.slice(i + 1, j).map((r) => r.toolCallId));
    for (const c of calls) {
      if (answered.has(c.id)) continue;
      changed = true;
      out.push({ role: "toolResult", toolCallId: c.id, toolName: c.name, content: [{ type: "text", text: LOST_RESULT_TEXT }], isError: true, timestamp: m.timestamp } as unknown as M);
    }
    i = j - 1;
  }
  return changed ? out : messages;
}

/** 把最后一条匹配的工具结果换成真实结果：审批等待期间它只是「已提交确认」的占位 */
export function withToolResult<M extends Msg>(messages: M[], tr: { tool_call_id: string; content: string }): M[] {
  const idx = messages.findLastIndex((m) => m.role === "toolResult" && m.toolCallId === tr.tool_call_id);
  if (idx < 0) throw new Error("历史里找不到要续跑的工具调用：" + tr.tool_call_id);
  const out = messages.slice();
  out[idx] = { ...out[idx], isError: false, content: [{ type: "text", text: tr.content }] } as M;
  return out;
}

/** 去掉结尾因出错或中止而不完整的 assistant 消息，让历史能从最后一条用户消息或工具结果接着走 */
export function stripFailedTail<M extends Msg>(messages: M[]): M[] {
  const out = messages.slice();
  while (out.length && out.at(-1)!.role === "assistant" && (out.at(-1)!.stopReason === "error" || out.at(-1)!.stopReason === "aborted")) out.pop();
  return out;
}

/**
 * 去掉开头的 system 消息。系统提示和工具声明每次启动都由 Go 重新给（任务模式、可用工具都可能变），
 * 存进历史会沿用旧的，还白占几 KB。
 */
/** 发给模型的上下文里最多保留几条带图片的工具结果，更早的换成占位文字 */
const KEEP_IMAGES = 2;

export function stripSystem<M extends Msg>(messages: M[]): M[] {
  const i = messages.findIndex((m) => m.role !== "system");
  return i < 0 ? [] : messages.slice(i);
}

/** 把对话历史（不含系统提示）交回 Go；失败只记日志（下个回合会再交一次），不中断运行 */
async function saveState(agent: Agent, bridge: Bridge, log: Log) {
  try {
    await bridge.state(dropOldImages(stripSystem(JSON.parse(JSON.stringify(agent.state.messages)))));
  } catch (e) {
    log("保存对话历史失败", { error: String((e as Error)?.message ?? e) });
  }
}

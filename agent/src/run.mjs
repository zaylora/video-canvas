import { Agent } from "@earendil-works/pi-agent-core";
import { createModels, createProvider } from "@earendil-works/pi-ai";
import { openAICompletionsApi } from "@earendil-works/pi-ai/api/openai-completions.lazy";
import { createBridge } from "./bridge-client.mjs";
import { SUMMARY_SYSTEM_PROMPT, createCompactor, summaryMaxTokens } from "./compaction.mjs";
import { dropOldImages, elideOldCatalogs, restoreImageUrls, trimContext } from "./context.mjs";
import { createSteering } from "./steer.mjs";
import { buildAllTools } from "./agent-tools/index.mjs";

const CONTINUE_TEXT = "请继续刚才的任务。";
const MAX_ERROR_CHARS = 300;
/** 收尾窗口里最多再跑几轮来处理迟到的插话 */
const MAX_TAIL_ROUNDS = 3;
const SKIPPED_TEXT = "本轮已暂停等待用户，这条调用没有执行；用户回应后如仍需要请重新调用。";

/**
 * 跑一个运行片段：开始、审批后续跑、或「继续」。Go 起一个进程调用它一次，片段结束进程就退出。
 *
 * 模型：pi 自带的 openai-completions 提供方，baseUrl 指向 Go 的桥（一个 OpenAI 兼容端点），apiKey 是一次性桥令牌；
 * 渠道密钥、计费、重试策略都在 Go，这里没有。模型上的 compat 把请求收成老的 OpenAI 兼容网关也认的形状。
 * 工具：每个工具的 execute 回调 Go；执行方式必须是 sequential，否则同一条消息里的多个画布写入会并发。
 *
 * @param {object} input 启动参数，见 docs/design 的 13.7
 * @param {{ control?: import("node:events").EventEmitter, fetchImpl?: typeof fetch, log?: (msg: string, extra?: object) => void }} [io]
 * @returns {Promise<{ status: "done" | "paused" | "error", message: string }>}
 */
export async function runAgent(input, io = {}) {
  const log = io.log ?? (() => {});
  const bridge = createBridge({ baseUrl: input.bridge_url, token: input.token, fetchImpl: io.fetchImpl });

  // 因工具要求终止（等审批、等回答、步数用尽）而停下，要和「真的跑完了」区分开：
  // 前者报 paused，Go 只收回令牌、不改运行状态，否则用户批准得快时，旧进程的收尾会误伤已经续跑的新片段
  let paused = false;
  const { agent, steering } = buildAgent(input, bridge, () => (paused = true), () => paused, log);
  agent.subscribe(async (e) => {
    if (e.type === "turn_end") await saveState(agent, bridge, log);
  });
  io.control?.on("steer", (text) => steering.push(text));
  // 用户中止是正常结束：运行的状态由 Go 在处理「停止」时设好，这里不能把它报成出错
  let aborted = false;
  io.control?.on("abort", () => {
    aborted = true;
    steering.dispose();
    agent.abort();
  });

  const resultOf = () => {
    const err = agent.state.errorMessage;
    if (err && !aborted) return { status: "error", message: String(err).slice(0, MAX_ERROR_CHARS) };
    return { status: paused && !aborted ? "paused" : "done", message: "" };
  };
  const settleOpts = { isPaused: () => paused, isAborted: () => aborted };

  let result;
  try {
    await start(agent, input);
    await settle(agent, steering, settleOpts);
    result = resultOf();
  } catch (e) {
    log("运行异常", { error: String(e?.message ?? e) });
    result = { status: "error", message: String(e?.message ?? e).slice(0, MAX_ERROR_CHARS) };
  }
  // 收尾窗口：settle 返回后、报告结束前（保存历史要一次 HTTP 往返），Go 那边运行仍是 running，插话照样能进来，
  // 但进了一个已经空闲的 Agent 队列就没人处理了，进程随即退出，用户看到自己的插话却没有回应。
  // 所以报告结束前再看一眼队列：有就再跑一轮把它处理掉（暂停中的会追加进历史），直到队列为空；轮数有上限，防止死循环
  for (let tail = 0; ; tail++) {
    steering.dispose();
    await saveState(agent, bridge, log);
    if (aborted || result.status === "error" || !agent.hasQueuedMessages()) break;
    if (tail >= MAX_TAIL_ROUNDS) {
      log("收尾阶段的插话轮数已达上限，剩余插话不再处理", { rounds: tail });
      break;
    }
    try {
      await settle(agent, steering, settleOpts);
      result = resultOf();
    } catch (e) {
      log("处理收尾插话时异常", { error: String(e?.message ?? e) });
      result = { status: "error", message: String(e?.message ?? e).slice(0, MAX_ERROR_CHARS) };
    }
  }
  steering.dispose();
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
async function settle(agent, steering, { isPaused, isAborted }) {
  for (;;) {
    await agent.waitForIdle();
    if (isAborted()) return agent.clearAllQueues();
    if (steering.consumeInterrupt() && agent.state.messages.at(-1)?.stopReason === "aborted") {
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
function buildAgent(input, bridge, onTerminate, isPaused, log) {
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
  const piModel = models.getModel("bridge", "agent");

  // 发给模型的视图：先把旧的画布目录换成占位文字（目录只有最新一份有用，且是最大的一块），再去旧图片，最后按预算裁剪旧工具结果；
  // 顺序保证裁剪是在瘦身之后量的，能不丢工具结果就不丢。压缩器量触发线也按这个视图
  const forModel = (msgs) => trimContext(dropOldImages(elideOldCatalogs(msgs), KEEP_IMAGES), { window: m.context_window });

  // 摘要压缩：历史超过触发线时把较早的消息写成摘要。摘要请求也走桥（Go 计费），请求头标明是内部调用，Go 不把它当助手的话推给前端
  const compactor = createCompactor({
    window: m.context_window,
    overhead: (input.system_prompt?.length ?? 0) + (input.tools ?? []).reduce((n, d) => n + d.description.length + JSON.stringify(d.parameters).length, 0),
    view: forModel,
    summarize: (prompt, signal) => summarize(models, piModel, prompt, signal, m.context_window),
    onError: (e) => log("摘要压缩失败，只做裁剪", { error: String(e?.message ?? e) }),
  });

  const agent = new Agent({
    // 历史通过 initialState 交给 pi：它会用这次的系统提示和工具声明重新生成开头的 system 消息；
    // 直接给 state.messages 赋值会丢掉或沿用旧的系统提示
    initialState: {
      systemPrompt: input.system_prompt, model: piModel, tools: buildAllTools({ bridge, input, onTerminate, log }),
      messages: stripSystem(input.messages ?? []),
    },
    streamFn: models.streamSimple.bind(models),
    transformContext: async (messages) => forModel(messages),
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
async function summarize(models, model, prompt, signal, window) {
  const msg = await models.completeSimple(
    model,
    { systemPrompt: SUMMARY_SYSTEM_PROMPT, messages: [{ role: "user", content: [{ type: "text", text: prompt }], timestamp: Date.now() }] },
    { signal, maxTokens: summaryMaxTokens(window), headers: { "X-Agent-Purpose": "summary" } },
  );
  if (msg.stopReason === "error" || msg.stopReason === "aborted") throw new Error(msg.errorMessage || "摘要请求失败");
  return msg.content.map((p) => (p.type === "text" ? p.text : "")).join("");
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
/** 发给模型的上下文里最多保留几条带图片的工具结果，更早的换成占位文字 */
const KEEP_IMAGES = 2;

export function stripSystem(messages) {
  const i = messages.findIndex((m) => m.role !== "system");
  return i < 0 ? [] : messages.slice(i);
}

/** 把对话历史（不含系统提示）交回 Go；失败只记日志（下个回合会再交一次），不中断运行 */
async function saveState(agent, bridge, log) {
  try {
    await bridge.state(dropOldImages(stripSystem(JSON.parse(JSON.stringify(agent.state.messages)))));
  } catch (e) {
    log("保存对话历史失败", { error: String(e?.message ?? e) });
  }
}

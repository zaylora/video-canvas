/**
 * 模型提供方：pi 把 Go 的模型代理（上下文模式的 OpenAI 兼容端点）当作模型提供方。
 * 渠道密钥、计费、重试策略都在 Go，这里没有。
 *
 * 难点：Go 的模型代理要求每个请求带 X-Agent-Context（运行 id、预算……），而一个 Harness 托管很多会话，
 * pi 的流选项（settings.stream.headers）是整个 Harness 共用的，不能按会话区分。做法（exp10 验证）：
 *   - 应用在每次提交前登记「会话 id → 运行上下文」（RunContexts.set）；
 *   - runContextExtension 的 beforeRequest 钩子里读出 pi 给这个会话生成的提供方会话 id，登记「提供方会话 id → 会话 id」；
 *   - 提供方的流函数按 options.sessionId 查到会话，再查到它当前的运行上下文，加进请求头。
 * 每次请求前都会重新登记，所以同一会话的下一轮运行换了上下文（批准后已花积分变了）也生效。
 */
import { createModels, createProvider, type Api, type Model, type Provider, type ProviderResponse, type StreamOptions } from "@earendil-works/pi-ai";
import { openAICompletionsApi } from "@earendil-works/pi-ai/api/openai-completions.lazy";
import { GenerationTask, ProviderDoc, defineExtension, hook, type Extension } from "@earendil-works/pi-durable";
import { restoreImageUrls } from "./context.ts";
import { encodeRunContext, type RunContext } from "./platform-client.ts";

export const PROVIDER_ID = "platform";
const CALL_ID_HEADER = "x-agent-call-id";
const ERROR_CODE_HEADER = "x-agent-error-code";

/** 一个 Agent 模型的规格，来自运行准备接口（Go 的模型配置） */
export interface ModelSpec {
  key: string;
  name: string;
  context_window: number;
  max_tokens: number;
  vision: boolean;
}

export interface RunContexts {
  /** 登记一个会话当前运行的上下文（每次提交前调用，换了上下文就覆盖） */
  set(conversationId: string, rc: RunContext): void;
  delete(conversationId: string): void;
  /** 会话当前的运行上下文，已编码成请求头的值；没登记返回 undefined */
  headerFor(conversationId: string): string | undefined;
  /** 提供方会话 id ↔ 会话 id（beforeRequest 钩子里登记） */
  bindSession(sessionId: string, conversationId: string): void;
  conversationOf(sessionId: string): string | undefined;
}

export function createRunContexts(): RunContexts {
  const byConversation = new Map<string, string>();
  const bySession = new Map<string, string>();
  return {
    set: (id, rc) => void byConversation.set(id, encodeRunContext(rc)),
    delete: (id) => void byConversation.delete(id),
    headerFor: (id) => byConversation.get(id),
    bindSession: (sessionId, id) => void bySession.set(sessionId, id),
    conversationOf: (sessionId) => bySession.get(sessionId),
  };
}

/** 登记「提供方会话 id → 会话 id」的扩展。每次模型请求前执行，装进 Harness 的 registry 即可 */
export function runContextExtension(contexts: RunContexts): Extension {
  return defineExtension({
    name: "run-context",
    hooks: [
      hook(GenerationTask, {
        beforeRequest: async (_request, api, ctx) => {
          const doc = await api.snapshot(ProviderDoc, api.conversationId, ctx);
          if (doc) contexts.bindSession(doc.sessionId, String(api.conversationId));
          return undefined; // 不改请求内容
        },
      }),
    ],
  });
}

export interface ModelCallInfo {
  /** 提供方会话 id（pi 生成，每个会话一个），用 RunContexts.conversationOf 找回会话 */
  sessionId: string | undefined;
  /** Go 给这次模型调用的 id（响应头，十进制字符串），事后据此查这次调用的积分 */
  callId: string;
}

export interface ModelErrorInfo {
  sessionId: string | undefined;
  /** HTTP 状态码 */
  status: number;
  /** Go 的业务错误码（响应头 X-Agent-Error-Code），比如本轮额度用完；不是业务错误时没有 */
  code: number | undefined;
}

export interface ModelRegistryOptions {
  /** Go 的桥地址 */
  baseUrl: string;
  serviceToken: string;
  contexts: RunContexts;
  /** 每次模型请求收到响应头时调用，带上 Go 给的调用 id */
  onCall?: (info: ModelCallInfo) => void;
  /** 模型代理回了错误（HTTP 4xx/5xx）时调用。OpenAI 格式的错误体只有文字，业务错误码在响应头里 */
  onError?: (info: ModelErrorInfo) => void;
}

/**
 * 创建 pi 的模型表和注册方法。模型规格会变（管理员改了上下文窗口、换了能不能看图），所以 ensure 可以在 Harness 打开之后反复调用：
 * 规格有变化才重新注册，返回是否注册了。
 */
export function createModelRegistry(opts: ModelRegistryOptions) {
  const models = createModels();
  const specs = new Map<string, ModelSpec>();
  const baseUrl = opts.baseUrl.replace(/\/+$/, "") + "/internal/platform/v1/model";
  const inner = openAICompletionsApi();

  /** 按会话加运行上下文，并从响应头取模型调用 id */
  const wrap = (o: StreamOptions = {}): StreamOptions => {
    const conv = o.sessionId ? opts.contexts.conversationOf(o.sessionId) : undefined;
    const ctx = conv ? opts.contexts.headerFor(conv) : undefined;
    return {
      ...o,
      ...(ctx && { headers: { ...o.headers, "X-Agent-Context": ctx } }),
      // 重试策略在 Go（渠道限流、上游重试），这里不再重试，否则同一次失败会被算两遍
      maxRetries: 0,
      // 错误响应不会走 onResponse（SDK 直接抛错），所以在 fetch 这一层看状态码和业务错误码
      fetch: async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
        const res = await (o.fetch ?? fetch)(input, init);
        if (res.status >= 400) {
          const code = Number(res.headers.get(ERROR_CODE_HEADER));
          opts.onError?.({ sessionId: o.sessionId, status: res.status, code: Number.isInteger(code) && code > 0 ? code : undefined });
        }
        return res;
      },
      // 对象存储里的图片只有地址：工具结果把地址放在 data 里，发出前还原成真正的地址
      onPayload: async (payload: unknown, model: Model<Api>) => restoreImageUrls((await o.onPayload?.(payload, model)) ?? payload),
      onResponse: async (res: ProviderResponse, model: Model<Api>) => {
        const callId = res.headers[CALL_ID_HEADER];
        if (callId) opts.onCall?.({ sessionId: o.sessionId, callId });
        await o.onResponse?.(res, model);
      },
    };
  };
  const api = {
    ...inner,
    stream: (m: Model<Api>, c: Parameters<typeof inner.stream>[1], o?: StreamOptions) => inner.stream(m, c, wrap(o)),
    streamSimple: (m: Model<Api>, c: Parameters<typeof inner.streamSimple>[1], o?: Parameters<typeof inner.streamSimple>[2]) => inner.streamSimple(m, c, wrap(o) as typeof o),
  };

  const build = (): Provider =>
    createProvider({
      id: PROVIDER_ID, name: PROVIDER_ID, baseUrl,
      // 服务令牌随每个请求发出；pi 取的是认证结果里的请求头
      auth: { apiKey: { name: PROVIDER_ID, resolve: async () => ({ auth: { apiKey: "unused", headers: { "X-Agent-Service-Token": opts.serviceToken } } }) } },
      models: [...specs.values()].map((s) => ({
        id: s.key, name: s.name, api: "openai-completions" as const, provider: PROVIDER_ID, baseUrl,
        reasoning: false, input: s.vision ? (["text", "image"] as const) : (["text"] as const),
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }, // 计费在 Go，这里不算钱
        contextWindow: s.context_window, maxTokens: s.max_tokens,
        // 去掉 store 和 max_completion_tokens 等较新的字段，老的 OpenAI 兼容网关不一定认
        compat: { maxTokensField: "max_tokens" as const, supportsStore: false, supportsDeveloperRole: false, supportsReasoningEffort: false },
      })),
      api,
    } as never);

  return {
    models,
    ensure(spec: ModelSpec): boolean {
      const old = specs.get(spec.key);
      if (old && JSON.stringify(old) === JSON.stringify(spec)) return false;
      specs.set(spec.key, spec);
      models.setProvider(build());
      return true;
    },
  };
}

/**
 * OpenAI 文本插件（内置，契约 v1）。
 *
 * 对接 OpenAI 官方接口（以及兼容它的网关），只做文本同步出正文：
 *   - 渠道设置 api = responses（默认）：POST /v1/responses
 *   - 渠道设置 api = chat_completions：POST /v1/chat/completions
 * 接口依据 OpenAI 官方 OpenAPI 规范；没验证过的地方见文末“未验证”。
 *
 * 渠道怎么填：
 *   - Key  ：OpenAI 的 API Key（sk-...），由宿主按 Bearer 注入；
 *   - base_url：官方填 https://api.openai.com（路径里已带 /v1，base_url 不要再写 /v1）；兼容网关填它的根地址；
 *   - 设置 api：接口，responses（默认）或 chat_completions。网关只实现了 Chat Completions 时选后者。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 * 插件没有网络与文件能力，只把任务翻译成请求描述、把响应翻译成统一结果。
 *
 * 模型 params（运营在模型上填的固定参数）：
 *   text : { max_tokens?: number, system?: string, extra?: object }
 *          max_tokens 是输出上限（Responses 发成 max_output_tokens，Chat 发成 max_completion_tokens）；
 *          system 是固定系统提示；extra 原样合并进请求体顶层（如 reasoning、reasoning_effort、top_p），只补充、不覆盖已有字段
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt（必填）、system（固定系统提示，优先于 params.system）、max_tokens（最大输出，不能超过 params.max_tokens）、
 *   temperature（没填就不发：推理模型不支持该参数）、images[0]（视觉模型，可选，发成签名 URL）
 * 产物：{type:"text", text, usage:{input_tokens, output_tokens}}，按 Token 计费的模型据此结算；上游没有可用数字就不带 usage。
 *
 * 刻意的选择：
 *   - Responses 请求固定带 store:false，不让 OpenAI 在它那边保存这次对话（我们自己保存结果）；
 *   - 请求固定 stream:false，一次拿完整结果；
 *   - Chat Completions 用 max_completion_tokens，不用已弃用且与 o 系列不兼容的 max_tokens。
 *
 * 未验证：
 *   - Responses 的 max_output_tokens 有最小值限制（官方文档提过不能太小），本插件不做校验，交给上游报错；
 *   - 兼容网关对 Responses / store / max_completion_tokens 的支持程度各不相同，不通时请切换 api 或用 extra 调整；
 *   - 导入时按模型名前缀（gpt- / chatgpt- / o+数字）与排除词粗筛，个别新模型名可能被误放或误排，运营在导入后核对；
 *   - 内容审核类错误只按错误码 content_policy_violation 与“content policy / safety system”文案识别，其它措辞会按 terminal 处理。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "openai",
    name: "OpenAI",
    version: "1.1.0",
    vendor: "openai",
    description: "对接 OpenAI 官方接口：文本（Responses / Chat Completions 可选，同步）",
    auth: { type: "bearer" },
    endpoints: {
      text: { mode: "sync", method: "POST", path: "/v1/responses" },
    },
    channelSettings: {
      api: {
        type: "enum",
        label: "接口",
        description: "responses：官方推荐的 Responses 接口；chat_completions：只支持 Chat Completions 的网关选这个",
        options: ["responses", "chat_completions"],
        default: "responses",
      },
    },
    import: { args: {} },
  },

  buildSubmitRequest: function (ctx) {
    if (useChat(ctx)) {
      return buildChatRequest(ctx);
    }
    return buildResponsesRequest(ctx);
  },

  parseSubmitResponse: function (ctx, resp) {
    if (useChat(ctx)) {
      return { immediate: parseChatResponse(resp.body || {}) };
    }
    return { immediate: parseResponsesResponse(resp.body || {}) };
  },

  // 非 2xx：OpenAI 错误体 {error:{message,type,code}}。额度 → provider_balance，内容策略 → moderation，
  // 429 与 5xx → retryable，其余（401 / 403 / 404 / 400）terminal。
  classifyError: function (ctx, resp) {
    var e = resp.body && typeof resp.body === "object" && resp.body.error && typeof resp.body.error === "object" ? resp.body.error : {};
    var msg = errorMessageOf(resp.body) || (resp.text || "").slice(0, 300);
    var code = typeof e.code === "string" ? e.code.toLowerCase() : "";
    var type = typeof e.type === "string" ? e.type.toLowerCase() : "";
    var lower = String(msg).toLowerCase();

    var cls;
    if (code === "insufficient_quota" || type === "insufficient_quota" || /^billing/.test(code) || /billing hard limit|exceeded your current quota/.test(lower)) {
      cls = "provider_balance";
    } else if (code === "content_policy_violation" || /content[ _-]?policy|safety system/.test(lower)) {
      cls = "moderation";
    } else if (resp.status === 429 || resp.status >= 500) {
      cls = "retryable";
    } else {
      cls = "terminal"; // 显式给出，避免宿主默认规则按文案里的 quota / credit 之类的词误判
    }
    var out = { class: cls, message: msg || "上游返回 HTTP " + resp.status };
    if (code) {
      out.code = code;
    }
    return out;
  },

  buildCheckRequest: function (ctx) {
    return { method: "GET", path: "/v1/models", timeout: 15 };
  },

  buildImportRequest: function (ctx, args) {
    return { method: "GET", path: "/v1/models", timeout: 30 };
  },

  // 只导入文本类模型（gpt-* / chatgpt-* / o 系列），草稿的 params 留空由运营补。
  parseImportResponse: function (ctx, resp, args) {
    var list = resp.body && isArray(resp.body.data) ? resp.body.data : [];
    var drafts = [];
    for (var i = 0; i < list.length; i++) {
      var id = list[i] && list[i].id;
      if (typeof id !== "string" || id === "" || !isTextModel(id)) {
        continue;
      }
      drafts.push({ upstreamModel: id, kind: "text", label: id, params: {} });
    }
    return drafts;
  },
};

// ---------------------------------------------------------------------------
// 请求
// ---------------------------------------------------------------------------

// 渠道设置 api 只有 chat_completions 才走 Chat；没填或其它值一律按默认 Responses。
function useChat(ctx) {
  var settings = ctx.channel && ctx.channel.settings ? ctx.channel.settings : {};
  return settings.api === "chat_completions";
}

function buildResponsesRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};
  var prompt = input.prompt === undefined ? "" : String(input.prompt);

  var body = { model: ctx.model.upstreamModel };
  var firstImage = first(input.images);
  if (firstImage) {
    // 视觉模型：input 用消息数组，图片给签名 URL（宿主把文件引用换成地址）
    body.input = [
      {
        role: "user",
        content: [
          { type: "input_text", text: prompt },
          { type: "input_image", image_url: { __fileRef: firstImage, as: "url" } },
        ],
      },
    ];
  } else {
    body.input = prompt;
  }
  var system = firstString(input.system, params.system);
  if (system) {
    body.instructions = system;
  }
  var maxTokens = capMaxTokens(input.max_tokens, params.max_tokens);
  if (maxTokens !== undefined) {
    body.max_output_tokens = maxTokens;
  }
  if (typeof input.temperature === "number") {
    body.temperature = input.temperature;
  }
  body.stream = false;
  body.store = false; // 刻意不在 OpenAI 侧保存对话
  mergeExtra(body, params.extra);
  return { method: "POST", path: "/v1/responses", json: body, timeout: 120 };
}

function buildChatRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};
  var messages = [];

  var system = firstString(input.system, params.system);
  if (system) {
    messages.push({ role: "system", content: system });
  }
  var userContent = input.prompt === undefined ? "" : String(input.prompt);
  var firstImage = first(input.images);
  if (firstImage) {
    userContent = [
      { type: "text", text: userContent },
      { type: "image_url", image_url: { url: { __fileRef: firstImage, as: "url" } } },
    ];
  }
  messages.push({ role: "user", content: userContent });

  var body = { model: ctx.model.upstreamModel, messages: messages, stream: false };
  if (typeof input.temperature === "number") {
    body.temperature = input.temperature;
  }
  var maxTokens = capMaxTokens(input.max_tokens, params.max_tokens);
  if (maxTokens !== undefined) {
    body.max_completion_tokens = maxTokens; // max_tokens 已弃用，且与 o 系列不兼容
  }
  mergeExtra(body, params.extra);
  return { method: "POST", path: "/v1/chat/completions", json: body, timeout: 120 };
}

// 用户传的 max_tokens 不能超过模型固定参数里的上限；只有上限没有用户值时用上限；都没有就不发，由上游按默认处理。
function capMaxTokens(fromInput, limit) {
  var hasLimit = typeof limit === "number" && limit > 0;
  if (typeof fromInput === "number" && fromInput > 0) {
    return hasLimit && fromInput > limit ? limit : fromInput;
  }
  return hasLimit ? limit : undefined;
}

// ---------------------------------------------------------------------------
// 响应
// ---------------------------------------------------------------------------

// Responses：只拼接 output 里 type=message 的 content 里 type=output_text 的文本（reasoning 等块忽略）。
function parseResponsesResponse(body) {
  var text = "";
  var refusal = "";
  var output = isArray(body.output) ? body.output : [];
  for (var i = 0; i < output.length; i++) {
    var item = output[i];
    if (!item || item.type !== "message" || !isArray(item.content)) {
      continue;
    }
    for (var j = 0; j < item.content.length; j++) {
      var c = item.content[j];
      if (!c) {
        continue;
      }
      if (c.type === "output_text" && typeof c.text === "string") {
        text += c.text;
      } else if (c.type === "refusal" && typeof c.refusal === "string") {
        refusal += c.refusal;
      }
    }
  }
  var usage = usageOf(body.usage);
  if (text !== "") {
    return succeeded(text, usage); // 即使 status 是 incomplete（被截断），已有的文本照常交付
  }
  if (refusal !== "") {
    return failedWith("moderation", "模型拒绝回答：" + refusal);
  }
  var err = errorMessageOf(body);
  if (err) {
    return failed(err);
  }
  if (body.status === "incomplete") {
    var reason = body.incomplete_details && body.incomplete_details.reason ? String(body.incomplete_details.reason) : "未知原因";
    if (reason === "content_filter") {
      return failedWith("moderation", "输出被内容过滤拦截");
    }
    return failedWith("terminal", "上游响应不完整且没有文本（" + reason + "）；max_output_tokens 可能全被推理占用，请调大输出上限");
  }
  if (body.status === "failed" || body.status === "cancelled") {
    return failedWith("terminal", "上游响应状态：" + body.status);
  }
  return failedWith("terminal", "上游返回了空内容");
}

// Chat Completions：choices[0].message.content；finish_reason 为 content_filter 或 message.refusal 非空且无文本按 moderation。
function parseChatResponse(body) {
  var choice = isArray(body.choices) && body.choices.length ? body.choices[0] : null;
  if (!choice) {
    var err = errorMessageOf(body);
    return err ? failed(err) : failedWith("terminal", "上游没有返回 choices");
  }
  var message = choice.message || {};
  var content = message.content;
  if (isArray(content)) {
    // 个别网关把内容拆成片段数组
    var parts = [];
    for (var i = 0; i < content.length; i++) {
      if (content[i] && typeof content[i].text === "string") {
        parts.push(content[i].text);
      }
    }
    content = parts.join("");
  }
  if (typeof content === "string" && content !== "") {
    return succeeded(content, usageOf(body.usage));
  }
  if (typeof message.refusal === "string" && message.refusal !== "") {
    return failedWith("moderation", "模型拒绝回答：" + message.refusal);
  }
  if (choice.finish_reason === "content_filter") {
    return failedWith("moderation", "输出被内容过滤拦截");
  }
  if (choice.finish_reason === "length") {
    return failedWith("terminal", "输出被长度上限截断且没有文本；推理模型可能把额度全用在了思考上，请调大输出上限");
  }
  return failedWith("terminal", "上游返回了空内容");
}

function succeeded(text, usage) {
  var out = { type: "text", text: text };
  if (usage) {
    out.usage = usage;
  }
  return { status: "succeeded", outputs: [out] };
}

// Responses 用 input_tokens / output_tokens，Chat 用 prompt_tokens / completion_tokens；没有可用数字返回 null，宿主按冻结额结算。
function usageOf(raw) {
  if (!raw || typeof raw !== "object") {
    return null;
  }
  var input = pick(raw.input_tokens, raw.prompt_tokens);
  var output = pick(raw.output_tokens, raw.completion_tokens);
  if (typeof input !== "number" && typeof output !== "number") {
    return null;
  }
  return {
    input_tokens: typeof input === "number" ? Math.max(0, Math.round(input)) : 0,
    output_tokens: typeof output === "number" ? Math.max(0, Math.round(output)) : 0,
  };
}

// ---------------------------------------------------------------------------
// 导入
// ---------------------------------------------------------------------------

function isTextModel(id) {
  var s = id.toLowerCase();
  if (!/^(gpt-|chatgpt-|o[0-9])/.test(s)) {
    return false;
  }
  return !/embed|tts|whisper|dall-e|image|audio|moderation|realtime|transcribe|instruct|search|computer-use|davinci|babbage/.test(s);
}

// ---------------------------------------------------------------------------
// 通用小工具
// ---------------------------------------------------------------------------

function first(list) {
  return isArray(list) && list.length > 0 ? list[0] : undefined;
}

function failed(message) {
  return failedWith(classifyMessage(message), message);
}

function failedWith(cls, message) {
  return { status: "failed", error: { class: cls, message: message } };
}

// 按错误文案判断失败分类（用于 200 + error 体的情形）：内容策略 → moderation；额度 → provider_balance；其余 terminal。
function classifyMessage(message) {
  var s = String(message || "").toLowerCase();
  if (/content[ _-]?policy|safety system|moderation|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/insufficient_quota|insufficient|exceeded your current quota|billing|balance|余额|额度/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

// 从响应里取错误文案：兼容 {error:{message}} 与 {error:"..."}。
function errorMessageOf(body) {
  if (!body || typeof body !== "object") {
    return "";
  }
  var e = body.error;
  if (e && typeof e === "object" && typeof e.message === "string" && e.message) {
    return e.message;
  }
  if (typeof e === "string" && e) {
    return e;
  }
  return "";
}

function mergeExtra(body, extra) {
  if (!extra || typeof extra !== "object") {
    return;
  }
  for (var k in extra) {
    if (Object.prototype.hasOwnProperty.call(extra, k) && body[k] === undefined) {
      body[k] = extra[k]; // 用户输入与固定字段优先，extra 只补充
    }
  }
}

function firstString() {
  for (var i = 0; i < arguments.length; i++) {
    if (typeof arguments[i] === "string" && arguments[i] !== "") {
      return arguments[i];
    }
  }
  return "";
}

function pick() {
  for (var i = 0; i < arguments.length; i++) {
    if (arguments[i] !== undefined && arguments[i] !== null && arguments[i] !== "") {
      return arguments[i];
    }
  }
  return undefined;
}

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

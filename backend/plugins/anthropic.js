/**
 * Anthropic（Claude）文本插件（内置，契约 v1）。
 *
 * 对接 Anthropic 官方 Messages API（按公开 HTTP 协议调用，也适用于兼容该协议的中转站）：
 *   - text：POST /v1/messages，同步出正文。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 * 插件没有网络与文件能力，只把任务翻译成请求描述、把响应翻译成统一结果。
 *
 * 渠道填写：
 *   - 渠道 Key：Anthropic 控制台创建的 API Key（sk-ant-...）。宿主按 meta.auth 以 `x-api-key` 请求头注入，
 *     插件自己不碰 Key；`anthropic-version: 2023-06-01` 由本插件写在请求头里。
 *   - base_url：官方填 https://api.anthropic.com（不带 /v1）；中转站填它的根地址，需支持 /v1/messages 与 x-api-key 鉴权。
 *
 * 模型 params（运营在模型上填的固定参数）：
 *   text : { max_tokens?: number, system?: string, extra?: object }
 *          max_tokens 是用户可选输出上限的天花板；extra 原样合并进请求体顶层（如 top_p、top_k、thinking、metadata），
 *          只补充、不覆盖插件已写的字段（model、messages、max_tokens 等）。
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt（必填）、system（固定系统提示，优先于 params.system；放进请求体顶层 system，不进 messages）、
 *   max_tokens（最大输出，不能超过 params.max_tokens）、temperature、
 *   images[0]（视觉模型，可选；以图片 URL 块交给上游，宿主把文件引用换成自有存储的签名 URL）。
 *   Anthropic 要求 max_tokens 必填：用户与 params 都没给时用 DEFAULT_MAX_TOKENS（4096）。
 * 产物：只拼接响应 content 里 type 为 text 的块（thinking / redacted_thinking / tool_use 等一律跳过）；
 *       usage（input_tokens / output_tokens）随文本产物回传，按 Token 计费的模型据此结算。
 *
 * 未验证（依据官方文档整理，没有真实账号实测）：
 *   - usage.input_tokens 是否需要加上 cache_creation_input_tokens / cache_read_input_tokens：按文档它们不含在 input_tokens 里，
 *     这里把它们加进输入 Token（只在模型启用 prompt caching 时才会有这两项）。
 *   - 429 里“达到月度花费上限”的文案：按含 spend / usage limit 的文案归为余额不足（不会自行恢复），具体措辞以实际为准。
 *   - 400 的审核类文案（content policy / usage policy）归 moderation，措辞以实际为准。
 *   - GET /v1/models 的 limit 参数上限按 1000 使用，默认分页只有 20 条；超过一页的模型列表不翻页。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "anthropic",
    name: "Anthropic",
    version: "1.0.0",
    description: "对接 Anthropic 官方 Messages API：Claude 文本模型（同步，支持图片输入）",
    auth: { type: "header", name: "x-api-key" },
    endpoints: {
      text: { mode: "sync" },
    },
    import: { args: {} },
  },

  buildSubmitRequest: function (ctx) {
    return buildMessagesRequest(ctx);
  },

  parseSubmitResponse: function (ctx, resp) {
    return { immediate: parseMessagesResponse(resp) };
  },

  classifyError: function (ctx, resp) {
    var msg = errorMessageOf(resp.body) || (resp.text || "").slice(0, 300);
    var type = resp.body && resp.body.error && resp.body.error.type;
    var cls = "";
    if (resp.status === 402 || type === "billing_error" || BALANCE_RE.test(String(msg).toLowerCase())) {
      cls = "provider_balance"; // 余额不足 / 触及花费上限：重试没有意义
    } else if (resp.status === 400 && MODERATION_RE.test(String(msg).toLowerCase())) {
      cls = "moderation";
    } else if (resp.status === 429 || resp.status >= 500) {
      cls = "retryable"; // 限流、api_error(500)、timeout_error(504)、overloaded_error(529)
    }
    if (!cls) {
      return null; // 其余（400/401/403/404/413 ...）交给宿主默认规则，按 terminal 处理
    }
    return { class: cls, code: type || undefined, message: msg || undefined };
  },

  buildCheckRequest: function (ctx) {
    return { method: "GET", path: "/v1/models", headers: versionHeaders(), timeout: 15 };
  },

  buildImportRequest: function (ctx, args) {
    return {
      method: "GET",
      path: "/v1/models",
      query: { limit: 1000 },
      headers: versionHeaders(),
      timeout: 30,
    };
  },

  parseImportResponse: function (ctx, resp, args) {
    var list = resp.body && isArray(resp.body.data) ? resp.body.data : [];
    var drafts = [];
    for (var i = 0; i < list.length; i++) {
      var item = list[i];
      var id = item && item.id;
      if (typeof id !== "string" || id === "") {
        continue;
      }
      drafts.push({
        upstreamModel: id,
        kind: "text", // Anthropic 的模型列表只有 Claude 文本模型
        label: firstString(item.display_name, id),
        params: {},
      });
    }
    return drafts;
  },
};

// Anthropic 要求 max_tokens 必填；用户与模型 params 都没给时用这个值（足够一般对话，又不会把积分冻结额撑得过大）。
var DEFAULT_MAX_TOKENS = 4096;
var BALANCE_RE = /credit balance|billing|spend|usage limit/;
var MODERATION_RE = /content[ _-]?policy|usage polic/;

function versionHeaders() {
  return { "anthropic-version": "2023-06-01" };
}

// ---------------------------------------------------------------------------
// 文本
// ---------------------------------------------------------------------------

function buildMessagesRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};

  var content = input.prompt === undefined ? "" : String(input.prompt);
  var firstImage = first(input.images);
  if (firstImage) {
    // 视觉模型：图片块放在文字前面（官方建议），用 URL 形式（宿主把文件引用换成自有存储的签名 URL）
    content = [
      { type: "image", source: { type: "url", url: { __fileRef: firstImage, as: "url" } } },
      { type: "text", text: content },
    ];
  }

  var body = {
    model: ctx.model.upstreamModel,
    max_tokens: capMaxTokens(input.max_tokens, params.max_tokens),
    messages: [{ role: "user", content: content }],
    stream: false,
  };
  var system = firstString(input.system, params.system);
  if (system) {
    body.system = system; // Messages API 的 system 是顶层字段，不能放进 messages
  }
  if (typeof input.temperature === "number") {
    body.temperature = input.temperature;
  }
  mergeExtra(body, params.extra);
  return {
    method: "POST",
    path: "/v1/messages",
    headers: versionHeaders(),
    json: body,
    timeout: 120,
  };
}

// 用户传的 max_tokens 不能超过模型固定参数里的上限；只有上限没有用户值时用上限；都没有用默认值。
function capMaxTokens(fromInput, limit) {
  var hasLimit = typeof limit === "number" && limit > 0;
  if (typeof fromInput === "number" && fromInput > 0) {
    return hasLimit && fromInput > limit ? limit : Math.round(fromInput);
  }
  return hasLimit ? Math.round(limit) : DEFAULT_MAX_TOKENS;
}

function parseMessagesResponse(resp) {
  var body = resp.body || {};
  var blocks = isArray(body.content) ? body.content : [];
  var msg = errorMessageOf(body);
  if (msg && blocks.length === 0) {
    return failed(msg);
  }
  if (body.stop_reason === "refusal") {
    return {
      status: "failed",
      error: { class: "moderation", code: "refusal", message: "模型拒绝回答该请求" },
    };
  }

  // 只拼接文本块：thinking / redacted_thinking / tool_use 等块不是给用户看的正文
  var parts = [];
  for (var i = 0; i < blocks.length; i++) {
    if (blocks[i] && blocks[i].type === "text" && typeof blocks[i].text === "string") {
      parts.push(blocks[i].text);
    }
  }
  var text = parts.join("");
  if (text === "") {
    if (body.stop_reason === "max_tokens") {
      return failed("输出在产生文本前就达到了 max_tokens 上限（可能全被思考占用），请调大最大输出");
    }
    return failed("上游返回了空内容");
  }
  var output = { type: "text", text: text };
  var usage = usageOf(body.usage);
  if (usage) {
    output.usage = usage;
  }
  return { status: "succeeded", outputs: [output] };
}

// Anthropic usage → 宿主用量。缓存写入 / 读取的 Token 不含在 input_tokens 里，加起来才是实际输入；
// 没有可用数字时返回 null，宿主按冻结额结算。
function usageOf(raw) {
  if (!raw || typeof raw !== "object") {
    return null;
  }
  if (typeof raw.input_tokens !== "number" && typeof raw.output_tokens !== "number") {
    return null;
  }
  var input = num(raw.input_tokens) + num(raw.cache_creation_input_tokens) + num(raw.cache_read_input_tokens);
  return { input_tokens: Math.round(input), output_tokens: Math.round(num(raw.output_tokens)) };
}

// ---------------------------------------------------------------------------
// 通用小工具
// ---------------------------------------------------------------------------

function num(v) {
  return typeof v === "number" && v > 0 ? v : 0;
}

function first(list) {
  return isArray(list) && list.length > 0 ? list[0] : undefined;
}

function failed(message) {
  return { status: "failed", error: { class: classifyMessage(message), message: message } };
}

// 按错误文案判断失败分类：审核 → moderation；余额 → provider_balance；其余 terminal。
function classifyMessage(message) {
  var s = String(message || "").toLowerCase();
  if (MODERATION_RE.test(s)) {
    return "moderation";
  }
  if (BALANCE_RE.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

// 错误体 {"type":"error","error":{"type":"...","message":"..."}}；兼容 error 是字符串、顶层 message。
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
  if (body.type === "error" && typeof body.message === "string") {
    return body.message;
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

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

/**
 * OpenAI 图片插件（内置，契约 v1）。
 *
 * 对接 OpenAI Images API（以及兼容它的中转站），同步出图：
 *   - 文生图：POST /v1/images/generations（JSON）
 *   - 图生图 / 编辑：有参考图时 POST /v1/images/edits（multipart）
 * 接口依据 OpenAI 官方 OpenAPI 规范；没验证过的地方见文末“未验证”。
 *
 * 重要限制：只支持上游返回图片 URL（data[].url）的情形。
 *   契约不支持内联 base64 产物（产物只能是 url / text / asset），而官方 gpt-image 系列只返回 base64（b64_json），不返回 URL。
 *   所以 dall-e-2 / dall-e-3 与会返回 data[].url 的兼容中转站可用；官方 gpt-image 系列直连会得到一条明确的失败：
 *   “上游只返回 base64 图片，本协议暂不支持”（terminal）。要支持官方 gpt-image，需要先扩展契约（让宿主能把 base64 落库成素材）。
 *
 * 渠道怎么填：
 *   - Key  ：OpenAI 的 API Key（sk-...），由宿主按 Bearer 注入；
 *   - base_url：官方填 https://api.openai.com（路径里已带 /v1，base_url 不要再写 /v1）；兼容中转站填它的根地址。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 * 插件没有网络与文件能力，只把任务翻译成请求描述、把响应翻译成统一结果。
 *
 * 模型系列（按 ctx.model.upstreamModel 前缀判断）：
 *   dall-e-2                 size 256x256 / 512x512 / 1024x1024，n 1-10，发 response_format:"url"，不发 quality / style；编辑只能 1 张
 *   dall-e-3                 size 1024x1024 / 1792x1024 / 1024x1792，n 只能 1，quality standard / hd，style vivid / natural，发 response_format:"url"；不支持编辑
 *   gpt-image-* / chatgpt-image-*  size 1024x1024 / 1536x1024 / 1024x1536 / auto，quality low / medium / high / auto，n 1-10；
 *                            官方对这些模型发 response_format 会 400，所以不发；编辑可多张（字段 image[]）；不发 style
 *   其它名字                 按通用处理：不发 response_format，size / quality / style 原样透传
 *
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt（必填）、op（t2i / i2i）、images（参考图，有则走编辑）、n 或 count、size、quality、style、aspect_ratio、resolution
 *   size 优先用用户给的；没给时有 aspect_ratio 就换算成该系列允许的最接近的 size；都没有就不发，让上游用默认。
 *   resolution 本接口没有对应概念，忽略（尺寸请用 size 或 aspect_ratio）。
 * 产物：data[] 里每个有 url 的成员一个 {type:"url", media_type:"image"} 产物，mime 按扩展名猜，猜不出用 image/png。
 *
 * 刻意没做：
 *   - base64 产物（见上）；
 *   - 遮罩图 mask、input_fidelity、background、output_format 等型号特有字段的输入；
 *   - 流式出图（stream / partial_images）。
 *
 * 未验证：
 *   - 全部按官方 OpenAPI 规范与记忆写成，没有对真实上游跑过；
 *   - dall-e-2 编辑要求 PNG（方形、小于 4MB），本插件不转格式也不校验，交给上游报错；
 *   - 中转站对 size / quality / response_format 的取舍各不相同，不通时请改用面板里的 size / quality 取值；
 *   - 未知模型名的 aspect_ratio 换算借用 gpt-image 的三档 size，个别中转站可能不认；
 *   - 内容审核只按错误码 content_policy_violation / moderation_blocked 与“content policy / safety system”文案识别，其它措辞按 terminal 处理。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "openai-image",
    name: "OpenAI · 图片",
    version: "1.1.0",
    vendor: "openai",
    description: "对接 OpenAI Images API：文生图与图片编辑（同步，只支持返回图片 URL 的模型 / 中转站）",
    auth: { type: "bearer" },
    endpoints: {
      image: { mode: "sync", method: "POST", path: "/v1/images/generations" },
    },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    if (refList(input.images).length > 0) {
      return buildEditRequest(ctx);
    }
    return buildGenerateRequest(ctx);
  },

  parseSubmitResponse: function (ctx, resp) {
    return { immediate: parseImageResponse(resp.body || {}) };
  },

  // 非 2xx：OpenAI 错误体 {error:{message,type,code}}。额度 → provider_balance，内容策略 → moderation，
  // 429 与 5xx → retryable，其余（400 / 401 / 403 / 404）terminal。
  classifyError: function (ctx, resp) {
    var msg = errorMessageOf(resp.body) || (resp.text || "").slice(0, 300);
    var code = errorCodeOf(resp.body);
    var out = { class: classifyOf(code, errorTypeOf(resp.body), msg, resp.status), message: msg || "上游返回 HTTP " + resp.status };
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

  // 只导入图片类模型（dall-e- / gpt-image- / chatgpt-image-）。
  parseImportResponse: function (ctx, resp, args) {
    var list = resp.body && isArray(resp.body.data) ? resp.body.data : [];
    var drafts = [];
    for (var i = 0; i < list.length; i++) {
      var id = list[i] && list[i].id;
      if (typeof id !== "string" || id === "" || seriesOf(id) === "other") {
        continue;
      }
      drafts.push({ upstreamModel: id, kind: "image", label: id });
    }
    return drafts;
  },
};

// ---------------------------------------------------------------------------
// 请求
// ---------------------------------------------------------------------------

// 各系列允许的 size；aspect_ratio 换算时从这里选最接近的。未知模型借用 gpt 的三档。
var SIZES = {
  dalle2: ["1024x1024"],
  dalle3: ["1024x1024", "1792x1024", "1024x1792"],
  gpt: ["1024x1024", "1536x1024", "1024x1536"],
  other: ["1024x1024", "1536x1024", "1024x1536"],
};

// 按模型名前缀判断系列：dalle2 / dalle3 / gpt（gpt-image- 与 chatgpt-image-）/ other。
function seriesOf(model) {
  var s = String(model || "").toLowerCase();
  if (/^dall-e-2/.test(s)) {
    return "dalle2";
  }
  if (/^dall-e-3/.test(s)) {
    return "dalle3";
  }
  if (/^(gpt-image-|chatgpt-image-)/.test(s)) {
    return "gpt";
  }
  return "other";
}

function buildGenerateRequest(ctx) {
  var input = ctx.input || {};
  var model = ctx.model.upstreamModel;
  var series = seriesOf(model);
  var prompt = requirePrompt(input, model);

  var body = { model: model, prompt: prompt };
  var n = imageCount(input, series, model);
  if (n !== undefined) {
    body.n = n;
  }
  var size = imageSize(series, input);
  if (size) {
    body.size = size;
  }
  var quality = firstString(input.quality);
  if (quality && series !== "dalle2") {
    body.quality = quality;
  }
  var style = firstString(input.style);
  if (style && (series === "dalle3" || series === "other")) {
    body.style = style;
  }
  if (series === "dalle2" || series === "dalle3") {
    body.response_format = "url"; // gpt-image 系发这个字段官方会 400，所以只对 dall-e 发
  }
  return { method: "POST", path: "/v1/images/generations", json: body, timeout: 120 };
}

function buildEditRequest(ctx) {
  var input = ctx.input || {};
  var model = ctx.model.upstreamModel;
  var series = seriesOf(model);
  var prompt = requirePrompt(input, model);
  var images = refList(input.images);

  if (series === "dalle3") {
    throw new Error(model + " 不支持图片编辑，不能带参考图");
  }
  if (series === "dalle2" && images.length > 1) {
    throw new Error(model + " 编辑只支持 1 张参考图");
  }

  var fields = { model: model, prompt: prompt };
  var n = imageCount(input, series, model);
  if (n !== undefined) {
    fields.n = String(n);
  }
  var size = imageSize(series, input);
  if (size) {
    fields.size = size;
  }
  var quality = firstString(input.quality);
  if (quality && series !== "dalle2") {
    fields.quality = quality;
  }
  if (series === "dalle2") {
    fields.response_format = "url";
  }

  // gpt-image 系固定用 image[]（可多张）；其它系列一张用 image，多张用 image[]
  var partName = series === "gpt" || images.length > 1 ? "image[]" : "image";
  var parts = [];
  for (var i = 0; i < images.length; i++) {
    parts.push({ name: partName, fileRef: images[i] });
  }
  return { method: "POST", path: "/v1/images/edits", multipart: { fields: fields, parts: parts }, timeout: 120 };
}

function requirePrompt(input, model) {
  var prompt = input.prompt === undefined || input.prompt === null ? "" : String(input.prompt);
  if (prompt === "") {
    throw new Error(model + " 需要提示词");
  }
  return prompt;
}

// 出图张数：用户没给就不发（上游默认 1）；给了必须是 1-10 的整数，dall-e-3 只能 1。
function imageCount(input, series, model) {
  var n = typeof input.count === "number" ? input.count : typeof input.n === "number" ? input.n : undefined;
  if (n === undefined) {
    return undefined;
  }
  var max = series === "dalle3" ? 1 : 10;
  if (n !== Math.floor(n) || n < 1 || n > max) {
    throw new Error(model + " 一次出图张数必须是 1 到 " + max + " 的整数");
  }
  return n;
}

// size 优先用用户给的；否则按 aspect_ratio 选该系列最接近的合法 size；都没有返回空串（不发）。
function imageSize(series, input) {
  var size = firstString(input.size);
  if (size) {
    return size;
  }
  var ratio = parseRatio(input.aspect_ratio);
  if (!ratio) {
    return "";
  }
  var list = SIZES[series] || SIZES.other;
  var best = list[0];
  var bestDiff = Infinity;
  for (var i = 0; i < list.length; i++) {
    var m = /^(\d+)x(\d+)$/.exec(list[i]);
    var diff = Math.abs(Math.log(ratio / (Number(m[1]) / Number(m[2]))));
    if (diff < bestDiff - 1e-9) {
      best = list[i];
      bestDiff = diff;
    }
  }
  return best;
}

// "16:9" -> 1.777…；不是 W:H 形式（如 adaptive）返回 0。
function parseRatio(v) {
  var m = /^(\d+(?:\.\d+)?)\s*[:x*]\s*(\d+(?:\.\d+)?)$/.exec(String(v === undefined || v === null ? "" : v).trim());
  if (!m) {
    return 0;
  }
  var a = parseFloat(m[1]);
  var b = parseFloat(m[2]);
  return a > 0 && b > 0 ? a / b : 0;
}

// ---------------------------------------------------------------------------
// 响应
// ---------------------------------------------------------------------------

// data[] 里每个 http/https 的 url 一个产物；没有 url 但有 b64_json 说明上游只返回 base64，明确告知不支持。
function parseImageResponse(body) {
  var list = isArray(body.data) ? body.data : [];
  var outputs = [];
  var hasBase64 = false;
  for (var i = 0; i < list.length; i++) {
    var item = list[i];
    if (!item) {
      continue;
    }
    if (typeof item.url === "string" && /^https?:\/\//i.test(item.url)) {
      outputs.push({ type: "url", url: item.url, media_type: "image", mime: mimeOfImage(item.url) });
    } else if (typeof item.b64_json === "string" && item.b64_json !== "") {
      hasBase64 = true;
    }
  }
  if (outputs.length > 0) {
    return { status: "succeeded", outputs: outputs };
  }
  var msg = errorMessageOf(body);
  if (msg) {
    return failedWith(classifyOf(errorCodeOf(body), errorTypeOf(body), msg, 200), msg, errorCodeOf(body));
  }
  if (hasBase64) {
    return failedWith("terminal", "上游只返回 base64 图片，本协议暂不支持");
  }
  return failedWith("terminal", "上游没有返回图片");
}

function mimeOfImage(url) {
  var m = /\.([a-z0-9]{2,5})(?:\?|#|$)/i.exec(url);
  var ext = m ? m[1].toLowerCase() : "";
  if (ext === "jpg" || ext === "jpeg") {
    return "image/jpeg";
  }
  if (ext === "webp") {
    return "image/webp";
  }
  if (ext === "gif") {
    return "image/gif";
  }
  return "image/png";
}

// ---------------------------------------------------------------------------
// 通用小工具
// ---------------------------------------------------------------------------

function failedWith(cls, message, code) {
  var err = { class: cls, message: message };
  if (code) {
    err.code = code;
  }
  return { status: "failed", error: err };
}

// 错误分类（HTTP 非 2xx 与 200 + error 体共用）：额度 / 账单 → provider_balance；内容策略 → moderation；
// 429 与 5xx → retryable；其余 terminal（显式给出，避免宿主默认规则按文案里的 quota / credit 之类的词误判）。
function classifyOf(code, type, msg, status) {
  var c = String(code || "").toLowerCase();
  var t = String(type || "").toLowerCase();
  var lower = String(msg || "").toLowerCase();
  if (c === "insufficient_quota" || t === "insufficient_quota" || /billing/.test(c) || /billing/.test(t) || /billing hard limit|exceeded your current quota/.test(lower)) {
    return "provider_balance";
  }
  if (c === "content_policy_violation" || c === "moderation_blocked" || /content[ _-]?policy|safety system/.test(lower)) {
    return "moderation";
  }
  if (status === 429 || status >= 500) {
    return "retryable";
  }
  return "terminal";
}

// 从响应里取错误信息：兼容 {error:{message,type,code}} 与 {error:"..."}。
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

function errorCodeOf(body) {
  var e = body && typeof body === "object" ? body.error : null;
  return e && typeof e === "object" && typeof e.code === "string" ? e.code : "";
}

function errorTypeOf(body) {
  var e = body && typeof body === "object" ? body.error : null;
  return e && typeof e === "object" && typeof e.type === "string" ? e.type : "";
}

function refList(v) {
  return isArray(v) ? v : [];
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

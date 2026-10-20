/**
 * OpenAI 兼容 · 对话生图插件（内置，契约 v1）。
 *
 * 对接“用 Chat Completions 出图”的 OpenAI 兼容网关（New API / One API 一类中转站上的 nano-banana、gemini-*-image 等模型），同步出图：
 *   POST /v1/chat/completions，提示词（和参考图）放在 user 消息里，图片由模型在回复里给出。
 * 和 openai-image 的区别：那个走官方 Images 接口（/v1/images/generations），这个走对话接口，响应里没有 data[].url，
 * 图片藏在回复内容里，所以要自己把图片地址从回复里抠出来。
 *
 * 渠道怎么填：
 *   - Key  ：网关的 API Key，由宿主按 Bearer 注入；
 *   - base_url：网关根地址（如 https://api.yingce.tv），路径里已带 /v1，base_url 不要再写 /v1。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 * 插件没有网络与文件能力，只把任务翻译成请求描述、把响应翻译成统一结果。
 *
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt（必填）、images（参考图，有则一并放进 user 消息，发成签名 URL）、aspect_ratio、resolution
 *   aspect_ratio / resolution 没有对话接口的标准字段，按 OpenRouter / Gemini 兼容网关的惯例放进 image_config
 *   （aspect_ratio、image_size）；用户没给就整个不发。网关不认时会被忽略，要控制比例请直接写进提示词。
 *   一次只出一张的模型居多，本协议不发 n。
 *
 * 从响应里取图（按顺序，按 URL 去重，只收 http / https）：
 *   1. choices[0].message.images[]：成员可以是 {image_url:{url}}、{image_url:"…"} 或直接是字符串；
 *   2. message.content 是数组时，type=image_url 的块；
 *   3. message.content 的文本里的 markdown 图片 ![…](url)，以及以图片扩展名结尾的裸 URL。
 *   产物 {type:"url", media_type:"image"}，mime 按扩展名猜，猜不出用 image/png。
 *
 * 重要限制：只支持上游返回图片 URL 的情形。
 *   契约不支持内联 base64 产物，宿主读响应体也有 5MB 上限。回复里是 data:image/...;base64 的，
 *   在没有任何 URL 的情况下会得到一条明确的失败：“上游只返回 base64 图片，本协议暂不支持”（terminal）。
 *
 * 刻意没做：
 *   - base64 产物（见上）；
 *   - 流式（固定 stream:false）；
 *   - system 提示、多轮对话上下文（生图任务每次都是独立的一问一答）。
 *
 * 未验证：
 *   - 全部按 OpenAI 兼容网关的常见写法写成，没有对真实上游跑过；各家网关把图片放在 content 还是 images 字段不一致，
 *     这里两处都认，仍取不到时错误信息里会带上模型返回的文字，方便照着调整；
 *   - image_config 的字段名（aspect_ratio / image_size）借自 OpenRouter 的约定，yingce 等网关是否认没确认；
 *   - 裸 URL 只认带图片扩展名的（png / jpg / jpeg / webp / gif / bmp / avif），没有扩展名的签名地址只有写成 markdown 图片才会被收下；
 *   - 导入按模型名粗筛（image / banana / imagen / dall-e / flux / seedream 等），个别生图模型名不含这些词会被漏掉，运营在导入后补；
 *   - 内容审核只按 message.refusal、finish_reason=content_filter、错误码 content_policy_violation / moderation_blocked
 *     与“content policy / safety system”文案识别，其它措辞按 terminal 处理。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "openai-chat-image",
    name: "OpenAI 兼容 · 对话生图",
    version: "1.1.0",
    vendor: "openai",
    description: "对接用 Chat Completions 出图的 OpenAI 兼容网关（如 nano-banana）：文生图与参考图生图（同步，只支持返回图片 URL 的网关）",
    auth: { type: "bearer" },
    endpoints: {
      image: { mode: "sync", method: "POST", path: "/v1/chat/completions" },
    },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var model = ctx.model.upstreamModel;
    var prompt = input.prompt === undefined || input.prompt === null ? "" : String(input.prompt);
    if (prompt === "") {
      throw new Error(model + " 需要提示词");
    }

    var content = prompt;
    var images = isArray(input.images) ? input.images : [];
    if (images.length > 0) {
      content = [{ type: "text", text: prompt }];
      for (var i = 0; i < images.length; i++) {
        content.push({ type: "image_url", image_url: { url: { __fileRef: images[i], as: "url" } } });
      }
    }

    var body = { model: model, messages: [{ role: "user", content: content }], stream: false };
    var config = {};
    var ratio = firstString(input.aspect_ratio);
    if (ratio) {
      config.aspect_ratio = ratio;
    }
    var resolution = firstString(input.resolution);
    if (resolution) {
      config.image_size = resolution;
    }
    if (ratio || resolution) {
      body.image_config = config;
    }
    return { method: "POST", path: "/v1/chat/completions", json: body, timeout: 180 };
  },

  parseSubmitResponse: function (ctx, resp) {
    return { immediate: parseChatImageResponse(resp.body || {}) };
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

  // 网关的模型列表什么都有，只导名字像生图模型的。
  parseImportResponse: function (ctx, resp, args) {
    var list = resp.body && isArray(resp.body.data) ? resp.body.data : [];
    var drafts = [];
    for (var i = 0; i < list.length; i++) {
      var id = list[i] && list[i].id;
      if (typeof id !== "string" || id === "" || !isImageModel(id)) {
        continue;
      }
      drafts.push({ upstreamModel: id, kind: "image", label: id });
    }
    return drafts;
  },
};

// ---------------------------------------------------------------------------
// 响应
// ---------------------------------------------------------------------------

var IMAGE_EXT = /\.(png|jpe?g|webp|gif|bmp|avif)(?:[?#]|$)/i;

// 取图顺序见文件头。有 URL 就成功；没有 URL 时按 base64 / 拒答 / 内容过滤 / 只有文字 / 空内容分别给出明确的失败。
function parseChatImageResponse(body) {
  var choice = isArray(body.choices) && body.choices.length ? body.choices[0] : null;
  if (!choice) {
    var err = errorMessageOf(body);
    if (err) {
      return failedWith(classifyOf(errorCodeOf(body), errorTypeOf(body), err, 200), err, errorCodeOf(body));
    }
    return failedWith("terminal", "上游没有返回 choices");
  }
  var message = choice.message || {};
  var found = { urls: [], base64: false };
  collectFromImages(message.images, found);
  var text = collectFromContent(message.content, found);

  if (found.urls.length > 0) {
    var outputs = [];
    for (var i = 0; i < found.urls.length; i++) {
      outputs.push({ type: "url", url: found.urls[i], media_type: "image", mime: mimeOfImage(found.urls[i]) });
    }
    return { status: "succeeded", outputs: outputs };
  }
  if (found.base64) {
    return failedWith("terminal", "上游只返回 base64 图片，本协议暂不支持");
  }
  if (typeof message.refusal === "string" && message.refusal !== "") {
    return failedWith("moderation", "模型拒绝生成：" + message.refusal);
  }
  if (choice.finish_reason === "content_filter") {
    return failedWith("moderation", "输出被内容过滤拦截");
  }
  if (text !== "") {
    return failedWith("terminal", "上游没有返回图片，模型回复的文字：" + text.slice(0, 200));
  }
  return failedWith("terminal", "上游没有返回图片");
}

// message.images[]：成员是 {image_url:{url}}、{image_url:"…"}、{url:"…"} 或字符串。
function collectFromImages(list, found) {
  if (!isArray(list)) {
    return;
  }
  for (var i = 0; i < list.length; i++) {
    addUrl(imageUrlOf(list[i]), found);
  }
}

// message.content：字符串，或网关拆成的片段数组。返回其中的全部文本（供“只有文字”的失败信息使用），同时收集图片。
function collectFromContent(content, found) {
  if (typeof content === "string") {
    scanText(content, found);
    return content;
  }
  if (!isArray(content)) {
    return "";
  }
  var texts = [];
  for (var i = 0; i < content.length; i++) {
    var part = content[i];
    if (typeof part === "string") {
      scanText(part, found);
      texts.push(part);
    } else if (part && typeof part === "object") {
      if (part.type === "image_url" || (part.image_url !== undefined && typeof part.text !== "string")) {
        addUrl(imageUrlOf(part), found);
      } else if (typeof part.text === "string") {
        scanText(part.text, found);
        texts.push(part.text);
      }
    }
  }
  return texts.join("");
}

function imageUrlOf(item) {
  if (typeof item === "string") {
    return item;
  }
  if (!item || typeof item !== "object") {
    return "";
  }
  var u = item.image_url;
  if (typeof u === "string") {
    return u;
  }
  if (u && typeof u === "object" && typeof u.url === "string") {
    return u.url;
  }
  return typeof item.url === "string" ? item.url : "";
}

// 文本里先收 markdown 图片 ![alt](url "title")，再收带图片扩展名的裸 URL；data:image 记为 base64。
function scanText(text, found) {
  var md = /!\[[^\]]*\]\(\s*<?([^\s)>]+)>?(?:\s+"[^"]*")?\s*\)/g;
  var m;
  while ((m = md.exec(text)) !== null) {
    addUrl(m[1], found);
  }
  var bare = /https?:\/\/[^\s"'<>()\[\]]+/g;
  while ((m = bare.exec(text)) !== null) {
    if (IMAGE_EXT.test(m[0])) {
      addUrl(m[0], found);
    }
  }
  if (/data:image\/[a-z0-9.+-]+;base64,/i.test(text)) {
    found.base64 = true;
  }
}

// http / https 的地址按 URL 去重收下；data:image 记为 base64；其它（ftp、相对路径）忽略。
function addUrl(url, found) {
  if (typeof url !== "string" || url === "") {
    return;
  }
  if (/^https?:\/\//i.test(url)) {
    for (var i = 0; i < found.urls.length; i++) {
      if (found.urls[i] === url) {
        return;
      }
    }
    found.urls.push(url);
  } else if (/^data:image\//i.test(url)) {
    found.base64 = true;
  }
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
// 导入
// ---------------------------------------------------------------------------

function isImageModel(id) {
  var s = id.toLowerCase();
  if (/video|embed|tts|whisper|audio|moderation|realtime|transcribe/.test(s)) {
    return false;
  }
  return /image|banana|imagen|dall-e|flux|seedream|seededit|midjourney|stable-diffusion|sdxl/.test(s);
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

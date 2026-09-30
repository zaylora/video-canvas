/**
 * New API 网关插件（内置，契约 v1）。
 *
 * 对接自建 / 第三方的 New API 网关（按 OpenAI 风格的公开 HTTP 协议调用）：
 *   - text  ：POST /v1/chat/completions，同步出正文；
 *   - video ：POST /v1/video/generations 提交，GET /v1/video/generations/{task_id} 轮询，异步出片；
 *   - image ：无参考图 POST /v1/images/generations（JSON），有参考图 POST /v1/images/edits（multipart），同步出图；
 *   - audio ：POST /v1/audio/speech（语音合成），响应是二进制音频，由宿主直接落库。
 * 本文件是我们自己按公开接口写的，没有复用 New API 仓库里任何官方插件的代码（那是 AGPLv3 代码）。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 * 插件没有网络与文件能力，只把任务翻译成请求描述、把响应翻译成统一结果；鉴权（Bearer）由宿主按渠道 Key 注入。
 *
 * 模型 params（运营在模型上填的固定参数）：
 *   text : { max_tokens?: number, system?: string, extra?: object }   extra 原样合并进请求体顶层
 *   video: { metadata?: object, extra?: object }                      metadata 放进请求体的 metadata，extra 合并进顶层
 *   image: { extra?: object }                                         extra 合并进请求体顶层（如 response_format、background）
 *   audio: { voice?: string, format?: string, speed?: number, extra?: object }   固定音色 / 格式 / 语速，用户没传时才用
 * 用户输入（input_schema 里声明的字段，按名字直传）：
 *   text : prompt（必填）、system、temperature、max_tokens（不能超过 params.max_tokens）、image（视觉模型，可选）
 *   video: prompt、image（首帧，可选）、duration、width、height、fps、seed、size、n
 *   image: prompt（必填）、image（参考图，可选，传了就走 edits）、n、size、quality、style
 *   audio: prompt（要朗读的文本，必填；也接受 text）、voice、format（mp3 / wav / opus / aac / flac）、speed
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "newapi",
    name: "New API",
    version: "1.1.0",
    description: "对接 New API 网关：文本、图片、语音（同步）与视频（异步）",
    auth: { type: "bearer" },
    allowedHosts: [],
    endpoints: {
      text: { mode: "sync" },
      video: { mode: "async" },
      image: { mode: "sync" },
      audio: { mode: "sync" }
    },
    import: { args: {} },
    poll: { firstDelay: 10, interval: 5, maxInterval: 15, jitter: 0.2 }
  },

  buildSubmitRequest: function (ctx) {
    switch (ctx.model.kind) {
      case "text":
        return buildChatRequest(ctx);
      case "image":
        return buildImageRequest(ctx);
      case "audio":
        return buildSpeechRequest(ctx);
      default:
        return buildVideoRequest(ctx);
    }
  },

  parseSubmitResponse: function (ctx, resp) {
    if (ctx.model.kind === "text") {
      return { immediate: parseChatResponse(resp) };
    }
    if (ctx.model.kind === "image") {
      return { immediate: parseImageResponse(resp) };
    }
    if (ctx.model.kind === "audio") {
      return { immediate: parseSpeechResponse(resp) };
    }
    var body = resp.body || {};
    var inner = body.data && !isArray(body.data) ? body.data : {};
    var id = pick(body.task_id, body.id, inner.task_id, inner.id);
    if (id === undefined) {
      var msg = errorMessageOf(body);
      if (msg) {
        return { immediate: failed(msg) };
      }
      throw new Error("上游没有返回 task_id");
    }
    return { providerTaskId: String(id) };
  },

  buildQueryRequest: function (ctx) {
    return {
      method: "GET",
      path: "/v1/video/generations/" + encodeURIComponent(ctx.task.providerTaskId),
      timeout: 30
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    var inner = body.data && !isArray(body.data) ? body.data : {};
    var raw = pick(body.status, inner.status);
    var status = mapVideoStatus(raw);
    var progress = parseProgress(pick(body.progress, inner.progress));

    if (status === "failed") {
      var r = failed(errorMessageOf(body) || errorMessageOf(inner) || ("上游任务失败：" + raw));
      if (progress !== undefined) {
        r.progress = progress;
      }
      return r;
    }
    if (status !== "succeeded") {
      var running = { status: status };
      if (progress !== undefined) {
        running.progress = progress;
      }
      return running;
    }

    var url = videoUrlOf(body, inner);
    if (!url) {
      return failed("上游返回成功，但没有视频地址");
    }
    return {
      status: "succeeded",
      progress: 100,
      outputs: [{ type: "url", url: url, media_type: "video", mime: mimeOfVideo(body, inner, url) }]
    };
  },

  classifyError: function (ctx, resp) {
    var msg = errorMessageOf(resp.body) || (resp.text || "").slice(0, 300);
    if (!msg) {
      return null;
    }
    var cls = classifyMessage(msg);
    if (cls === "terminal") {
      return null; // 其余交给宿主默认规则（429 / 5xx 重试，其它 terminal）
    }
    return { class: cls, message: msg };
  },

  buildCheckRequest: function (ctx) {
    return { method: "GET", path: "/v1/models", timeout: 15 };
  },

  buildImportRequest: function (ctx, args) {
    return { method: "GET", path: "/v1/models", timeout: 30 };
  },

  parseImportResponse: function (ctx, resp, args) {
    var list = resp.body && isArray(resp.body.data) ? resp.body.data : [];
    var drafts = [];
    for (var i = 0; i < list.length; i++) {
      var id = list[i] && list[i].id;
      if (typeof id !== "string" || id === "") {
        continue;
      }
      var kind = guessKind(id);
      if (kind === "") {
        continue; // 本插件目前只支持文本与视频
      }
      drafts.push({
        upstreamModel: id,
        kind: kind,
        label: id,
        params: {},
        inputSchema: inputSchemaOf(kind)
      });
    }
    return drafts;
  }
};

// ---------------------------------------------------------------------------
// 文本
// ---------------------------------------------------------------------------

function buildChatRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};
  var messages = [];

  var system = firstString(input.system, params.system);
  if (system) {
    messages.push({ role: "system", content: system });
  }
  var userContent = input.prompt === undefined ? "" : String(input.prompt);
  if (input.image) {
    // 视觉模型：提示词 + 图片地址（宿主把文件引用换成自有存储的签名 URL）
    userContent = [
      { type: "text", text: userContent },
      { type: "image_url", image_url: { url: { __fileRef: input.image, as: "url" } } }
    ];
  }
  messages.push({ role: "user", content: userContent });

  var body = { model: ctx.model.upstreamModel, messages: messages, stream: false };
  if (typeof input.temperature === "number") {
    body.temperature = input.temperature;
  }
  var maxTokens = capMaxTokens(input.max_tokens, params.max_tokens);
  if (maxTokens !== undefined) {
    body.max_tokens = maxTokens;
  }
  mergeExtra(body, params.extra);
  return { method: "POST", path: "/v1/chat/completions", json: body, timeout: 120 };
}

// 用户传的 max_tokens 不能超过模型固定参数里的上限；只有上限没有用户值时用上限。
function capMaxTokens(fromInput, limit) {
  var hasLimit = typeof limit === "number" && limit > 0;
  if (typeof fromInput === "number" && fromInput > 0) {
    return hasLimit && fromInput > limit ? limit : fromInput;
  }
  return hasLimit ? limit : undefined;
}

function parseChatResponse(resp) {
  var body = resp.body || {};
  var msg = errorMessageOf(body);
  if (msg && !(body.choices && body.choices.length)) {
    return failed(msg);
  }
  var choice = body.choices && body.choices.length ? body.choices[0] : null;
  var content = choice && choice.message ? choice.message.content : null;
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
  if (typeof content !== "string" || content === "") {
    return failed("上游返回了空内容");
  }
  var result = { status: "succeeded", outputs: [{ type: "text", text: content }] };
  return result;
}

// ---------------------------------------------------------------------------
// 视频
// ---------------------------------------------------------------------------

function buildVideoRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};
  var body = { model: ctx.model.upstreamModel };
  if (input.prompt !== undefined) {
    body.prompt = String(input.prompt);
  }
  if (input.image) {
    body.image = { __fileRef: input.image, as: "url" };
  }
  var passthrough = ["duration", "width", "height", "fps", "seed", "size", "n"];
  for (var i = 0; i < passthrough.length; i++) {
    var k = passthrough[i];
    if (input[k] !== undefined && input[k] !== null && input[k] !== "") {
      body[k] = input[k];
    }
  }
  if (params.metadata && typeof params.metadata === "object") {
    body.metadata = params.metadata;
  }
  mergeExtra(body, params.extra);
  return { method: "POST", path: "/v1/video/generations", json: body, timeout: 60 };
}

function mapVideoStatus(raw) {
  var s = String(raw === undefined || raw === null ? "" : raw).toLowerCase();
  switch (s) {
    case "completed":
    case "succeeded":
    case "success":
    case "done":
    case "finished":
      return "succeeded";
    case "failed":
    case "failure":
    case "error":
    case "cancelled":
    case "canceled":
    case "expired":
      return "failed";
    case "in_progress":
    case "running":
    case "processing":
    case "generating":
      return "running";
    default:
      // queued / submitted / not_start / pending / 未知：继续等待，由任务 deadline 兜底
      return "queued";
  }
}

function videoUrlOf(body, inner) {
  var candidates = [body.url, body.video_url, inner.url, inner.video_url];
  if (body.metadata) {
    candidates.push(body.metadata.url);
  }
  if (body.result) {
    candidates.push(body.result.url);
  }
  if (body.output) {
    candidates.push(body.output.url);
  }
  if (isArray(body.data) && body.data.length && body.data[0]) {
    candidates.push(body.data[0].url);
  }
  for (var i = 0; i < candidates.length; i++) {
    if (typeof candidates[i] === "string" && candidates[i] !== "") {
      return candidates[i];
    }
  }
  return "";
}

function mimeOfVideo(body, inner, url) {
  var fmt = String(pick(body.format, inner.format, "") || "").toLowerCase();
  if (!fmt) {
    var m = /\.([a-z0-9]{2,5})(?:\?|#|$)/i.exec(url);
    fmt = m ? m[1].toLowerCase() : "";
  }
  if (fmt === "webm") {
    return "video/webm";
  }
  if (fmt === "mov") {
    return "video/quicktime";
  }
  return "video/mp4";
}

// ---------------------------------------------------------------------------
// 图片
// ---------------------------------------------------------------------------

var IMAGE_PASSTHROUGH = ["n", "size", "quality", "style"];

// 没有参考图走 /v1/images/generations（JSON）；有参考图走 /v1/images/edits（multipart，图片由宿主按文件引用上传）。
function buildImageRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};
  var fields = { model: ctx.model.upstreamModel };
  if (input.prompt !== undefined) {
    fields.prompt = String(input.prompt);
  }
  for (var i = 0; i < IMAGE_PASSTHROUGH.length; i++) {
    var k = IMAGE_PASSTHROUGH[i];
    if (input[k] !== undefined && input[k] !== null && input[k] !== "") {
      fields[k] = input[k];
    }
  }
  mergeExtra(fields, params.extra);

  if (input.image) {
    return {
      method: "POST",
      path: "/v1/images/edits",
      multipart: { fields: fields, parts: [{ name: "image", fileRef: "input:image" }] },
      timeout: 180
    };
  }
  return { method: "POST", path: "/v1/images/generations", json: fields, timeout: 180 };
}

function parseImageResponse(resp) {
  var body = resp.body || {};
  var list = isArray(body.data) ? body.data : [];
  var outputs = [];
  var sawBase64 = false;
  for (var i = 0; i < list.length; i++) {
    var item = list[i];
    if (item && typeof item.url === "string" && item.url !== "") {
      outputs.push({ type: "url", url: item.url, media_type: "image", mime: mimeOfImage(item.url) });
    } else if (item && typeof item.b64_json === "string" && item.b64_json !== "") {
      sawBase64 = true;
    }
  }
  if (outputs.length > 0) {
    return { status: "succeeded", outputs: outputs };
  }
  var msg = errorMessageOf(body);
  if (msg) {
    return failed(msg);
  }
  if (sawBase64) {
    // 产物只支持下载地址与二进制响应，不接收内联 base64
    return failed("上游只返回了 base64 图片，请在模型 params.extra 里指定 response_format 为 url，或换用支持返回地址的模型");
  }
  return failed("上游没有返回图片");
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
// 语音（文本转语音）
// ---------------------------------------------------------------------------

function buildSpeechRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};
  var text = pick(input.prompt, input.text, input.input);
  var body = {
    model: ctx.model.upstreamModel,
    input: text === undefined ? "" : String(text),
    voice: firstString(input.voice, params.voice) || "alloy"
  };
  var format = firstString(input.format, params.format);
  if (format) {
    body.response_format = format;
  }
  var speed = typeof input.speed === "number" ? input.speed : params.speed;
  if (typeof speed === "number") {
    body.speed = speed;
  }
  mergeExtra(body, params.extra);
  // 响应是音频字节：宿主直接写入素材存储，不经过插件
  return { method: "POST", path: "/v1/audio/speech", json: body, responseType: "binary", timeout: 120 };
}

function parseSpeechResponse(resp) {
  if (!resp.asset || !resp.asset.id) {
    return failed(errorMessageOf(resp.body) || "上游没有返回音频");
  }
  return {
    status: "succeeded",
    outputs: [{ type: "asset" }] // 素材 id、地址、mime 由宿主补全
  };
}

// ---------------------------------------------------------------------------
// 导入
// ---------------------------------------------------------------------------

// 按模型名粗略判断种类；本插件支持 text / video / image / audio（语音合成），其它（向量、转写、音乐…）返回空串表示不导入。
function guessKind(id) {
  var s = id.toLowerCase();
  if (/embed|rerank|moderation|whisper|transcribe|suno|midjourney|audio/.test(s)) {
    return "";
  }
  if (/video|sora|kling|veo|hailuo|seedance|wan[-_.0-9]|runway|pika|luma|cogvideo|vidu|hunyuan-video/.test(s)) {
    return "video";
  }
  if (/tts|speech|voice/.test(s)) {
    return "audio";
  }
  if (/image|dall|flux|imagen|sdxl|stable-diffusion|seedream/.test(s)) {
    return "image";
  }
  return "text";
}

function inputSchemaOf(kind) {
  switch (kind) {
    case "video":
      return videoInputSchema();
    case "image":
      return imageInputSchema();
    case "audio":
      return audioInputSchema();
    default:
      return textInputSchema();
  }
}

function textInputSchema() {
  return {
    prompt: { type: "text", label: "提示词", required: true, port: "text", max_length: 20000 }
  };
}

function imageInputSchema() {
  return {
    prompt: { type: "text", label: "提示词", required: true, port: "text", max_length: 4000 },
    image: { type: "image", label: "参考图（可选）", port: "image" },
    size: {
      type: "enum",
      label: "尺寸",
      options: [
        { value: "1024x1024", label: "1024×1024" },
        { value: "1536x1024", label: "1536×1024（横）" },
        { value: "1024x1536", label: "1024×1536（竖）" }
      ],
      default: "1024x1024"
    }
  };
}

function audioInputSchema() {
  return {
    prompt: { type: "text", label: "朗读文本", required: true, port: "text", max_length: 4096 }
  };
}

function videoInputSchema() {
  return {
    prompt: { type: "text", label: "提示词", required: true, port: "text", max_length: 2000 },
    image: { type: "image", label: "首帧图（可选）", port: "image" },
    duration: {
      type: "enum",
      label: "时长",
      options: [{ value: 5, label: "5 秒" }, { value: 10, label: "10 秒" }],
      default: 5
    }
  };
}

// ---------------------------------------------------------------------------
// 通用小工具
// ---------------------------------------------------------------------------

function failed(message) {
  return { status: "failed", error: { class: classifyMessage(message), message: message } };
}

// 按错误文案判断失败分类：审核 → moderation；额度 → provider_balance；其余 terminal。
function classifyMessage(message) {
  var s = String(message || "").toLowerCase();
  if (/sensitive|nsfw|moderation|content[ _-]?policy|violat|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/insufficient|balance|quota|credit|余额|额度/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

// 从响应里取错误文案：兼容 {error:{message}}、{error:"..."}、{message:"..."}、{fail_reason:"..."}。
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
  if (typeof body.fail_reason === "string" && body.fail_reason) {
    return body.fail_reason;
  }
  if (typeof body.message === "string" && body.message && body.status !== undefined && mapVideoStatus(body.status) === "failed") {
    return body.message;
  }
  return "";
}

function parseProgress(v) {
  if (typeof v === "number") {
    return v;
  }
  if (typeof v === "string") {
    var n = parseFloat(v);
    return isNaN(n) ? undefined : n;
  }
  return undefined;
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

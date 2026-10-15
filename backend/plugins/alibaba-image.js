/**
 * 阿里云百炼（DashScope）图片插件（内置，契约 v1）：万相图片、千问图像、Z-Image。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/alibaba/1.4.2，按 video-canvas 协议插件契约 v1 重写。
 *
 * 渠道 base_url：国内站 https://dashscope.aliyuncs.com ，国际站 https://dashscope-intl.aliyuncs.com 。
 * 鉴权：Authorization: Bearer <百炼 API Key>，由宿主按渠道 Key 注入（渠道 Key 直接填 sk- 开头的百炼 Key）。
 *
 * 真实上游接口（均在 base_url 下）：
 *   图片提交 按模型系列分派（见下），异步的带 X-DashScope-Async: enable，体 { model, input, parameters }；
 *   任务查询 GET /api/v1/tasks/{task_id}，output.task_status 为 PENDING / RUNNING / SUCCEEDED / FAILED / CANCELED / UNKNOWN。
 *
 * 图片模型系列（按 upstreamModel 分派，带日期的快照名按去日期后缀的名字处理）：
 *   image27   wan2.7-image-pro / wan2.7-image          异步 image-generation/generation，input.messages，最多 9 张参考图
 *   image26   wan2.6-image                              异步 image-generation/generation，必须有参考图（文生图用 wan2.6-t2i），最多 4 张
 *   t2i       wan2.6-t2i                                异步 image-generation/generation，只文生图
 *   legacy    wan2.5-t2i-preview wan2.2-t2i-flash wan2.2-t2i-plus wanx2.1-t2i-turbo wanx2.1-t2i-plus wanx2.0-t2i-turbo
 *                                                       异步 text2image/image-synthesis，input.prompt，结果在 output.results[].url
 *   legacy_edit  wan2.5-i2i-preview                     异步 image2image/image-synthesis，input.images（1-3 张）
 *   legacy_imageedit  wanx2.1-imageedit                 异步 image2image/image-synthesis，input.function + base_image_url（本插件只做指令编辑 description_edit）
 *   qwen      qwen-image / -plus / -max / -2.0 / -2.0-pro / -3.0 / -3.0-pro、qwen-image-edit / -plus / -max、z-image-turbo
 *                                                       同步 multimodal-generation/generation，响应直接带 output.choices[].message.content[].image
 *
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt、op（t2i / i2i）、images（文件引用，交给宿主换成签名 URL）、
 *   count（或 n，默认 1）、aspect_ratio、resolution（1K / 2K / 4K）、size（宽*高，优先于前两者）、seed、watermark、prompt_extend、negative_prompt、
 *   thinking_mode（boolean，可选，没填不发；运营在面板的生成参数里加同名参数）
 *
 * 与上游相比刻意没移植：OpenAI Responses / Images 网关协议、usageSchema 计费与重算、SSE 流式（wan2.6-image 图文混排 enable_interleave）、
 *   组图 enable_sequential、wanx2.1-imageedit 除指令编辑外的其它 function、遮罩图、Base64 直传。
 * 未核实：错误码（Arrearage / Throttling* / DataInspectionFailed 等）取自记忆里的百炼文档，没有逐条对照；
 *   连通检查用的 GET /compatible-mode/v1/models 是 OpenAI 兼容端点，没有在上游代码里出现；
 *   千问图像各模型的合法 size 列表没有逐个核对，按宽高比和像素上限推算（宽高取 8 的倍数）。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "alibaba-image",
    name: "阿里云百炼 · 图片",
    version: "1.1.0",
    vendor: "alibaba",
    description: "对接阿里云百炼 DashScope 图片：万相图片、千问图像、Z-Image",
    auth: { type: "bearer" },
    endpoints: {
      // 图片多数是异步任务，千问图像系列是同步接口：同步的在提交响应里直接返回 immediate
      image: { mode: "async", method: "POST", path: "/api/v1/services/aigc/…" },
    },
    poll: { firstDelay: 10, interval: 8, maxInterval: 20, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    return buildImageRequest(ctx);
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    var out = body.output || {};
    if (imageClass(ctx.model.upstreamModel) === "qwen") {
      // 同步接口：响应就是最终结果
      if (body.code) {
        return { immediate: failed(codeMessage(body.code, body.message)) };
      }
      return { immediate: imageResult(out) };
    }
    if (!out.task_id) {
      if (body.code || body.message) {
        return { immediate: failed(codeMessage(body.code, body.message)) };
      }
      throw new Error("上游没有返回 task_id");
    }
    return { providerTaskId: String(out.task_id) };
  },

  buildQueryRequest: function (ctx) {
    return {
      method: "GET",
      path: "/api/v1/tasks/" + encodeURIComponent(ctx.task.providerTaskId),
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    var out = body.output || {};
    var st = out.task_status;
    if (st === "PENDING") {
      return { status: "queued" };
    }
    if (st === "RUNNING") {
      return { status: "running" };
    }
    if (st === "SUCCEEDED") {
      return imageResult(out);
    }
    if (st === "FAILED" || st === "CANCELED" || st === "UNKNOWN") {
      var msg = codeMessage(out.code || body.code, out.message || body.message);
      return failed(msg || "上游任务失败：" + st);
    }
    // 没见过的状态：当作进行中，继续轮询（任务整体有截止时间兜底）
    return { status: "running" };
  },

  classifyError: function (ctx, resp) {
    var body = resp.body && typeof resp.body === "object" ? resp.body : {};
    var code = typeof body.code === "string" ? body.code : "";
    var msg = codeMessage(code, body.message) || (resp.text || "").slice(0, 300);
    if (!msg) {
      return null;
    }
    // 限流类错误码里带 quota 字样（Throttling.RateQuota），必须先于“额度不足”判断
    if (/^Throttling|^LimitRequests/.test(code)) {
      return { class: "retryable", code: code, message: msg };
    }
    var cls = classifyMessage(msg);
    if (cls === "terminal") {
      return null; // 其余交给宿主默认规则（429 / 5xx 重试，其它 terminal）
    }
    return { class: cls, code: code || undefined, message: msg };
  },

  buildCheckRequest: function (ctx) {
    // OpenAI 兼容模式的模型列表，便宜且需要 Key；未在上游代码里核实
    return { method: "GET", path: "/compatible-mode/v1/models", timeout: 15 };
  },
};


// ---------------------------------------------------------------------------
// 图片
// ---------------------------------------------------------------------------

var IMAGE_CLASSES = {
  "wan2.7-image-pro": "image27",
  "wan2.7-image": "image27",
  "wan2.6-image": "image26",
  "wan2.6-t2i": "t2i",
  "wan2.5-t2i-preview": "legacy",
  "wan2.2-t2i-flash": "legacy",
  "wan2.2-t2i-plus": "legacy",
  "wanx2.1-t2i-turbo": "legacy",
  "wanx2.1-t2i-plus": "legacy",
  "wanx2.0-t2i-turbo": "legacy",
  "wan2.5-i2i-preview": "legacy_edit",
  "wanx2.1-imageedit": "legacy_imageedit",
  "qwen-image": "qwen",
  "qwen-image-plus": "qwen",
  "qwen-image-max": "qwen",
  "qwen-image-2.0": "qwen",
  "qwen-image-2.0-pro": "qwen",
  "qwen-image-3.0": "qwen",
  "qwen-image-3.0-pro": "qwen",
  "qwen-image-edit": "qwen",
  "qwen-image-edit-plus": "qwen",
  "qwen-image-edit-max": "qwen",
  "z-image-turbo": "qwen",
};


// 千问系列的出图张数上限与输入图张数（minInput=1 表示只能编辑）
var QWEN_LIMITS = {
  "qwen-image": { maxN: 1, maxInput: 0 },
  "qwen-image-plus": { maxN: 1, maxInput: 0 },
  "qwen-image-max": { maxN: 1, maxInput: 0 },
  "qwen-image-2.0": { maxN: 6, maxInput: 3 },
  "qwen-image-2.0-pro": { maxN: 6, maxInput: 3 },
  "qwen-image-3.0": { maxN: 6, maxInput: 3 },
  "qwen-image-3.0-pro": { maxN: 6, maxInput: 3 },
  "qwen-image-edit": { maxN: 1, maxInput: 3, minInput: 1 },
  "qwen-image-edit-plus": { maxN: 6, maxInput: 3, minInput: 1 },
  "qwen-image-edit-max": { maxN: 6, maxInput: 3, minInput: 1 },
  "z-image-turbo": { maxN: 1, maxInput: 0 },
};

// 清单之外的新模型按名字猜：qwen-image / z-image 开头走同步接口，wan 开头带 image 的按 image27 风格走异步，其余报错
function imageClass(model) {
  var key = modelKey(model);
  if (Object.prototype.hasOwnProperty.call(IMAGE_CLASSES, key)) {
    return IMAGE_CLASSES[key];
  }
  if (/^qwen-image|^z-image/.test(key)) {
    return "qwen";
  }
  if (/^wan\d/.test(key) && /image/.test(key)) {
    return "image27";
  }
  throw new Error("不支持的百炼图片模型：" + model);
}

function buildImageRequest(ctx) {
  var input = ctx.input || {};
  var model = ctx.model.upstreamModel;
  var key = modelKey(model);
  var cls = imageClass(model);
  var prompt = typeof input.prompt === "string" ? input.prompt : "";
  if (!prompt) {
    throw new Error(model + " 需要提示词");
  }
  var images = refList(input.images);
  var par = {};
  var inp = {};

  var limits = QWEN_LIMITS[key] || { maxN: 1, maxInput: 3 };
  var maxN = cls === "qwen" ? limits.maxN : cls === "legacy_imageedit" ? 1 : 4;
  var n = typeof input.count === "number" ? input.count : typeof input.n === "number" ? input.n : 1;
  if (n !== Math.floor(n) || n < 1 || n > maxN) {
    throw new Error(model + " 一次出图张数必须是 1 到 " + maxN + " 的整数");
  }
  par.n = n;
  copyDefined(par, input, ["seed", "watermark", "prompt_extend", "thinking_mode"]);

  var maxInput = 0;
  if (cls === "image27") {
    maxInput = 9;
  } else if (cls === "image26") {
    maxInput = 4;
  } else if (cls === "qwen") {
    maxInput = limits.maxInput;
  } else if (cls === "legacy_edit") {
    maxInput = 3;
  } else if (cls === "legacy_imageedit") {
    maxInput = 1;
  }
  if (images.length > maxInput) {
    throw new Error(maxInput === 0 ? model + " 只支持文生图，不能带参考图" : model + " 最多支持 " + maxInput + " 张参考图");
  }

  if (cls === "legacy") {
    inp.prompt = prompt;
    if (typeof input.negative_prompt === "string" && input.negative_prompt) {
      inp.negative_prompt = input.negative_prompt;
    }
  } else if (cls === "legacy_edit") {
    if (images.length < 1) {
      throw new Error(model + " 需要 1 到 3 张参考图");
    }
    inp.prompt = prompt;
    inp.images = urlRefs(images);
    if (typeof input.negative_prompt === "string" && input.negative_prompt) {
      inp.negative_prompt = input.negative_prompt;
    }
  } else if (cls === "legacy_imageedit") {
    if (images.length !== 1) {
      throw new Error(model + " 需要恰好 1 张参考图");
    }
    inp.function = "description_edit"; // 只支持指令编辑；其它 function 未移植
    inp.prompt = prompt;
    inp.base_image_url = urlRef(images[0]);
  } else {
    if (cls === "image26" && images.length === 0) {
      throw new Error("wan2.6-image 是图像编辑模型，需要参考图；文生图请用 wan2.6-t2i");
    }
    if (cls === "qwen" && limits.minInput && images.length < limits.minInput) {
      throw new Error(model + " 是图像编辑模型，至少需要一张参考图");
    }
    var content = [];
    for (var i = 0; i < images.length; i++) {
      content.push({ image: urlRef(images[i]) });
    }
    content.push({ text: prompt });
    inp.messages = [{ role: "user", content: content }];
    if (typeof input.negative_prompt === "string" && input.negative_prompt) {
      par.negative_prompt = input.negative_prompt;
    }
  }

  if (cls !== "legacy_imageedit") {
    var size = imageSize(cls, key, input, images.length);
    if (size) {
      par.size = size;
    }
  }

  var service;
  if (cls === "legacy") {
    service = "text2image/image-synthesis";
  } else if (cls === "legacy_edit" || cls === "legacy_imageedit") {
    service = "image2image/image-synthesis";
  } else if (cls === "qwen") {
    service = "multimodal-generation/generation";
  } else {
    service = "image-generation/generation";
  }
  var req = {
    method: "POST",
    path: "/api/v1/services/aigc/" + service,
    json: { model: model, input: inp, parameters: par },
    timeout: cls === "qwen" ? 120 : 60, // 千问图像是同步接口，要等出图
  };
  if (cls !== "qwen") {
    req.headers = { "X-DashScope-Async": "enable" };
  }
  return req;
}

// 出图尺寸：优先用户给的 size（宽*高 或 1K/2K/4K 预设），其次由 aspect_ratio + resolution 推算；都没有就不传，用上游默认。
function imageSize(cls, key, input, imageCount) {
  var modern = cls === "image27" || cls === "image26" || cls === "t2i";
  var raw = typeof input.size === "string" ? input.size.replace(/x/i, "*") : "";
  var res = typeof input.resolution === "string" ? input.resolution.toUpperCase() : "";
  var ratio = typeof input.aspect_ratio === "string" ? input.aspect_ratio : "";
  var supports4K = key === "wan2.7-image-pro" && imageCount === 0;
  if (raw) {
    if (/^[1-4]K$/.test(raw.toUpperCase())) {
      return modern ? clampPreset(raw.toUpperCase(), supports4K) : "";
    }
    return raw;
  }
  if (!ratio && !res) {
    return "";
  }
  if (modern && !ratio && /^[1-4]K$/.test(res)) {
    return clampPreset(res, supports4K);
  }
  var edge = res === "4K" ? 4096 : res === "2K" ? 2048 : 1024;
  var maxArea = cls === "image27" ? (supports4K ? 4096 * 4096 : 2048 * 2048) : cls === "legacy_edit" ? 1280 * 1280 : cls === "legacy" ? 1440 * 1440 : 2048 * 2048;
  if (edge * edge > maxArea) {
    edge = Math.floor(Math.sqrt(maxArea));
  }
  return sizeFromRatio(ratio || "1:1", edge, maxArea, cls === "legacy");
}

function clampPreset(preset, supports4K) {
  return preset === "4K" && !supports4K ? "2K" : preset;
}

// 按宽高比和目标边长推算 宽*高：面积约为 edge*edge，宽高取 8 的倍数，面积不超过 maxArea。
function sizeFromRatio(ratio, edge, maxArea, clampSide) {
  var m = /^(\d+):(\d+)$/.exec(ratio);
  if (!m || Number(m[1]) === 0 || Number(m[2]) === 0) {
    throw new Error("画面比例格式不对：" + ratio);
  }
  var rw = Number(m[1]);
  var rh = Number(m[2]);
  var w = Math.floor(Math.sqrt((edge * edge * rw) / rh) / 8) * 8;
  var h = Math.floor(((w * rh) / rw) / 8) * 8;
  if (clampSide) {
    w = Math.min(1440, Math.max(512, w));
    h = Math.min(1440, Math.max(512, h));
  }
  var guard = 0;
  while (w * h > maxArea && guard < 512) {
    w -= 8;
    h = Math.floor(((w * rh) / rw) / 8) * 8;
    guard++;
  }
  return w + "*" + h;
}

// 从 output 里收集图片：旧接口在 results[].url，新接口在 choices[].message.content[].image。
function imageUrlsOf(out) {
  var urls = [];
  var results = isArray(out.results) ? out.results : [];
  for (var i = 0; i < results.length; i++) {
    if (results[i] && typeof results[i].url === "string" && results[i].url) {
      urls.push(results[i].url);
    }
  }
  var choices = isArray(out.choices) ? out.choices : [];
  for (var c = 0; c < choices.length; c++) {
    var msg = choices[c] && choices[c].message;
    var parts = msg && isArray(msg.content) ? msg.content : [];
    for (var p = 0; p < parts.length; p++) {
      if (parts[p] && typeof parts[p].image === "string" && parts[p].image) {
        urls.push(parts[p].image);
      }
    }
  }
  return urls;
}

function imageResult(out) {
  var urls = imageUrlsOf(out || {});
  if (urls.length === 0) {
    var msg = codeMessage(out && out.code, out && out.message);
    return failed(msg || "上游没有返回图片");
  }
  var outputs = [];
  for (var i = 0; i < urls.length; i++) {
    outputs.push({ type: "url", url: urls[i], media_type: "image", mime: mimeOfImage(urls[i]) });
  }
  return { status: "succeeded", progress: 100, outputs: outputs };
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


// 带日期的快照（qwen-image-2.0-pro-2026-03-03）按去日期后缀的名字处理；wan2.1-* 是 wanx2.1-* 的别名。
function modelKey(model) {
  return String(model || "")
    .replace(/-\d{4}-\d{2}-\d{2}$/, "")
    .replace(/^wan2\.1-/, "wanx2.1-");
}

// ---------------------------------------------------------------------------
// 通用小工具
// ---------------------------------------------------------------------------

function failed(message) {
  return { status: "failed", error: { class: classifyMessage(message), message: message } };
}

// 错误码 + 文案拼成一条，分类时两部分都参与匹配。
function codeMessage(code, message) {
  var c = typeof code === "string" ? code : "";
  var m = typeof message === "string" ? message : "";
  if (c && m) {
    return c + "：" + m;
  }
  return c || m;
}

// 按错误文案判断分类：审核 → moderation；欠费 / 额度 → provider_balance；其余 terminal。
function classifyMessage(message) {
  var s = String(message || "").toLowerCase();
  if (/datainspectionfailed|ipinfringement|inappropriate|sensitive|nsfw|moderation|content[ _-]?policy|violat|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/arrearage|insufficient|balance|quota|credit|overdue|欠费|余额|额度/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

function urlRef(ref) {
  return { __fileRef: ref, as: "url" };
}

function urlRefs(list) {
  var out = [];
  for (var i = 0; i < list.length; i++) {
    out.push(urlRef(list[i]));
  }
  return out;
}

function refList(v) {
  return isArray(v) ? v : [];
}

// 把 src 里已定义的这些键拷到 dst。
function copyDefined(dst, src, keys) {
  for (var i = 0; i < keys.length; i++) {
    if (src[keys[i]] !== undefined && src[keys[i]] !== null) {
      dst[keys[i]] = src[keys[i]];
    }
  }
}

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

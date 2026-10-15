/**
 * 火山引擎豆包 · 图片（方舟 Ark Seedream）插件（内置，契约 v1）。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/doubao/1.3.0，按 video-canvas 协议插件契约 v1 重写。
 *
 * 直连方舟的公开 HTTP 接口（渠道 base_url 填 https://ark.cn-beijing.volces.com，路径带 /api/v3）：
 *   POST /api/v3/images/generations，同步出图（data[].url；组图时 data 里可有多张，失败的成员只带 error）。
 * 鉴权：Authorization: Bearer <方舟 API Key>，由宿主按渠道 Key 注入。渠道 Key 直接填方舟控制台创建的 API Key。
 * 上游的网关协议适配（openai_image）、usageSchema 计费、提交前的本地参数校验（像素范围、参考图上限）一律没有移植：
 * 参数是否合法交给方舟返回 400，分辨率等取值范围由运营在模型能力里配置。
 *
 * 上游 meta.models（模型名即方舟的模型 ID 或接入点 ID，upstreamModel 照填）：
 *   图片 Seedream：doubao-seedream-5-0-pro-260628、doubao-seedream-5-0-flash-260915、doubao-seedream-5-0-lite-260128、
 *     doubao-seedream-5-0-260128、doubao-seedream-4-5-251128、doubao-seedream-4-0-250828
 *
 * 各系列差异（本插件不做本地校验，只在这里记录，配置模型能力时参考）：
 *   - 图片 size 档位：5.0 pro / flash 为 1K、1.5K、2K，5.0 lite / 5.0 / 4.5 起步 2K，4.0 支持 1K、2K、4K；
 *     组图（sequential_image_generation）、联网（tools）、output_format、图层拆分（layer_decomposition）只有部分型号支持。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 *
 * 模型 params（运营在模型上填的固定参数）：
 *   { watermark?: boolean, extra?: object }
 *   extra 里放型号特有的字段，如 output_format、sequential_image_generation、sequential_image_generation_options、
 *   tools、optimize_prompt_options、layer_decomposition、guidance_scale；用户输入与固定字段优先
 * 用户输入（宿主按模型能力校验后传入）：
 *   prompt、op、size（如 2K 或 2048x2048）、resolution（没有 size 时当 size 用）、aspect_ratio（与 resolution 档位一起换算成 WxH；
 *   方舟没有单独的比例字段）、seed；images 作参考图（文件引用换成签名 URL，1 张传字符串，多张传数组）。n 被忽略：方舟没有单次张数，多张靠组图
 *
 * 未验证（上游代码里没有，凭方舟文档印象写的）：连通检查借用任务列表接口 GET /api/v3/contents/generations/tasks?page_size=1
 * 只验证 Key 是否有效；错误码关键字（余额 / 审核 / 限流）；aspect_ratio 换算 WxH 的取整规则。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "doubao-image",
    name: "豆包（火山方舟） · 图片",
    version: "1.1.0",
    vendor: "volcengine",
    description: "火山引擎豆包 Seedream 图片生成（同步）",
    auth: { type: "bearer" },
    endpoints: {
      image: { mode: "sync", method: "POST", path: "/api/v3/images/generations" },
    },
  },

  buildSubmitRequest: function (ctx) {
    return buildImageRequest(ctx);
  },

  parseSubmitResponse: function (ctx, resp) {
    return { immediate: parseImageResponse(resp) };
  },

  classifyError: function (ctx, resp) {
    var msg = errorMessageOf(resp.body) || (resp.text || "").slice(0, 300);
    var code = errorCodeOf(resp.body);
    if (!msg && !code) {
      return null;
    }
    var cls = classifyText(code + " " + msg);
    if (cls === "terminal") {
      return null; // 其余交给宿主默认规则（429 / 5xx 重试，其它 terminal）
    }
    return { class: cls, code: code || undefined, message: msg || code };
  },

  buildCheckRequest: function (ctx) {
    return { method: "GET", path: "/api/v3/contents/generations/tasks", query: { page_num: 1, page_size: 1 }, timeout: 15 };
  },
};

// ---------------------------------------------------------------------------
// 图片（Seedream）
// ---------------------------------------------------------------------------

// 各档位的像素面积（方形），用来把 aspect_ratio + 档位换算成 WxH。
var PRESET_PIXELS = { "1K": 1048576, "1.5K": 2359296, "2K": 4194304, "3K": 9437184, "4K": 16777216 };

function buildImageRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};
  var body = { model: ctx.model.upstreamModel, response_format: "url" };
  if (input.prompt !== undefined && String(input.prompt) !== "") {
    body.prompt = String(input.prompt);
  }
  var images = isArray(input.images) ? input.images : [];
  if (images.length === 1) {
    body.image = { __fileRef: images[0], as: "url" };
  } else if (images.length > 1) {
    body.image = [];
    for (var i = 0; i < images.length; i++) {
      body.image.push({ __fileRef: images[i], as: "url" });
    }
  }
  var size = imageSize(input);
  if (size) {
    body.size = size;
  }
  if (typeof input.seed === "number") {
    body.seed = input.seed;
  }
  body.watermark = typeof params.watermark === "boolean" ? params.watermark : false;
  mergeExtra(body, params.extra);
  return { method: "POST", path: "/api/v3/images/generations", json: body, timeout: 180 };
}

// size 优先；否则取 resolution 档位，有 aspect_ratio 时换算成 WxH（面积不超过档位，宽高取 8 的倍数）。
function imageSize(input) {
  var size = firstString(input.size);
  if (size) {
    return size;
  }
  var preset = firstString(input.resolution).toUpperCase();
  if (!preset) {
    return "";
  }
  var ratio = parseRatio(input.aspect_ratio);
  if (!ratio || !PRESET_PIXELS[preset]) {
    return preset;
  }
  var w = Math.floor(Math.sqrt(PRESET_PIXELS[preset] * ratio) / 8) * 8;
  var h = Math.floor(w / ratio / 8) * 8;
  while (w * h > PRESET_PIXELS[preset] && h > 8) {
    h -= 8;
  }
  return w + "x" + h;
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

function parseImageResponse(resp) {
  var body = resp.body || {};
  var list = isArray(body.data) ? body.data : [];
  var outputs = [];
  var itemError = "";
  var itemCode = "";
  for (var i = 0; i < list.length; i++) {
    var item = list[i];
    if (item && typeof item.url === "string" && item.url !== "") {
      outputs.push({ type: "url", url: item.url, media_type: "image", mime: mimeOfImage(item) });
    } else if (item && item.error && !itemError) {
      itemError = errorMessageOf(item);
      itemCode = errorCodeOf(item);
    }
  }
  if (outputs.length > 0) {
    return { status: "succeeded", outputs: outputs }; // 组图里个别失败的成员忽略
  }
  var msg = errorMessageOf(body) || itemError;
  if (msg) {
    return failed(msg, errorCodeOf(body) || itemCode);
  }
  return failed("上游没有返回图片");
}

function mimeOfImage(item) {
  if (item.output_format === "png") {
    return "image/png";
  }
  if (item.output_format === "jpeg") {
    return "image/jpeg";
  }
  var m = /\.([a-z0-9]{2,5})(?:\?|#|$)/i.exec(item.url);
  var ext = m ? m[1].toLowerCase() : "";
  if (ext === "png") {
    return "image/png";
  }
  if (ext === "webp") {
    return "image/webp";
  }
  return "image/jpeg";
}

// ---------------------------------------------------------------------------
// 通用小工具
// ---------------------------------------------------------------------------

function failed(message, code) {
  var err = { class: classifyText((code || "") + " " + message), message: message };
  if (code) {
    err.code = code;
  }
  return { status: "failed", error: err };
}

// 按错误码 + 文案判断失败分类：审核 → moderation；余额 / 额度 / 欠费 → provider_balance；限流 / 过载 → retryable；其余 terminal。
function classifyText(text) {
  var s = String(text || "").toLowerCase();
  if (/sensitive|moderation|contentsecurity|content[ _-]?policy|violat|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/overdue|insufficient|balance|quota|setlimitexceeded|credit|欠费|余额|额度/.test(s)) {
    return "provider_balance";
  }
  if (/ratelimit|rate limit|overloaded|toomanyrequests|限流|过载/.test(s)) {
    return "retryable";
  }
  return "terminal";
}

// 方舟的错误体：{error:{code,message}}（任务查询里 failed 任务同样带 error）。
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
  if (!body || typeof body !== "object") {
    return "";
  }
  var e = body.error;
  return e && typeof e === "object" && typeof e.code === "string" ? e.code : "";
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

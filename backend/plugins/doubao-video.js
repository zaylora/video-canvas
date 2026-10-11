/**
 * 火山引擎豆包 · 视频（方舟 Ark Seedance）插件（内置，契约 v1）。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/doubao/1.3.0，按 video-canvas 协议插件契约 v1 重写。
 *
 * 直连方舟的公开 HTTP 接口（渠道 base_url 填 https://ark.cn-beijing.volces.com，路径带 /api/v3）：
 *   POST /api/v3/contents/generations/tasks 提交（返回 {id}），GET /api/v3/contents/generations/tasks/{id} 轮询，异步出片；
 *   状态 queued / running / succeeded / failed / expired / cancelled，成功后 content.video_url 是成片地址。
 * 鉴权：Authorization: Bearer <方舟 API Key>，由宿主按渠道 Key 注入。渠道 Key 直接填方舟控制台创建的 API Key。
 * 上游的网关协议适配（openai_responses / openai_video）、usageSchema 计费、草稿任务改写、
 * 提交前的本地参数校验（分辨率档位、参考图上限）一律没有移植：参数是否合法交给方舟返回 400，
 * 分辨率 / 时长等取值范围由运营在模型能力里配置。
 *
 * 上游 meta.models（模型名即方舟的模型 ID 或接入点 ID，upstreamModel 照填）：
 *   视频 Seedance：doubao-seedance-1-0-pro-250528、doubao-seedance-1-0-lite-t2v、doubao-seedance-1-0-lite-i2v、
 *     doubao-seedance-1-5-pro-251215（有声）、doubao-seedance-2-0-260128、doubao-seedance-2-0-fast-260128、
 *     doubao-seedance-2-0-mini-260615、doubao-seedance-2-5-260628（上游标注 2.x 支持参考视频，最长 15 秒，2.5 最长 30 秒）
 *
 * 各系列差异（本插件不做本地校验，只在这里记录，配置模型能力时参考）：
 *   - 视频分辨率：1.0 / 1.5 为 480p-1080p，2.0 为 480p-4k，2.0 fast / mini 只有 480p-720p，2.5 为 480p-1080p；
 *     duration 传 -1 表示让模型自己定时长（1.5 pro 与 2.x）；generate_audio 只有 1.5 pro 与 2.x 有意义，默认有声。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 *
 * 模型 params（运营在模型上填的固定参数）：
 *   { watermark?: boolean, camera_fixed?: boolean, return_last_frame?: boolean, service_tier?: string, extra?: object }
 *   watermark 不填按 false（方舟默认加水印）；extra 原样合并进请求体顶层，用户输入与上面的固定字段优先
 * 用户输入（宿主按模型能力校验后传入）：
 *   prompt、op、images / videos / audios（参考素材，文件引用换成签名 URL）、
 *   aspect_ratio -> ratio（如 16:9、adaptive）、resolution、duration、seed、generate_audio
 *   素材：op=omni 时全部图片作 reference_image；否则 1 张图是首帧（不写 role），2 张是首帧 + 尾帧，多于 2 张按参考图；
 *   videos -> reference_video，audios -> reference_audio
 *
 * 未验证（上游代码里没有，凭方舟文档印象写的）：连通检查用的任务列表接口 GET /api/v3/contents/generations/tasks?page_size=1；
 * 错误码关键字（余额 / 审核 / 限流）；首帧 / 尾帧 / 参考素材的 role 取值。
 * 没有移植：取消任务（方舟只能取消排队中的任务，行为未核实，走软取消）、列模型导入（方舟模型清单是控制面接口，需要 AK/SK 签名）。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "doubao-video",
    name: "豆包（火山方舟） · 视频",
    version: "1.0.0",
    description: "火山引擎豆包 Seedance 视频生成（异步）",
    auth: { type: "bearer" },
    endpoints: {
      video: { mode: "async" },
    },
    poll: { firstDelay: 15, interval: 10, maxInterval: 20, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    return buildVideoRequest(ctx);
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (body.id === undefined || body.id === null || body.id === "") {
      var msg = errorMessageOf(body);
      if (msg) {
        return { immediate: failed(msg, errorCodeOf(body)) };
      }
      throw new Error("上游没有返回任务 id");
    }
    return { providerTaskId: String(body.id) };
  },

  buildQueryRequest: function (ctx) {
    return {
      method: "GET",
      path: TASKS_PATH + "/" + encodeURIComponent(ctx.task.providerTaskId),
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    switch (String(body.status || "").toLowerCase()) {
      case "succeeded":
        var content = body.content || {};
        if (typeof content.video_url !== "string" || content.video_url === "") {
          return failed("上游返回成功，但没有视频地址");
        }
        return {
          status: "succeeded",
          progress: 100,
          outputs: [{ type: "url", url: content.video_url, media_type: "video", mime: "video/mp4" }],
        };
      case "failed":
        return failed(errorMessageOf(body) || "上游任务失败", errorCodeOf(body));
      case "expired":
        return failed(errorMessageOf(body) || "上游任务超时未完成（expired）", errorCodeOf(body) || "expired");
      case "cancelled":
        return failed(errorMessageOf(body) || "上游任务已被取消", errorCodeOf(body) || "cancelled");
      case "running":
      case "processing":
        return { status: "running" };
      default:
        // queued / pending / 未知：继续等待，由任务 deadline 兜底
        return { status: "queued" };
    }
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
    return { method: "GET", path: TASKS_PATH, query: { page_num: 1, page_size: 1 }, timeout: 15 };
  },
};


var TASKS_PATH = "/api/v3/contents/generations/tasks";

// ---------------------------------------------------------------------------
// 视频（Seedance）
// ---------------------------------------------------------------------------

function buildVideoRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};
  var body = { model: ctx.model.upstreamModel, content: videoContent(input) };

  setIf(body, "ratio", input.aspect_ratio);
  setIf(body, "resolution", input.resolution);
  if (typeof input.duration === "number") {
    body.duration = input.duration; // -1 表示让模型自己定时长
  }
  if (typeof input.seed === "number") {
    body.seed = input.seed;
  }
  if (typeof input.generate_audio === "boolean") {
    body.generate_audio = input.generate_audio;
  }
  body.watermark = typeof params.watermark === "boolean" ? params.watermark : false;
  if (typeof params.camera_fixed === "boolean") {
    body.camera_fixed = params.camera_fixed;
  }
  if (typeof params.return_last_frame === "boolean") {
    body.return_last_frame = params.return_last_frame;
  }
  setIf(body, "service_tier", params.service_tier);
  mergeExtra(body, params.extra);
  return { method: "POST", path: TASKS_PATH, json: body, timeout: 60 };
}

// content 数组：文本在前，随后是图片 / 视频 / 音频参考。素材都用签名 URL 交给方舟拉取。
function videoContent(input) {
  var content = [];
  if (input.prompt !== undefined && String(input.prompt) !== "") {
    content.push({ type: "text", text: String(input.prompt) });
  }
  var images = isArray(input.images) ? input.images : [];
  for (var i = 0; i < images.length; i++) {
    var item = { type: "image_url", image_url: { url: { __fileRef: images[i], as: "url" } } };
    var role = imageRole(input.op, images.length, i);
    if (role) {
      item.role = role;
    }
    content.push(item);
  }
  var videos = isArray(input.videos) ? input.videos : [];
  for (var v = 0; v < videos.length; v++) {
    content.push({
      type: "video_url",
      video_url: { url: { __fileRef: videos[v], as: "url" } },
      role: "reference_video",
    });
  }
  var audios = isArray(input.audios) ? input.audios : [];
  for (var a = 0; a < audios.length; a++) {
    content.push({
      type: "audio_url",
      audio_url: { url: { __fileRef: audios[a], as: "url" } },
      role: "reference_audio",
    });
  }
  return content;
}

// 图片在视频里的角色：全能参考与多于 2 张都是参考图；2 张是首帧 + 尾帧；1 张是首帧（方舟默认，不写 role）。
function imageRole(op, total, index) {
  if (op === "omni" || total > 2) {
    return "reference_image";
  }
  if (total === 2) {
    return index === 0 ? "first_frame" : "last_frame";
  }
  return "";
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

function setIf(body, key, value) {
  if (value !== undefined && value !== null && value !== "") {
    body[key] = value;
  }
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

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

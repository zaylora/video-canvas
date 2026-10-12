/**
 * xAI Grok Imagine 视频插件（内置，契约 v1）。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/xai/1.1.0，按 video-canvas 协议插件契约 v1 重写。
 *
 * 对接 xAI 视频接口（渠道 base_url 填 https://api.x.ai）：
 *   - 生成：POST /v1/videos/generations（文生、图生、参考生）；
 *   - 编辑：POST /v1/videos/edits（对输入视频按提示词改写）；
 *   - 延长：POST /v1/videos/extensions（给输入视频续写 2-10 秒）；
 *   - 轮询：GET /v1/videos/{request_id}，状态 pending / done / failed / expired（xAI 渲染中可能回 202）。
 *   提交都返回 {request_id}；完成后 video.url 是可直接下载的地址。
 * 鉴权：bearer，渠道 Key 直接填 xAI API Key（xai-...），宿主注入 Authorization: Bearer。
 *
 * 上游模型（meta.models）：
 *   grok-imagine-video       分辨率 480p / 720p；唯一能接收视频输入的模型（编辑、延长只能用它）
 *   grok-imagine-video-1.5、grok-imagine-video-1.5-lite   分辨率 480p / 720p / 1080p
 *
 * 模型 params：{ video_action?: "edit" | "extend" }  有视频参考时的默认动作（默认 edit），用户输入的同名参数优先。
 * 用户输入（宿主按模型 capabilities 校验后传入）：
 *   prompt（编辑 / 延长 / 文生 / 参考生必填；纯图生可空）、op（t2v / i2v / omni）；
 *   duration（生成 1-15 秒，默认 8；延长 2-10 秒，默认 6；编辑不发）；
 *   resolution（默认 480p，大小写不敏感）、aspect_ratio（1:1 / 16:9 / 9:16 / 4:3 / 3:4 / 3:2 / 2:3）、generate_audio（布尔）；
 *   images：op 为 omni 或多于一张 -> reference_images（最多 7，不支持 1080p）；op 为 i2v 且只有一张 -> image；
 *   videos[0] -> 编辑 / 延长的输入视频 video；audios -> reference_audios（{url}，最多 3，且与参考图一起触发参考生视频）；
 *   video_action（edit / extend）：有 videos 时选编辑还是延长，运营需要把它配成一个开放的 enum 参数。
 *   编辑与延长沿用输入视频的分辨率和画幅，所以不发 resolution / aspect_ratio。
 *
 * 和上游相比刻意没移植：
 *   - 按秒 / 输入图片 / 输入视频秒数的计费事实，以及按输出 MP4 头读分辨率（utils.fetch 探测，本项目没有）；
 *   - keyframes、last_frame、reference_audios 的 voice_id、user、output.upload_url 这些高级字段（本项目任务输入表达不了）；
 *   - OpenAI Responses 与 openai_video 网关协议适配、new_api 上游的 /xai 前缀路由。
 * 未验证：GET /v1/models 作连通检查（上游代码没有，xAI 官方文档列有该接口）；
 *        延长 duration 的合法范围与默认值沿用上游常量，没有对照官方文档。
 */
var VIDEO_MODELS = {
  "grok-imagine-video": { resolutions: ["480p", "720p"], videoInput: true },
  "grok-imagine-video-1.5": { resolutions: ["480p", "720p", "1080p"], videoInput: false },
  "grok-imagine-video-1.5-lite": { resolutions: ["480p", "720p", "1080p"], videoInput: false },
};
var ASPECT_RATIOS = ["1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"];
var DEFAULT_RESOLUTION = "480p";
var DEFAULT_DURATION = 8;
var MAX_DURATION = 15;
var DEFAULT_EXTENSION_SECONDS = 6;
var MIN_EXTENSION_SECONDS = 2;
var MAX_EXTENSION_SECONDS = 10;
var MAX_REFERENCE_IMAGES = 7;
var MAX_REFERENCE_AUDIOS = 3;

module.exports = {
  meta: {
    apiVersion: 1,
    key: "xai",
    name: "xAI",
    version: "1.0.0",
    description: "xAI Grok Imagine 视频：文生、图生、参考生视频，视频编辑与延长（异步）",
    auth: { type: "bearer" },
    endpoints: {
      video: { mode: "async" },
    },
    poll: { firstDelay: 10, interval: 6, maxInterval: 20, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var params = ctx.model.params || {};
    var model = ctx.model.upstreamModel;
    var videos = isArray(input.videos) ? input.videos : [];

    if (videos.length > 0) {
      var action = firstString(input.video_action, params.video_action) === "extend" ? "extend" : "edit";
      return buildVideoInputRequest(action, input, model, videos[0]);
    }
    return buildGenerationRequest(input, model);
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    var id = typeof body.request_id === "string" ? body.request_id.trim() : "";
    if (!id) {
      throw new Error("xAI 没有返回 request_id");
    }
    return { providerTaskId: id };
  },

  buildQueryRequest: function (ctx) {
    return {
      method: "GET",
      path: "/v1/videos/" + encodeURIComponent(ctx.task.providerTaskId),
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var data = resp.body && typeof resp.body === "object" ? resp.body : null;
    // 渲染中 xAI 可能回 202 且没有正文
    if (!data) {
      return { status: "running" };
    }
    var status = data.status;
    if (status === "done") {
      var video = data.video && typeof data.video === "object" ? data.video : {};
      if (video.respect_moderation === false) {
        return moderated("生成的视频被内容审核拦截");
      }
      var url = typeof video.url === "string" ? video.url.trim() : "";
      if (!url) {
        return failedResult("terminal", "xAI 返回完成，但没有视频地址");
      }
      return {
        status: "succeeded",
        progress: 100,
        outputs: [{ type: "url", url: url, media_type: "video", mime: "video/mp4" }],
      };
    }
    if (status === "failed") {
      var msg = errorText(data.error) || "视频生成失败";
      // xAI 对被审核拦截的请求照样计费：失败结果带了费用，按审核拦截处理
      if (chargedFailure(data)) {
        return moderated(msg);
      }
      return failedResult(classifyMessage(msg), msg);
    }
    if (status === "expired") {
      return failedResult("terminal", "视频在取回前已过期");
    }
    // pending 与未知状态：继续等待，由任务 deadline 兜底
    var res = { status: "running" };
    var p = Number(data.progress);
    if (p > 0 && p < 100) {
      res.progress = p;
    }
    return res;
  },

  classifyError: function (ctx, resp) {
    var body = resp.body;
    var msg = errorText(body && body.error) || (body && typeof body.message === "string" ? body.message : "") || (resp.text || "").slice(0, 300);
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
};

// 文生 / 图生 / 参考生：POST /v1/videos/generations。时长与分辨率总是发，避免上游默认值变动。
function buildGenerationRequest(input, model) {
  var spec = VIDEO_MODELS[model];
  if (!spec) {
    throw new Error(model + " 不是受支持的 xAI 视频模型");
  }
  var images = isArray(input.images) ? input.images : [];
  var audios = isArray(input.audios) ? input.audios : [];
  var reference = audios.length > 0 || images.length > 1 || (images.length > 0 && input.op === "omni");
  var imageMode = !reference && images.length === 1;

  var prompt = typeof input.prompt === "string" ? input.prompt.trim() : "";
  if (!prompt && !imageMode) {
    throw new Error("prompt 不能为空");
  }
  var resolution = input.resolution ? String(input.resolution).toLowerCase() : DEFAULT_RESOLUTION;
  if (spec.resolutions.indexOf(resolution) < 0) {
    throw new Error(model + " 的 resolution 必须是 " + spec.resolutions.join(" / ") + " 之一");
  }
  if (reference && resolution === "1080p") {
    throw new Error("参考生视频只支持 480p 或 720p");
  }
  var body = {
    model: model,
    duration: intField(input.duration, DEFAULT_DURATION, 1, MAX_DURATION, "duration"),
    resolution: resolution,
  };
  if (prompt) {
    body.prompt = prompt;
  }
  if (input.aspect_ratio !== undefined && input.aspect_ratio !== null && input.aspect_ratio !== "") {
    if (ASPECT_RATIOS.indexOf(input.aspect_ratio) < 0) {
      throw new Error("aspect_ratio 必须是 " + ASPECT_RATIOS.join(" / ") + " 之一");
    }
    body.aspect_ratio = input.aspect_ratio;
  }
  if (typeof input.generate_audio === "boolean") {
    body.generate_audio = input.generate_audio;
  }
  if (imageMode) {
    body.image = urlRef(images[0]);
  } else if (images.length > 0) {
    if (images.length > MAX_REFERENCE_IMAGES) {
      throw new Error("参考图最多 " + MAX_REFERENCE_IMAGES + " 张");
    }
    body.reference_images = images.map(urlRef);
  }
  if (audios.length > 0) {
    if (audios.length > MAX_REFERENCE_AUDIOS) {
      throw new Error("参考音频最多 " + MAX_REFERENCE_AUDIOS + " 个");
    }
    body.reference_audios = audios.map(urlRef);
  }
  return { method: "POST", path: "/v1/videos/generations", json: body, timeout: 60 };
}

// 编辑 / 延长：只有 grok-imagine-video 接收视频输入；输出沿用输入视频的分辨率与画幅，所以不发这两项。
function buildVideoInputRequest(action, input, model, videoRef) {
  var spec = VIDEO_MODELS[model];
  if (!spec || !spec.videoInput) {
    throw new Error("视频编辑与延长只能用 grok-imagine-video");
  }
  var prompt = typeof input.prompt === "string" ? input.prompt.trim() : "";
  if (!prompt) {
    throw new Error("prompt 不能为空");
  }
  var body = { model: model, prompt: prompt, video: urlRef(videoRef) };
  var path = "/v1/videos/edits";
  if (action === "extend") {
    path = "/v1/videos/extensions";
    body.duration = intField(input.duration, DEFAULT_EXTENSION_SECONDS, MIN_EXTENSION_SECONDS, MAX_EXTENSION_SECONDS, "duration");
  }
  return { method: "POST", path: path, json: body, timeout: 60 };
}

// xAI 的素材字段都是 {url}；url 由宿主把文件引用换成自有存储的签名地址。
function urlRef(ref) {
  return { url: { __fileRef: ref, as: "url" } };
}

// 取整数，没填用默认值；超出范围或不是整数直接报错（由宿主记成插件级失败）。
function intField(value, dflt, min, max, name) {
  if (value === undefined || value === null || value === "") {
    return dflt;
  }
  var n = typeof value === "string" ? Number(value) : value;
  if (typeof n !== "number" || n !== Math.floor(n) || n < min || n > max) {
    throw new Error(name + " 必须是 " + min + " 到 " + max + " 之间的整数");
  }
  return n;
}

// xAI 对每个视频结果都带 usage.cost_in_usd_ticks；失败结果仍有费用说明被审核拦截（上游同样的判断）。
function chargedFailure(data) {
  var usage = data && data.usage && typeof data.usage === "object" ? data.usage : null;
  return Boolean(usage && Number(usage.cost_in_usd_ticks) > 0);
}

function moderated(message) {
  return failedResult("moderation", message);
}

function failedResult(cls, message) {
  return { status: "failed", error: { class: cls, message: message } };
}

function errorText(e) {
  if (e && typeof e === "object" && typeof e.message === "string" && e.message.trim()) {
    return e.message.trim();
  }
  if (typeof e === "string" && e.trim()) {
    return e.trim();
  }
  return "";
}

// 按错误文案判断分类：审核 → moderation；额度 → provider_balance；其余 terminal。
function classifyMessage(message) {
  var s = String(message || "").toLowerCase();
  if (/moderation|content[ _-]?policy|usage guidelines|violat|safety|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/insufficient|balance|quota|credit|spending limit|余额|额度/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
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

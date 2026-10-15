/**
 * MiniMax 海螺视频插件（内置，契约 v1）。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/hailuo/1.2.1，按 video-canvas 协议插件契约 v1 重写。
 *
 * 对接 MiniMax 开放平台的视频生成接口，只出 video，异步：
 *   - 老系列（v1）：POST /v1/video_generation 提交，GET /v1/query/video_generation?task_id= 轮询；
 *       轮询到 Success 后只有 file_id，还要 GET /v1/files/retrieve?file_id= 换下载地址。
 *       契约里一次轮询只能发一个请求，所以插件用 state 分两步：第一步查状态，Success 时把 file_id 记进 state 并继续返回 running，
 *       下一次轮询改发 retrieve 请求取地址，取到才算 succeeded。
 *   - MiniMax-H3（v2）：POST /v2/video_generation 提交（多模态 content 数组），GET /v2/query/video_generation/{task_id} 轮询，
 *       响应 {task:{status, content:{url}, error:{message}}}，直接带公网地址。
 * 渠道 base_url 填 MiniMax 的接口域名（如 https://api.minimaxi.com，国际站是 https://api.minimax.io）。
 *
 * 模型清单（上游 meta.models）：
 *   MiniMax-H3                                            （v2；4–15 秒，768P / 2K，文/图/参考图/参考视频/参考音频）
 *   MiniMax-Hailuo-2.3、MiniMax-Hailuo-2.3-Fast、MiniMax-Hailuo-02   （v1；6 / 10 秒，768P / 1080P，02 另有 512P；2.3-Fast 只支持图生视频）
 *   T2V-01-Director、T2V-01、I2V-01-Director、I2V-01-live、I2V-01、S2V-01 （v1；固定 6 秒，720P；S2V-01 是主体参考）
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 * 鉴权：Bearer（渠道 Key 就是 MiniMax 的 API Key，原样填写），由宿主注入。
 *
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt、op（t2v / i2v / omni）、duration（秒）、resolution（512P / 768P / 1080P / 2K，大小写不敏感）、aspect_ratio（只对 H3 生效）、
 *   images（参考图，每项是文件引用 "input:images.0"）、videos / audios（只对 H3 生效）、
 *   prompt_optimizer / fast_pretreatment / aigc_watermark（boolean，可选，没填不发；运营在面板的生成参数里加同名参数）
 *   v1 老系列：images[0] -> first_frame_image，images[1] -> last_frame_image（URL）；S2V-01 的 images 作为 subject_reference 人物参考
 *   H3：op 为 omni，或带了 videos / audios 时，图片按 reference_image、视频按 reference_video、音频按 reference_audio 传；
 *       否则图片按首帧 / 尾帧（最多两张）传。ratio 取自 aspect_ratio（adaptive / 21:9 / 16:9 / 4:3 / 1:1 / 3:4 / 9:16），没填时有图或视频用 adaptive，否则 16:9
 *
 * 错误码（MiniMax base_resp.status_code，以下含义凭上游文档记忆，未逐条验证）：
 *   1002 / 1039 限流，1000 / 1001 / 1013 / 1021 未知 / 超时 / 内部错误 -> retryable；
 *   1008 余额不足 -> provider_balance；1026 / 1027 输入 / 输出敏感 -> moderation；其余按文案判断，兜底 terminal。
 *
 * 和上游相比刻意没移植：OpenAI Responses / openai_video 网关协议、usageSchema 计费、extractUsage、下载代理（listArtifacts / buildContentRequest）；
 *   上游的 metadata.content 整体透传（H3）不做，改由 images / videos / audios + op 生成 content。
 * 未验证：老系列 first_frame_image / last_frame_image 用签名 URL 是否被 MiniMax 接受（上游允许 URL 或 base64）；
 *   S2V-01 的 subject_reference 结构 [{type:"character", image:[url]}]（上游只是透传，这里是按 MiniMax 文档记忆拼的）；
 *   各档位分辨率 / 时长组合不在插件里校验，交给上游返回错误；没有写连通检查（没确认过便宜的只读接口）与模型导入（无列模型接口）。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "hailuo",
    name: "MiniMax 海螺",
    version: "1.1.0",
    vendor: "minimax",
    description: "对接 MiniMax 海螺视频：文生视频、图生视频、主体参考，以及 MiniMax-H3 多模态参考生视频（异步）",
    auth: { type: "bearer" },
    endpoints: {
      video: { mode: "async", method: "POST", path: "/v1/video_generation" },
    },
    poll: { firstDelay: 20, interval: 10, maxInterval: 20, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var model = ctx.model.upstreamModel;
    var body = isH3(model) ? buildH3Body(ctx) : buildV1Body(ctx);
    return {
      method: "POST",
      path: isH3(model) ? "/v2/video_generation" : "/v1/video_generation",
      json: body,
      timeout: 60,
    };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    var err = apiErrorOf(body);
    if (err) {
      return { immediate: failedWith(err.code, err.message) };
    }
    var id = body.task_id;
    if (id === undefined || id === null || id === "") {
      throw new Error("上游没有返回 task_id");
    }
    // 记下走的是 v1 还是 v2：轮询请求和响应的结构都不同
    return { providerTaskId: String(id), state: { v2: isH3(ctx.model.upstreamModel) } };
  },

  buildQueryRequest: function (ctx) {
    var state = ctx.task.state || {};
    var id = encodeURIComponent(ctx.task.providerTaskId);
    if (state.fileId) {
      return {
        method: "GET",
        path: "/v1/files/retrieve",
        query: { file_id: state.fileId },
        timeout: 30,
      };
    }
    if (useV2(ctx)) {
      return { method: "GET", path: "/v2/query/video_generation/" + id, timeout: 30 };
    }
    return {
      method: "GET",
      path: "/v1/query/video_generation",
      query: { task_id: ctx.task.providerTaskId },
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    var state = ctx.task.state || {};
    var err = apiErrorOf(body);

    // 第二步：用 file_id 换下载地址
    if (state.fileId) {
      var file = body.file || {};
      if (typeof file.download_url === "string" && file.download_url !== "") {
        return succeeded(file.download_url);
      }
      if (err) {
        return failedOrWait(err);
      }
      return failedWith(0, "上游没有返回视频下载地址");
    }

    if (useV2(ctx)) {
      return parseV2Query(body, err);
    }
    return parseV1Query(body, err);
  },

  classifyError: function (ctx, resp) {
    var body = resp.body || {};
    var err = apiErrorOf(body);
    var msg = err ? err.message : (resp.text || "").slice(0, 300);
    if (!msg) {
      return null;
    }
    var cls = classifyCode(err ? err.code : 0, msg);
    if (cls === "terminal") {
      return null; // 其余交给宿主默认规则（429 / 5xx 重试，其它 terminal）
    }
    return { class: cls, code: err && err.code ? String(err.code) : undefined, message: msg };
  },
};

// ---------------------------------------------------------------------------
// 提交：老系列（v1）
// ---------------------------------------------------------------------------

function isH3(model) {
  return model === "MiniMax-H3";
}

function isModern(model) {
  return model === "MiniMax-Hailuo-2.3" || model === "MiniMax-Hailuo-2.3-Fast" || model === "MiniMax-Hailuo-02";
}

function buildV1Body(ctx) {
  var input = ctx.input || {};
  var model = ctx.model.upstreamModel;
  var body = { model: model };
  if (input.prompt !== undefined && input.prompt !== "") {
    body.prompt = String(input.prompt);
  }
  var duration = Number(input.duration);
  body.duration = duration > 0 ? duration : 6;
  body.resolution = normalizeResolution(input.resolution, model);

  var images = isArray(input.images) ? input.images : [];
  if (model === "S2V-01") {
    // 主体参考：用图片里的人物
    if (images.length > 0) {
      body.subject_reference = [{ type: "character", image: refList(images) }];
    }
  } else {
    if (images.length > 0) {
      body.first_frame_image = ref(images[0]);
    }
    if (images.length > 1) {
      body.last_frame_image = ref(images[1]);
    }
  }
  copyFlags(body, input);
  return body;
}

// 分辨率归一成上游的写法：现代系列 720 视为 768P，其余默认 720P。
function normalizeResolution(raw, model) {
  var s = String(raw === undefined || raw === null ? "" : raw);
  if (s.indexOf("1080") >= 0) {
    return "1080P";
  }
  if (s.indexOf("768") >= 0) {
    return "768P";
  }
  if (s.indexOf("720") >= 0) {
    return isModern(model) ? "768P" : "720P";
  }
  if (s.indexOf("512") >= 0) {
    return "512P";
  }
  return isModern(model) ? "768P" : "720P";
}

// ---------------------------------------------------------------------------
// 提交：MiniMax-H3（v2）
// ---------------------------------------------------------------------------

function buildH3Body(ctx) {
  var input = ctx.input || {};
  var images = isArray(input.images) ? input.images : [];
  var videos = isArray(input.videos) ? input.videos : [];
  var audios = isArray(input.audios) ? input.audios : [];
  var content = [];
  if (input.prompt !== undefined && input.prompt !== "") {
    content.push({ type: "text", text: String(input.prompt) });
  }
  var reference = input.op === "omni" || videos.length > 0 || audios.length > 0;
  var i;
  for (i = 0; i < images.length; i++) {
    var role = reference ? "reference_image" : i === 0 ? "first_frame" : "last_frame";
    content.push(mediaItem("image_url", images[i], role));
  }
  for (i = 0; i < videos.length; i++) {
    content.push(mediaItem("video_url", videos[i], "reference_video"));
  }
  for (i = 0; i < audios.length; i++) {
    content.push(mediaItem("audio_url", audios[i], "reference_audio"));
  }

  var duration = Number(input.duration);
  var body = {
    model: ctx.model.upstreamModel,
    content: content,
    resolution: String(input.resolution || "").toUpperCase().indexOf("2K") >= 0 ? "2K" : "768P",
    duration: duration > 0 ? duration : 5,
    ratio: h3Ratio(input.aspect_ratio, images.length + videos.length > 0),
  };
  copyFlags(body, input);
  return body;
}

var H3_RATIOS = ["adaptive", "21:9", "16:9", "4:3", "1:1", "3:4", "9:16"];

// ratio 上游必填；adaptive 只有在有图片或视频可继承比例时才成立。
function h3Ratio(raw, hasVisual) {
  var r = String(raw === undefined || raw === null ? "" : raw).trim();
  if (r !== "" && H3_RATIOS.indexOf(r) >= 0 && (r !== "adaptive" || hasVisual)) {
    return r;
  }
  return hasVisual ? "adaptive" : "16:9";
}

function mediaItem(type, fileRef, role) {
  var item = { type: type, role: role };
  item[type] = { url: ref(fileRef) };
  return item;
}

// ---------------------------------------------------------------------------
// 轮询
// ---------------------------------------------------------------------------

function useV2(ctx) {
  var state = ctx.task.state || {};
  if (state.v2 !== undefined) {
    return !!state.v2;
  }
  return isH3(ctx.model.upstreamModel);
}

// v1 状态：Preparing / Queueing 排队，Processing 进行中，Success 成功（还要换地址），Fail 失败。
function parseV1Query(body, err) {
  var status = body.status;
  if (err && status !== "Fail") {
    return failedOrWait(err);
  }
  switch (status) {
    case "Preparing":
    case "Queueing":
      return { status: "queued" };
    case "Processing":
      return { status: "running", progress: 50 };
    case "Success":
      if (!body.file_id) {
        return failedWith(0, "上游返回成功，但没有 file_id");
      }
      // 地址要用 file_id 再查一次，记进 state，下一次轮询发 retrieve 请求
      return { status: "running", progress: 90, state: { v2: false, fileId: String(body.file_id) } };
    case "Fail":
      return failedWith(err ? err.code : 0, (err && err.message) || "视频生成失败");
    default:
      // 未知状态继续等待，由任务 deadline 兜底
      return { status: "queued" };
  }
}

// v2 状态：queued / running / succeeded / failed / cancelled，结果在 task 里。
function parseV2Query(body, err) {
  var task = body.task && typeof body.task === "object" ? body.task : null;
  if (!task) {
    if (err) {
      return failedOrWait(err);
    }
    return { status: "queued" };
  }
  switch (task.status) {
    case "queued":
      return { status: "queued" };
    case "running":
      return { status: "running", progress: 50 };
    case "succeeded":
      var url = task.content && typeof task.content.url === "string" ? task.content.url : "";
      return url ? succeeded(url) : failedWith(0, "上游返回成功，但没有视频地址");
    case "failed":
    case "cancelled":
      var msg = (task.error && task.error.message) || "任务" + (task.status === "failed" ? "失败" : "已取消");
      return failedWith(task.error && task.error.code, msg);
    default:
      return { status: "queued" };
  }
}

function succeeded(url) {
  return {
    status: "succeeded",
    progress: 100,
    outputs: [{ type: "url", url: url, media_type: "video", mime: "video/mp4" }],
  };
}

// ---------------------------------------------------------------------------
// 错误
// ---------------------------------------------------------------------------

// 取响应里的业务错误：v1 / v2 的 base_resp（status_code 非 0），v2 的 {error:{message, http_code}}；没有错误返回 null。
function apiErrorOf(body) {
  if (!body || typeof body !== "object") {
    return null;
  }
  var base = body.base_resp;
  if (base && typeof base === "object" && base.status_code !== undefined && base.status_code !== 0) {
    return { code: base.status_code, message: String(base.status_msg || "上游返回错误码 " + base.status_code) };
  }
  var e = body.error;
  if (e && typeof e === "object" && typeof e.message === "string" && e.message) {
    return { code: Number(e.http_code || e.code || 0) || 0, message: e.message };
  }
  return null;
}

function classifyCode(code, message) {
  var n = Number(code) || 0;
  if (n === 1026 || n === 1027) {
    return "moderation";
  }
  if (n === 1008) {
    return "provider_balance";
  }
  if (n === 1002 || n === 1039 || n === 1000 || n === 1001 || n === 1013 || n === 1021 || n === 408 || n === 429 || (n >= 500 && n < 600)) {
    return "retryable";
  }
  var s = String(message || "").toLowerCase();
  if (/sensitive|nsfw|moderation|content[ _-]?policy|violat|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/insufficient|balance|quota|credit|余额|额度/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

function failedWith(code, message) {
  var e = { class: classifyCode(code, message), message: String(message || "上游失败") };
  if (code) {
    e.code = String(code);
  }
  return { status: "failed", error: e };
}

// 查询时的业务错误：限流 / 超时 / 内部错误是暂时的，继续轮询；其它按失败。
function failedOrWait(err) {
  if (classifyCode(err.code, err.message) === "retryable") {
    return { status: "running" };
  }
  return failedWith(err.code, err.message);
}

// ---------------------------------------------------------------------------
// 通用小工具
// ---------------------------------------------------------------------------

function ref(fileRef) {
  return { __fileRef: fileRef, as: "url" };
}

function refList(list) {
  var out = [];
  for (var i = 0; i < list.length; i++) {
    out.push(ref(list[i]));
  }
  return out;
}

// 用户填了的布尔开关原样放进请求体顶层，没填不发。
function copyFlags(body, input) {
  var keys = ["prompt_optimizer", "fast_pretreatment", "aigc_watermark"];
  for (var i = 0; i < keys.length; i++) {
    if (typeof input[keys[i]] === "boolean") {
      body[keys[i]] = input[keys[i]];
    }
  }
}

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

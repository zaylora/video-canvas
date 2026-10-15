/**
 * 阿里云百炼（DashScope）视频插件（内置，契约 v1）：万相视频（文生 / 图生 / 首尾帧 / 数字人）。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/alibaba/1.4.2，按 video-canvas 协议插件契约 v1 重写。
 *
 * 渠道 base_url：国内站 https://dashscope.aliyuncs.com ，国际站 https://dashscope-intl.aliyuncs.com 。
 * 鉴权：Authorization: Bearer <百炼 API Key>，由宿主按渠道 Key 注入（渠道 Key 直接填 sk- 开头的百炼 Key）。
 *
 * 真实上游接口（均在 base_url 下）：
 *   视频提交 POST /api/v1/services/aigc/video-generation/video-synthesis （首尾帧 wan2.2-kf2v-flash、数字人 wan2.2-s2v 走 .../image2video/video-synthesis），
 *            请求头 X-DashScope-Async: enable，体 { model, input, parameters }；
 *   任务查询 GET /api/v1/tasks/{task_id}，output.task_status 为 PENDING / RUNNING / SUCCEEDED / FAILED / CANCELED / UNKNOWN。
 *
 * 视频模型系列（按 upstreamModel 分派）：
 *   wan3.0-video / wan3.0-video-prime   文 / 图 / 参考生成，input.media，时长 2-30 或 -1（智能时长），分辨率 480P/720P/1080P，比例含 adaptive
 *   wan2.7-t2v、wan2.7-i2v（含日期快照）  t2v 用 parameters.resolution + ratio；i2v 用 input.media（首帧 / 尾帧 / 驱动音频 / 首段视频），时长 2-15
 *   wan2.6-t2v(-us)、wan2.5-t2v-preview、wan2.2-t2v-plus、wanx2.1-t2v-*   文生视频，用 parameters.size（宽*高）
 *   wan2.6-i2v(-flash/-us)、wan2.5-i2v-preview、wan2.2-i2v-*、wanx2.1-i2v-*   图生视频，input.img_url，分辨率走 parameters.resolution
 *   wan2.2-kf2v-flash   首尾帧：input.first_frame_url / last_frame_url
 *   wan2.2-s2v          图 + 音频生成数字人视频：input.image_url / audio_url，时长跟随音频
 *
 * 模型 params（运营在模型上填的固定参数）：
 *   parameters: object   补充进请求体 parameters（如 prompt_extend、watermark、shot_type），用户输入与插件算出的字段优先
 *   input: object        补充进请求体 input（如 negative_prompt、template），同样只补充
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt、op（t2v / i2v / omni）、images / videos / audios（文件引用，交给宿主换成签名 URL）、
 *   duration（秒）、resolution（480P / 720P / 1080P，大小写与是否带 P 都可）、aspect_ratio（16:9 / 9:16 / 1:1 / 4:3 / 3:4 / adaptive）、
 *   generate_audio（boolean，对应 parameters.audio）、seed、watermark、prompt_extend、negative_prompt
 *   wan3.0 / wan2.7-i2v：images[0] 首帧、images[1] 尾帧；op 为 omni 时 images / videos / audios 全部当参考素材（wan3.0）
 *
 * 与上游相比刻意没移植：OpenAI Responses / Video 网关协议、usageSchema 计费与重算。
 * 未核实：错误码（Arrearage / Throttling* / DataInspectionFailed 等）取自记忆里的百炼文档，没有逐条对照；
 *   连通检查用的 GET /compatible-mode/v1/models 是 OpenAI 兼容端点，没有在上游代码里出现。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "alibaba-video",
    name: "阿里云百炼 · 视频",
    version: "1.1.0",
    vendor: "alibaba",
    description: "对接阿里云百炼 DashScope 万相视频：文生 / 图生 / 首尾帧 / 数字人",
    auth: { type: "bearer" },
    endpoints: {
      video: { mode: "async", method: "POST", path: "/api/v1/services/aigc/…/video-synthesis" },
    },
    poll: { firstDelay: 10, interval: 8, maxInterval: 20, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    return buildVideoRequest(ctx);
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    var out = body.output || {};
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
      var url = videoUrlOf(out);
      if (!url) {
        return failed("上游返回成功，但没有视频地址");
      }
      return {
        status: "succeeded",
        progress: 100,
        outputs: [{ type: "url", url: url, media_type: "video", mime: "video/mp4" }],
      };
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
// 视频
// ---------------------------------------------------------------------------

// 各视频模型的协议能力（取自上游）。kind：
//   all    wan3.0：input.media，比例含 adaptive
//   t2v    wan2.7 文生：parameters.resolution + ratio
//   media  wan2.7 图生：input.media（首帧 / 尾帧 / 驱动音频 / 首段视频）
//   size   旧文生：parameters.size = 宽*高
//   image  图生：input.img_url + parameters.resolution
//   frames 首尾帧；speech 数字人
var WAN_MODELS = {
  "wan3.0-video": { kind: "all", resolutions: ["480P", "720P", "1080P"], def: "1080P", maxDuration: 30 },
  "wan3.0-video-prime": { kind: "all", resolutions: ["480P", "720P", "1080P"], def: "1080P", maxDuration: 30 },
  "wan2.7-t2v": { kind: "t2v", resolutions: ["720P", "1080P"], def: "1080P", maxDuration: 15 },
  "wan2.7-i2v": { kind: "media", resolutions: ["720P", "1080P"], def: "1080P", maxDuration: 15 },
  "wan2.6-t2v": { kind: "size", resolutions: ["720P", "1080P"], def: "1080P", maxDuration: 15 },
  "wan2.6-t2v-us": { kind: "size", resolutions: ["720P", "1080P"], def: "1080P", durations: [5, 10, 15] },
  "wan2.6-i2v": { kind: "image", resolutions: ["720P", "1080P"], def: "1080P", maxDuration: 15 },
  "wan2.6-i2v-flash": { kind: "image", resolutions: ["720P", "1080P"], def: "1080P", maxDuration: 15 },
  "wan2.6-i2v-us": { kind: "image", resolutions: ["720P", "1080P"], def: "1080P", durations: [5, 10, 15] },
  "wan2.5-t2v-preview": { kind: "size", resolutions: ["480P", "720P", "1080P"], def: "1080P", durations: [5, 10] },
  "wan2.5-i2v-preview": { kind: "image", resolutions: ["480P", "720P", "1080P"], def: "1080P", durations: [5, 10] },
  "wan2.2-t2v-plus": { kind: "size", resolutions: ["480P", "1080P"], def: "1080P", durations: [5] },
  "wan2.2-i2v-flash": { kind: "image", resolutions: ["480P", "720P", "1080P"], def: "720P", durations: [5] },
  "wan2.2-i2v-plus": { kind: "image", resolutions: ["480P", "1080P"], def: "1080P", durations: [5] },
  "wan2.2-kf2v-flash": { kind: "frames", resolutions: ["480P", "720P", "1080P"], def: "720P", durations: [5] },
  "wan2.2-s2v": { kind: "speech", resolutions: ["480P", "720P"], def: "480P" },
  "wanx2.1-t2v-plus": { kind: "size", resolutions: ["720P"], def: "720P", durations: [5] },
  "wanx2.1-t2v-turbo": { kind: "size", resolutions: ["480P", "720P"], def: "720P", durations: [5] },
  "wanx2.1-i2v-plus": { kind: "image", resolutions: ["720P"], def: "720P", durations: [5] },
  "wanx2.1-i2v-turbo": { kind: "image", resolutions: ["480P", "720P"], def: "720P", durations: [3, 4, 5] },
};

// 旧文生视频（kind=size）的像素尺寸表：分辨率 -> 宽高比 -> 宽*高
var LEGACY_SIZES = {
  "480P": { "16:9": "832*480", "9:16": "480*832", "1:1": "624*624" },
  "720P": { "16:9": "1280*720", "9:16": "720*1280", "1:1": "960*960", "4:3": "1088*832", "3:4": "832*1088" },
  "1080P": { "16:9": "1920*1080", "9:16": "1080*1920", "1:1": "1440*1440", "4:3": "1632*1248", "3:4": "1248*1632" },
};

// 带日期的快照（wan2.7-t2v-2026-04-25）按去日期后缀的名字处理；wan2.1-* 是 wanx2.1-* 的别名。
function modelKey(model) {
  return String(model || "")
    .replace(/-\d{4}-\d{2}-\d{2}$/, "")
    .replace(/^wan2\.1-/, "wanx2.1-");
}

// 清单之外的新模型：按名字里的 i2v / t2v 猜协议，不校验分辨率与时长，交给上游报错。
function videoProfile(model) {
  var key = modelKey(model);
  if (Object.prototype.hasOwnProperty.call(WAN_MODELS, key)) {
    return WAN_MODELS[key];
  }
  if (/i2v/.test(key)) {
    return { kind: "image", resolutions: null, def: "" };
  }
  if (/t2v/.test(key)) {
    return { kind: "t2v", resolutions: null, def: "" };
  }
  throw new Error("不支持的百炼视频模型：" + model);
}

function buildVideoRequest(ctx) {
  var input = ctx.input || {};
  var params = ctx.model.params || {};
  var model = ctx.model.upstreamModel;
  var profile = videoProfile(model);
  var kind = profile.kind;
  var body = { model: model, input: {}, parameters: {} };
  var inp = body.input;
  var par = body.parameters;

  var prompt = typeof input.prompt === "string" ? input.prompt.replace(/^\s+|\s+$/g, "") : "";
  if (prompt) {
    inp.prompt = input.prompt;
  }
  if (typeof input.negative_prompt === "string" && input.negative_prompt) {
    inp.negative_prompt = input.negative_prompt;
  }

  // 通用参数：用户输入优先，其次模型固定参数，prompt_extend 默认开（数字人除外）
  if (kind !== "speech") {
    par.prompt_extend = true;
  }
  copyDefined(par, input, ["prompt_extend", "watermark", "seed"]);
  if (typeof input.generate_audio === "boolean") {
    par.audio = input.generate_audio;
  }

  // 分辨率与比例
  var resolution = normResolution(input.resolution) || profile.def;
  if (profile.resolutions && resolution && indexOf(profile.resolutions, resolution) < 0) {
    throw new Error(model + " 的分辨率只能是 " + profile.resolutions.join(" / ") + "，收到 " + resolution);
  }
  var ratio = typeof input.aspect_ratio === "string" && input.aspect_ratio ? input.aspect_ratio : "";
  if (kind === "size") {
    var r = ratio || "16:9";
    var table = LEGACY_SIZES[resolution] || {};
    if (!table[r]) {
      throw new Error(model + " 在 " + resolution + " 下不支持画面比例 " + r);
    }
    par.size = table[r];
  } else if (kind === "t2v" || kind === "all") {
    if (resolution) {
      par.resolution = resolution;
    }
    var rr = ratio || (kind === "all" ? "adaptive" : "16:9");
    if (indexOf(["16:9", "9:16", "1:1", "4:3", "3:4"].concat(kind === "all" ? ["adaptive"] : []), rr) < 0) {
      throw new Error(model + " 不支持画面比例 " + rr);
    }
    par.ratio = rr;
  } else if (resolution) {
    par.resolution = resolution; // 图生视频的比例跟随首帧，不可配置
  }

  // 时长
  if (kind === "speech") {
    // 数字人视频的时长跟随音频，不传 duration
  } else {
    var d = typeof input.duration === "number" ? input.duration : 5;
    if (profile.durations) {
      if (indexOf(profile.durations, d) < 0) {
        throw new Error(model + " 的时长只能是 " + profile.durations.join(" / ") + " 秒");
      }
    } else if (profile.maxDuration && !(kind === "all" && d === -1) && (d !== Math.floor(d) || d < 2 || d > profile.maxDuration)) {
      throw new Error(model + " 的时长必须是 2 到 " + profile.maxDuration + " 的整数" + (kind === "all" ? "（或 -1 智能时长）" : ""));
    }
    par.duration = d;
  }

  // 参考素材
  var images = refList(input.images);
  var videos = refList(input.videos);
  var audios = refList(input.audios);
  if (kind === "all") {
    inp.media = [];
    if (input.op === "omni") {
      pushMedia(inp.media, "reference_image", images, 10);
      pushMedia(inp.media, "reference_video", videos, 5);
      pushMedia(inp.media, "reference_audio", audios, 5);
    } else {
      pushMedia(inp.media, "first_frame", images.slice(0, 1), 1);
      pushMedia(inp.media, "last_frame", images.slice(1, 2), 1);
      pushMedia(inp.media, "reference_audio", audios, 1);
    }
    if (inp.media.length === 0) {
      delete inp.media;
      if (!prompt) {
        throw new Error(model + " 需要提示词或参考素材");
      }
    }
  } else if (kind === "media") {
    inp.media = [];
    if (videos.length > 0) {
      pushMedia(inp.media, "first_clip", videos, 1);
    } else {
      pushMedia(inp.media, "first_frame", images.slice(0, 1), 1);
      pushMedia(inp.media, "last_frame", images.slice(1, 2), 1);
      pushMedia(inp.media, "driving_audio", audios, 1);
    }
    if (inp.media.length === 0) {
      throw new Error(model + " 需要首帧图片或首段视频");
    }
  } else if (kind === "frames") {
    if (images.length === 0) {
      throw new Error(model + " 需要首帧图片");
    }
    inp.first_frame_url = urlRef(images[0]);
    if (images.length > 1) {
      inp.last_frame_url = urlRef(images[1]);
    }
  } else if (kind === "speech") {
    if (images.length === 0 || audios.length === 0) {
      throw new Error(model + " 需要一张人物图片和一段音频");
    }
    inp.image_url = urlRef(images[0]);
    inp.audio_url = urlRef(audios[0]);
  } else if (kind === "image") {
    if (images.length === 0) {
      throw new Error(model + " 需要参考图片");
    }
    inp.img_url = urlRef(images[0]);
  } else {
    // t2v / size：文生视频，提示词必填；可带一段音频驱动口型 / 配乐
    if (!prompt) {
      throw new Error(model + " 需要提示词");
    }
    if (audios.length > 0) {
      inp.audio_url = urlRef(audios[0]);
    }
  }

  mergeMissing(inp, params.input);
  mergeMissing(par, params.parameters);

  var service = kind === "frames" || kind === "speech" ? "image2video" : "video-generation";
  return {
    method: "POST",
    path: "/api/v1/services/aigc/" + service + "/video-synthesis",
    headers: { "X-DashScope-Async": "enable" },
    json: body,
    timeout: 60,
  };
}

function videoUrlOf(out) {
  if (typeof out.video_url === "string" && out.video_url) {
    return out.video_url;
  }
  var res = out.results;
  if (res && typeof res.video_url === "string" && res.video_url) {
    return res.video_url;
  }
  return "";
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

// 480p / 720 / 1080P 统一成 480P / 720P / 1080P。
function normResolution(v) {
  var s = String(v === undefined || v === null ? "" : v).replace(/^\s+|\s+$/g, "").toUpperCase();
  if (!s) {
    return "";
  }
  return /P$/.test(s) ? s : s + "P";
}


function urlRef(ref) {
  return { __fileRef: ref, as: "url" };
}


// 往 input.media 里追加同一类型的素材，最多 max 个。
function pushMedia(media, type, refs, max) {
  for (var i = 0; i < refs.length && i < max; i++) {
    media.push({ type: type, url: urlRef(refs[i]) });
  }
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

// 模型固定参数只补充、不覆盖。
function mergeMissing(dst, extra) {
  if (!extra || typeof extra !== "object") {
    return;
  }
  for (var k in extra) {
    if (Object.prototype.hasOwnProperty.call(extra, k) && dst[k] === undefined) {
      dst[k] = extra[k];
    }
  }
}

function indexOf(list, v) {
  for (var i = 0; i < list.length; i++) {
    if (list[i] === v) {
      return i;
    }
  }
  return -1;
}

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

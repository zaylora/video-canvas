/**
 * New API 视频插件（内置，契约 v1）。
 *
 * 对接 New API 网关的 OpenAI 视频协议（openai-video，渠道 base_url 填网关地址，不带路径，如 https://api.yingce.tv）：
 *   - 提交：POST /v1/videos，JSON 请求体（model、prompt、seconds，参考素材与画幅等放在 metadata 里）；
 *   - 轮询：GET /v1/videos/{id}，状态 queued / in_progress / completed / failed，progress 是 0-100；
 *   - 取片：状态 completed 后再 GET /v1/videos/{id}/content，响应是 mp4 二进制，由宿主直接落库（asset 产物）。
 *     钩子是纯函数、一次只能发一个请求，所以拆成两轮询：第一轮看到 completed 时返回 running 并在 state 里记 ready，
 *     下一轮 buildQueryRequest 看到 ready 就改取 /content（responseType=binary）。
 *
 * 鉴权：bearer，渠道 Key 直接填网关令牌，宿主注入 Authorization: Bearer。
 *
 * 请求体里哪些字段不能乱放（网关的计费校验会读它们）：
 *   - 网关按 seconds（数字）、size（枚举）、videos（数字，生成条数）计量，这些顶层键的值类型错了整个请求会被 400
 *     plugin_usage_invalid（plugin usage value must be a number）拒绝；
 *   - 所以参考素材不能放在顶层 images / videos / audios（顶层 videos 会被当成“生成几条视频”的数量），
 *     而是和 Seedance 类插件的 openai_video 协议一致，放进 metadata.content（带 role 的 image_url / video_url / audio_url 数组）。
 *
 * 上游模型（upstreamModel 照填，例如 sd2.5-c1）：一个模型 ID 对应一套能力，素材上限与时长范围由运营在模型能力里配置，
 * 插件不做本地校验，超限交给宿主（提交前）和上游（提交后）。
 *
 * 用户输入（宿主按模型 capabilities 校验后传入）：
 *   prompt（必填）、duration（秒，也接受 seconds；以数字发送）、size（WIDTHxHEIGHT，没传则不发）；
 *   aspect_ratio -> metadata.ratio、resolution -> metadata.resolution、generate_audio -> metadata.generate_audio；
 *   images / videos / audios：参考素材数组，每项是文件引用，宿主替换成签名 URL 后放进 metadata.content。
 *   图片角色：op=omni 或多于 2 张都是 reference_image；2 张是 first_frame + last_frame；1 张是首帧（不写 role）；
 *   视频 -> reference_video，音频 -> reference_audio。没有可发的 metadata 字段时不发 metadata。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 *
 * 未验证（网关文档没有核实，metadata 的形状参照上游 Seedance 类插件的 openai_video 协议）：
 *   - 网关是否接受 metadata.content / ratio / resolution / generate_audio 这些键；
 *   - seconds 取数字、size 缺省时网关怎么处理；
 *   - 失败分类靠文案关键字（审核 / 余额），错误码表没有核对。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "new-api-video",
    name: "New API 视频",
    version: "1.1.0",
    vendor: "new-api",
    description: "New API 网关的 OpenAI 视频协议：文生视频、图生视频、多图 / 视频 / 音频参考（异步，完成后下载 mp4）",
    auth: { type: "bearer" },
    endpoints: {
      video: { mode: "async", method: "POST", path: "/v1/videos" },
    },
    poll: { firstDelay: 15, interval: 10, maxInterval: 30, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var body = { model: ctx.model.upstreamModel };
    if (input.prompt !== undefined && String(input.prompt) !== "") {
      body.prompt = String(input.prompt);
    }
    // seconds 发数字：网关按它计量，类型不对会被 400 plugin_usage_invalid 拒绝
    var seconds = toNumber(pick(input.duration, input.seconds));
    if (seconds !== undefined) {
      body.seconds = seconds;
    }
    if (typeof input.size === "string" && input.size !== "") {
      body.size = input.size;
    }

    var metadata = {};
    if (typeof input.aspect_ratio === "string" && input.aspect_ratio !== "") {
      metadata.ratio = input.aspect_ratio;
    }
    if (typeof input.resolution === "string" && input.resolution !== "") {
      metadata.resolution = input.resolution;
    }
    if (typeof input.generate_audio === "boolean") {
      metadata.generate_audio = input.generate_audio;
    }
    var content = refContent(input);
    if (content.length > 0) {
      metadata.content = content;
    }
    if (hasKeys(metadata)) {
      body.metadata = metadata;
    }
    return { method: "POST", path: "/v1/videos", json: body, timeout: 60 };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    var id = pick(body.id, body.task_id);
    if (id === undefined) {
      var msg = errorMessageOf(body);
      if (msg) {
        return { immediate: failed(msg, body.error) };
      }
      throw new Error("上游没有返回视频任务 id");
    }
    return { providerTaskId: String(id) };
  },

  buildQueryRequest: function (ctx) {
    var id = encodeURIComponent(ctx.task.providerTaskId);
    var state = ctx.task.state;
    if (state && state.ready) {
      // 上一轮已看到 completed：取 mp4 内容，宿主直接落库
      return { method: "GET", path: "/v1/videos/" + id + "/content", responseType: "binary", timeout: 120 };
    }
    return { method: "GET", path: "/v1/videos/" + id, timeout: 30 };
  },

  parseQueryResponse: function (ctx, resp) {
    if (resp.asset && resp.asset.id) {
      return { status: "succeeded", progress: 100, outputs: [{ type: "asset" }] };
    }
    var body = resp.body || {};
    var raw = String(body.status === undefined || body.status === null ? "" : body.status).toLowerCase();
    var progress = parseProgress(body.progress);

    if (raw === "failed" || raw === "cancelled" || raw === "canceled" || raw === "expired") {
      return failed(errorMessageOf(body) || "视频任务失败：" + raw, body.error);
    }
    if (raw === "completed" || raw === "succeeded") {
      // 视频已生成，下一轮去取内容
      return { status: "running", progress: 99, state: { ready: true } };
    }
    var res = { status: raw === "in_progress" || raw === "processing" ? "running" : "queued" };
    if (progress !== undefined) {
      res.progress = Math.min(progress, 99);
    }
    return res;
  },

  classifyError: function (ctx, resp) {
    var body = resp.body;
    var msg = errorMessageOf(body) || (resp.text || "").slice(0, 300);
    if (!msg) {
      return null;
    }
    var code = errorCodeOf(body);
    var cls = classifyFailure(msg, body && body.error);
    if (cls === "terminal") {
      // 限流与 5xx 交给宿主默认规则（重试）；其余 4xx 终止，但要把网关的错误码和原文带出来，
      // 否则宿主默认规则只会给一句“上游拒绝请求”，看不出是哪个参数被拒
      if (resp.status === 429 || resp.status >= 500) {
        return null;
      }
      var err = { class: "terminal", message: msg };
      if (code) {
        err.code = code;
      }
      return err;
    }
    return { class: cls, message: msg };
  },
};

// metadata.content：图片 / 视频 / 音频参考，素材用文件引用，宿主替换成签名 URL。
function refContent(input) {
  var content = [];
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

// 图片在视频里的角色：全能参考与多于 2 张都是参考图；2 张是首帧 + 尾帧；1 张是首帧（默认，不写 role）。
function imageRole(op, total, index) {
  if (op === "omni" || total > 2) {
    return "reference_image";
  }
  if (total === 2) {
    return index === 0 ? "first_frame" : "last_frame";
  }
  return "";
}

function failed(message, errObj) {
  return { status: "failed", error: { class: classifyFailure(message, errObj), message: message } };
}

// 按错误码 / 文案判断分类：审核 → moderation；额度 / 余额 → provider_balance；其余 terminal。
function classifyFailure(message, errObj) {
  var code = errObj && typeof errObj === "object" && typeof errObj.code === "string" ? errObj.code : "";
  var s = (String(message || "") + " " + code).toLowerCase();
  if (/moderation|content[ _-]?policy|violat|safety|sensitive|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/insufficient|billing|balance|quota|credit|余额|额度/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

// 兼容 {error:{message}}、{error:"..."} 和网关的顶层 {code, message}。
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
  return typeof body.message === "string" ? body.message : "";
}

// 错误码：兼容 {error:{code}} 与网关的顶层 {code:"..."}；只取字符串。
function errorCodeOf(body) {
  if (!body || typeof body !== "object") {
    return "";
  }
  var e = body.error;
  if (e && typeof e === "object" && typeof e.code === "string" && e.code) {
    return e.code;
  }
  return typeof body.code === "string" ? body.code : "";
}

// 把数字或数字字符串转成数字；转不出来返回 undefined（不发这个字段）。
function toNumber(v) {
  if (typeof v === "number") {
    return isNaN(v) ? undefined : v;
  }
  if (typeof v === "string" && v !== "") {
    var n = Number(v);
    return isNaN(n) ? undefined : n;
  }
  return undefined;
}

function parseProgress(v) {
  if (typeof v === "number") {
    return Math.max(0, v);
  }
  if (typeof v === "string") {
    var n = parseFloat(v);
    return isNaN(n) ? undefined : Math.max(0, n);
  }
  return undefined;
}

function hasKeys(obj) {
  for (var k in obj) {
    if (Object.prototype.hasOwnProperty.call(obj, k)) {
      return true;
    }
  }
  return false;
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

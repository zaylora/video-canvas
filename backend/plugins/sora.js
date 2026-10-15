/**
 * OpenAI Sora 视频插件（内置，契约 v1）。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/sora/1.1.0，按 video-canvas 协议插件契约 v1 重写。
 *
 * 对接 OpenAI 的视频接口（渠道 base_url 填 https://api.openai.com 或兼容 /v1/videos 的网关）：
 *   - 提交：POST /v1/videos。无参考图发 JSON；有参考图发 multipart，参考图作为文件字段 input_reference 上传；
 *   - 轮询：GET /v1/videos/{id}，状态 queued / in_progress / completed / failed，progress 是 0-100；
 *   - 取片：状态 completed 后再 GET /v1/videos/{id}/content，响应是 mp4 二进制，由宿主直接落库（asset 产物）。
 *     钩子是纯函数、一次只能发一个请求，所以拆成两轮询：第一轮看到 completed 时返回 running 并在 state 里记 ready，
 *     下一轮 buildQueryRequest 看到 ready 就改取 /content（responseType=binary）。
 *
 * 上游模型（meta.models）：sora-2、sora-2-pro。
 * 鉴权：bearer，渠道 Key 直接填 OpenAI API Key（sk-...），宿主注入 Authorization: Bearer。
 *
 * 用户输入（宿主按模型 capabilities 校验后传入）：
 *   prompt（必填）、op（t2v / i2v）；
 *   duration（秒，也接受 seconds；官方取值 4 / 8 / 12，原样转成字符串发送）；
 *   size（WIDTHxHEIGHT，官方取值 720x1280 / 1280x720 / 1792x1024 / 1024x1792，后两个仅 sora-2-pro）；
 *   没传 size 时由 aspect_ratio（16:9 / 9:16）与 resolution（1080p 或 high 选 1792x1024 / 1024x1792，其余 720p 档）推出；
 *   images[0] -> input_reference（图生视频，只取第一张）。
 *
 * 和上游相比刻意没移植：
 *   - remix（POST /v1/videos/{id}/remix）：它要引用一个已有的 Sora 视频 id，而本项目的任务输入里没有这种引用，暂不支持；
 *   - 按秒 / 尺寸计费事实（usageSchema）、OpenAI Responses 与 openai_video 网关协议适配。
 * 未验证：JSON 提交时 seconds 用字符串（按官方文档的枚举是字符串，上游代码是原样透传客户端值）；
 *        无参考图时是否必须 multipart 没有核对，这里沿用上游：无文件发 JSON。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "sora",
    name: "Sora",
    version: "1.1.0",
    vendor: "openai",
    description: "OpenAI Sora 视频生成：文生视频、图生视频（异步，完成后下载 mp4）",
    auth: { type: "bearer" },
    endpoints: {
      video: { mode: "async", method: "POST", path: "/v1/videos" },
    },
    poll: { firstDelay: 15, interval: 10, maxInterval: 30, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var fields = { model: ctx.model.upstreamModel };
    if (input.prompt !== undefined) {
      fields.prompt = String(input.prompt);
    }
    var seconds = pick(input.duration, input.seconds);
    if (seconds !== undefined) {
      fields.seconds = String(seconds);
    }
    var size = sizeOf(input);
    if (size) {
      fields.size = size;
    }

    var images = isArray(input.images) ? input.images : [];
    if (images.length > 0) {
      // 有参考图：multipart，文件字段 input_reference（宿主按文件引用流式上传）
      return {
        method: "POST",
        path: "/v1/videos",
        multipart: {
          fields: stringifyFields(fields),
          parts: [{ name: "input_reference", fileRef: images[0] }],
        },
        timeout: 120,
      };
    }
    return { method: "POST", path: "/v1/videos", json: fields, timeout: 60 };
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
      var r = failed(errorMessageOf(body) || "Sora 任务失败：" + raw, body.error);
      return r;
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
    var cls = classifyFailure(msg, body && body.error);
    if (cls === "terminal") {
      return null; // 其余交给宿主默认规则（429 / 5xx 重试，其它 terminal）
    }
    return { class: cls, message: msg };
  },

  buildCheckRequest: function (ctx) {
    return { method: "GET", path: "/v1/models", timeout: 15 };
  },
};

// 优先用 size；没有时按画幅 + 分辨率推：横屏 / 竖屏，高档（1080p / high）用 sora-2-pro 的 1792x1024 / 1024x1792。
function sizeOf(input) {
  if (typeof input.size === "string" && input.size !== "") {
    return input.size;
  }
  var ratio = String(input.aspect_ratio || "");
  var res = String(input.resolution || "").toLowerCase();
  var high = res === "1080p" || res === "high" || res === "1792x1024" || res === "1024x1792";
  if (ratio === "16:9") {
    return high ? "1792x1024" : "1280x720";
  }
  if (ratio === "9:16") {
    return high ? "1024x1792" : "720x1280";
  }
  return "";
}

// multipart 的字段值都是字符串；对象转成 JSON 文本。
function stringifyFields(fields) {
  var out = {};
  for (var k in fields) {
    if (!Object.prototype.hasOwnProperty.call(fields, k)) {
      continue;
    }
    var v = fields[k];
    if (v === undefined || v === null) {
      continue;
    }
    out[k] = typeof v === "object" ? JSON.stringify(v) : String(v);
  }
  return out;
}

function failed(message, errObj) {
  return { status: "failed", error: { class: classifyFailure(message, errObj), message: message } };
}

// 按错误码 / 文案判断分类：审核 → moderation；额度 → provider_balance；其余 terminal。
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

// 兼容 {error:{message}}、{error:"..."}。
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

/**
 * Google Veo 视频插件，Gemini API 版（内置，契约 v1）。
 *
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/google/1.0.2，按 video-canvas 协议插件契约 v1 重写。
 *
 * 真实上游接口（Gemini API，generativelanguage.googleapis.com，长任务 Operation 模式）：
 *   - 提交：POST {base_url}/{apiVersion}/models/{model}:predictLongRunning
 *           body { instances: [{ prompt, image? }], parameters: { durationSeconds, aspectRatio, resolution, numberOfVideos } }
 *           响应 { name: "models/.../operations/xxx" }（偶尔 done=true 直接带结果）
 *   - 查询：GET  {base_url}/{apiVersion}/{operation name}
 *           响应 { name, done?, error?, response: { generateVideoResponse: { generatedVideos: [{ video: { uri } }] } } }
 *           Google 的 Operation 在未完成时省略 done，所以 done 不是 true 就当进行中。
 *   - 鉴权：请求头 x-goog-api-key: <渠道 Key>（meta.auth = header，由宿主注入）。
 *
 * 支持的模型（上游 meta.models）：veo-3.0-generate-001、veo-3.0-fast-generate-001、
 *   veo-3.1-generate-preview、veo-3.1-fast-generate-preview。
 *
 * 渠道设置：
 *   apiVersion : 接口版本，v1beta（默认）或 v1。
 * 用户输入（ctx.input，按运营在后台配置的参数名传入）：
 *   prompt（必填）、images[0]（有就走图生视频）、duration（4 / 6 / 8 秒）、
 *   aspect_ratio（16:9 / 9:16）、resolution（720p / 1080p / 4k）、seed、negative_prompt。
 *
 * 未验证 / 已知限制：
 *   1. 结果地址 generatedVideos[0].video.uri 是 files/...:download?alt=media 形式，下载需要 Key。做法同 sora.js：
 *      看到 done 的那一轮先返回 running 并在 state 里记 { ready, url }，下一轮查询对该地址发 responseType=binary 的请求，
 *      该地址与渠道 base_url 同源，宿主会注入 x-goog-api-key，视频直接落库成素材（asset 产物），Key 不会进入任何地址。
 *      若 uri 不是渠道同源，宿主不注入 Key，下载会失败（未验证）。
 *   2. 图生视频的 image 用 inlineData { mimeType, data }（上游插件的写法）；宿主文件引用只给内容、不给类型，参考图 MIME 固定写 image/png。
 *   3. raiMediaFilteredCount / raiMediaFilteredReasons 当作内容审核拒绝处理，这是 Gemini API 文档里的字段，上游插件没处理。
 *   4. 没有实现取消（上游没有）。
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，用 var / function，不用可选链与空值合并。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "google",
    name: "Google Veo (Gemini API)",
    version: "1.1.0",
    vendor: "google",
    description: "Google Veo 视频生成（文生视频、图生视频），Gemini API 版本",
    auth: { type: "header", name: "x-goog-api-key" },
    endpoints: { video: { mode: "async", method: "POST", path: "/{apiVersion}/models/{model}:predictLongRunning" } },
    channelSettings: {
      apiVersion: { type: "enum", label: "接口版本", options: ["v1beta", "v1"], default: "v1beta" },
    },
    poll: { firstDelay: 15, interval: 10, maxInterval: 20, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var parameters = {};
    if (Number(input.duration) > 0) {
      parameters.durationSeconds = Number(input.duration);
    }
    if (input.aspect_ratio) {
      parameters.aspectRatio = String(input.aspect_ratio);
    }
    if (input.resolution) {
      parameters.resolution = String(input.resolution).toLowerCase();
    }
    if (input.seed !== undefined && input.seed !== null && input.seed !== "") {
      parameters.seed = Number(input.seed);
    }
    if (input.negative_prompt) {
      parameters.negativePrompt = String(input.negative_prompt);
    }
    parameters.numberOfVideos = 1;

    var instance = { prompt: input.prompt };
    var images = input.images;
    if (images && images.length > 0) {
      instance.image = {
        inlineData: {
          mimeType: "image/png",
          data: { __fileRef: images[0], as: "base64" },
        },
      };
    }
    return {
      method: "POST",
      path: "/" + apiVersion(ctx) + "/models/" + encodeURIComponent(ctx.model.upstreamModel) + ":predictLongRunning",
      json: { instances: [instance], parameters: parameters },
      timeout: 60,
    };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    var name = String(body.name || "");
    if (!name) {
      throw new Error("上游没有返回 operation name");
    }
    var r = parseOperation(body);
    if (r.status === "failed") {
      return { providerTaskId: name, immediate: r };
    }
    // 提交即完成时也不能直接交地址：下一轮查询再按 state 取视频内容
    return r.state ? { providerTaskId: name, state: r.state } : { providerTaskId: name };
  },

  buildQueryRequest: function (ctx) {
    var st = ctx.task.state;
    if (st && st.ready && st.url) {
      // 上一轮看到 done：取视频内容。地址与渠道 base_url 同源，宿主会注入 x-goog-api-key，视频由宿主直接落库
      return { method: "GET", url: st.url, responseType: "binary", timeout: 120 };
    }
    // operation name 形如 models/veo-3.0-generate-001/operations/abc，直接拼在版本号后面；按段编码，保留斜杠
    var segs = String(ctx.task.providerTaskId).split("/");
    for (var i = 0; i < segs.length; i++) {
      segs[i] = encodeURIComponent(segs[i]);
    }
    return { method: "GET", path: "/" + apiVersion(ctx) + "/" + segs.join("/"), timeout: 30 };
  },

  parseQueryResponse: function (ctx, resp) {
    if (resp.asset && resp.asset.id) {
      return { status: "succeeded", progress: 100, outputs: [{ type: "asset" }] };
    }
    var body = resp.body;
    if (!body || typeof body !== "object" || (!body.name && !body.error)) {
      throw new Error("无法识别的 operation 响应");
    }
    return parseOperation(body);
  },

  classifyError: function (ctx, resp) {
    var msg = errorMessageOf(resp.body) || String(resp.text || "").slice(0, 300);
    var low = msg.toLowerCase();
    var status = resp.status;
    if (status === 429) {
      return { class: "retryable", message: msg };
    }
    if (status >= 500) {
      return { class: "retryable", message: msg };
    }
    if (/billing|insufficient|credit|prepay|余额/.test(low)) {
      return { class: "provider_balance", message: msg };
    }
    if (isModerationText(low)) {
      return { class: "moderation", message: msg };
    }
    return null; // 其余交给宿主默认规则
  },

  buildCheckRequest: function (ctx) {
    return { method: "GET", path: "/" + apiVersion(ctx) + "/models", query: { pageSize: 1 }, timeout: 15 };
  },
};

// 渠道设置里的接口版本，缺省 v1beta。
function apiVersion(ctx) {
  var s = (ctx.channel && ctx.channel.settings) || {};
  return s.apiVersion === "v1" ? "v1" : "v1beta";
}

// 取 Google 风格错误体 { error: { code, message, status } } 的文字。
function errorMessageOf(body) {
  if (!body || typeof body !== "object") {
    return "";
  }
  var e = body.error;
  if (e && typeof e === "object") {
    return String(e.message || e.status || "");
  }
  return typeof e === "string" ? e : "";
}

function isModerationText(low) {
  return /safety|blocked|policy|filtered|prohibited|sensitive|violat|审核/.test(low);
}

// 把一次 Operation（提交响应或查询响应）翻译成统一结果。
function parseOperation(body) {
  if (body.error) {
    var msg = errorMessageOf(body) || "上游任务失败";
    var cls = isModerationText(msg.toLowerCase()) ? "moderation" : "terminal";
    return { status: "failed", error: { class: cls, code: body.error.code ? String(body.error.code) : undefined, message: msg } };
  }
  if (body.done !== true) {
    return { status: "running" };
  }
  var gen = (body.response && body.response.generateVideoResponse) || {};
  var videos = gen.generatedVideos || [];
  var uri = videos.length > 0 && videos[0] && videos[0].video ? String(videos[0].video.uri || "") : "";
  if (uri) {
    // 视频已生成：先返回 running 并把地址记进 state，下一轮查询取内容（钩子一次只能发一个请求）
    return { status: "running", progress: 99, state: { ready: true, url: uri } };
  }
  if (Number(gen.raiMediaFilteredCount) > 0) {
    var reasons = gen.raiMediaFilteredReasons || [];
    return { status: "failed", error: { class: "moderation", message: reasons.length ? String(reasons[0]) : "视频被内容安全策略过滤" } };
  }
  return { status: "failed", error: { class: "terminal", message: "上游任务完成，但没有视频地址" } };
}

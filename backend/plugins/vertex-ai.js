/**
 * Google Veo 视频插件，Vertex AI 版（内置，契约 v1）。
 *
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/vertex-ai/1.0.2，按 video-canvas 协议插件契约 v1 重写。
 *
 * 真实上游接口（Vertex AI，长任务 Operation 模式）：
 *   - 换 token：POST https://oauth2.googleapis.com/token（grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer，
 *               assertion 是服务账号私钥签的 RS256 JWT，scope=cloud-platform），响应 { access_token, expires_in }；
 *               走契约的准备阶段（buildPrepareRequests / parsePrepareResponses），宿主不为它注入渠道 Key（auth: none）。
 *   - 提交：POST {base_url}/v1/projects/{project}/locations/{location}/publishers/google/models/{model}:predictLongRunning
 *           body { instances: [{ prompt, image? }], parameters: { durationSeconds, aspectRatio, resolution, sampleCount, generateAudio, storageUri } }
 *           响应 { name: "projects/.../locations/.../publishers/google/models/.../operations/xxx" }
 *   - 查询：POST {base_url}/v1/projects/{project}/locations/{location}/publishers/google/models/{model}:fetchPredictOperation
 *           body { operationName }；project / location / model 都从 operation name 里解析。
 *           响应 { name, done?, error?, response: { videos: [{ gcsUri | bytesBase64Encoded, mimeType }], raiMediaFilteredCount, ... } }
 *   - 鉴权：Authorization: Bearer <access_token> 加 x-goog-user-project，由插件自己写（meta.auth = custom）。
 *
 * 渠道配置：
 *   - 渠道 Key 填服务账号 JSON 的完整文本（含 client_email、private_key、project_id）；
 *   - 渠道必须开启 allow_credentials（否则宿主不给 ctx.credentials，插件拿不到服务账号，会报明确错误）；
 *   - base_url 填区域域名，如 https://us-central1-aiplatform.googleapis.com（全局端点 https://aiplatform.googleapis.com），不带 /v1。
 * 渠道设置：
 *   location   : 区域，默认 us-central1（要和 base_url 的区域一致）；
 *   storageUri : 视频输出目录，gs://bucket/prefix/，必填（原因见下方限制 1）；
 *   gcsBaseUrl : 取视频的 GCS 地址，默认 https://storage.googleapis.com，同样只在联调时改（access token 会发往这个主机）；
 *   tokenUrl   : 换 token 的地址，默认 https://oauth2.googleapis.com/token，只在走代理或联调时改。
 * 支持的模型（上游 meta.models）：veo-3.0-generate-001、veo-3.0-fast-generate-001、
 *   veo-3.1-generate-preview、veo-3.1-fast-generate-preview。
 * 模型 params：imageMimeType（参考图 MIME，只能 image/png 或 image/jpeg，默认 image/png）；
 *   extra（原样合并进 parameters，如 negativePrompt、personGeneration）。
 * 用户输入：prompt（必填）、images[0]（有就走图生视频）、duration（4 / 6 / 8）、aspect_ratio（16:9 / 9:16）、
 *   resolution（720p / 1080p / 4k）、generate_audio（布尔）、seed、negative_prompt。
 *
 * token 的传递：宿主只在提交阶段把准备结果放进 ctx.prepared，查询阶段没有。所以提交成功时把 access_token、过期时间
 * 存进 state，查询阶段从 state 取。token 约 1 小时有效，Veo 任务一般几分钟；查询时 token 已过期会报插件级错误
 * （查询阶段没有网络，换不了新 token）。
 *
 * 未验证 / 已知限制：
 *   1. 上游插件在没配 storageUri 时让 Veo 返回内联 base64（bytesBase64Encoded）再包成 data: 地址；契约不支持内联 base64 产物，
 *      所以本插件要求配 storageUri。看到 done 的那一轮把 gs://bucket/obj 转成 https://storage.googleapis.com/bucket/obj 记进 state
 *      （running），下一轮查询带 Authorization: Bearer <access_token> 发 responseType=binary 请求取视频，宿主直接落库成素材。
 *      自写 Authorization 发往非渠道域名，宿主允许（request.go 只在 auth 为 custom 且开了 allow_credentials 时放行，不校验目标主机），
 *      所以由插件自己保证 token 只发往固定的 GCS 主机；Go 的 http.Client 跨域重定向会剥掉 Authorization。
 *      服务账号需要对该桶有 storage.objects.get 权限（未对真实 GCS 验证）。
 *   2. 参考图 MIME 无法从文件引用得到，取 params.imageMimeType；
 *   3. 上游默认区域是 global，这里默认 us-central1（Veo 文档里的区域）；
 *   4. 准备结果（含 access_token）会被宿主持久化，state 里也带着 token；没有做取消、渠道连通检查（没有不带 token 的便宜接口）。
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，用 var / function，不用可选链与空值合并。
 */
var TOKEN_URL = "https://oauth2.googleapis.com/token";

module.exports = {
  meta: {
    apiVersion: 1,
    key: "vertex-ai",
    name: "Google Veo (Vertex AI)",
    version: "1.0.0",
    description: "Google Veo 视频生成（文生视频、图生视频），Vertex AI 版本，服务账号鉴权",
    auth: { type: "custom" },
    endpoints: { video: { mode: "async" } },
    channelSettings: {
      location: { type: "string", label: "区域（location）", default: "us-central1" },
      storageUri: { type: "string", label: "输出目录（gs://bucket/prefix/）" },
      tokenUrl: { type: "string", label: "令牌地址（高级）", default: TOKEN_URL },
      gcsBaseUrl: { type: "string", label: "GCS 下载地址（高级）", default: "https://storage.googleapis.com" },
    },
    poll: { firstDelay: 15, interval: 10, maxInterval: 20, jitter: 0.2 },
  },

  buildPrepareRequests: function (ctx) {
    var sa = serviceAccount(ctx);
    var settings = (ctx.channel && ctx.channel.settings) || {};
    var tokenUrl = String(settings.tokenUrl || TOKEN_URL);
    var claims = {
      iss: sa.client_email,
      scope: "https://www.googleapis.com/auth/cloud-platform",
      aud: tokenUrl,
      iat: ctx.now,
      exp: ctx.now + 3600,
    };
    return [
      {
        method: "POST",
        url: tokenUrl,
        form: { grant_type: "urn:ietf:params:oauth:grant-type:jwt-bearer", assertion: utils.jwtSignRS256(claims, sa.private_key) },
        auth: { type: "none" },
        timeout: 30,
      },
    ];
  },

  parsePrepareResponses: function (ctx, resps) {
    var body = (resps && resps[0] && resps[0].body) || {};
    if (!body.access_token) {
      throw new Error("换取 access token 失败：" + String((resps && resps[0] && resps[0].text) || "").slice(0, 200));
    }
    var ttl = Number(body.expires_in) > 0 ? Number(body.expires_in) : 3600;
    return { accessToken: String(body.access_token), expiresAt: ctx.now + ttl, projectId: serviceAccount(ctx).project_id };
  },

  buildSubmitRequest: function (ctx) {
    var tok = tokenOf(ctx);
    var settings = (ctx.channel && ctx.channel.settings) || {};
    var storageUri = String(settings.storageUri || "");
    if (!storageUri) {
      throw new Error("渠道设置里没有填 storageUri（gs://bucket/prefix/）：没有它 Veo 只返回内联 base64，本插件无法交付");
    }
    var input = ctx.input || {};
    var params = ctx.model.params || {};
    var parameters = {};
    var extra = params.extra;
    if (extra && typeof extra === "object" && !Array.isArray(extra)) {
      for (var k in extra) {
        if (Object.prototype.hasOwnProperty.call(extra, k)) {
          parameters[k] = extra[k];
        }
      }
    }
    if (Number(input.duration) > 0) {
      parameters.durationSeconds = Number(input.duration);
    }
    if (input.aspect_ratio) {
      parameters.aspectRatio = String(input.aspect_ratio);
    }
    if (input.resolution) {
      parameters.resolution = String(input.resolution).toLowerCase();
    }
    if (typeof input.generate_audio === "boolean") {
      parameters.generateAudio = input.generate_audio;
    }
    if (input.seed !== undefined && input.seed !== null && input.seed !== "") {
      parameters.seed = Number(input.seed);
    }
    if (input.negative_prompt) {
      parameters.negativePrompt = String(input.negative_prompt);
    }
    parameters.sampleCount = 1;
    parameters.storageUri = storageUri;

    var instance = { prompt: input.prompt };
    var images = input.images;
    if (images && images.length > 0) {
      var mime = params.imageMimeType || "image/png";
      if (mime !== "image/png" && mime !== "image/jpeg") {
        throw new Error("Vertex Veo 的参考图只支持 image/png 或 image/jpeg");
      }
      instance.image = { bytesBase64Encoded: { __fileRef: images[0], as: "base64" }, mimeType: mime };
    }
    return {
      method: "POST",
      path: modelPath(tok.projectId, locationOf(ctx), ctx.model.upstreamModel) + ":predictLongRunning",
      headers: authHeaders(tok),
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
    var tok = tokenOf(ctx);
    var r = parseOperation(body, ctx);
    if (r.status === "failed") {
      return { providerTaskId: name, state: tokenState(tok), immediate: r };
    }
    // 提交即完成时也不直接交结果：下一轮查询再按 state 取视频
    var st = tokenState(tok);
    if (r.state) {
      st.ready = true;
      st.url = r.state.url;
    }
    return { providerTaskId: name, state: st };
  },

  buildQueryRequest: function (ctx) {
    var name = String(ctx.task.providerTaskId || "");
    var project = partOf(name, "projects/");
    var location = partOf(name, "locations/");
    var model = partOf(name, "models/");
    if (!project || !location || !model) {
      throw new Error("无法从 operation name 解析 project / location / model：" + name);
    }
    var tok = tokenOf(ctx);
    var st = ctx.task.state;
    if (st && st.ready && st.url) {
      // 上一轮看到 done：自己带 Bearer 去 GCS 取视频，宿主直接落库。
      // 目标主机固定是 gcsBaseUrl（默认 storage.googleapis.com），token 不会发往别处。
      return { method: "GET", url: st.url, headers: { Authorization: "Bearer " + tok.accessToken }, responseType: "binary", timeout: 120 };
    }
    return {
      method: "POST",
      path: modelPath(project, location, model) + ":fetchPredictOperation",
      headers: authHeaders(tok),
      json: { operationName: name },
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    if (resp.asset && resp.asset.id) {
      return { status: "succeeded", progress: 100, outputs: [{ type: "asset" }] };
    }
    var body = resp.body;
    if (!body || typeof body !== "object" || (!body.name && !body.error)) {
      throw new Error("无法识别的 operation 响应");
    }
    var r = parseOperation(body, ctx);
    if (r.status === "running" && r.state) {
      // state 是整体替换：连 token 一起带上
      var st = tokenState(tokenOf(ctx));
      st.ready = true;
      st.url = r.state.url;
      r.state = st;
    }
    return r;
  },

  classifyError: function (ctx, resp) {
    var msg = errorMessageOf(resp.body) || String(resp.text || "").slice(0, 300);
    var low = msg.toLowerCase();
    if (resp.status === 429 || resp.status >= 500) {
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
};

// 解析渠道 Key（服务账号 JSON）。宿主只在渠道开启 allow_credentials 时才给 ctx.credentials。
function serviceAccount(ctx) {
  var raw = ctx.credentials && ctx.credentials.apiKey;
  if (!raw) {
    throw new Error("拿不到服务账号：请在渠道上开启 allow_credentials，并把服务账号 JSON 填进渠道 Key");
  }
  var sa;
  try {
    sa = JSON.parse(raw);
  } catch (e) {
    throw new Error("渠道 Key 不是合法的服务账号 JSON");
  }
  if (!sa || !sa.client_email || !sa.private_key || !sa.project_id) {
    throw new Error("服务账号 JSON 缺少 client_email / private_key / project_id");
  }
  return sa;
}

// 取 access token：提交阶段在 ctx.prepared，查询阶段在 state 里；过期（留 30 秒余量）按插件级错误处理。
function tokenOf(ctx) {
  var t = ctx.prepared;
  if (!t || !t.accessToken) {
    t = ctx.task && ctx.task.state;
  }
  if (!t || !t.accessToken) {
    throw new Error("没有可用的 access token（准备阶段结果缺失）");
  }
  if (Number(t.expiresAt) <= ctx.now + 30) {
    throw new Error("access token 已过期，查询阶段无法刷新");
  }
  return t;
}

function tokenState(tok) {
  return { accessToken: tok.accessToken, expiresAt: tok.expiresAt, projectId: tok.projectId };
}

function authHeaders(tok) {
  var h = { Authorization: "Bearer " + tok.accessToken };
  if (tok.projectId) {
    h["x-goog-user-project"] = tok.projectId;
  }
  return h;
}

function locationOf(ctx) {
  var s = (ctx.channel && ctx.channel.settings) || {};
  return String(s.location || "us-central1");
}

function modelPath(project, location, model) {
  return (
    "/v1/projects/" + encodeURIComponent(project) + "/locations/" + encodeURIComponent(location) +
    "/publishers/google/models/" + encodeURIComponent(model)
  );
}

// 从 operation name 取 marker 后面的一段，如 "projects/" 后面的项目号。
function partOf(name, marker) {
  var i = name.indexOf(marker);
  if (i < 0) {
    return "";
  }
  return name.slice(i + marker.length).split("/")[0];
}

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

// gs://bucket/path/a.mp4 → https://storage.googleapis.com/bucket/path/a.mp4
function gcsToHttps(uri, ctx) {
  var s = (ctx && ctx.channel && ctx.channel.settings) || {};
  var base = String(s.gcsBaseUrl || "https://storage.googleapis.com").replace(/\/+$/, "");
  var rest = uri.slice(5);
  var segs = rest.split("/");
  for (var i = 0; i < segs.length; i++) {
    segs[i] = encodeURIComponent(segs[i]);
  }
  return base + "/" + segs.join("/");
}

// 把一次 Operation（提交响应或查询响应）翻译成统一结果。
function parseOperation(body, ctx) {
  if (body.error) {
    var msg = errorMessageOf(body) || "上游任务失败";
    var cls = isModerationText(msg.toLowerCase()) ? "moderation" : "terminal";
    return { status: "failed", error: { class: cls, code: body.error.code ? String(body.error.code) : undefined, message: msg } };
  }
  if (body.done !== true) {
    return { status: "running" };
  }
  var resp = body.response || {};
  var videos = resp.videos || [];
  var url = "";
  var inline = false;
  for (var i = 0; i < videos.length; i++) {
    var v = videos[i] || {};
    var uri = String(v.gcsUri || "");
    if (uri.indexOf("gs://") === 0) {
      if (!url) {
        url = gcsToHttps(uri, ctx);
      }
    } else if (v.bytesBase64Encoded) {
      inline = true;
    }
  }
  if (url) {
    // 视频已生成：先返回 running 并记下地址，下一轮查询取内容（钩子一次只能发一个请求）
    return { status: "running", progress: 99, state: { url: url } };
  }
  if (Number(resp.raiMediaFilteredCount) > 0) {
    var reasons = resp.raiMediaFilteredReasons || [];
    return { status: "failed", error: { class: "moderation", message: reasons.length ? String(reasons[0]) : "视频被内容安全策略过滤" } };
  }
  if (inline) {
    return { status: "failed", error: { class: "terminal", message: "上游返回了内联 base64 视频，本插件不支持；请在渠道设置里配置 storageUri" } };
  }
  return { status: "failed", error: { class: "terminal", message: "上游任务完成，但没有视频地址" } };
}

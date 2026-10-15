/**
 * 生数 Vidu 视频插件（内置，契约 v1）。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/vidu/1.0.2，按 video-canvas 协议插件契约 v1 重写。
 *
 * 对接 Vidu 企业版开放接口（渠道 base_url 填 https://api.vidu.cn，国际站是 https://api.vidu.com，不带路径）：
 *   - 提交：POST /ent/v2/{text2video | img2video | start-end2video | reference2video}，按参考图张数选路径：
 *       0 张 文生视频；1 张 图生视频；2 张 首尾帧；3 张及以上 参考生视频
 *   - 轮询：GET /ent/v2/tasks/{task_id}/creations，state：created / queueing / processing / success / failed
 *     成功后视频在 creations[0].url；失败原因在 err_code。
 * 网关协议适配（responses / openai_video）与计费（usageSchema、credits 估算）一律没有移植。
 *
 * 模型清单（上游 meta.models，填在模型的 upstreamModel 里；按它分派参数差异）：
 *   viduq2   ：时长 1–10 秒，默认 5 秒、720p；参考生视频固定用 viduq2 这个型号
 *   viduq1   ：时长固定 5 秒，分辨率固定 1080p
 *   vidu2.0  ：只支持有参考图；时长 4（360p / 720p / 1080p）或 8（仅 720p），默认 4 秒、360p
 *   vidu1.5  ：时长默认 5 秒、1080p，不额外限制
 * 组合不合法时钩子抛异常（宿主记为插件级失败），所以运营在后台配这些模型的参数范围时要与上面一致。
 *
 * 鉴权：meta.auth 是 custom。Vidu 的头是 Authorization: Token <key>（不是 Bearer），宿主的 bearer 注入凑不出来，
 * 所以插件自己写这个头。因此用 auth: custom（宿主直接提供 ctx.credentials）；渠道 Key 填 Vidu 控制台生成的 API Key 原文即可（不要带 "Token " 前缀）。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 *
 * 模型 params（运营在模型上填的固定参数）：{ extra?: object }  extra 合并进请求体顶层，只补充不覆盖（如 bgm、off_peak、style）
 * 用户输入（宿主按模型能力校验后传入）：
 *   prompt、images（参考图，数量决定路径，见上）、duration（秒）、resolution（360p / 540p / 720p / 1080p，大小写不敏感）、
 *   aspect_ratio（文生与参考生视频才有意义）、seed、movement_amplitude（auto / small / medium / large，默认 auto）
 *
 * 未验证（上游代码里没有、官方文档页也没能抓到）：
 *   - 各种 HTTP 错误码与 err_code 的取值表；失败分类按文案关键字（audit / illegal / credit / insufficient 等）猜，可能漏判。
 *   - 没有找到便宜的连通检查接口，所以没有实现 buildCheckRequest；上游也没有列模型接口，没有实现导入。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "vidu",
    name: "生数 Vidu",
    version: "1.1.0",
    vendor: "vidu",
    description: "生数 Vidu 视频生成：文生视频、图生视频、首尾帧、参考生视频，异步轮询出片",
    auth: { type: "custom" },
    endpoints: {
      video: { mode: "async", method: "POST", path: "/ent/v2/text2video" },
    },
    poll: { firstDelay: 15, interval: 10, maxInterval: 30, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var params = ctx.model.params || {};
    var model = ctx.model.upstreamModel || "viduq1";
    var images = isArray(input.images) ? input.images : [];
    var path = pathFor(images.length);
    if (images.length > 2 && model.indexOf("viduq2") === 0) {
      model = "viduq2"; // 参考生视频只认 viduq2
    }

    var duration = outboundDuration(model, input.duration);
    var resolution = outboundResolution(model, input.resolution);
    checkCombo(model, duration, resolution, images.length > 0);

    var body = { model: model, duration: duration, resolution: resolution };
    if (input.prompt !== undefined && input.prompt !== "") {
      body.prompt = String(input.prompt);
    }
    if (images.length > 0) {
      var refs = [];
      for (var i = 0; i < images.length; i++) {
        refs.push({ __fileRef: images[i], as: "url" });
      }
      body.images = refs;
    }
    if (typeof input.seed === "number") {
      body.seed = input.seed;
    }
    if (input.aspect_ratio && (images.length === 0 || images.length > 2)) {
      body.aspect_ratio = String(input.aspect_ratio);
    }
    body.movement_amplitude = firstString(input.movement_amplitude) || "auto";
    mergeExtra(body, params.extra);

    return {
      method: "POST",
      path: "/ent/v2/" + path,
      headers: authHeaders(ctx),
      json: body,
      timeout: 60,
    };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (body.state === "failed") {
      return { immediate: failedOf(body.err_code || "上游提交即失败") };
    }
    if (!body.task_id) {
      throw new Error("上游没有返回 task_id");
    }
    return { providerTaskId: String(body.task_id) };
  },

  buildQueryRequest: function (ctx) {
    return {
      method: "GET",
      path: "/ent/v2/tasks/" + encodeURIComponent(ctx.task.providerTaskId) + "/creations",
      headers: authHeaders(ctx),
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    var status = mapState(body.state);
    if (status === "failed") {
      return failedOf(body.err_code || "上游任务失败");
    }
    if (status !== "succeeded") {
      return { status: status };
    }
    var list = isArray(body.creations) ? body.creations : [];
    var url = list.length > 0 && list[0] ? list[0].url : "";
    if (typeof url !== "string" || url === "") {
      return failedOf("上游返回成功，但没有视频地址");
    }
    var result = {
      status: "succeeded",
      progress: 100,
      outputs: [{ type: "url", url: url, media_type: "video", mime: "video/mp4" }],
    };
    if (typeof body.credits === "number") {
      result.providerCost = body.credits; // Vidu 积分，只写对账日志
    }
    return result;
  },

  classifyError: function (ctx, resp) {
    var body = resp.body && typeof resp.body === "object" ? resp.body : {};
    var msg = firstString(body.err_code, body.message, body.error, body.msg) || (resp.text || "").slice(0, 300);
    var cls = classifyMessage(msg);
    if (cls === "terminal") {
      return null; // 429 / 5xx / 其它交给宿主默认规则
    }
    return { class: cls, message: msg };
  },
};

// ---------------------------------------------------------------------------
// 鉴权
// ---------------------------------------------------------------------------

function authHeaders(ctx) {
  var key = ctx.credentials && ctx.credentials.apiKey;
  if (typeof key !== "string" || key === "") {
    throw new Error("没有拿到渠道 Key：Vidu 插件需要在渠道里填写 API Key");
  }
  return { Authorization: "Token " + key };
}

// ---------------------------------------------------------------------------
// 参数规整与校验
// ---------------------------------------------------------------------------

function pathFor(imageCount) {
  if (imageCount === 0) {
    return "text2video";
  }
  if (imageCount === 1) {
    return "img2video";
  }
  if (imageCount === 2) {
    return "start-end2video";
  }
  return "reference2video";
}

function isQ2(model) {
  return model.indexOf("viduq2") === 0;
}

function defaultDuration(model) {
  return model === "vidu2.0" ? 4 : 5;
}

function defaultResolution(model) {
  if (isQ2(model)) {
    return "720p";
  }
  return model === "vidu2.0" ? "360p" : "1080p";
}

function outboundDuration(model, raw) {
  var n = Number(raw);
  return isFinite(n) && n > 0 ? n : defaultDuration(model);
}

// viduq1 只有 1080p；其余把 "720p" / "720P" / "1280x720" 规整成 360p / 540p / 720p / 1080p，认不出来用模型默认。
function outboundResolution(model, raw) {
  if (model === "viduq1") {
    return "1080p";
  }
  var s = String(raw === undefined || raw === null ? "" : raw).toLowerCase().replace(/^\s+|\s+$/g, "");
  if (s === "360p" || s === "540p" || s === "720p" || s === "1080p") {
    return s;
  }
  var m = /^(\d+)\s*[x*]\s*(\d+)$/.exec(s);
  if (m) {
    var longSide = Math.max(Number(m[1]), Number(m[2]));
    if (longSide >= 1920) {
      return "1080p";
    }
    if (longSide >= 1280) {
      return "720p";
    }
    if (longSide >= 960) {
      return "540p";
    }
    return "360p";
  }
  return defaultResolution(model);
}

// 各模型的时长 / 分辨率组合限制（上游文档，非法组合发出去会被拒，不如提前给清楚的原因）。
function checkCombo(model, duration, resolution, hasImages) {
  if (model === "vidu2.0" && !hasImages) {
    throw new Error("vidu2.0 不支持文生视频，请提供参考图");
  }
  if (model === "viduq1") {
    if (duration !== 5) {
      throw new Error("viduq1 的时长只能是 5 秒");
    }
    return;
  }
  if (model === "vidu2.0") {
    if (duration === 4) {
      if (resolution !== "360p" && resolution !== "720p" && resolution !== "1080p") {
        throw new Error("vidu2.0 时长 4 秒只支持 360p、720p、1080p");
      }
      return;
    }
    if (duration === 8) {
      if (resolution !== "720p") {
        throw new Error("vidu2.0 时长 8 秒只支持 720p");
      }
      return;
    }
    throw new Error("vidu2.0 的时长只能是 4 或 8 秒");
  }
  if (isQ2(model) && (Math.floor(duration) !== duration || duration < 1 || duration > 10)) {
    throw new Error("viduq2 的时长应是 1 到 10 的整数秒");
  }
}

// ---------------------------------------------------------------------------
// 响应与错误
// ---------------------------------------------------------------------------

function mapState(raw) {
  switch (String(raw === undefined || raw === null ? "" : raw).toLowerCase()) {
    case "success":
      return "succeeded";
    case "failed":
      return "failed";
    case "processing":
      return "running";
    default:
      return "queued"; // created / queueing / 未知：继续等待，由任务 deadline 兜底
  }
}

function failedOf(message) {
  return { status: "failed", error: { class: classifyMessage(message), message: message } };
}

// 审核 → moderation；积分 / 余额 → provider_balance；其余 terminal。
function classifyMessage(message) {
  var s = String(message || "").toLowerCase();
  if (/audit|illegal|moderation|sensitive|violat|policy|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/credit|insufficient|balance|quota|余额|积分|额度/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
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

/**
 * 快手可灵（Kling）视频插件（内置，契约 v1）。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/kling/1.1.0，按 video-canvas 协议插件契约 v1 重写。
 *
 * 对接可灵开放平台的原生 HTTP 接口（渠道 base_url 填 https://api-beijing.klingai.com 或 https://api.klingai.com，不带路径）：
 *   - 无参考图：POST /v1/videos/text2video  文生视频
 *   - 有参考图：POST /v1/videos/image2video 图生视频（images[0] 作首帧，images[1] 作尾帧 image_tail）
 *   - 轮询    ：GET  /v1/videos/{text2video|image2video}/{task_id}（路径跟提交时一致，提交时记在 state.action 里）
 * 本插件只做原生可灵协议；上游里“把另一台 New API 当网关”的分支（sk- 开头的 Key）与网关协议适配、计费一律没有移植。
 *
 * 模型清单（上游 meta.models）：kling-v1、kling-v1-6、kling-v2-master（填在模型的 upstreamModel 里，其余型号也可以直接填）。
 *   kling-v2-master 只支持 pro 模式，mode 强制为 pro；其它模型默认 std。
 *
 * 鉴权：meta.auth 是 custom。可灵用 AccessKey / SecretKey 签 HS256 JWT（iss=AccessKey，exp=now+1800，nbf=now-5），
 * 以 Authorization: Bearer <jwt> 发出。因此：
 *   1. auth: custom 时宿主直接把渠道 Key 放进 ctx.credentials.apiKey；
 *   2. 渠道 Key 的填写格式是 "AccessKey|SecretKey"（竖线分隔，两段首尾空白会被去掉）。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 *
 * 模型 params（运营在模型上填的固定参数）：
 *   { mode?: "std" | "pro", negative_prompt?: string, cfg_scale?: number, extra?: object }   extra 合并进请求体顶层，只补充不覆盖
 * 用户输入（宿主按模型能力校验后传入）：
 *   prompt、images（0 张文生视频；1 张首帧；2 张首尾帧，多于 2 张忽略多出的）、duration（秒，可灵只认 "5" / "10"，发出时转字符串）、
 *   aspect_ratio（仅文生视频带；16:9 / 9:16 / 1:1）、mode、negative_prompt、cfg_scale
 *
 * 响应与错误：
 *   2xx 响应体是 {code, message, data}，code 非 0 表示失败（插件翻译成 failed 结果）；
 *   任务状态 submitted / processing / succeed / failed，成功后视频在 data.task_result.videos[0].url。
 *
 * 未验证（凭上游代码与可灵官方文档印象，没法在线核对）：
 *   - 错误码分类（1102 余额、1301 内容审核、1302 / 1303 限流、5xx 服务端）：上游插件没有，按官方文档记忆写的；另外兜底按文案关键字判断。
 *   - buildCheckRequest 用的 GET /v1/videos/text2video?pageNum=1&pageSize=1（任务列表）是否是官方连通检查的最佳选择未核实。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "kling",
    name: "可灵 Kling",
    version: "1.1.0",
    vendor: "kling",
    description: "快手可灵视频生成：文生视频、图生视频（含首尾帧），异步轮询出片",
    auth: { type: "custom" },
    endpoints: {
      video: { mode: "async", method: "POST", path: "/v1/videos/text2video" },
    },
    poll: { firstDelay: 15, interval: 10, maxInterval: 30, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var params = ctx.model.params || {};
    var model = ctx.model.upstreamModel || "kling-v1";
    var images = isArray(input.images) ? input.images : [];
    var action = images.length > 0 ? "image2video" : "text2video";

    var body = { model_name: model, mode: resolveMode(model, firstString(input.mode, params.mode)) };
    if (input.prompt !== undefined && input.prompt !== "") {
      body.prompt = String(input.prompt);
    }
    var negative = firstString(input.negative_prompt, params.negative_prompt);
    if (negative) {
      body.negative_prompt = negative;
    }
    var cfg = typeof input.cfg_scale === "number" ? input.cfg_scale : params.cfg_scale;
    if (typeof cfg === "number") {
      body.cfg_scale = cfg;
    }
    var duration = input.duration;
    if (duration !== undefined && duration !== null && duration !== "") {
      body.duration = String(duration);
    }
    if (action === "text2video") {
      if (input.aspect_ratio) {
        body.aspect_ratio = String(input.aspect_ratio);
      }
    } else {
      body.image = { __fileRef: images[0], as: "url" };
      if (images.length > 1) {
        body.image_tail = { __fileRef: images[1], as: "url" };
      }
    }
    mergeExtra(body, params.extra);

    return {
      method: "POST",
      path: "/v1/videos/" + action,
      headers: authHeaders(ctx),
      json: body,
      timeout: 60,
    };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (body.code !== undefined && body.code !== 0) {
      // 200 + 业务错误码：翻译成 failed 结果，不当插件故障
      return { immediate: failedOf(body.code, body.message) };
    }
    var data = body.data || {};
    if (!data.task_id) {
      throw new Error("上游没有返回 task_id");
    }
    var action = actionOf(ctx);
    return { providerTaskId: String(data.task_id), state: { action: action } };
  },

  buildQueryRequest: function (ctx) {
    return {
      method: "GET",
      path: "/v1/videos/" + stateAction(ctx) + "/" + encodeURIComponent(ctx.task.providerTaskId),
      headers: authHeaders(ctx),
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (body.code !== undefined && body.code !== 0) {
      return failedOf(body.code, body.message);
    }
    var data = body.data || {};
    var status = mapStatus(data.task_status);
    if (status === "failed") {
      return failedOf(0, data.task_status_msg || "上游任务失败");
    }
    if (status !== "succeeded") {
      return { status: status };
    }
    var videos = data.task_result && isArray(data.task_result.videos) ? data.task_result.videos : [];
    var url = videos.length > 0 && videos[0] ? videos[0].url : "";
    if (typeof url !== "string" || url === "") {
      return failedOf(0, "上游返回成功，但没有视频地址");
    }
    var result = {
      status: "succeeded",
      progress: 100,
      outputs: [{ type: "url", url: url, media_type: "video", mime: "video/mp4" }],
    };
    var units = parseFloat(data.final_unit_deduction);
    if (!isNaN(units) && units > 0) {
      result.providerCost = units; // 可灵资源包单位数，只写对账日志
    }
    return result;
  },

  // 非 2xx：可灵的 HTTP 状态码和业务码是配套的，优先按业务码分类，其次按文案。
  classifyError: function (ctx, resp) {
    var body = resp.body && typeof resp.body === "object" ? resp.body : {};
    var message = typeof body.message === "string" && body.message ? body.message : (resp.text || "").slice(0, 300);
    var code = typeof body.code === "number" ? body.code : 0;
    var cls = classifyCode(code, message);
    if (cls === "terminal" && resp.status !== 401 && resp.status !== 403) {
      return null; // 429 / 5xx / 其它交给宿主默认规则
    }
    return { class: cls, code: code ? String(code) : undefined, message: message || undefined };
  },

  // 可灵没有专门的探活接口，用最便宜的“任务列表”接口验证 AccessKey / SecretKey 是否有效。
  buildCheckRequest: function (ctx) {
    return {
      method: "GET",
      path: "/v1/videos/text2video",
      query: { pageNum: 1, pageSize: 1 },
      headers: authHeaders(ctx),
      timeout: 15,
    };
  },
};

// ---------------------------------------------------------------------------
// 鉴权
// ---------------------------------------------------------------------------

// 用渠道 Key（"AccessKey|SecretKey"）签 30 分钟有效的 JWT，放进 Authorization 头。
function authHeaders(ctx) {
  var key = ctx.credentials && ctx.credentials.apiKey;
  if (typeof key !== "string" || key === "") {
    throw new Error("没有拿到渠道 Key：可灵插件需要在渠道里填写 AccessKey|SecretKey");
  }
  var parts = key.split("|");
  if (parts.length !== 2 || trim(parts[0]) === "" || trim(parts[1]) === "") {
    throw new Error("渠道 Key 格式不对，应为 AccessKey|SecretKey");
  }
  var now = ctx.now || utils.unixNow();
  var token = utils.jwtSignHS256({ iss: trim(parts[0]), exp: now + 1800, nbf: now - 5 }, trim(parts[1]));
  return { Authorization: "Bearer " + token };
}

// ---------------------------------------------------------------------------
// 请求与响应的小工具
// ---------------------------------------------------------------------------

// kling-v2-master 只有 pro；其余默认 std，用户或固定参数给了 std / pro 就照用，别的值回落默认。
function resolveMode(model, mode) {
  if (model === "kling-v2-master") {
    return "pro";
  }
  return mode === "pro" ? "pro" : "std";
}

function actionOf(ctx) {
  var images = ctx.input && isArray(ctx.input.images) ? ctx.input.images : [];
  return images.length > 0 ? "image2video" : "text2video";
}

function stateAction(ctx) {
  var s = ctx.task && ctx.task.state;
  return s && s.action === "image2video" ? "image2video" : "text2video";
}

function mapStatus(raw) {
  switch (String(raw === undefined || raw === null ? "" : raw).toLowerCase()) {
    case "succeed":
    case "success":
      return "succeeded";
    case "failed":
    case "failure":
      return "failed";
    case "processing":
      return "running";
    default:
      return "queued"; // submitted / 未知：继续等待，由任务 deadline 兜底
  }
}

function failedOf(code, message) {
  var msg = message || "上游任务失败";
  return {
    status: "failed",
    error: { class: classifyCode(code, msg), code: code ? String(code) : undefined, message: msg },
  };
}

// 先看业务码，再看文案：审核 → moderation；余额 / 资源包 → provider_balance；限流 / 服务端 → retryable。
function classifyCode(code, message) {
  if (code === 1102 || code === 1101) {
    return "provider_balance";
  }
  if (code === 1301) {
    return "moderation";
  }
  if (code === 1302 || code === 1303 || (code >= 5000 && code < 6000)) {
    return "retryable";
  }
  var s = String(message || "").toLowerCase();
  if (/sensitive|moderation|content[ _-]?policy|violat|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/insufficient|balance|arrears|quota|resource pack|余额|欠费|资源包|额度/.test(s)) {
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

function trim(s) {
  return String(s).replace(/^\s+|\s+$/g, "");
}

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

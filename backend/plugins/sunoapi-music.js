/**
 * SunoAPI 音乐生成插件（内置，契约 v1）。
 *
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/sunoapi/1.1.0，按 video-canvas 协议插件契约 v1 重写。
 *
 * 对接 Suno-API 项目（https://github.com/Suno-API/Suno-API，非官方）以及兼容它 /suno 路径的网关（如 New API）：
 *   - audio（音乐）：POST /suno/submit/MUSIC 提交，POST /suno/fetch（body {ids:[task_id]}）轮询，异步出歌。
 *   提交响应 {code:"success", message, data:"<task_id>"}；查询响应 {code:"success", data:[{task_id, action, status,
 *   fail_reason, data}]}，其中 data 是歌曲条目（数组，或被包了一层的对象）。
 *   任务状态：NOT_START / SUBMITTED / QUEUED / IN_PROGRESS / SUCCESS / FAILURE。
 *
 * 渠道设置：
 *   - Base URL 填 Suno-API 服务地址（不带 /suno 后缀）；
 *   - 渠道 Key 直接填该服务的 Key，宿主按 Bearer 注入（Authorization: Bearer <Key>）。
 *
 * 上游模型（ctx.model.upstreamModel）：以 chirp- 开头时当作 mv，否则用默认 chirp-v3-0；其它值（如上游旧名 suno_music）忽略。
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt：歌曲描述（gpt_description_prompt，必填）；
 *   make_instrumental（布尔参数，可选）：是否纯音乐，默认 false。
 *   Suno 模型版本 mv 不是输入：取 chirp- 开头的上游模型名，再没有就是默认 chirp-v3-0。
 *
 * 产物：任务成功后每首歌的 audio_url（audio/mpeg）。
 * 刻意没移植：封面图 image_url（音频模型不出图）、视频 video_url、自定义歌词 / 风格 tags / 续写 / 拼接等进阶字段
 * （上游插件也只透传 gateway 请求体，字段表没有核实）；批量查询的多任务合并；网关协议适配与 usageSchema 计费。
 * 未验证：Suno-API 项目本身没有核对过接口文档，路径与字段全部按上游插件的写法；没有便宜的连通检查接口，不提供 buildCheckRequest；
 * 提交里除 gpt_description_prompt / make_instrumental / mv 之外的字段本插件不发。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "sunoapi-music",
    name: "SunoAPI 音乐",
    version: "1.1.0",
    vendor: "sunoapi",
    description: "SunoAPI 项目：音乐生成（音频），异步",
    auth: { type: "bearer" },
    endpoints: {
      audio: { mode: "async", method: "POST", path: "/suno/submit/MUSIC" },
    },
    poll: { firstDelay: 10, interval: 6, maxInterval: 15, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var prompt = typeof input.prompt === "string" ? input.prompt : "";
    if (prompt.trim() === "") {
      throw new Error("歌曲描述 prompt 不能为空");
    }
    var body = {};
    body.gpt_description_prompt = prompt;
    body.make_instrumental = input.make_instrumental === true;
    body.mv = mvOf(ctx.model.upstreamModel);
    return { method: "POST", path: "/suno/submit/MUSIC", json: body, timeout: 60 };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (body.code !== "success") {
      return { immediate: failed(String(body.message || "上游拒绝了请求")) };
    }
    if (body.data === undefined || body.data === null || body.data === "") {
      throw new Error("上游没有返回 task_id");
    }
    return { providerTaskId: String(body.data) };
  },

  buildQueryRequest: function (ctx) {
    return {
      method: "POST",
      path: "/suno/fetch",
      json: { ids: [ctx.task.providerTaskId] },
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (body.code !== "success") {
      return failed(String(body.message || "查询任务失败"));
    }
    var item = null;
    var list = isArray(body.data) ? body.data : [];
    for (var i = 0; i < list.length; i++) {
      if (list[i] && String(list[i].task_id) === String(ctx.task.providerTaskId)) {
        item = list[i];
      }
    }
    if (!item && list.length === 1) {
      item = list[0];
    }
    if (!item) {
      return { status: "queued" }; // 上游暂时查不到这个任务，继续等待，由任务 deadline 兜底
    }

    var progress = parseProgress(item.progress);
    switch (String(item.status || "").toUpperCase()) {
      case "SUCCESS":
        return succeededResult(item);
      case "FAILURE":
        return failed(String(item.fail_reason || "上游任务失败"));
      case "IN_PROGRESS":
        return progress === undefined ? { status: "running" } : { status: "running", progress: progress };
      default:
        return { status: "queued" }; // NOT_START / SUBMITTED / QUEUED / 未知
    }
  },

  classifyError: function (ctx, resp) {
    var body = resp.body;
    var msg = "";
    if (body && typeof body === "object") {
      if (typeof body.message === "string") {
        msg = body.message;
      } else if (typeof body.error === "string") {
        msg = body.error;
      } else if (body.error && typeof body.error.message === "string") {
        msg = body.error.message;
      } else if (typeof body.detail === "string") {
        msg = body.detail;
      }
    }
    msg = msg || (resp.text || "").slice(0, 300);
    if (!msg) {
      return null;
    }
    var cls = classifyMessage(msg);
    if (cls === "terminal") {
      return null; // 其余交给宿主默认规则（429 / 5xx 重试，其它 terminal）
    }
    return { class: cls, message: msg };
  },
};

function succeededResult(item) {
  var clips = clipsOf(item.data);
  var outputs = [];
  for (var k = 0; k < clips.length; k++) {
    var url = typeof clips[k].audio_url === "string" ? clips[k].audio_url.trim() : "";
    if (url) {
      outputs.push({ type: "url", url: url, media_type: "audio", mime: "audio/mpeg" });
    }
  }
  if (outputs.length === 0) {
    return failed("上游返回成功，但没有音频地址");
  }
  return { status: "succeeded", progress: 100, outputs: outputs };
}

// 任务的 data 可能是条目数组、单个条目，或再包了一层 {task_id, data: ...}（网关转存的形态）；统一成条目数组。
function clipsOf(data) {
  if (data && !isArray(data) && typeof data === "object" && data.task_id && data.data !== undefined) {
    data = data.data;
  }
  var list = isArray(data) ? data : data && typeof data === "object" ? [data] : [];
  var out = [];
  for (var i = 0; i < list.length; i++) {
    if (list[i] && typeof list[i] === "object") {
      out.push(list[i]);
    }
  }
  return out;
}

// mv 优先级：chirp- 开头的上游模型名 > 默认 chirp-v3-0。
function mvOf(upstreamModel) {
  if (typeof upstreamModel === "string" && upstreamModel.indexOf("chirp-") === 0) {
    return upstreamModel;
  }
  return "chirp-v3-0";
}

function failed(message) {
  return { status: "failed", error: { class: classifyMessage(message), message: message } };
}

// 按错误文案判断失败分类：审核 → moderation；余额 → provider_balance；其余 terminal。
function classifyMessage(message) {
  var s = String(message || "").toLowerCase();
  if (/sensitive|nsfw|moderation|content[ _-]?policy|violat|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/insufficient|balance|quota|credit|余额|额度/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

// "50%" / 50 → 50；不能解析返回 undefined。
function parseProgress(v) {
  var n = typeof v === "number" ? v : typeof v === "string" ? parseFloat(v) : NaN;
  return isNaN(n) ? undefined : n;
}

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

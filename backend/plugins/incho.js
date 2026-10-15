/**
 * 音潮 Incho 音乐生成插件（内置，契约 v1）。
 *
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/incho/1.0.2，按 video-canvas 协议插件契约 v1 重写。
 *
 * 对接 Incho 开放平台（文档 https://platform.yinchaoyongxian.com/docs ），只做“提示词生成歌曲”：
 *   - audio：POST /api/v1/song/generate 提交（task_type 固定 normal），GET /api/v1/task/query?task_id=<id> 轮询，异步出歌。
 *     一个任务里有 n 首歌（choices），每首有自己的状态 pending / running / stream / done / fail / cancelled。
 *
 * 渠道设置：
 *   - Base URL 填 https://open.yinchaoyongxian.com（插件契约里没有默认 Base URL，需要手填）；
 *   - 渠道 Key 直接填 Incho 的 API Key，宿主按 Bearer 注入（Authorization: Bearer <Key>）。
 *
 * 上游模型（ctx.model.upstreamModel）：v4.0（默认）与 v3.5；填成 incho_music 或留空时按 v4.0。
 *
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt（歌曲描述，上游最长 1000 字；不写歌词时由模型自己作词）
 *   n（可选，生成首数 1 或 2；没有时不发，用上游默认的 2 首）
 *   lyric（可选；能力参数只有 enum / number / boolean，通常拿不到，留给直接调用的场景）
 *
 * 任务结果：所有歌曲都进入终态后，只要有一首 done 就算成功（部分失败也成功，产物只含 done 的歌）；
 * 全部失败 / 取消才算失败，失败原因拼接各首的 error（error_code）。
 *
 * 刻意没移植：歌词生成、参考音频 / 续写（上游只支持 v3.5，且上游插件也未做）；批量查询 /task/querys；
 * 网关协议适配与 usageSchema 计费。
 * 未验证：没有便宜的连通检查接口，所以不提供 buildCheckRequest；上游没有“列模型”接口，不提供导入。
 * 上游错误体是 { "detail": "..." }（也可能是校验错误数组），HTTP 4xx / 5xx 的具体错误码文案没有逐一核对。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "incho",
    name: "音潮 Incho",
    version: "1.1.0",
    vendor: "incho",
    description: "音潮 Incho 音乐生成（提示词生成歌曲、多语种人声，异步）",
    auth: { type: "bearer" },
    endpoints: {
      audio: { mode: "async", method: "POST", path: "/api/v1/song/generate" },
    },
    poll: { firstDelay: 15, interval: 8, maxInterval: 20, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var prompt = trimmed(input.prompt);
    var lyric = trimmed(input.lyric);
    if (!prompt && !lyric) {
      throw new Error("prompt 与 lyric 至少要有一个");
    }
    if (codePoints(prompt) > MAX_PROMPT) {
      throw new Error("prompt 最长 " + MAX_PROMPT + " 个字符");
    }
    if (codePoints(lyric) > MAX_LYRIC) {
      throw new Error("lyric 最长 " + MAX_LYRIC + " 个字符");
    }

    var model = trimmed(ctx.model.upstreamModel);
    if (model === "" || model === "incho_music") {
      model = DEFAULT_MODEL;
    }
    var body = { model: model, task_type: "normal" };
    if (prompt) {
      body.prompt = prompt;
    }
    if (lyric) {
      body.lyric = lyric;
    }
    var n = pick(input.n);
    if (n !== undefined) {
      n = Number(n);
      if (n !== 1 && n !== 2) {
        throw new Error("n 只能是 1 或 2");
      }
      body.n = n;
    }
    return {
      method: "POST",
      path: "/api/v1/song/generate",
      headers: sourceHeaders(),
      json: body,
      timeout: 60,
    };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    var msg = detailMessage(body.detail);
    if (msg) {
      return { immediate: failed(msg) };
    }
    var id = trimmed(body.id);
    if (!id) {
      throw new Error("上游没有返回任务 id");
    }
    return { providerTaskId: id };
  },

  buildQueryRequest: function (ctx) {
    return {
      method: "GET",
      path: "/api/v1/task/query",
      query: { task_id: ctx.task.providerTaskId },
      headers: sourceHeaders(),
      timeout: 30,
    };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    var msg = detailMessage(body.detail);
    if (msg) {
      return failed(msg);
    }
    var choices = isArray(body.choices) ? body.choices : [];
    if (choices.length === 0) {
      return { status: "queued" }; // 刚提交，歌曲还没生成出条目
    }

    var done = [];
    var terminal = 0;
    var running = false;
    var reasons = [];
    for (var i = 0; i < choices.length; i++) {
      var song = choices[i] || {};
      var st = trimmed(song.status);
      if (st === "done") {
        done.push(song);
        terminal++;
      } else if (st === "fail" || st === "cancelled") {
        terminal++;
        var reason = trimmed(song.error) + (song.error_code ? " (" + song.error_code + ")" : "");
        if (reason) {
          reasons.push(reason);
        } else if (st === "cancelled") {
          reasons.push("歌曲被取消");
        }
      } else if (st === "running" || st === "stream") {
        running = true;
      }
      // pending 与未知状态：继续等待，由任务 deadline 兜底
    }

    if (terminal < choices.length) {
      var res = { status: running ? "running" : "queued" };
      // 进度按完成的歌曲数估算，留出最后一截给“全部完成”
      res.progress = Math.min(95, Math.round((terminal / choices.length) * 100));
      return res;
    }
    if (done.length === 0) {
      return failed(reasons.join("; ") || "歌曲生成失败");
    }

    var outputs = [];
    for (var k = 0; k < done.length; k++) {
      var url = trimmed(done[k].audio_url);
      if (url) {
        outputs.push({ type: "url", url: url, media_type: "audio", mime: "audio/mpeg" });
      }
    }
    if (outputs.length === 0) {
      return failed("上游返回成功，但没有音频地址");
    }
    return { status: "succeeded", progress: 100, outputs: outputs };
  },

  classifyError: function (ctx, resp) {
    var msg = detailMessage(resp.body && resp.body.detail) || (resp.text || "").slice(0, 300);
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

var DEFAULT_MODEL = "v4.0";
var MAX_PROMPT = 1000;
var MAX_LYRIC = 3000;

// 上游用来区分来源的请求头，不带用户数据。
function sourceHeaders() {
  return { Accept: "application/json", "X-Incho-Source": "plugins" };
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

// detail 可能是字符串、校验错误数组（元素带 msg）或对象；没有错误时返回空串。
function detailMessage(detail) {
  if (detail === undefined || detail === null || detail === "") {
    return "";
  }
  if (typeof detail === "string") {
    return trimmed(detail);
  }
  if (isArray(detail)) {
    var parts = [];
    for (var i = 0; i < detail.length; i++) {
      var item = detail[i];
      parts.push(item && typeof item.msg === "string" ? item.msg : JSON.stringify(item));
    }
    return parts.join("; ");
  }
  return typeof detail === "object" ? JSON.stringify(detail) : String(detail);
}

// 按 Unicode 码点计长度（上游限制按字符数）。
function codePoints(s) {
  return Array.from(s).length;
}

function trimmed(v) {
  return String(v === undefined || v === null ? "" : v).trim();
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

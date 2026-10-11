/**
 * 火山引擎即梦视频插件（内置，契约 v1）。
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/jimeng/1.1.0，按 video-canvas 协议插件契约 v1 重写。
 *
 * 对接火山引擎视觉智能开放平台的即梦视频生成（官方 HTTP 接口），只出 video，异步：
 *   - 提交：POST /?Action=CVSync2AsyncSubmitTask&Version=2022-08-31，body {req_key, prompt, aspect_ratio?, frames?, seed?}；
 *   - 轮询：POST /?Action=CVSync2AsyncGetResult&Version=2022-08-31，body {req_key, task_id}；
 *   - 响应统一是 {code, message, data}，code 为 10000 才是成功；data.status：in_queue / generating / done / not_found / expired，
 *     done 时 data.video_url 是成片地址。
 * 渠道 base_url 填 https://visual.volcengineapi.com（不要带路径）。
 *
 * 鉴权：火山引擎 V4 签名（HMAC-SHA256，Region cn-north-1，Service cv），不是 Bearer，所以 auth 用 custom：
 *   插件读 ctx.credentials.apiKey 自己签名，渠道必须开启 allow_credentials，否则报错。
 *   渠道 Key 的填写格式：AccessKeyId|SecretAccessKey（用半竖线隔开，如 AKLTxxxx|Secretxxxx）。
 *   签名的头：content-type;host;x-content-sha256;x-date；密钥逐级派生用 utils.hmacSHA256 / utils.hmacSHA256Hex。
 *   请求体的哈希要和宿主实际发出的字节一致：宿主对 json 请求体用 Go 的 json.Marshal（键按字典序、转义 < > & 等），
 *   所以本插件自己按同一套规则序列化来算哈希（见 goJSON），键序与转义要和宿主保持一致，改请求体字段时不要破坏这一点。
 *
 * 模型清单（上游 meta.models 只有 jimeng_vgfm_t2v_l20，另外按上游的 req_key 转换规则支持 3.0 系列，3.0 系列的 req_key 属于记忆，未验证）：
 *   jimeng_vgfm_t2v_l20            S2.0 Pro，固定 5 秒；
 *   jimeng_v30_720p / jimeng_v30_1080p   即梦 3.0，5 或 10 秒（frames 121 / 241），文生视频时自动改成 jimeng_t2v_v30_720p / _1080p；
 *   jimeng_v30_pro                 即梦 3.0 Pro，文生视频时改成 jimeng_ti2v_v30_pro。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 *
 * 模型 params（运营在模型上填的固定参数）：
 *   { extra?: object }   原样合并进请求体顶层（用户输入与固定字段优先）；注意要把新增字段按 JSON 键序参与签名，插件已统一处理
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt（文生视频必填）、duration（3.0 系列 5 / 10 秒，其余固定 5）、aspect_ratio（16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9）、seed（可选，不传由上游随机）
 *
 * 和上游相比刻意没移植 / 做不到的部分：
 *   1. 图生视频与首尾帧（req_key 里的 i2v、first_tail 系列）没有做：签名要对最终请求体字节求哈希，
 *      而参考图的签名 URL / base64 是宿主在发请求时才替换进去的，插件算不出那一刻的哈希。传了 images 会直接报插件错误。
 *      要支持需要契约增加“文件引用解析后再签名”的能力（例如宿主在发请求前回调一个签名钩子）。
 *   2. 上游的 New API 网关转发模式（Bearer sk-... + /jimeng/ 路径）、OpenAI Responses / openai_video 协议、usageSchema 计费都丢弃；
 *   3. 没有写连通检查：没有确认过便宜的只读接口（对不存在的 task_id 发查询会得到业务错误而不是鉴权结果，不适合当检查）；无模型导入接口。
 *
 * 未验证（凭火山引擎文档记忆）：业务错误码含义——50411 / 50412 / 50413 / 50511 / 50512 审核未通过 -> moderation，
 *   50429 / 50430 限流 / 并发超限、50500 / 50501 内部错误 -> retryable；seed 缺省时上游默认随机（-1）；
 *   HTTP 层签名失败等错误体是 {ResponseMetadata:{Error:{Code,Message}}}。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "jimeng",
    name: "即梦（火山引擎）",
    version: "1.0.0",
    description: "对接火山引擎即梦视频：文生视频（V4 签名，Key 填 AccessKeyId|SecretAccessKey，异步）",
    auth: { type: "custom" },
    endpoints: {
      video: { mode: "async" },
    },
    poll: { firstDelay: 20, interval: 8, maxInterval: 20, jitter: 0.2 },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var params = ctx.model.params || {};
    if (isArray(input.images) && input.images.length > 0) {
      throw new Error("即梦插件暂不支持参考图（图生视频 / 首尾帧）：参考图地址由宿主在发请求时才填入，无法参与 V4 签名");
    }
    var reqKey = textToVideoKey(ctx.model.upstreamModel);
    var body = {};
    if (ASPECT_RATIOS.indexOf(input.aspect_ratio) >= 0) {
      body.aspect_ratio = input.aspect_ratio;
    }
    if (isV3(reqKey)) {
      body.frames = Number(input.duration) === 10 ? 241 : 121;
    }
    if (input.prompt !== undefined && input.prompt !== "") {
      body.prompt = String(input.prompt);
    }
    body.req_key = reqKey;
    if (typeof input.seed === "number") {
      body.seed = input.seed;
    }
    mergeExtra(body, params.extra);
    return signedRequest(ctx, "CVSync2AsyncSubmitTask", body);
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (body.code !== 10000) {
      return { immediate: failedWith(body.code, body.message) };
    }
    var id = body.data && body.data.task_id;
    if (id === undefined || id === null || id === "") {
      throw new Error("上游没有返回 task_id");
    }
    // 轮询时要带同一个 req_key，记进 state
    return { providerTaskId: String(id), state: { req_key: textToVideoKey(ctx.model.upstreamModel) } };
  },

  buildQueryRequest: function (ctx) {
    var state = ctx.task.state || {};
    var reqKey = typeof state.req_key === "string" && state.req_key ? state.req_key : textToVideoKey(ctx.model.upstreamModel);
    return signedRequest(ctx, "CVSync2AsyncGetResult", { req_key: reqKey, task_id: ctx.task.providerTaskId });
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (body.code !== 10000) {
      // 限流 / 内部错误是暂时的，继续轮询；其它按失败
      if (classifyCode(body.code, body.message) === "retryable") {
        return { status: "running" };
      }
      return failedWith(body.code, body.message);
    }
    var data = body.data || {};
    switch (data.status) {
      case "in_queue":
        return { status: "queued" };
      case "generating":
        return { status: "running" };
      case "done":
        if (typeof data.video_url !== "string" || data.video_url === "") {
          return failedWith(0, "上游返回成功，但没有视频地址");
        }
        return {
          status: "succeeded",
          progress: 100,
          outputs: [{ type: "url", url: data.video_url, media_type: "video", mime: "video/mp4" }],
        };
      case "not_found":
      case "expired":
        return failedWith(0, "任务不存在或已过期（" + data.status + "）");
      default:
        // 未知状态继续等待，由任务 deadline 兜底
        return { status: "queued" };
    }
  },

  classifyError: function (ctx, resp) {
    var body = resp.body || {};
    var meta = body.ResponseMetadata && body.ResponseMetadata.Error ? body.ResponseMetadata.Error : null;
    var code = body.code !== undefined ? body.code : meta ? meta.Code : 0;
    var msg = (typeof body.message === "string" && body.message) || (meta && meta.Message) || (resp.text || "").slice(0, 300);
    if (!msg) {
      return null;
    }
    var cls = classifyCode(code, msg + " " + (meta && meta.Code ? meta.Code : ""));
    if (cls === "terminal") {
      return null; // 其余交给宿主默认规则（429 / 5xx 重试，其它 terminal）
    }
    return { class: cls, code: code ? String(code) : undefined, message: msg };
  },
};

var API_VERSION = "2022-08-31";
var REGION = "cn-north-1";
var SERVICE = "cv";
var ASPECT_RATIOS = ["16:9", "9:16", "1:1", "4:3", "3:4", "21:9"];

// ---------------------------------------------------------------------------
// req_key
// ---------------------------------------------------------------------------

function isV3(reqKey) {
  return String(reqKey || "").indexOf("v30") >= 0;
}

// 文生视频的 req_key：3.0 系列要把 jimeng_v30_* 换成 jimeng_t2v_v30_*（Pro 是 jimeng_ti2v_v30_pro），其余原样。
function textToVideoKey(upstreamModel) {
  var key = String(upstreamModel || "") || "jimeng_vgfm_t2v_l20";
  if (key.indexOf("jimeng_v30") < 0) {
    return key;
  }
  if (key === "jimeng_v30_pro") {
    return "jimeng_ti2v_v30_pro";
  }
  return key.replace("jimeng_v30", "jimeng_t2v_v30");
}

// ---------------------------------------------------------------------------
// V4 签名
// ---------------------------------------------------------------------------

// 生成带签名的 POST 请求描述。body 的哈希按宿主发出的字节（Go json.Marshal 的输出）计算。
function signedRequest(ctx, action, body) {
  var apiKey = ctx.credentials && ctx.credentials.apiKey;
  if (!apiKey) {
    throw new Error("即梦需要渠道开启 allow_credentials，才能拿到 AccessKeyId|SecretAccessKey 做签名");
  }
  var parts = String(apiKey).split("|");
  if (parts.length !== 2 || !parts[0].trim() || !parts[1].trim()) {
    throw new Error("即梦渠道 Key 格式应为 AccessKeyId|SecretAccessKey");
  }
  var host = hostOf(ctx.channel.baseUrl);
  var query = { Action: action, Version: API_VERSION };
  var bodyText = goJSON(body);
  var headers = volcHeaders(parts[0].trim(), parts[1].trim(), host, query, bodyText, ctx.now);
  return { method: "POST", path: "/", query: query, headers: headers, json: body, timeout: 30 };
}

function volcHeaders(accessKey, secretKey, host, query, bodyText, now) {
  var xDate = new Date(now * 1000).toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z"); // 20060102T150405Z
  var shortDate = xDate.slice(0, 8);
  var bodyHash = utils.sha256(bodyText);
  var signedHeaders = "content-type;host;x-content-sha256;x-date";
  var canonicalRequest = [
    "POST",
    "/",
    canonicalQuery(query),
    "content-type:application/json\nhost:" + host + "\nx-content-sha256:" + bodyHash + "\nx-date:" + xDate + "\n",
    signedHeaders,
    bodyHash,
  ].join("\n");
  var scope = shortDate + "/" + REGION + "/" + SERVICE + "/request";
  var stringToSign = ["HMAC-SHA256", xDate, scope, utils.sha256(canonicalRequest)].join("\n");

  // 逐级派生密钥：第一步的密钥是 SecretKey 原文，之后每一步用上一步输出的十六进制当密钥
  var kDate = utils.hmacSHA256(secretKey, shortDate);
  var kRegion = utils.hmacSHA256Hex(kDate, REGION);
  var kService = utils.hmacSHA256Hex(kRegion, SERVICE);
  var kSigning = utils.hmacSHA256Hex(kService, "request");
  var signature = utils.hmacSHA256Hex(kSigning, stringToSign);
  return {
    "X-Date": xDate,
    "X-Content-Sha256": bodyHash,
    Authorization: "HMAC-SHA256 Credential=" + accessKey + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature,
  };
}

// 渠道 base_url 的 host（含端口），和宿主发请求时的 Host 头一致。
function hostOf(baseUrl) {
  var m = /^https?:\/\/([^\/?#]+)/i.exec(String(baseUrl || ""));
  if (!m) {
    throw new Error("渠道 base_url 不合法");
  }
  return m[1];
}

function canonicalQuery(query) {
  var keys = Object.keys(query).sort();
  var out = [];
  for (var i = 0; i < keys.length; i++) {
    out.push(encodeURIComponent(keys[i]) + "=" + encodeURIComponent(String(query[keys[i]])));
  }
  return out.join("&");
}

// 按 Go json.Marshal 的规则序列化：对象键按字典序；字符串转义 " \ 控制字符，以及 < > & U+2028 U+2029（\u 形式）。
function goJSON(v) {
  if (v === null || v === undefined) {
    return "null";
  }
  if (typeof v === "string") {
    return goQuote(v);
  }
  if (typeof v === "number" || typeof v === "boolean") {
    return JSON.stringify(v);
  }
  var i;
  if (isArray(v)) {
    var items = [];
    for (i = 0; i < v.length; i++) {
      items.push(goJSON(v[i]));
    }
    return "[" + items.join(",") + "]";
  }
  var keys = Object.keys(v).sort();
  var fields = [];
  for (i = 0; i < keys.length; i++) {
    if (v[keys[i]] !== undefined) {
      fields.push(goQuote(keys[i]) + ":" + goJSON(v[keys[i]]));
    }
  }
  return "{" + fields.join(",") + "}";
}

function goQuote(s) {
  var out = '"';
  for (var i = 0; i < s.length; i++) {
    var ch = s.charAt(i);
    var c = s.charCodeAt(i);
    if (ch === '"') {
      out += '\\"';
    } else if (ch === "\\") {
      out += "\\\\";
    } else if (ch === "\n") {
      out += "\\n";
    } else if (ch === "\r") {
      out += "\\r";
    } else if (ch === "\t") {
      out += "\\t";
    } else if (c === 8) {
      out += "\\b";
    } else if (c === 12) {
      out += "\\f";
    } else if (c < 0x20 || ch === "<" || ch === ">" || ch === "&" || c === 0x2028 || c === 0x2029) {
      out += "\\u" + ("0000" + c.toString(16)).slice(-4);
    } else {
      out += ch;
    }
  }
  return out + '"';
}

// ---------------------------------------------------------------------------
// 错误
// ---------------------------------------------------------------------------

function classifyCode(code, message) {
  var n = Number(code) || 0;
  if (n === 50411 || n === 50412 || n === 50413 || n === 50511 || n === 50512) {
    return "moderation";
  }
  if (n === 50429 || n === 50430 || n === 50500 || n === 50501) {
    return "retryable";
  }
  var s = String(message || "").toLowerCase();
  if (/sensitive|nsfw|moderation|content[ _-]?policy|violat|违规|审核|敏感/.test(s)) {
    return "moderation";
  }
  if (/insufficient|balance|quota|credit|arrear|overdue|余额|额度|欠费/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

function failedWith(code, message) {
  var text = String(message || "即梦返回错误码 " + code);
  var e = { class: classifyCode(code, text), message: text };
  if (code) {
    e.code = String(code);
  }
  return { status: "failed", error: e };
}

// ---------------------------------------------------------------------------
// 通用小工具
// ---------------------------------------------------------------------------

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

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

/**
 * 火山引擎豆包 · 音频生成（豆包语音 Seed Audio）插件（内置，契约 v1）。
 *
 * 对接豆包语音的音频生成 HTTP 接口（非流式，文档：https://docs.volcengine.com/docs/DoubaoVoice/audio-generation-http）：
 *   POST /api/v3/tts/create，同步出音频：一段提示词里可以同时描述环境音、配乐、角色与台词，单次最长 120 秒。
 * 渠道设置：Base URL 填 https://openspeech.bytedance.com；渠道 Key 填控制台「API Key 管理」里的 Key，
 * 宿主按 X-Api-Key 头注入（新版控制台的单头鉴权；旧版 X-Api-App-Id + X-Api-Access-Key 双头鉴权即将下线，不支持）。
 *
 * 上游模型（ctx.model.upstreamModel）：目前只有 seed-audio-1.0；留空时用它。
 *
 * 写法说明：goja 只保证 ES5.1 + 部分 ES6，所以这里用 var / function，不用可选链与空值合并。
 *
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt：提示词或待合成文本（text_prompt，必填，上游上限 3000 字符）。有参考音频时用 @音频1、@音频2 引用，
 *     编号从 1 起，与 audios 的顺序一一对应；
 *   format（可选）：wav / mp3 / pcm / ogg_opus，默认 mp3（上游默认 wav，浏览器直接播 mp3 更省事）；
 *   sample_rate / speech_rate / loudness_rate / pitch_rate（数值，可选）：采样率、语速 [-50,100]、音量 [-50,100]、音调 [-12,12]，
 *     没填就不发，取值范围由运营在模型能力里配置、由上游校验；
 *   speaker（字符串，可选）：音色 ID（豆包语音合成模型 2.0 音色或声音复刻音色），作为 references 里单独的一项；
 *   audios：参考音频（最多 3 条、单条 ≤30 秒、≤10 MB，wav / mp3 / pcm / ogg_opus），文件引用换成签名 URL，按顺序各占一项。
 *     注意：宿主目前只对音频模型开放图片参考，不接收参考音频（modelcfg 校验会拒绝 refs.audio.on），所以线上暂时传不进 audios；
 *     插件先按上游文档支持，宿主放开后无需再改插件；
 *   images：参考图片（最多 1 张，≤10 MB，jpeg / png / webp），换成签名 URL。图片参考不能和音色、参考音频混用，混用直接报错、不发请求。
 *
 * 产物：响应里的 url（带过期时间，有效期 2 小时，宿主拿到后会转存）。响应里的 base64 audio 不用：本契约不支持内联产物。
 * 刻意没做：字幕（enable_subtitle 返回的字级时间戳，没有地方展示）、水印（watermark 不传即不加水印）、
 *   X-Api-Request-Id 追踪头、按 original_duration 计费（响应没有 usage 字段，计费走模型配置）。
 * 未验证：文档没有给错误码表和错误响应样例，按火山通用约定把 code 为 0 / 20000000 当成功码、其余非零当失败，
 *   错误分类只看文案关键字（审核 / 余额）和 HTTP 状态；没有便宜的连通检查接口，不提供 buildCheckRequest。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "doubao-audio",
    name: "豆包（火山语音） · 音频",
    version: "1.1.0",
    vendor: "volcengine",
    description: "火山引擎豆包语音 Seed Audio 音频生成（同步）",
    auth: { type: "header", name: "X-Api-Key" },
    endpoints: {
      audio: { mode: "sync", method: "POST", path: "/api/v3/tts/create" },
    },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var prompt = typeof input.prompt === "string" ? input.prompt : "";
    if (prompt.trim() === "") {
      throw new Error("提示词 prompt 不能为空");
    }

    var body = {
      model: modelOf(ctx.model.upstreamModel),
      text_prompt: prompt,
      audio_config: audioConfigOf(input),
    };
    var references = referencesOf(input);
    if (references.length > 0) {
      body.references = references;
    }
    return { method: "POST", path: "/api/v3/tts/create", json: body, timeout: 300 };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (typeof body.code === "number" && body.code !== 0 && body.code !== 20000000) {
      return { immediate: failed(String(body.message || "上游拒绝了请求")) };
    }
    var url = typeof body.url === "string" ? body.url.trim() : "";
    if (!url) {
      return { immediate: failed("上游返回成功，但没有音频地址") };
    }
    return {
      immediate: {
        status: "succeeded",
        progress: 100,
        outputs: [{ type: "url", url: url, media_type: "audio", mime: mimeOf(formatOf(ctx.input || {})) }],
      },
    };
  },

  classifyError: function (ctx, resp) {
    var body = resp.body;
    var msg = "";
    if (body && typeof body === "object") {
      if (typeof body.message === "string") {
        msg = body.message;
      } else if (body.error && typeof body.error.message === "string") {
        msg = body.error.message;
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

function modelOf(upstreamModel) {
  return typeof upstreamModel === "string" && upstreamModel.trim() !== "" ? upstreamModel.trim() : "seed-audio-1.0";
}

function formatOf(input) {
  return typeof input.format === "string" && input.format !== "" ? input.format : "mp3";
}

// 只发用户给了的数值字段：没填的交给上游默认值，不补 0。
function audioConfigOf(input) {
  var cfg = { format: formatOf(input) };
  var keys = ["sample_rate", "speech_rate", "loudness_rate", "pitch_rate"];
  for (var i = 0; i < keys.length; i++) {
    if (typeof input[keys[i]] === "number") {
      cfg[keys[i]] = input[keys[i]];
    }
  }
  return cfg;
}

// references 每一项只放一种来源（音色、音频 URL、图片 URL 互斥是单项内部的约束）；
// 顺序是音色在前、参考音频按上传顺序在后，@音频N 的编号只数参考音频。
function referencesOf(input) {
  var audios = isArray(input.audios) ? input.audios : [];
  var images = isArray(input.images) ? input.images : [];
  var speaker = typeof input.speaker === "string" ? input.speaker.trim() : "";

  if (images.length > 0 && (audios.length > 0 || speaker !== "")) {
    throw new Error("图片参考不能和音色、参考音频同时使用");
  }

  var refs = [];
  if (speaker !== "") {
    refs.push({ speaker: speaker });
  }
  for (var i = 0; i < audios.length; i++) {
    refs.push({ audio_url: { __fileRef: audios[i], as: "url" } });
  }
  for (var j = 0; j < images.length; j++) {
    refs.push({ image_url: { __fileRef: images[j], as: "url" } });
  }
  return refs;
}

function mimeOf(format) {
  switch (format) {
    case "wav":
      return "audio/wav";
    case "ogg_opus":
      return "audio/ogg";
    case "pcm":
      return "audio/pcm";
    default:
      return "audio/mpeg";
  }
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
  if (/insufficient|balance|quota|credit|overdue|余额|额度|欠费/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

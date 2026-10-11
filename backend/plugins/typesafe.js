/**
 * TypeSafe Jev 结构化判断插件（内置，契约 v1）。
 *
 * 改写自 QuantumNous/new-api-plugins（Apache-2.0）plugins/tasks/typesafe/1.1.0，按 video-canvas 协议插件契约 v1 重写。
 *
 * 对接 TypeSafe System One API（https://docs.typesafe.ai/api ）：
 *   - text：POST /v1/systemone，同步返回。请求体 {model, state, questions}，响应 {model, answers, usage:{input_tokens, ...}}。
 *     Jev 读一份“状态”（state），再按若干个问题（questions）逐个作答。question.type 有三种：
 *       choice（criteria 是 1–255 个选项的对象）、score（criteria 是 2–10 个等级的数组）、noul（是 / 否，criteria 可选，只能描述 true / false）。
 *     answers 的键与 questions 一一对应，每个 answer 的 type 与问题一致。
 *   Jev 一次最多接收 64k 输入 token（state 与全部问题共用）；输出不计费。
 *
 * 渠道设置：
 *   - Base URL 填 https://api.typesafe.ai（末尾带 /v1 也行，插件会去重）；
 *   - 渠道 Key 直接填 TypeSafe 的 API Key，宿主按 Bearer 注入（Authorization: Bearer <Key>）。
 *
 * 上游模型（ctx.model.upstreamModel）：jev-1.13.0、jev-latest、jev-preview（来自上游 meta.models；这里不强校验，新模型名可直接填）。
 *
 * 模型 params（运营在模型上填的固定参数）：
 *   text: { questions: object }   必填：要问 Jev 的问题集，格式同上游 questions（键是问题 id）。
 *         questions 是模型的“判断规则”，由运营固定；用户每次提供的是被判断的内容。
 * 用户输入（宿主按模型能力 capabilities 校验后传入）：
 *   prompt：被判断的内容，原样作为 state（文本）。system / max_tokens 不适用于该接口，忽略。
 *
 * 产物：一个 text 产物，正文是 answers 的 JSON（缩进 2 空格）；usage 带 input_tokens（上游返回的实际值）与 output_tokens（输出免费，记 0），
 * 按 Token 计费的模型据此结算，上游没有给出合法用量时不带 usage，宿主按冻结额结算。
 *
 * 刻意没移植：state 为对象 / 数组 / null 的结构化写法与逐问题 instructions（宿主输入只有一段提示词，且 JS 重排整数键会改变
 * Jev 看到的成员顺序，上游插件为此还要求宿主保序）；流式；按 64k 输入预冻结的 usageSchema 计费。
 * 未验证：没有便宜的连通检查接口（不提供 buildCheckRequest）；HTTP 错误体的格式与错误码没有核对，分类只按文案关键词，其余走宿主默认规则。
 */
module.exports = {
  meta: {
    apiVersion: 1,
    key: "typesafe",
    name: "TypeSafe",
    version: "1.0.0",
    description: "TypeSafe Jev 结构化判断：按预设问题（选择 / 打分 / 是否）对输入内容作答，同步返回",
    auth: { type: "bearer" },
    endpoints: {
      text: { mode: "sync" },
    },
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    var params = ctx.model.params || {};
    var questions = params.questions;
    validateQuestions(questions);
    var state = typeof input.prompt === "string" ? input.prompt : "";
    if (state.trim() === "") {
      throw new Error("被判断的内容 prompt 不能为空");
    }

    // Base URL 末尾带 /v1 时不重复拼
    var base = String((ctx.channel && ctx.channel.baseUrl) || "").replace(/\/+$/, "");
    var path = /\/v1$/.test(base) ? "/systemone" : "/v1/systemone";
    return {
      method: "POST",
      path: path,
      headers: { Accept: "application/json" },
      json: { model: ctx.model.upstreamModel, state: state, questions: questions },
      timeout: 120,
    };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body;
    if (!isObject(body) || !isObject(body.answers)) {
      return { immediate: failed("TypeSafe 响应缺少 answers") };
    }
    var questions = (ctx.model.params || {}).questions;
    for (var id in questions) {
      if (!Object.prototype.hasOwnProperty.call(questions, id)) {
        continue;
      }
      var answer = Object.prototype.hasOwnProperty.call(body.answers, id) ? body.answers[id] : null;
      if (!isObject(answer) || answer.type !== questions[id].type) {
        return { immediate: failed("TypeSafe 响应缺少与问题「" + id + "」匹配的回答") };
      }
    }
    var output = { type: "text", text: JSON.stringify(body.answers, null, 2) };
    var tokens = body.usage && body.usage.input_tokens;
    if (typeof tokens === "number" && tokens >= 0 && Math.floor(tokens) === tokens) {
      // 输出免费；输入 token 以上游返回为准
      output.usage = { input_tokens: tokens, output_tokens: 0 };
    }
    return { immediate: { status: "succeeded", outputs: [output] } };
  },

  classifyError: function (ctx, resp) {
    var body = resp.body;
    var msg = "";
    if (body && typeof body === "object") {
      if (typeof body.detail === "string") {
        msg = body.detail;
      } else if (typeof body.message === "string") {
        msg = body.message;
      } else if (typeof body.error === "string") {
        msg = body.error;
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

// 校验模型固定参数里的 questions（规则取自上游文档）；配置不对属于运营配置错误，抛异常按插件级失败处理。
function validateQuestions(questions) {
  if (!isObject(questions) || Object.keys(questions).length === 0) {
    throw new Error("模型 params.questions 必须是非空对象");
  }
  var ids = Object.keys(questions);
  for (var i = 0; i < ids.length; i++) {
    var q = questions[ids[i]];
    if (!isObject(q) || ["choice", "score", "noul"].indexOf(q.type) < 0) {
      throw new Error("问题「" + ids[i] + "」的 type 必须是 choice / score / noul");
    }
    var criteria = q.criteria;
    if (q.type === "choice") {
      var n = isObject(criteria) ? Object.keys(criteria).length : 0;
      if (n < 1 || n > 255) {
        throw new Error("choice 问题「" + ids[i] + "」的 criteria 必须有 1 到 255 个选项");
      }
    } else if (q.type === "score") {
      if (!isArray(criteria) || criteria.length < 2 || criteria.length > 10) {
        throw new Error("score 问题「" + ids[i] + "」的 criteria 必须有 2 到 10 个等级");
      }
    } else if (criteria !== undefined && criteria !== null) {
      var keys = isObject(criteria) ? Object.keys(criteria) : ["?"];
      for (var k = 0; k < keys.length; k++) {
        if (keys[k] !== "true" && keys[k] !== "false") {
          throw new Error("noul 问题「" + ids[i] + "」的 criteria 只能描述 true 与 false");
        }
      }
    }
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
  if (/insufficient|balance|quota|credit|余额|额度/.test(s)) {
    return "provider_balance";
  }
  return "terminal";
}

function isObject(v) {
  return v !== null && typeof v === "object" && !isArray(v);
}

function isArray(v) {
  return Object.prototype.toString.call(v) === "[object Array]";
}

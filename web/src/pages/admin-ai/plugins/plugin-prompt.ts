/**
 * “让 AI 帮忙写插件”的提示词。管理员复制后，连同目标平台的 API 文档一起发给 AI，即可得到符合契约的插件 .js。
 * 内容是 backend/docs/plugin-contract.md（契约 v1）的浓缩版 + 一个可对照的最小示例；契约变了这里要同步改。
 * 注意：文本里不要出现反引号与 ${，它们会破坏模板字符串。
 */
export const PLUGIN_AUTHORING_PROMPT = `你是一名资深后端工程师。请根据我在文末提供的“目标平台 API 文档”，为 video-canvas 编写一个【协议插件】（单个 JavaScript 文件）。
插件的职责只有两件：把任务翻译成 HTTP 请求描述、把上游响应翻译成统一结果。发请求、鉴权、轮询、重试、转存都由宿主完成。

# 一、运行环境（务必遵守）
- 插件是一个 CommonJS 文件，以 module.exports = { meta: {...}, 钩子函数... } 导出。
- 运行在 goja：只保证 ES5.1 + 部分 ES6。请用 var / function，不要用可选链(?.)、空值合并(??)、async/await、Promise、class、模块 import/export、模板字符串里的复杂表达式。
- 没有网络、文件、定时器、require，不能调用 eval / Function。全局只有 module、exports、utils 和标准内置对象（JSON、Math、Date、Object、Array）。
- 钩子必须是纯函数：同样输入同样输出，不依赖全局可变状态；需要跨调用保留的数据放进返回值的 state（≤64KB）。
- 返回值必须能 JSON.stringify。单次钩子执行限时 200ms，文件 ≤512KB。
- 不要在插件里写死密钥。鉴权由宿主按渠道 Key 注入（见 meta.auth）。

# 二、meta
module.exports.meta 示例：
{
  apiVersion: 1,                    // 固定为 1
  key: "my-platform",               // 小写字母/数字/连字符，以字母或数字开头，≤30 字符
  name: "我的平台",
  version: "1.0.0",                 // semver；同一 key 下唯一，上传后不可修改，改代码必须升版本号
  description: "一句话说明",
  auth: { type: "bearer" },         // none / bearer / header(需 name) / query(需 name) / custom
  allowedHosts: [],                 // 结果文件可能下载自哪些域名（小写域名或 *.example.com，不要 IP/端口/协议）
  endpoints: {                      // 键是模型种类：text / video / image / audio
    text:  { mode: "sync" },        // sync：一次请求直接出结果
    video: { mode: "async" }        // async：先提交拿到任务 id，再轮询
  },
  channelSettings: {                // 可选：渠道上的非敏感设置（如区域），渲染成表单
    region: { type: "enum", label: "区域", options: ["cn", "global"], default: "cn" }
  },
  import: { args: {} },             // 可选：声明支持“从渠道导入模型”；没有该字段时管理端的“导入模型”按钮是灰的，args 为空对象表示不需要参数
  poll: { firstDelay: 10, interval: 5, maxInterval: 15, jitter: 0.2 }   // 可选：异步轮询节奏（秒）
}
规则：
- auth.type 为 header / query 时必须写 auth.name（请求头名 / 查询参数名）。
- 每个 endpoint 都必须实现 buildSubmitRequest 与 parseSubmitResponse；mode 为 async 的还必须实现 buildQueryRequest 与 parseQueryResponse。
- 实现了 buildPrepareRequests 必须同时实现 parsePrepareResponses；buildImportRequest 与 parseImportResponse 必须成对出现。
- channelSettings / import.args 每项：type 为 string / number / boolean / enum；label 必填；enum 必须有 options，且 default 必须在 options 内；名字匹配 ^[A-Za-z_][A-Za-z0-9_]{0,31}$。
- 导入模型要同时满足两点才可用：meta 里声明 import（哪怕 args 是空对象），并且实现 buildImportRequest 与 parseImportResponse。只写钩子不写 meta.import，管理端会判定“不支持导入”，按钮置灰。
- 连通性检查只看是否实现了 buildCheckRequest；没实现时管理端提示“插件不支持连通性检查”。
- channelSettings / import.args 的 enum 选项 options 是字符串数组（["cn", "global"]）；模型 capabilities.params 里 enum 的 options 是字符串或数字数组，写法类似。
- 只声明目标平台真实支持的 endpoint，不要为了凑数声明用不上的种类。

# 三、钩子一览
buildPrepareRequests(ctx)            可选。提交前要先发的请求（如先上传文件），返回请求描述数组（≤8 个）
parsePrepareResponses(ctx, resps)    可选。返回 prepared（任意 JSON），提交时通过 ctx.prepared 取用
buildSubmitRequest(ctx)              必须。返回请求描述
parseSubmitResponse(ctx, resp)       必须。返回 { providerTaskId?, state?, immediate? }
buildQueryRequest(ctx)               async 必须。用 ctx.task.providerTaskId 组装轮询请求
parseQueryResponse(ctx, resp)        async 必须。返回统一结果
buildCancelRequest(ctx)              可选。没有就走软取消
classifyError(ctx, resp)             可选。上游非 2xx 时先调它，返回 { class, code?, message? } 或 null（null 表示走宿主默认规则）
buildCheckRequest(ctx)               可选。连通性检查，任意 2xx 算通。请返回一个廉价、只读、不产生费用的请求（如列模型、查询一条历史记录），不要提交生成任务；宿主自动注入 Key
buildImportRequest(ctx, args) / parseImportResponse(ctx, resp, args)   可选，需配合 meta.import。导入模型，parse 返回 [{ upstreamModel, kind, label, params? }]（模型能力由运营在后台手填，草稿不带）
- 钩子之间互相调用请直接调用文件里的普通函数，或写 module.exports.xxx(ctx)；不要依赖 this（宿主调用钩子时 this 不一定指向 module.exports）。

# 四、ctx（每次调用新建，是副本）
{
  task:    { id, providerTaskId, state },           // providerTaskId 提交前为空；state 是插件上次返回的私有状态
  model:   { key, kind, upstreamModel, params },    // params：运营在模型上填的固定参数，结构由插件自己约定并读取
  input:   { prompt: "...", image: "input:image", duration: 5 },   // 用户输入；媒体字段是文件引用字符串 "input:<字段名>"，没填的可选字段没有该键
  channel: { baseUrl, settings },
  prepared, credentials, now                        // credentials 只有 auth.type 为 custom 且渠道开启授权时才有
}

# 五、请求描述（build* 的返回值）
{
  method: "POST",                        // GET / POST / PUT / PATCH / DELETE
  path: "/v1/xxx",                       // 相对渠道 baseUrl，必须以 / 开头，不能含 .. 或以 // 开头；与 url 二选一
  url: "https://cdn.example.com/x",      // 绝对地址，主机必须在 allowedHosts 内；与 path 二选一
  query: { a: 1, b: "x" },               // 值只能是字符串/数字/布尔，null 与 undefined 会被忽略
  headers: { "X-Foo": "bar" },           // 禁止 Authorization、Cookie、Host、Content-Length 等；鉴权由宿主注入
  json: { ... },                         // 与 form / multipart 三选一，都不写表示无请求体
  form: { k: "v" },                      // application/x-www-form-urlencoded
  multipart: { fields: { k: "v" }, parts: [ { name: "file", fileRef: "input:image", filename: "a.png" } ] },
  responseType: "json",                  // json(默认) / text / binary
  timeout: 30,                           // 秒，默认 30，上限 120
  auth: { type: "query", name: "apiKey" }   // 可选：只为这次请求换一种注入方式（bearer / header / query / none），Key 仍由宿主注入
}
- 文件引用：json / form / multipart.fields 的任意位置可以写 { __fileRef: "input:image", as: "url" }，as 取 url（自有存储签名地址）/ base64 / dataUrl。引用必须指向 ctx.input 里的媒体字段。
- 请求体编码后 ≤1MB。base64 / dataUrl 会把文件读进内存（默认上限 10MB），能用 url 就用 url。
- responseType 为 binary（如语音合成直接返回音频字节）时，宿主把响应体写入素材存储，parse 钩子拿到的 resp 是 { status, headers, asset: { id, mime, size } }，此时产物写 { type: "asset" } 即可。

# 六、响应对象（parse* / classifyError 的第二个参数）
{ status: 200, headers: { "content-type": "..." }, body: <解析后的 JSON，解析失败为 null>, text: "原始文本" }
- 非 2xx 不会进入 parse*，会先走 classifyError；所以 parse* 里可以假设 2xx。
- 上游用 HTTP 200 + 错误码/错误文案表达失败时，要在 parse* 里自己识别并返回 failed。
- 读取响应字段要防御：字段可能缺失、类型可能是字符串或数字，先判断再用。

# 七、统一结果（parseQueryResponse 的返回值，也是 parseSubmitResponse 里 immediate 的格式）
{
  status: "queued" | "running" | "succeeded" | "failed",
  progress: 40,                          // 可选，0–100
  outputs: [                             // succeeded 时必填且非空
    { type: "url",  url: "https://...", media_type: "video", mime: "video/mp4" },
    { type: "text", text: "..." },       // 仅 text 模型使用，≤256KB
    { type: "asset" }                    // 二进制响应已由宿主落库
  ],
  error: { class: "terminal", code: "xxx", message: "给用户看的原因" },   // failed 时必填
  state: { },                            // 可选；省略表示状态不变
  providerCost: 0.12                     // 可选，仅对账
}
- error.class 只能是：retryable（可重试）/ terminal（终止）/ moderation（内容审核不通过）/ provider_balance（上游余额不足）。
- text 模型的产物必须是 text；其他模型不能返回 text。url 产物必须是 http/https，且主机在 allowedHosts 或渠道 baseUrl 主机内（所以要把结果文件所在的域名写进 allowedHosts）。
- 只支持 url 与 asset 两种文件产物，不支持内联 base64；上游只给 base64 时，返回 failed 并说明原因。
- sync endpoint：parseSubmitResponse 必须返回 { immediate: 统一结果 }，且 status 只能是 succeeded 或 failed。
- async endpoint：parseSubmitResponse 返回 { providerTaskId: "上游任务id" }（字符串或数字）；之后宿主按 poll 节奏调用 buildQueryRequest / parseQueryResponse，直到 succeeded 或 failed。未知的中间状态一律当作 queued 或 running 继续等待，不要当失败。
- 上游明确返回“失败/取消/过期”才返回 failed，并尽量带上上游给的原因文案。

# 八、utils（宿主注入的同步函数）
utils.uuid()、utils.unixNow()、utils.base64(s)、utils.base64Decode(s)、utils.base64URL(s)、utils.sha256(s)、utils.hmacSHA256(key, msg)、utils.jwtSignHS256(claims, secret)、utils.log(msg)（只进试跑追踪，单次最多 50 条）。
只有目标平台要求自签名（auth.type 为 custom）时才需要用到签名函数。

# 九、模型 params 与用户输入
- ctx.model.params 由你自己约定结构（例如 { extra: {...} } 原样合并进请求体），请在文件顶部注释里写清每个字段的含义。
- 用户输入由宿主按运营在模型 capabilities 里配置的能力校验后放进 ctx.input，键固定为：prompt（提示词）、op（生成方式 t2v / i2v / omni / t2i / i2i，仅视频、图片）、运营给生成参数起的名字（如 duration、aspect_ratio、resolution，值已按类型规范化）、参考素材数组 images / videos / audios（每项是文件引用字符串 "input:images.0"，用 { __fileRef: ... } 或 multipart 的 fileRef 引用，不是素材 ID）；文本模型另有 system（固定系统提示）与 max_tokens（最大输出）。请在文件顶部注释里列出插件会读取的键，并说明每个键应该对应上游的哪个字段。
- 用户输入优先于 params 里的固定值；params 只做补充。

# 九·补、导入模型
parseImportResponse 返回的每个草稿：{ upstreamModel: "上游模型名（由插件自己解释）", kind: "text|image|video|audio", label: "给人看的名字", params: {} }。草稿只预填模型编辑器的这几项，生成方式、参考素材、生成参数等能力由运营在后台按种类预填后手改，插件不声明。
- 插件在 buildSubmitRequest 里读到的 ctx.input 见上一节；没开放、没填的可选键没有。
- 上游没有“模型列表”接口时，可以把已知模型写死在插件里：buildImportRequest 返回一个廉价只读请求（如复用 buildCheckRequest 的请求），parseImportResponse 忽略响应，直接返回写死的草稿数组；别忘了声明 meta.import。
- 如果上游模型名不是“模型名”而是分组 id / 工作流 id，就把它放进 upstreamModel，在 buildSubmitRequest 里通过 ctx.model.upstreamModel 读取。

# 十、输出要求
1. 只输出一个完整的 .js 文件（放在一个代码块里），不要拆成多个文件，不要省略代码。
2. 文件顶部用注释写明：对接的平台与接口、支持的 endpoint、params 结构、读取的输入字段、已知限制。
3. 注释和错误文案使用中文。
4. 代码后另附一段“配置说明”：渠道 base_url 应该填什么；推荐的 auth 类型；每种 endpoint 建议的 capabilities 示例（JSON：ops、refs、prompt、params）和固定参数 params 示例；哪些地方是根据文档推断的、需要人工试跑确认。
5. 文档里没写清楚的地方不要编造，用注释标出“待确认”，并在“配置说明”里列出。

# 十一、最小示例（同步文本 + 异步视频，仅供对照格式，不要照抄接口）
module.exports = {
  meta: {
    apiVersion: 1,
    key: "demo",
    name: "Demo 平台",
    version: "1.0.0",
    auth: { type: "bearer" },
    allowedHosts: [],
    endpoints: { text: { mode: "sync" }, video: { mode: "async" } }
  },

  buildSubmitRequest: function (ctx) {
    var input = ctx.input || {};
    if (ctx.model.kind === "text") {
      return {
        method: "POST",
        path: "/v1/chat/completions",
        json: { model: ctx.model.upstreamModel, messages: [{ role: "user", content: String(input.prompt) }] },
        timeout: 120
      };
    }
    var body = { model: ctx.model.upstreamModel, prompt: String(input.prompt) };
    if (input.image) {
      body.image = { __fileRef: input.image, as: "url" };
    }
    return { method: "POST", path: "/v1/video/generations", json: body, timeout: 60 };
  },

  parseSubmitResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (ctx.model.kind === "text") {
      var content = body.choices && body.choices[0] && body.choices[0].message && body.choices[0].message.content;
      if (typeof content !== "string" || content === "") {
        return { immediate: { status: "failed", error: { class: "terminal", message: "上游返回了空内容" } } };
      }
      return { immediate: { status: "succeeded", outputs: [{ type: "text", text: content }] } };
    }
    if (!body.task_id) {
      throw new Error("上游没有返回 task_id");
    }
    return { providerTaskId: String(body.task_id) };
  },

  buildQueryRequest: function (ctx) {
    return { method: "GET", path: "/v1/video/generations/" + encodeURIComponent(ctx.task.providerTaskId) };
  },

  parseQueryResponse: function (ctx, resp) {
    var body = resp.body || {};
    if (body.status === "completed" && body.url) {
      return { status: "succeeded", progress: 100, outputs: [{ type: "url", url: body.url, media_type: "video", mime: "video/mp4" }] };
    }
    if (body.status === "failed") {
      return { status: "failed", error: { class: "terminal", message: String(body.error || "上游任务失败") } };
    }
    return { status: body.status === "in_progress" ? "running" : "queued" };
  }
};

# 十二、写完后请自查
- meta.apiVersion 为 1，key 与 version 格式合法，endpoints 里声明的每种 kind 都有对应的提交/解析分支。
- async endpoint 实现了 buildQueryRequest / parseQueryResponse；sync endpoint 的 parseSubmitResponse 返回了 immediate。
- 没有使用 ES6 以上语法、没有 require / 网络 / 定时器；所有返回值可 JSON.stringify。
- 请求里没有手写 Authorization；文件用 __fileRef / fileRef 引用，没有把文件内容内联进请求。
- 产物的域名已写进 allowedHosts（或就在渠道 baseUrl 的主机上）。
- 想让“检查”“导入模型”可用：已实现 buildCheckRequest；已在 meta 里声明 import，且 buildImportRequest / parseImportResponse 成对实现，草稿只含 upstreamModel / kind / label / params。
- 改了代码就升 meta.version（同一 key 下版本不可覆盖），并提醒使用者把渠道切到新版本。
- 失败路径都有明确的 error.class 和中文原因。

# 目标平台 API 文档（在下面粘贴，越完整越好：鉴权方式、提交/查询接口、请求与响应示例、状态枚举、错误格式）
【在此粘贴文档】
`;

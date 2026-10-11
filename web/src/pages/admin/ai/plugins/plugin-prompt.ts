/**
 * “让 AI 帮忙写插件”的提示词。管理员复制后，连同目标平台的 API 文档一起发给 AI，即可得到符合契约的插件 .js。
 * 内容是 backend/docs/plugin-contract.md（契约 v1）的浓缩版 + 一个可对照的最小示例；契约变了这里要同步改。
 * 结构：核心（常用写法，AI 绝大多数插件只需要这一部分）→ 附录“进阶写法”（遇到再看）→ 文末粘贴 API 文档的位置。
 * 核心部分的长度有上限（见测试），新增内容优先放附录；设计依据见 docs/design/插件简化/插件简化.md §6.4。
 * 注意：文本里不要出现反引号与 ${，它们会破坏模板字符串。
 */
export const PLUGIN_AUTHORING_PROMPT = `你是一名资深后端工程师。请根据我在文末提供的“目标平台 API 文档”，为 video-canvas 编写一个【协议插件】（单个 JavaScript 文件）。
插件的职责只有两件：把任务翻译成 HTTP 请求描述、把上游响应翻译成统一结果。发请求、鉴权、轮询、重试、转存、计费、并发控制都由宿主完成。

# 零、先检查信息是否完整（必须执行）
开始写插件代码前，先按本次需求检查必要信息是否齐全：目标 API 的 base_url 与鉴权方式、提交接口和请求字段、同步/异步模式、成功与失败响应、查询接口与状态字段、文件上传方式（如果输入含素材）、输出结果字段，以及一个不产生生成任务和费用的连通性检查请求（方法、路径、鉴权和成功响应）。工作流类 API 还要有节点 ID、fieldName、可选 fieldData 和字段可接受值，或有可调用的只读接口来取得这些信息。
- 本提示词生成的模型/工作流插件默认必须支持连通性检查和模型导入：必须确认一个廉价、只读的检查请求，以及模型列表、工作流详情、节点查询或固定模型导入方案。导入方案要能确定 upstreamModel、kind、label 和 buildSubmitRequest 所需的 params；不能只返回展示名。
- 导入是固定模型或固定工作流时，仍需设计完整的导入闭环：声明 meta.import，成对实现 buildImportRequest / parseImportResponse，并返回写死但可提交的模型草稿。buildImportRequest 可以复用同一个只读检查请求，但绝不能提交生成任务来“探测”模型。
- 如果文档没有给出安全的只读检查或导入请求，先向用户询问所需信息；不得用生成接口代替检查/导入，也不得静默省略这两个能力。只有用户明确排除模型导入，或确认目标 API 确实不存在任何可行的只读请求并接受不支持检查，才可以不实现对应能力，并在配置说明中明确限制。
- 关键信息缺失、文档互相矛盾，或无法从已提供文档和只读查询结果确认时，信息不完整时先向用户提问，不要猜接口路径、节点 ID、字段名、字段值或返回格式。每轮优先只问一个最关键的问题；收到回答后重新检查剩余条件，再继续提问。
- 在必要信息不完整时，不得输出插件代码、伪代码或声称插件已经可用。只有本次范围内的必要条件都已确认，或用户明确决定排除某项能力后，才生成完整插件；推断但未验证的内容仍须标成“待确认”，不能冒充已验证事实。检查通过前不得输出插件代码。

# 一、运行环境
- 插件是一个 CommonJS 文件，以 module.exports = { meta: {...}, 钩子函数... } 导出。
- 运行在 goja，ES2020 的常见语法都能用：const / let、箭头函数、模板字符串、?.、??、解构、展开、class。钩子不能是 async 函数，也不能返回 Promise；没有 import / export、require、网络、文件、定时器；不能调用 eval / Function。全局只有 module、exports、utils 和标准内置对象（JSON、Math、Date、Object、Array）。
- 钩子必须是纯函数：同样输入同样输出，不依赖全局可变状态和 this（互相调用直接调文件里的普通函数）；返回值要能 JSON.stringify；单次限时 200ms，文件 ≤512KB。
- 不要写死密钥：鉴权由宿主按渠道 Key 注入（meta.auth）。

# 二、meta
module.exports.meta 示例：
{
  apiVersion: 1,                    // 固定为 1
  key: "my-platform",               // 小写字母/数字/连字符，以字母或数字开头，≤30 字符
  name: "我的平台",
  version: "1.0.0",                 // 可以不写或不改：上传时服务端自动决定最终版本号
  auth: { type: "bearer" },         // none / bearer / header(需 name) / query(需 name)；自己签名的见附录
  endpoints: {                      // 键是模型种类 text / image / video / audio；一个插件只能写一种，平台有多种类型就分开写多个插件
    image: { mode: "async" }        // sync：一次请求直接出结果；async：先提交拿到任务 id，再轮询
  },
  import: { args: {} }              // 必填：声明“从渠道导入模型”；args 为空对象表示不需要参数
}
- 每个 endpoint 都要实现 buildSubmitRequest 与 parseSubmitResponse；async 的还要实现 buildQueryRequest 与 parseQueryResponse。轮询节奏 poll、渠道设置项 channelSettings 可选，一般不写，见附录。

# 三、钩子
buildSubmitRequest(ctx)              必须。返回请求描述
parseSubmitResponse(ctx, resp)       必须。sync 返回 { immediate: 统一结果 }（status 只能是 succeeded 或 failed）；async 返回 { providerTaskId: "上游任务id" }
buildQueryRequest(ctx)               async 必须。用 ctx.task.providerTaskId 组装轮询请求
parseQueryResponse(ctx, resp)        async 必须。返回统一结果
buildCheckRequest(ctx)               模型/工作流插件必须实现。连通性检查，任意 2xx 算通：返回一个廉价、只读、不产生费用的请求（如列模型），不要提交生成任务；只能用 ctx.channel、ctx.credentials、ctx.now
buildImportRequest(ctx, args) / parseImportResponse(ctx, resp, args)   必须成对实现并配合 meta.import；parse 返回 [{ upstreamModel, kind, label, params? }]
- 可选钩子（准备请求、取消、错误分类）见附录。

# 四、ctx 与用户输入
ctx = {
  task:    { id, providerTaskId, state },
  model:   { key, kind, upstreamModel, params },   // params：运营在模型上填的固定参数，结构由你自己约定并读取，请在文件顶部注释里写清
  input:   { prompt, op, images, videos, audios, ...生成参数 },
  channel: { baseUrl, settings },
  now      // Unix 秒
}
- ctx.input 由宿主按运营配置的模型能力校验后传入：prompt（提示词）、op（生成方式 t2v / i2v / omni / t2i / i2i，仅视频和图片）、运营起了名字的生成参数（如 duration、aspect_ratio、resolution，已按类型规范化）、参考素材数组 images / videos / audios（每项是文件引用字符串 "input:images.0"）；文本模型另有 system 与 max_tokens。没开放、没填的可选键没有。请在文件顶部注释里列出读取了哪些键、对应上游的哪个字段。
- 用户输入优先于 params 里的固定值。积分、并发、生成数量都由宿主处理：插件不要处理积分、不要自己限流排队，也不要让上游一次出多个结果（一个任务只对应一个结果）。按秒计费的视频模型有个 duration 参数（秒），传给上游的时长字段即可。

# 五、请求描述（build* 的返回值）
{
  method: "POST",                   // GET / POST / PUT / PATCH / DELETE
  path: "/v1/xxx",                  // 相对渠道 baseUrl，必须以 / 开头；与 url 二选一（绝对地址 url 见附录）
  query: { a: 1, b: "x" },          // 值只能是字符串/数字/布尔，null 与 undefined 会被忽略
  headers: { "X-Foo": "bar" },      // 禁止 Authorization、Cookie、Host 等；鉴权由宿主注入
  json: { ... },                    // 请求体；与 form / multipart（见附录）三选一，都不写表示无请求体
  responseType: "json",             // json(默认) / text / binary（见附录）
  timeout: 30                       // 秒，默认 30，上限 120
}
- 文件引用：json 里任意位置可以写 { __fileRef: ctx.input.images[0], as: "url" }，宿主替换成自有存储的签名地址（as 还可取 base64 / dataUrl，上游明确要求时才用）。引用字符串取 ctx.input 里的原值，不要自己拼。请求体 ≤1MB。

# 六、响应对象（parse* 的第二个参数）
{ status: 200, headers: { ... }, body: <解析后的 JSON，解析失败为 null>, text: "原始文本" }
- 非 2xx 不会进入 parse*（宿主按默认规则处理：429 与 5xx 重试，其余终止），所以 parse* 里可以假设 2xx；上游用 HTTP 200 + 错误码表达失败时，要自己识别并返回 failed。字段可能缺失、类型可能是字符串或数字，先判断再用。

# 七、统一结果（parseQueryResponse 的返回值，也是 immediate 的格式）
最简写法：
  成功：{ status: "succeeded", outputs: ["https://结果文件地址"] }
  失败：{ status: "failed", error: "给用户看的失败原因" }
  进行中：{ status: "running" }（或 "queued"，可带 progress: 0–100）
- outputs 里写结果文件的 URL 字符串（文本模型写正文字符串），文件可以放在任意公网域名，不需要声明域名；完整对象写法是 { type: "url", url, media_type: "video", mime: "video/mp4" }。只支持 URL 产物，上游只给 base64 时返回 failed 并说明原因。
- 文本模型请带上实际 Token 用量：{ type: "text", text: "...", usage: { input_tokens: 120, output_tokens: 800 } }（OpenAI 风格取 usage.prompt_tokens / completion_tokens），按 Token 计费的模型据此多退少补，不带就按冻结额全扣。
- error 写字符串即可，默认当作终止失败；要区分类型时写 { class, code?, message }，class 只能是 retryable / terminal / moderation / provider_balance。
    - 上游因为“并发已满 / 系统繁忙 / 限流”拒绝提交，一定要用 retryable：宿主会退避重试；用 terminal 会让任务直接失败并退款。
  - 参数错误、鉴权失败才用 terminal；内容审核不通过用 moderation，余额不足用 provider_balance。
- async：未知的中间状态当 queued 或 running 继续等待，不要当失败；上游明确返回“失败/取消/过期”才返回 failed，并带上上游给的原因。
- 写了就要写对：status 只能是 queued / running / succeeded / failed（不要写 SUCCESS、completed），class 只能是上面四个之一。

# 八、导入模型
parseImportResponse 返回的每个草稿：{ upstreamModel: "上游模型名", kind: "text|image|video|audio", label: "给人看的名字", params: {} }。生成方式、参考素材、生成参数等能力由运营在后台配置，插件不声明。
- 上游有模型列表接口就从响应生成草稿；只有单一固定模型时写死在插件里，也要返回完整的固定草稿：buildImportRequest 返回一个廉价只读请求（可复用 buildCheckRequest 的），parseImportResponse 忽略响应、直接返回写死的草稿。导入阶段绝不能调用生成接口。
- 上游模型名是分组 id / 工作流 id 时，放进 upstreamModel，在 buildSubmitRequest 里通过 ctx.model.upstreamModel 读取。工作流类、不同档位是不同模型 id 等复杂情况见附录。

# 九、输出要求
1. 只输出一个完整的 .js 文件（放在一个代码块里），不要拆分，不要省略代码。
2. 文件顶部用中文注释写明：对接的平台与接口、支持的 endpoint、params 结构、读取的输入字段、已知限制；注释和错误文案都用中文。
3. 代码后另附一段“配置说明”：渠道 base_url 应该填什么；推荐的 auth 类型；每种 endpoint 建议的 capabilities 示例和固定参数 params 示例；哪些地方是根据文档推断的、需要人工试跑确认。
4. 文档里没写清楚的地方不要编造，用注释标出“待确认”，并在“配置说明”里列出。
5. 规则必须从协议契约和本次目标 API 文档推导，保持平台无关；不要为某个供应商、某个工作流编号、某个节点编号增加特例。

# 十、最小示例（异步图片，仅供对照格式，不要照抄接口；同步接口把提交响应直接写成 { immediate: 统一结果 }）
module.exports = {
  meta: { apiVersion: 1, key: "demo", name: "Demo 平台", auth: { type: "bearer" }, endpoints: { image: { mode: "async" } }, import: { args: {} } },

  // 连通性检查与导入：都只能是只读请求；这里模型写死，导入只是复用检查请求
  buildCheckRequest: (ctx) => ({ method: "GET", path: "/v1/models" }),
  buildImportRequest: (ctx, args) => ({ method: "GET", path: "/v1/models" }),
  parseImportResponse: (ctx, resp, args) => [{ upstreamModel: "demo-image", kind: "image", label: "Demo 图片", params: {} }],

  buildSubmitRequest: (ctx) => {
    const input = ctx.input || {};
    const body = { model: ctx.model.upstreamModel, prompt: String(input.prompt) };
    if (input.images && input.images.length) body.image = { __fileRef: input.images[0], as: "url" };  // 参考图
    if (input.aspect_ratio) body.aspect_ratio = input.aspect_ratio;
    return { method: "POST", path: "/v1/images/generations", json: body, timeout: 60 };
  },
  parseSubmitResponse: (ctx, resp) => {
    const body = resp.body || {};
    if (!body.task_id) throw new Error("上游没有返回 task_id");
    return { providerTaskId: String(body.task_id) };
  },
  buildQueryRequest: (ctx) => ({ method: "GET", path: "/v1/images/tasks/" + encodeURIComponent(ctx.task.providerTaskId) }),
  parseQueryResponse: (ctx, resp) => {
    const body = resp.body || {};
    if (body.status === "done" && body.url) return { status: "succeeded", outputs: [body.url] };
    if (body.status === "failed") return { status: "failed", error: body.error || "上游任务失败" };
    return { status: body.status === "processing" ? "running" : "queued" };
  }
};

# 十一、写完后请自查
- meta.apiVersion 为 1，endpoints 只声明一种 kind（一个插件只管一种类型），提交/解析都按这一种写；async 实现了 query 两个钩子；sync 的 parseSubmitResponse 返回了 immediate。
- 没有 async / Promise / require / 网络 / 定时器；请求里没有手写 Authorization；文件用 __fileRef / fileRef 引用，没有内联文件内容。
- 读取的是 ctx.input.images / videos / audios 数组；没有让上游一次出多个结果；文本产物带了 usage；没有处理积分或生成数量。
- 连通性检查与模型导入是默认交付内容：已实现只读的 buildCheckRequest；meta.import 已声明，buildImportRequest / parseImportResponse 成对实现；检查和导入都没有提交生成任务。
- 失败路径都有明确的失败原因（中文）；上游限流、繁忙用 retryable。
- 版本号可以不改，上传时自动处理。
- 生成前的信息检查已完成；若有必要信息缺失，先逐项向用户提问并等待回答，不能提前生成代码。

# 附录：进阶写法（遇到再看，大部分插件用不到）
## A1 准备请求：提交前要先发的请求（如先上传文件）
- buildPrepareRequests(ctx) 返回请求描述数组（≤8 个），宿主依次执行；parsePrepareResponses(ctx, resps) 返回 prepared（任意 JSON），宿主可能在后续提交中放入 ctx.prepared。二者必须成对实现。
- 若使用它们，buildSubmitRequest 必须先判断 ctx.prepared 是否存在且结构正确，再读取其中的值，并在缺失时返回清楚的中文错误或使用可行的直接文件引用；不得因为实现了准备钩子就无条件解引用 ctx.prepared。能直接使用 __fileRef 时，不要为了上传而增加准备阶段。
## A2 请求体与响应的其他形态
- form: { k: "v" } 发 application/x-www-form-urlencoded；multipart: { fields: { k: "v" }, parts: [ { name: "file", fileRef: "input:images.0", filename: "a.png" } ] } 流式上传文件（上游要求 multipart 上传时才用，fileRef 取 ctx.input.images 里的原值）；form / json / multipart 三选一。
- responseType 为 binary（如语音合成直接返回音频字节）时，宿主把响应体写入素材存储，parse 钩子拿到的 resp 是 { status, headers, asset: { id, mime, size } }，此时产物写 { type: "asset" } 即可。
- 上游字段接受 URL 字符串时，优先在最终提交请求的嵌套 JSON 字段里直接放 { __fileRef: ctx.input.images[0], as: "url" }；工作流节点的字段值、数组元素和对象属性同样可以嵌套文件引用。不要把上传结果或素材 ID 猜成 URL。base64 / dataUrl 会把文件读进内存（上限 10MB）。
## A3 绝对地址 url
- 请求描述可以用 url 代替 path，写任意公网 http/https 地址（如上游返回的预签名上传地址），但只有和渠道 baseUrl 的协议、域名、端口都一致时，宿主才会注入 Key，其他地址不会带 Key；需要平台 Key 的接口请用 path。
## A4 鉴权的其他写法
- 请求级 auth: { type: "query", name: "apiKey" } 只为这一次请求换注入方式（bearer / header / query / none），Key 仍由宿主注入。
- meta.auth.type 为 custom（插件自己签名）时，Key 只在渠道开启“允许插件读取 Key”后才出现在 ctx.credentials；可用 utils.hmacSHA256(key, msg)、utils.sha256(s)、utils.jwtSignHS256(claims, secret)、utils.base64(s)、utils.base64URL(s)、utils.base64Decode(s)、utils.uuid()、utils.unixNow()、utils.log(msg)（只进试跑追踪，单次最多 50 条）。
## A5 其他可选钩子与字段
- buildCancelRequest(ctx)：取消任务，没有就走软取消。
- classifyError(ctx, resp)：上游非 2xx 时先调它，返回 { class, code?, message? } 或 null（null 走宿主默认规则）。
- state：parseSubmitResponse / parseQueryResponse 可返回 state（≤64KB，私有状态），后续每个钩子的 ctx.task.state 会带回；省略表示不变。
- providerCost：统一结果里可带 providerCost（数字，仅写对账日志，不影响扣费）。
- poll: { firstDelay: 10, interval: 5, maxInterval: 15, jitter: 0.2 }：异步轮询节奏（秒），不写用默认值。
- channelSettings: { region: { type: "enum", label: "区域", options: ["cn", "global"], default: "cn" } }：渠道上的非敏感设置，渲染成表单，值在 ctx.channel.settings 里；type 为 string / number / boolean / enum，label 必填，enum 必须有 options 且 default 在其中；import.args 的写法相同。
## A6 导入的进阶写法
- 一个模型的不同清晰度/档位在上游是不同的模型 id 时（如 1K、2K、4K 各一个 id），不要拆成多个草稿：按系列导出一个草稿，把“档位 → 上游 id”对照表放进 params（字段名自己定，如 { groups: { "1K": "…", "4K": "…" } }），在 buildSubmitRequest 里按 ctx.input.resolution 查表；用户选了表里没有的档位就抛错并列出可选档位。
- paramHints（可选，在草稿里和 params 并列）：告诉平台这个模型的生成参数建议配成什么，平台在导入那一刻覆盖默认模板里的同名参数，之后运营可以随意改。键必须是模板里的参数名（视频：aspect_ratio / resolution / duration / generate_audio / count；图片：aspect_ratio / resolution / count），对不上的会被忽略；不能新增参数。每个参数可以给：enum 的 options（最多 20 个）与 default；number 的 min、max、step、default（0–3600 的整数）；boolean 的 default；任意类型的 open（是否开放给用户）、remove: true（模型没有这一项，去掉）。例：{ resolution: { options: ["2K", "4K"], default: "2K" }, generate_audio: { remove: true } }。可选值要和 params 里的对照表一致（从同一张表生成）。
## A7 工作流 / 节点式 API
- 对任何节点式或工作流式 API，params 应保存输入键到节点的映射，例如 params.nodes.prompt = { nodeId: "节点标识", fieldName: "文本字段" }；固定工作流值放 params.fixedNodes，并在上游要求时一并保留 fieldData。buildSubmitRequest 必须按文档要求生成节点列表字段（字段名可能是 nodeInfoList 或其它名称）；提示词输入必须绑定到工作流节点。映射缺失或节点列表为空时，在本地以清楚的中文错误终止，不得提交空节点列表。
- 工作流导入时，parseImportResponse 必须把识别出的节点映射写入返回草稿的 params.nodes（如 prompt、duration 等实际输入映射），并补上必要的 params.fixedNodes；若节点信息不足以可靠识别必需映射，应先询问用户，不得返回空 params 假装配置完成。能读取到 fieldData 且上游需要它时，应保留在映射中。
- 自动生成的映射要与 buildSubmitRequest 实际读取的 params 结构完全一致，并在返回前检查必需节点存在；找不到必需节点时返回明确错误，不生成无法提交的模型草稿。提交请求里工作流节点字段接受媒体 URL 时，可在 fieldValue 等嵌套值中直接放 __fileRef 对象，不能假定导入阶段或准备阶段会把文件结果自动带入。
## A8 并发与生成数量的细节
- 宿主已经在渠道层面限制了“同时在上游生成的任务数”（运营在渠道里配“最大同时生成数”），超出的任务会在平台排队，不会打到上游。
- 用户选生成 N 个时，宿主拆成 N 个独立任务，每个任务各调一次插件；拆成多个任务时宿主会给每个任务放一个随机的 input.seed（模型自己配了 seed 参数时用用户的取值），上游支持随机种子就把它传过去，避免 N 个结果一模一样。

# 目标平台 API 文档（在下面粘贴，越完整越好：鉴权方式、提交/查询接口、请求与响应示例、状态枚举、错误格式）
【在此粘贴文档】
`;

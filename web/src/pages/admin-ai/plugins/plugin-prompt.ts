/**
 * “让 AI 帮忙写插件”的提示词。管理员复制后，连同目标平台的 API 文档一起发给 AI，即可得到符合契约的插件 .js。
 * 内容是 backend/docs/plugin-contract.md（契约 v1）的浓缩版 + 一个可对照的最小示例；契约变了这里要同步改。
 * 注意：文本里不要出现反引号与 ${，它们会破坏模板字符串。
 */
export const PLUGIN_AUTHORING_PROMPT = `你是一名资深后端工程师。请根据我在文末提供的“目标平台 API 文档”，为 video-canvas 编写一个【协议插件】（单个 JavaScript 文件）。
插件的职责只有两件：把任务翻译成 HTTP 请求描述、把上游响应翻译成统一结果。发请求、鉴权、轮询、重试、转存都由宿主完成。

# 零、先检查信息是否完整（必须执行）
开始写插件代码前，先按本次需求检查必要信息是否齐全：目标 API 的 base_url 与鉴权方式、提交接口和请求字段、同步/异步模式、成功与失败响应、查询接口与状态字段、文件上传方式（如果输入含素材）、输出结果字段与结果域名，以及一个不产生生成任务和费用的连通性检查请求（方法、路径、鉴权和成功响应）。工作流类 API 还要有节点 ID、fieldName、可选 fieldData 和字段可接受值，或有可调用的只读接口来取得这些信息。
- 本提示词生成的模型/工作流插件默认必须支持连通性检查和模型导入：必须确认一个廉价、只读的检查请求，以及模型列表、工作流详情、节点查询或固定模型导入方案。导入方案要能确定 upstreamModel、kind、label 和 buildSubmitRequest 所需的 params；不能只返回展示名。
- 导入是固定模型或固定工作流时，仍需设计完整的导入闭环：声明 meta.import，成对实现 buildImportRequest / parseImportResponse，并返回写死但可提交的模型草稿。buildImportRequest 可以复用同一个只读检查请求，但绝不能提交生成任务来“探测”模型。
- 如果文档没有给出安全的只读检查或导入请求，先向用户询问所需信息；不得用生成接口代替检查/导入，也不得静默省略这两个能力。只有用户明确排除模型导入，或确认目标 API 确实不存在任何可行的只读请求并接受不支持检查，才可以不实现对应能力，并在配置说明中明确限制。
- 关键信息缺失、文档互相矛盾，或无法从已提供文档和只读查询结果确认时，信息不完整时先向用户提问，不要猜接口路径、节点 ID、字段名、字段值或返回格式。每轮优先只问一个最关键的问题；收到回答后重新检查剩余条件，再继续提问。
- 在必要信息不完整时，不得输出插件代码、伪代码或声称插件已经可用。只有本次范围内的必要条件都已确认，或用户明确决定排除某项能力后，才生成完整插件；推断但未验证的内容仍须标成“待确认”，不能冒充已验证事实。检查通过前不得输出插件代码。

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
  import: { args: {} },             // 模型/工作流插件默认必填：声明“从渠道导入模型”；args 为空对象表示不需要参数
  poll: { firstDelay: 10, interval: 5, maxInterval: 15, jitter: 0.2 }   // 可选：异步轮询节奏（秒）
}
规则：
- auth.type 为 header / query 时必须写 auth.name（请求头名 / 查询参数名）。
- 每个 endpoint 都必须实现 buildSubmitRequest 与 parseSubmitResponse；mode 为 async 的还必须实现 buildQueryRequest 与 parseQueryResponse。
- 实现了 buildPrepareRequests 必须同时实现 parsePrepareResponses；buildImportRequest 与 parseImportResponse 必须成对出现。
- channelSettings / import.args 每项：type 为 string / number / boolean / enum；label 必填；enum 必须有 options，且 default 必须在 options 内；名字匹配 ^[A-Za-z_][A-Za-z0-9_]{0,31}$。
- 本提示词生成的模型/工作流插件必须同时具备三项：meta.import、buildImportRequest / parseImportResponse 成对钩子、buildCheckRequest。只写钩子不写 meta.import，管理端会判定“不支持导入”并置灰按钮；不实现 buildCheckRequest，管理端会提示“插件不支持连通性检查”。
- buildCheckRequest 必须返回廉价、只读、不产生费用的请求；任意 2xx 算通，检查响应无需进入生成流程。它只能依赖检查钩子提供的 ctx.channel、ctx.credentials、ctx.now，不能读取任务输入或假定有 model。
- channelSettings / import.args 的 enum 选项 options 是字符串数组（["cn", "global"]）；模型 capabilities.params 里 enum 的 options 是字符串或数字数组，写法类似。
- 只声明目标平台真实支持的 endpoint，不要为了凑数声明用不上的种类。
- 如果已知固定节点映射或能通过只读 API 查询节点，模型导入应自动返回必要的 params；不要让导入草稿只包含展示名和 upstreamModel。导入草稿的 params 应和 buildSubmitRequest 实际读取的数据结构一致。

# 三、钩子一览
buildPrepareRequests(ctx)            可选。提交前要先发的请求（如先上传文件），返回请求描述数组（≤8 个）
parsePrepareResponses(ctx, resps)    可选。返回 prepared（任意 JSON），宿主可能在后续提交中放入 ctx.prepared；提交代码不得无条件假定它存在
buildSubmitRequest(ctx)              必须。返回请求描述
parseSubmitResponse(ctx, resp)       必须。返回 { providerTaskId?, state?, immediate? }
buildQueryRequest(ctx)               async 必须。用 ctx.task.providerTaskId 组装轮询请求
parseQueryResponse(ctx, resp)        async 必须。返回统一结果
buildCancelRequest(ctx)              可选。没有就走软取消
classifyError(ctx, resp)             可选。上游非 2xx 时先调它，返回 { class, code?, message? } 或 null（null 表示走宿主默认规则）
buildCheckRequest(ctx)               模型/工作流插件必须实现。连通性检查，任意 2xx 算通。请返回一个廉价、只读、不产生费用的请求（如列模型、查询详情或读取一条历史记录），不要提交生成任务；宿主自动注入 Key
buildImportRequest(ctx, args) / parseImportResponse(ctx, resp, args)   模型/工作流插件必须成对实现并配合 meta.import。导入模型，parse 返回 [{ upstreamModel, kind, label, params?, paramHints? }]（模型能力由运营在后台配置；paramHints 是可选的生成参数预填建议，见第九节补）
- 钩子之间互相调用请直接调用文件里的普通函数，或写 module.exports.xxx(ctx)；不要依赖 this（宿主调用钩子时 this 不一定指向 module.exports）。

# 四、ctx（每次调用新建，是副本）
{
  task:    { id, providerTaskId, state },           // providerTaskId 提交前为空；state 是插件上次返回的私有状态
  model:   { key, kind, upstreamModel, params },    // params：运营在模型上填的固定参数，结构由插件自己约定并读取
  input:   { prompt: "...", op: "i2v", images: ["input:images.0"], duration: 5 },   // 用户输入（见第九节）；参考素材是文件引用字符串数组，没填的可选键没有
  channel: { baseUrl, settings },
  prepared, credentials, now                        // prepared 可能为空或不存在；credentials 只有 auth.type 为 custom 且渠道开启授权时才有
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
  multipart: { fields: { k: "v" }, parts: [ { name: "file", fileRef: "input:images.0", filename: "a.png" } ] },
  responseType: "json",                  // json(默认) / text / binary
  timeout: 30,                           // 秒，默认 30，上限 120
  auth: { type: "query", name: "apiKey" }   // 可选：只为这次请求换一种注入方式（bearer / header / query / none），Key 仍由宿主注入
}
- 文件引用：json / form / multipart.fields 的任意位置可以写 { __fileRef: "input:images.0", as: "url" }，as 取 url（自有存储签名地址）/ base64 / dataUrl。引用写法就是 ctx.input.images / videos / audios 里的字符串原样（如 { __fileRef: ctx.input.images[0], as: "url" }），只能引用本次任务里已有的素材。
- 当上游字段接受 URL 字符串时，优先在最终提交请求的嵌套 JSON 字段里直接放 { __fileRef: ctx.input.images[0], as: "url" }；工作流节点的字段值、数组元素和对象属性同样可以嵌套文件引用。不要把上传结果或素材 ID 猜成 URL，也不要把文件内容内联成 base64，除非文档明确要求且确实无法使用 URL。
- buildPrepareRequests / parsePrepareResponses 是可选的上传前置流程。若使用它们，buildSubmitRequest 必须先判断 ctx.prepared 是否存在且结构正确，再读取其中的值，并在缺失时返回清楚的中文错误或使用可行的直接文件引用；不得因为实现了准备钩子就无条件解引用 ctx.prepared。能直接使用 __fileRef 时，不要为了上传而增加准备阶段。
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
    { type: "text", text: "...",         // 仅 text 模型使用，≤256KB
      usage: { input_tokens: 120, output_tokens: 800 } },   // 可选但强烈建议：实际 Token 用量，按 Token 计费的模型据此结算
    { type: "asset" }                    // 二进制响应已由宿主落库
  ],
  error: { class: "terminal", code: "xxx", message: "给用户看的原因" },   // failed 时必填
  state: { },                            // 可选；省略表示状态不变
  providerCost: 0.12                     // 可选，仅对账
}
- error.class 只能是：retryable（可重试）/ terminal（终止）/ moderation（内容审核不通过）/ provider_balance（上游余额不足）。
- 上游因为“账号并发已满 / 系统繁忙 / 限流（如 HTTP 429、对应业务错误码）”拒绝提交时，一定要归为 retryable，不要归为 terminal：宿主会退避后重试（提交阶段最多 5 次）；归为 terminal 会直接让任务失败并退款。参数错误、鉴权失败、余额不足、内容审核不通过才是 terminal / provider_balance / moderation。
- 宿主已经在渠道层面限制了“同时在上游生成的任务数”（运营在渠道里配“最大同时生成数”），超出的任务会在平台排队，不会打到上游；插件不需要自己做并发控制或排队。
- text 模型的产物必须是 text；其他模型不能返回 text。url 产物必须是 http/https，且主机在 allowedHosts 或渠道 baseUrl 主机内（所以要把结果文件所在的域名写进 allowedHosts）。
- 只支持 url 与 asset 两种文件产物，不支持内联 base64；上游只给 base64 时，返回 failed 并说明原因。
- 文本产物请带上 usage：从上游响应里取实际 Token 用量（OpenAI 风格是 usage.prompt_tokens / completion_tokens），换成 { input_tokens, output_tokens }。按 Token 计费的模型会先按上限冻结积分，完成后按这个用量多退少补；不带 usage 时按冻结额全扣。
- 一个任务对应画布上的一个节点、一个结果：不要让上游一次生成多张 / 多段（如 n、imageCount 大于 1），否则多出来的结果用户只付了一份钱、画布上也只显示一个。
- sync endpoint：parseSubmitResponse 必须返回 { immediate: 统一结果 }，且 status 只能是 succeeded 或 failed。
- async endpoint：parseSubmitResponse 返回 { providerTaskId: "上游任务id" }（字符串或数字）；之后宿主按 poll 节奏调用 buildQueryRequest / parseQueryResponse，直到 succeeded 或 failed。未知的中间状态一律当作 queued 或 running 继续等待，不要当失败。
- 上游明确返回“失败/取消/过期”才返回 failed，并尽量带上上游给的原因文案。

# 八、utils（宿主注入的同步函数）
utils.uuid()、utils.unixNow()、utils.base64(s)、utils.base64Decode(s)、utils.base64URL(s)、utils.sha256(s)、utils.hmacSHA256(key, msg)、utils.jwtSignHS256(claims, secret)、utils.log(msg)（只进试跑追踪，单次最多 50 条）。
只有目标平台要求自签名（auth.type 为 custom）时才需要用到签名函数。

# 九、模型 params 与用户输入
- ctx.model.params 由你自己约定结构（例如 { extra: {...} } 原样合并进请求体），请在文件顶部注释里写清每个字段的含义。
- 对任何节点式或工作流式 API，params 应保存输入键到节点的映射，例如 params.nodes.prompt = { nodeId: "节点标识", fieldName: "文本字段" }；固定工作流值放 params.fixedNodes，并在上游要求时一并保留 fieldData。buildSubmitRequest 必须按文档要求生成节点列表字段（字段名可能是 nodeInfoList 或其它名称）；提示词输入必须绑定到工作流节点。映射缺失或节点列表为空时，在本地以清楚的中文错误终止，不得提交空节点列表。
- 用户输入由宿主按运营在模型 capabilities 里配置的能力校验后放进 ctx.input，键固定为：prompt（提示词）、op（生成方式 t2v / i2v / omni / t2i / i2i，仅视频、图片）、运营给生成参数起的名字（如 duration、aspect_ratio、resolution，值已按类型规范化）、参考素材数组 images / videos / audios（每项是文件引用字符串 "input:images.0"，用 { __fileRef: ... } 或 multipart 的 fileRef 引用，不是素材 ID）；文本模型另有 system（固定系统提示）与 max_tokens（最大输出）。请在文件顶部注释里列出插件会读取的键，并说明每个键应该对应上游的哪个字段。
- 用户输入优先于 params 里的固定值；params 只做补充。
- 积分由宿主按运营配置的定价计算并冻结、结算，插件不参与计费，也不要在请求或结果里处理积分。
- 「生成数量」由宿主处理：用户选生成 N 个时，宿主拆成 N 个独立任务，每个任务各调一次插件，生成数量这个参数不会出现在 ctx.input 里。拆成多个任务时宿主会给每个任务放一个随机的 input.seed（模型自己配了 seed 参数时用用户的取值）；上游支持随机种子就把它传过去，避免 N 个结果一模一样。
- 按秒计费的视频模型由运营配置一个叫 duration 的数字参数（单位秒），插件把 input.duration 传给上游对应的时长字段。

# 九·补、导入模型
parseImportResponse 返回的每个草稿：{ upstreamModel: "上游模型名（由插件自己解释）", kind: "text|image|video|audio", label: "给人看的名字", params: {}, paramHints: {} }。生成方式、参考素材、生成参数等能力由运营在后台配置，插件不声明；导入时平台按种类套默认能力模板预填编辑器。
- 本提示词生成的模型/工作流插件必须声明 meta.import，并成对实现 buildImportRequest / parseImportResponse；导入钩子的 ctx 只有 channel、credentials、now，args 是 meta.import.args 对应的表单值。若文档显示目标只是单一固定模型，也必须返回一个完整的固定草稿，不能省略导入能力。
- 工作流导入时，parseImportResponse 必须把识别出的节点映射写入返回草稿的 params.nodes（如 prompt、duration 等实际输入映射），并补上必要的 params.fixedNodes；若节点信息不足以可靠识别必需映射，应先询问用户，不得返回空 params 假装配置完成。能读取到 fieldData 且上游需要它时，应保留在映射中。
- 自动生成的映射要与 buildSubmitRequest 实际读取的 params 结构完全一致，并在返回前检查必需节点存在；找不到必需节点时返回明确错误，不生成无法提交的模型草稿。提交请求里工作流节点字段接受媒体 URL 时，可在 fieldValue 等嵌套值中直接放 __fileRef 对象，不能假定导入阶段或准备阶段会把文件结果自动带入。
- 一个模型的不同清晰度 / 档位在上游是不同的模型 id 时（如 1K、2K、4K 各一个 id），不要拆成多个草稿：按系列导出一个草稿，把「档位 → 上游 id」对照表放进 params（字段名自己定，如 { groups: { "1K": "…", "4K": "…" } }），在 buildSubmitRequest 里按 ctx.input.resolution 查表；用户选了表里没有的档位就抛错并列出可选档位。
- paramHints（可选）：告诉平台这个模型的生成参数建议配成什么，平台在导入那一刻覆盖默认模板里的同名参数，之后运营可以随意改，校验和下单都不读它。键必须是模板里的参数名（视频：aspect_ratio / resolution / duration / generate_audio / count；图片：aspect_ratio / resolution / count），对不上的会被忽略；不能新增参数，也不能改 spec、fanout。每个参数可以给：
  - enum：options（可选值，字符串或数字，最多 20 个）、default；
  - number：min、max、step、default（0 – 3600 的整数）；
  - boolean：default；
  - 任意类型：open（是否开放给用户）、remove: true（模型没有这一项，去掉）。
  例：{ resolution: { options: ["2K", "4K"], default: "2K" }, generate_audio: { remove: true } }。可选值要和 params 里的对照表一致（都从插件里同一张表生成），否则用户会选到插件不认识的档位。只有一个档位时用 remove: true 去掉清晰度参数，插件按唯一的档位处理。
- 插件在 buildSubmitRequest 里读到的 ctx.input 见上一节；没开放、没填的可选键没有。
- 上游没有“模型列表”接口时，可以把已知模型或工作流写死在插件里：buildImportRequest 返回一个廉价只读请求（可以复用 buildCheckRequest 的同一只读请求），parseImportResponse 忽略响应，直接返回包含完整 params 的草稿数组；响应不可用也不能改为调用生成接口。只有在没有任何安全只读请求时，才暂停并向用户索要接口信息或明确确认不支持导入/检查。
- 如果上游模型名不是“模型名”而是分组 id / 工作流 id，就把它放进 upstreamModel，在 buildSubmitRequest 里通过 ctx.model.upstreamModel 读取。

# 十、输出要求
1. 只输出一个完整的 .js 文件（放在一个代码块里），不要拆成多个文件，不要省略代码。
2. 文件顶部用注释写明：对接的平台与接口、支持的 endpoint、params 结构、读取的输入字段、已知限制。
3. 注释和错误文案使用中文。
4. 代码后另附一段“配置说明”：渠道 base_url 应该填什么；推荐的 auth 类型；每种 endpoint 建议的 capabilities 示例（JSON：ops、refs、prompt、params）和固定参数 params 示例；哪些地方是根据文档推断的、需要人工试跑确认。
5. 文档里没写清楚的地方不要编造，用注释标出“待确认”，并在“配置说明”里列出。
6. 规则必须从协议契约和本次目标 API 文档推导，保持平台无关；不要为某个供应商、某个工作流编号、某个节点编号或某个响应字段增加特例，也不要把示例中的接口路径当成通用规则。

# 十一、最小示例（同步文本 + 异步视频，仅供对照格式，不要照抄接口）
module.exports = {
  meta: {
    apiVersion: 1,
    key: "demo",
    name: "Demo 平台",
    version: "1.0.0",
    auth: { type: "bearer" },
    allowedHosts: [],
    endpoints: { text: { mode: "sync" }, video: { mode: "async" } },
    import: { args: {} }
  },

  buildCheckRequest: function (ctx) {
    // 只读请求；实际插件必须根据目标平台文档替换路径
    return { method: "GET", path: "/v1/health", responseType: "json" };
  },

  buildImportRequest: function (ctx, args) {
    // 固定模型示例：导入也只能调用只读请求
    return { method: "GET", path: "/v1/models", responseType: "json" };
  },

  parseImportResponse: function (ctx, resp, args) {
    // 固定模型示例；真实插件应从只读响应或已确认的固定配置生成完整 params
    return [{ upstreamModel: "demo-video", kind: "video", label: "Demo 视频", params: {} }];
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
    // 参考图：images 是文件引用数组，取第一张交给上游
    if (input.images && input.images.length) {
      body.image = { __fileRef: input.images[0], as: "url" };
    }
    if (typeof input.duration === "number") {
      body.duration = input.duration;
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
      var output = { type: "text", text: content };
      if (body.usage) {
        output.usage = { input_tokens: body.usage.prompt_tokens || 0, output_tokens: body.usage.completion_tokens || 0 };
      }
      return { immediate: { status: "succeeded", outputs: [output] } };
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
- 连通性检查与模型导入是本次生成的默认交付内容：已实现廉价只读的 buildCheckRequest；已在 meta 里声明 import，且 buildImportRequest / parseImportResponse 成对实现；检查和导入都没有提交生成任务；导入草稿含 upstreamModel / kind / label / params / paramHints，工作流草稿的 params 映射与 buildSubmitRequest 完全一致；paramHints 的参数名是模板里的，可选值和 params 里的档位对照表一致。
- 文件输入已按上游字段要求选择直接 __fileRef、multipart fileRef 或经过校验的 prepared 值；提交代码没有无条件读取可能不存在的 ctx.prepared，也没有把文件内容内联进请求。
- 读取的是 ctx.input.images / videos / audios 数组，不是旧的单个 image 字段；没有让上游一次出多个结果；文本产物带了 usage；没有在插件里处理积分或生成数量。
- 改了代码就升 meta.version（同一 key 下版本不可覆盖），并提醒使用者把渠道切到新版本。
- 失败路径都有明确的 error.class 和中文原因。
- 生成前的信息检查已完成；若有必要信息缺失，先逐项向用户提问并等待回答，不能提前生成代码。

# 目标平台 API 文档（在下面粘贴，越完整越好：鉴权方式、提交/查询接口、请求与响应示例、状态枚举、错误格式）
【在此粘贴文档】
`;

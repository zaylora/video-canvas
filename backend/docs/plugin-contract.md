# 协议插件契约 v1（apiVersion: 1）

> 本文是 [协议插件设计](../../docs/design/协议插件设计.md) 6.2–6.4 的落地细节，也是写插件的人的参考。设计文档讲“为什么”，本文讲“每个 JSON 长什么样”。
> 宿主（`internal/provider/plugin`）、runner（`internal/provider/pluginrunner`）、内置插件（`backend/plugins/*.js`）都以本文为准；改契约先改本文。

## 1. 形态

一个插件 = 一个 CommonJS 风格的 JS 文件，导出 `meta` 和若干钩子：

```js
module.exports = { meta: {...}, buildSubmitRequest(ctx) {...}, parseSubmitResponse(ctx, resp) {...}, ... };
```

- 运行在 goja（ES5.1 + 大部分 ES6，没有 ES 模块、没有 async/await/Promise/定时器/`require`）。
- **没有网络、文件、定时器**。插件只做两件事：把任务翻译成请求描述（`build*`）、把响应翻译成统一结果（`parse*`）。
- 全局只有 `module`、`exports`、`utils`，以及标准内置对象（`JSON`、`Math`、`Date`、`Object`、`Array`…）。`eval` 与 `Function` 构造器被移除，内置对象原型被冻结。
- 钩子必须是**纯函数**：同样的输入给同样的输出；不要依赖全局可变状态（Runtime 会池化复用，也会被随时丢弃）。需要跨调用保留的数据放进返回的 `state`。
- 钩子返回值必须能 `JSON.stringify`（不能有循环引用、函数、`undefined` 之外的不可序列化值）。

## 2. `meta`

```js
meta: {
  apiVersion: 1,
  key: "demo-video",           // 小写字母、数字、连字符，≤30
  name: "Demo 视频",
  version: "1.0.0",           // semver，可以不写：上传时由服务端决定最终登记的版本号（见下）；同一 key 下唯一，登记后不可修改
  description: "...",
  vendor: "demo",             // 可选：厂商 slug（小写字母、数字、连字符），模型弹窗的协议列表显示用
  auth: { type: "bearer" },   // none / bearer / header(需 name) / query(需 name) / custom
  endpoints: { video: { mode: "async", method: "POST", path: "/v1/video/generations" } },  // 只能声明一个 kind：text/video/image/audio（agent 不需要声明，见 admin-ai-api.md）；一个插件只管一种类型，同一平台有多种类型就拆成多个插件
  channelSettings: { region: { type: "enum", label: "区域", options: ["cn","global"], default: "cn" } },  // 协议设置项：在模型的“协议设置”里填，值存在模型配置里（channels[0].protocol.settings），插件里通过 ctx.channel.settings 读取
  import: { args: { } },      // 已弃用：渠道不再绑协议，“拉取模型”由宿主内置的通用请求完成，宿主不再调用导入钩子；旧插件里写了也不报错
  poll: { firstDelay: 10, interval: 5, maxInterval: 15, jitter: 0.2 }   // 可选：异步轮询节奏（秒）
}
```

预检规则（任一不通过就拒绝上传，Issue 的 `path` 精确到字段，如 `meta.endpoints.video.mode`）：

- 能编译并执行到导出；导出对象上有 `meta`；`meta.apiVersion === 1`。
- `key` 匹配 `^[a-z0-9][a-z0-9-]{0,29}$`；`name` 非空。`version` 不是预检项：没写、不是 semver、写成数字都不报问题。
- `auth.type` 是枚举值；`header` / `query` 必须有 `auth.name`（合法头名 / 非空参数名）。
- `endpoints` 恰好一项（**一个插件只能支持一种类型**，声明多个会报 `meta.endpoints`）；键必须是 text/video/image/audio；`mode` 是 sync/async；
  - `async` 必须实现 `buildQueryRequest` 与 `parseQueryResponse`；
  - 任何 endpoint 都必须实现 `buildSubmitRequest` 与 `parseSubmitResponse`；
  - `sync` 的 `parseSubmitResponse` 必须返回 `immediate`（运行时检查，预检只检查钩子存在）。
- 不再有 `allowedHosts`：结果文件可以放在任意公网域名，不需要声明。旧插件里写了这个字段也不报错，宿主直接忽略。
- `vendor`（可选）匹配 `^[a-z0-9][a-z0-9-]{0,63}$`；`endpoints.<kind>.method`（可选）是 GET / POST / PUT / PATCH / DELETE，`endpoints.<kind>.path`（可选）以 `/` 开头、≤200 字符。**这两项只用于管理端协议列表的展示**（“POST /v1/messages”），宿主不据此发请求；缺省时列表显示“—”。
- `channelSettings`（协议设置项）/ `import.args`（已弃用）每项：`type` 是 string/number/boolean/enum；`label` 非空；`enum` 必须有非空 `options`；`default`（若有）类型匹配、enum 的 default 必须在 options 里；名字匹配 `^[A-Za-z_][A-Za-z0-9_]{0,31}$`。
- `poll` 各值非负，`jitter` 在 0–1。
- 成对钩子：实现了 `buildPrepareRequests` 必须同时实现 `parsePrepareResponses`；`buildImportRequest` 与 `parseImportResponse` 必须同时存在；导出的函数名必须是契约认识的钩子（多余的导出函数只警告不拒绝——**这里不拒绝**）。
- 文件 ≤512KB；`meta` 编码后 ≤64KB。
- **版本号由 service 层在上传时决定**（不在 runner），改了代码不用手动升版本号：
  1. 这个 key 还没有版本：`meta.version` 是合法 semver（`1.2.3`，可带 `-rc.1` 后缀，≤32 字符）就用它，否则用 `1.0.0`；
  2. 代码与已有某个版本完全相同（sha256 一样）：拒绝，提示没有产生新版本；
  3. `meta.version` 合法、没被占用、并且比已有最高版本高（按 semver 比较）：用它；
  4. 其他情况（没写、不合法、被占用、比最高版本低）：取已有最高版本，补丁位加 1，预发布后缀去掉（`1.3.1` → `1.3.2`，`2.0.0-rc.1` → `2.0.1`）。

  登记的版本号会同步写回库里的 `meta_json`，代码原文与 sha256 不变。**内置插件**（`backend/plugins/*.js`）不适用：版本号由发版决定，必须写合法的 semver，否则启动登记报错。

## 3. `ctx`

每次钩子调用新建，JSON 传入，插件拿到的是副本。所有字段驼峰。

```jsonc
{
  "task": { "id": 123, "providerTaskId": "", "state": null }, // state：插件上次返回的私有状态；providerTaskId 提交前为空
  "model": {
    "key": "kling-i2v",
    "kind": "video",
    "upstreamModel": "kling-v2-master",
  }, // 模型上没有别的配置：插件需要的输入都来自 input（面板里配置的生成参数），固定行为写在插件代码里
  "input": {
    "prompt": "...",
    "op": "i2v",
    "images": ["input:images.0", "input:images.1"],
    "duration": 5,
  }, // 已按模型 capabilities 校验规范化；键是 prompt、op、运营起的生成参数名，以及参考素材数组 images / videos / audios，数组每项是文件引用字符串 "input:<数组名>.<下标>"。文本模型另有 system（固定系统提示）与 max_tokens（最大输出）
  "channel": { "baseUrl": "https://...", "settings": { "region": "cn" } },
  "prepared": null, // 仅 buildSubmitRequest：准备阶段的结果
  "credentials": { "apiKey": "...", "secretKey": "..." }, // 仅当 meta.auth.type == "custom"（宿主直接提供，不再有渠道级开关）；其余情况没有这个字段。secretKey 只在渠道设置了 Secret Key 时才有
  "now": 1760000000, // Unix 秒
}
```

- `ctx.channel.settings` 的名字沿用（旧插件零改动），但值现在是**模型的协议设置项**（按所选版本的 `channelSettings` 校验并补默认值），不再是渠道上的设置。`ctx.channel.baseUrl` 仍是渠道地址。
- 渠道的**自定义请求头**由宿主自动加到发往 `base_url` 同源的请求上（优先级：宿主默认 UA < 渠道自定义头 < 插件请求描述里的头 < 宿主注入的鉴权），插件不用处理，也读不到。
- `buildCheckRequest` / `buildImportRequest` / `parseImportResponse` 已弃用：渠道不再绑协议，连通性在模型的“测试模型”里验证，拉取模型是宿主内置的 `GET {base_url}/v1/models`。预检仍然接受它们，已上传的插件不受影响，但宿主不再调用。
- `input` 里可选的媒体字段没填就没有该键。

## 4. 请求描述（`build*` 的返回值）

```jsonc
{
  "method": "POST",                      // GET / POST / PUT / PATCH / DELETE
  "path": "/v1/video/generations",       // 相对渠道 base_url，必须以 / 开头；与 url 二选一
  "url": "https://cdn.example.com/x",    // 绝对 URL，任意公网 http/https 地址；只有和 base_url 同源（协议、域名、端口都一致）时宿主才注入 Key；与 path 二选一
  "query": { "a": 1, "b": "x", "c": true },   // 值是字符串 / 数字 / 布尔，null 与 undefined 的键被忽略
  "headers": { "X-Foo": "bar" },
  "json": { ... },                       // 与 form / multipart 三选一，都没有表示无请求体
  "form": { "k": "v" },                  // application/x-www-form-urlencoded
  "multipart": { "fields": { "k": "v" }, "parts": [ { "name": "file", "fileRef": "input:images.0", "filename": "a.png" } ] },
  "responseType": "json",                // json（默认）/ text / binary
  "timeout": 30,                         // 秒，默认 30，上限 120；同步接口（mode: "sync"）的提交请求例外，时限取模型配置的任务超时
  "auth": { "type": "query", "name": "apiKey" }   // 可选：只为这一次请求换一种注入方式（bearer / header(需 name) / query(需 name) / none），Key 仍由宿主注入
}
```

**文件引用**：`json` / `form` / `multipart.fields` 的任意位置可以放 `{ "__fileRef": "input:images.0", "as": "url" }`，宿主替换成：

- `as: "url"`：自有存储的签名 URL（字符串）；
- `as: "base64"`：文件内容的标准 base64（字符串）；
- `as: "dataUrl"`：`data:<mime>;base64,<...>`。

`multipart.parts[].fileRef` 是文件引用（字符串 `"input:<数组名>.<下标>"`，如 `input:images.0`），宿主流式上传文件内容；`filename` 可选。`base64` / `dataUrl` 会把文件读进内存，受 `media_inline_max_bytes`（默认 10MB）限制。引用必须指向 `ctx.input` 里已填写的参考素材；宿主校验素材归属当前任务用户。

**宿主对请求描述的校验**（任一不通过就是 `terminal` + 插件级失败，不发请求）：

- `method` 合法；`path`/`url` 二选一；`path` 以 `/` 开头、不以 `//` 开头、不含 `..` 段、拼出来的主机必须仍是 `base_url` 的主机与协议；
- `url` 只允许 http/https、不含用户名密码、主机不限（内网地址在拨号时一律拦截，没有“允许访问内网”的例外）；
- `headers`：禁止 `Authorization`、`Proxy-Authorization`、`Cookie`、`Host`、`Content-Length`、`Transfer-Encoding`、`Connection`、`Upgrade`（不区分大小写），以及与 `meta.auth.name`（`header` 类型）同名的头；值不得含换行；
- 请求体编码后 ≤1MB（`multipart` 的文件内容不计入）；
- `timeout` 超过上限按上限；`responseType` 合法；
- `auth.type` 只能是 bearer / header / query / none。

**鉴权注入**（宿主在请求描述通过校验后做）：`bearer` → `Authorization: Bearer <key>`；`header` → `<auth.name>: <key>`；`query` → 查询参数 `<auth.name>=<key>`；`custom` / `none` → 宿主不注入。请求级 `auth` 覆盖 `meta.auth`（但 `custom` 插件的请求级 `auth` 同样只能是上面四种之一）。

**Key 只注入同源请求**：目标地址和渠道 `base_url` 的协议、主机名、端口都一致（不区分大小写，缺省端口按协议补齐）时才注入 Key。`path` 形式的请求永远同源；`url` 形式发往别的域名、或同域名不同端口时，宿主不注入 Key（请求照常发出），试跑追踪里会多一条 http 步骤说明“目标不是渠道地址，未注入 Key”。所以预签名上传地址这类自带签名的地址可以直接写成 `url`，上游需要平台 Key 的接口要放在渠道 `base_url` 下。

## 5. 响应对象（`parse*` / `classifyError` 的第二个参数）

```jsonc
{
  "status": 200,
  "headers": { "content-type": "application/json" },   // 键全部小写
  "body": { ... },        // responseType=json 且解析成功时是解析后的值；解析失败为 null；responseType=text 时是字符串
  "text": "..."           // 原始文本（≤1MB，超出截断）；binary 时没有
}
```

- `responseType: "binary"`：宿主不把响应体交给插件，而是流式写入素材存储，响应对象是 `{ status, headers, asset: { id, mime, size } }`；产物在 `outputs` 里写 `{ "type": "asset" }`（宿主用最近一次 binary 响应落库的素材）。
- 宿主的响应体读取上限 5MB（json/text），超出按 `terminal`。
- **非 2xx 状态码不会进入 `parse*` 钩子**：宿主先调 `classifyError(ctx, resp)`（如果实现了），没有实现或返回 null 时用默认规则（见 §8）。所以 `parse*` 里可以放心假设 2xx。上游用 200 + 错误码表达失败的，由 `parseSubmitResponse` / `parseQueryResponse` 返回 `failed`。

## 6. 钩子

| 钩子                                   | 入参                                          | 返回                                                                                               |
| -------------------------------------- | --------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `buildPrepareRequests(ctx)`            | ctx                                           | 请求描述数组（≤8 个）；可选                                                                        |
| `parsePrepareResponses(ctx, resps)`    | ctx，响应对象数组                             | `prepared`（任意 JSON）；放进 `ctx.prepared` 给提交用，并持久化                                    |
| `buildSubmitRequest(ctx)`              | ctx                                           | 请求描述                                                                                           |
| `parseSubmitResponse(ctx, resp)`       | ctx，响应                                     | `{ providerTaskId?, state?, immediate? }`                                                          |
| `buildQueryRequest(ctx)`               | ctx（含 `task.providerTaskId`、`task.state`） | 请求描述；async endpoint 必须实现                                                                  |
| `parseQueryResponse(ctx, resp)`        | ctx，响应                                     | 统一结果                                                                                           |
| `buildCancelRequest(ctx)`              | ctx                                           | 请求描述；可选，没有就走软取消                                                                     |
| `classifyError(ctx, resp)`             | ctx，非 2xx 响应                              | `{ class, code?, message? }` 或 null；可选                                                         |
| `buildCheckRequest(ctx)`               | ctx                                           | **已弃用**，宿主不再调用；可选                                                                  |
| `buildImportRequest(ctx, args)`        | ctx，导入参数                                 | **已弃用**，宿主不再调用；可选                                                                                     |
| `parseImportResponse(ctx, resp, args)` | ctx，响应，导入参数                           | （已弃用）模型草稿数组 `[{ upstreamModel, kind, label, paramHints? }]`（`paramHints` 见下文）；可选 |

**提交流程**：`buildPrepareRequests`（若有且 `ctx.prepared` 还没有）→ 宿主依次执行 → `parsePrepareResponses` → 持久化 prepared → `buildSubmitRequest` → 执行 → `parseSubmitResponse`。准备请求与提交请求走同样的校验、鉴权注入、SSRF、限流。

**`parseSubmitResponse` 的返回**：

- `providerTaskId`（字符串或数字，宿主转成字符串）：异步任务必须有，除非返回了 `immediate`；
- `state`：私有状态，≤64KB，宿主持久化，后续每个钩子的 `ctx.task.state` 里带回；
- `immediate`：与统一结果同形。**提交即出结果**（同步接口，或上游立刻完成 / 立刻失败）。`sync` endpoint 必须返回 `immediate`，且 `immediate.status` 只能是 `succeeded` 或 `failed`。

**统一结果**（`immediate` 与 `parseQueryResponse` 的返回值）：

```jsonc
{
  "status": "queued" | "running" | "succeeded" | "failed",
  "progress": 40,                                   // 可选，0–100
  "outputs": [                                      // succeeded 时必填且非空
    { "type": "url",  "url": "https://...", "media_type": "video", "mime": "video/mp4" },
    { "type": "text", "text": "...",                // 文本正文，≤256KB
      "usage": { "input_tokens": 120, "output_tokens": 800 } },   // 可选：实际 Token 用量。按 Token 计费的模型据此结算（扣 min(实际, 冻结)），不填按冻结额扣
    { "type": "asset" }                             // 二进制响应已由宿主落库
  ],
  "error": { "class": "moderation", "code": "...", "message": "..." },   // failed 时必填；class 只能是 retryable / terminal / moderation / provider_balance
  "state": { },                                     // 可选，≤64KB；省略表示状态不变
  "providerCost": 0.12                              // 可选，只写对账日志
}
```

**简写**：宿主在校验之前先补全几种常见的漏写，完整写法照旧可用。只放宽“漏写”，不放宽“写错”：

| 写法 | 补全成 |
| --- | --- |
| `outputs: ["https://a.com/x.png"]`（非文本模型） | `{ "type": "url", "url": "https://a.com/x.png" }`，`media_type` 取模型种类 |
| `outputs: ["正文"]`（文本模型） | `{ "type": "text", "text": "正文" }` |
| `outputs` 里的对象缺 `type`，有 `url` | `type` 补 `"url"` |
| `outputs` 里的对象缺 `type`，有 `text` | `type` 补 `"text"` |
| `error: "原因"`（非空字符串） | `{ "class": "terminal", "message": "原因" }` |
| `error` 是对象但缺 `class` | `class` 补 `"terminal"` |

不补全、仍按插件故障处理的情况：`status` 不是四个合法值之一（宿主不替插件猜“上游的哪个状态算成功”）；`error.class` 写了但不合法；`error` 是空字符串或空对象；对象既没有 `type` 也没有 `url` / `text`；非文本模型的字符串不是 http/https 地址（含 `data:` 的 base64，本契约不支持内联 base64 产物）。`immediate` 与 `parseQueryResponse` 的返回值走同一套规则。

宿主校验（不合规就按 `terminal` 失败并告警，且算插件级失败）：`status` 是四个值之一；`succeeded` 必须有非空 `outputs`；`url` 产物必须是 http/https（主机不限，内网地址在下载时拦截）；`text` 产物的 `text` 是字符串且 ≤256KB；`media_type` 缺省取模型 kind（text 模型的产物必须是 `text` 类型、其余模型的产物不能是 `text`）；`failed` 必须有 `error`，`error.class` 合法；`progress` 夹到 0–100；`state` ≤64KB。

**导入草稿的参数预填建议（`paramHints`，可选）**：模型能力由运营在后台配置，插件不声明；但导入时插件可以告诉平台“这个模型的某个生成参数建议配成什么”，平台在导入那一刻按种类套默认能力模板后，用它覆盖**模板里同名参数**的取值设置。之后运营可以随意修改，后端校验与下单都不读它。

```jsonc
"paramHints": {
  "resolution":     { "options": ["2K", "4K"], "default": "2K" },   // enum：可选值、默认值
  "duration":       { "min": 4, "max": 15, "step": 1, "default": 5 }, // number：最小 / 最大 / 步长 / 默认
  "generate_audio": { "default": false },                            // boolean：默认值
  "aspect_ratio":   { "open": false },                               // 任意类型：是否开放给用户
  "count":          { "remove": true }                               // 任意类型：模型没有这一项，去掉
}
```

- 键是模板里的参数名（视频：`aspect_ratio / resolution / duration / generate_audio / count`；图片：`aspect_ratio / resolution / count`）；对不上的键被忽略，导入时提示运营。不能新增模板里没有的参数，也不能改 `spec`（规格价格维度）、`fanout`（生成数量）。
- 宿主只校验格式：参数名是小写字母 / 数字 / 下划线；`options` 最多 20 个、每个是非空字符串或数字；`default` 是字符串、数字或布尔；`min / max / step` 是 0 – 3600 的整数。不合规时整次导入按插件故障失败。
- 超出平台固定范围的值不会被自动修正，编辑器里照常标红，运营改完才能发布。

## 7. `utils`（宿主注入的同步函数）

| 函数                                                               | 说明                                                                   |
| ------------------------------------------------------------------ | ---------------------------------------------------------------------- |
| `utils.uuid()`                                                     | 随机 UUID v4 字符串                                                    |
| `utils.unixNow()`                                                  | 当前 Unix 秒                                                           |
| `utils.base64(s)` / `utils.base64Decode(s)` / `utils.base64URL(s)` | 文本 ↔ base64（标准 / URL 安全无填充）；`base64Decode` 返回 UTF-8 文本 |
| `utils.sha256(s)`                                                  | 小写十六进制                                                           |
| `utils.hmacSHA256(key, msg)`                                       | 小写十六进制                                                           |
| `utils.hmacSHA256Hex(hexKey, msg)`                                 | 密钥是十六进制文本（解码成字节再签），输出小写十六进制；上一步 `hmacSHA256` 的输出可直接当下一步密钥，用于逐级派生密钥的签名（火山引擎 V4） |
| `utils.jwtSignHS256(claims, secret)`                               | 紧凑序列化的 JWT（header 固定 `{"alg":"HS256","typ":"JWT"}`）          |
| `utils.jwtSignRS256(claims, pem)`                                  | RS256 JWT（header 固定 `{"alg":"RS256","typ":"JWT"}`），`pem` 是 PKCS#8 或 PKCS#1 的 RSA 私钥（服务账号 JSON 里的 `private_key`） |
| `utils.log(msg)`                                                   | 进试跑追踪，不进普通日志；单次钩子调用最多 50 条，每条截断到 1KB       |

`utils` 里的函数都要自行限制输入大小（单个输入 ≤256KB），因为 `vm.Interrupt()` 中断不了正在执行的 Go 函数。

## 8. 错误与重试

| 场景                                                  | 处理                                                                                                                                                                                                                             |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 钩子抛异常 / 超时被中断 / 返回值不合规 / 请求描述非法 | `terminal`，插件级失败，退积分；异常信息进追踪与告警，不给用户看                                                                                                                                                                 |
| runner 连不上                                         | `retryable`，错误码 `plugin_runner_unavailable`；任务保持 pending 直到恢复（不消耗重试次数）                                                                                                                                     |
| 调用进行中 runner 崩溃                                | `retryable`，错误码 `plugin_runner_crashed`；任务重试一次，再次崩溃就失败退积分                                                                                                                                                  |
| 上游非 2xx                                            | 先 `classifyError`；没实现或返回 null 则用默认规则：响应文本（小写）含 `balance` / `insufficient` / `quota` / `credit` / `余额` / `额度` → `provider_balance`；429 与 5xx → `retryable`；404 / 410 → `terminal`；其余 `terminal` |
| 网络错误 / 连不上上游                                 | `retryable`                                                                                                                                                                                                                      |
| 提交请求已发出但没收到完整响应（读超时等）            | `submit_unknown`（失败并退积分，告警人工核对）                                                                                                                                                                                   |
| 请求被 SSRF 防护拒绝                                  | `terminal`，错误码 `ssrf_blocked`                                                                                                                                                                                                |
| 渠道 Key 未设置                                       | `terminal`，错误码 `secret_unavailable`                                                                                                                                                                                          |

`classifyError` 返回的 `class` 只能是 `retryable / terminal / moderation / provider_balance`；不合规按默认规则处理并告警。

## 9. 沙箱预算（提案值，压测后定）

单次钩子 200ms；`ctx` 与返回值编码后各 ≤1MB；`state` ≤64KB；插件文件 ≤512KB；每个插件版本最多 8 个 Runtime；每次调用最多 50 条 `utils.log`。

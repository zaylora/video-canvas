# AI 管理接口（协议插件设计落地后）

> 路径前缀 `/api/v1/admin/ai`，都需要登录（JWT）。角色：`user` / `admin`（运营）/ `super_admin`（运维）。
> 权限：**读、模型相关的写、导入模型 = `admin` 或 `super_admin`；写插件 / 渠道（上传、启停、删除、建渠道、改渠道、设 Key、连通性检查）= 仅 `super_admin`**。
> `admin` 调用 super_admin 接口返回 403（错误码 10004）；普通用户调用任何管理接口都是 403；没带有效 token 是 401（10003 / 10006）。
> 首个 `super_admin` 只能用 SQL 提升（`UPDATE users SET role = 'super_admin' WHERE ...`），接口不提供提升角色的途径；角色最多缓存 30 秒。

## 响应格式

```jsonc
{ "code": 0, "msg": "success", "data": { }, "request_id": "..." }
```

- 成功时 `code` 为 0、`msg` 固定为 `"success"`，HTTP 200。**没有返回内容的接口（启停、设 Key、删除版本等）整个 `data` 字段都省略**（不是 `data: null`）。
- 出错时 `code` 非 0，`msg` 是给人看的原因，HTTP 状态码由错误码决定（见文末错误码表）；错误响应没有 `data`。
- 列表接口的 `data` 是数组，没有内容时是 `[]`，不会是 `null`。
- 参数绑定 / 格式错误统一是 400 + `10001`，`msg` 是中文原因。

已经删除的旧接口：`/providers*`、`/import/runninghub`、`/secrets*`、`/schema/provider`、`/webhooks/:provider/:secret`。渠道 Key 统一走 `PUT /channels/:key/secret`。

## 当前用户

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/me` | admin | `{ "user_id": 1, "role": "admin" \| "super_admin" }`，前端据此隐藏写操作。`user` 调用得到 403 |

## 插件（super_admin 写，admin 只读）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/plugins` | 插件列表（admin），见下 `PluginView`，按 key 升序，版本新到旧 |
| POST | `/plugins` | 上传插件：`multipart/form-data`，字段 `file`（.js，≤512KB）。**预检不通过也返回 200**：`{ "accepted": false, "issues": [{ "path": "meta.endpoints.video.mode", "message": "..." }], "version": null }`；通过则登记为新版本 `{ "accepted": true, "issues": [], "version": PluginVersionView }`。版本号已存在 → `issues` 里报 `meta.version`（不是 HTTP 错误）。没有 `file` 字段 / 不是 multipart / 空文件 400（10001）；文件超限 413（50007，请求体总大小也有上限，超过同样 413）；往内置插件的 key 上传 409（50006）；runner 不可用 503（50021） |
| PUT | `/plugins/:key/enabled` | `{ "enabled": true }`，停用后所有使用它的渠道不再接新任务（进行中的任务按快照继续）；插件不存在 404（50001） |
| DELETE | `/plugins/:key/versions/:version` | 只能删没有渠道引用、也没有非终态任务引用的版本（否则 409，50005）；内置插件 409（50006）；插件或版本不存在 404（50001）。删除最后一个版本后插件行一并消失 |

```jsonc
// PluginView
{ "key": "newapi", "name": "New API", "source": "builtin" /* builtin | uploaded */, "enabled": true, "updated_at": "...",
  "versions": [ /* 新到旧 */
    { "id": 3, "plugin_key": "newapi", "version": "1.0.0", "sha256": "…", "created_at": "...", "created_by": 0,
      "channel_count": 1,                 // 固定在这个版本上的渠道数
      "meta": { /* pluginmeta.Meta：apiVersion、key、name、version、auth、allowedHosts、endpoints、channelSettings（有序对象）、import、poll；库里 meta 损坏时为 null */ } } ] }
```

响应里永远不含插件代码。`sha256` 是服务端对上传文件自己算的哈希。

内置插件（`backend/plugins/*.js`）在服务启动时按 `key + version` 自动登记为 `builtin`：同一版本重复启动是空操作；版本号没升但代码变了会报错并保持库里的旧代码（版本不可变）；key 已被上传的插件占用会报错、不覆盖。新版本的内置插件随发版登记，渠道仍要显式切换。

## 渠道（super_admin 写，admin 只读；导入 admin 也能调）

```jsonc
// ChannelView
{ "key": "newapi-main", "name": "自建 New API", "plugin_key": "newapi", "plugin_version_id": 3, "plugin_version": "1.0.0",
  "base_url": "https://gw.example.com", "trusted_internal": false, "allow_credentials": false,
  "settings": { "region": "cn" }, "rate_limit": { "rps": 5, "max_concurrency": 20, "max_running": 2 },
  "enabled": true, "secret_set": true,       // Key 只写不读，这里只告诉有没有设置
  "updated_by": 1, "updated_at": "...", "created_at": "..." }
```

`plugin_version` 是渠道固定的插件版本号（semver）；`plugin_version_id` 是版本行 id。`rate_limit` 里 0 表示不限：`rps` / `max_concurrency` 限制发往上游的 HTTP 请求（每秒数、同时数）；`max_running` 限制**同时在上游生成的任务数**（跨实例，由 worker 领取任务时在数据库里执行），超出的任务留在 `pending`（用户看到“排队中”），等有空位再提交；排队超过任务的 `deadline_at` 会置为 `expired`（文案“排队超时，积分已退回”）。任务真正提交给上游时，`deadline_at` 会从提交时刻重新计算一个完整的模型超时。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/channels`、`/channels/:key` | 列表（按 key 升序）/ 详情（admin）；详情渠道不存在 404（50011） |
| GET | `/channels/loads` | 各渠道当前的任务负载（admin）→ `[ { "channel": "yswg", "running": 2, "waiting": 5 } ]`。`running` = 同时生成数（`queued` / `running` + 正在提交的 `pending`），与 `rate_limit.max_running` 比较；`waiting` = 还没调用上游、在等名额的 `pending`；没有未完成任务的渠道不返回，没有任何任务时返回 `[]` |
| POST | `/channels` | 新建（super_admin）：`{ key, name, plugin_key, plugin_version, base_url, trusted_internal?, allow_credentials?, settings?, rate_limit?, enabled? }`。`plugin_version` 是 semver 字符串（固定到这个版本）；`enabled` 不传按 true。key 重复 409（50012）；缺必填字段 400（10001）；业务校验失败 400（50013，所有问题一次报出，原因在 msg）：key 格式（`^[a-z0-9][a-z0-9-]{0,63}$`）、name 非空且 ≤128 字、插件版本存在**且插件启用**、`base_url` 是 http/https、有主机、**无用户名密码、无查询参数与片段**、`settings` 符合插件 `channelSettings`（未声明的名字、类型、enum 取值、必填项；没填的项补默认值）、`rate_limit` 的 `rps` / `max_concurrency` / `max_running` 非负 |
| PUT | `/channels/:key` | 更新（super_admin），字段都可选（不传表示不改）：`name`、`plugin_version`、`base_url`、`trusted_internal`、`allow_credentials`、`settings`（整体替换）、`rate_limit`（整体替换）、`enabled`。插件本身不能换（要换插件请新建渠道），改 `plugin_version` 即“升级插件后切换渠道”：新版本必须存在且插件启用（不切版本时，插件停用不挡其他修改），并且 `settings`（不传则用现有取值）按新版本的 `channelSettings` 重新校验。渠道不存在 404（50011）；校验失败 400（50013） |
| PUT | `/channels/:key/secret` | 设置渠道 Key：`{ "value": "..." }`（去掉首尾空白，≤4096 字节），存 `ai_secrets`，名字 `channel:<key>`，只写、响应无 `data`、不回显。没有配置主密钥 `APP_AI_SECRET_KEY` 时 500 并在 msg 里说明；渠道不存在 404 |
| POST | `/channels/:key/check` | 连通性检查（super_admin）→ `{ "ok": true, "message": "HTTP 200", "duration_ms": 120 }`。上游不通是正常的检查结果 `ok=false`（原因在 message，已脱敏）；插件没实现 `buildCheckRequest` 时 `ok=false, message="插件不支持连通性检查"`；需要宿主注入鉴权（插件 `auth.type` 不是 `none`）而 Key 没设置 409（50015）；runner 不可用 503（50021）；插件本身出错（钩子异常、请求描述非法等）502（50022，msg 已脱敏） |
| POST | `/channels/:key/import` | 从渠道导入模型（admin）：`{ "args": { } }`（取值按插件 `meta.import.args` 校验：未声明 / 类型 / 必填，没有请求体等同空 args）→ `{ "drafts": [ { "upstream_model": "kling-v2", "kind": "video", "label": "...", "params": { }, "param_hints": { "resolution": { "options": ["2K", "4K"], "default": "2K" } } } ] }`（模型能力由运营在后台配置；`param_hints` 是插件给的生成参数预填建议，可省略，只在导入时预填编辑器，见 plugin-contract.md），只预填编辑器，不落库。参数不合法 400（10001）；Key 没设置 409（50015）；插件没实现导入钩子 502（50022）；runner 不可用 503 |

### 审计日志

写插件 / 渠道都写 `ai_audit_logs`（操作人 `actor_id`、时间、目标；`detail_json` 里**不含任何凭证与插件代码**）。写入失败不回滚已提交的变更，只记 Error 日志。

| 动作 | 触发 | detail |
|---|---|---|
| `plugin.upload` | 上传插件版本成功；内置插件首次登记（操作人 0，多一个 `source: "builtin"`） | `version`、`sha256` |
| `plugin.enable` | 启停插件 | `enabled` |
| `plugin.delete` | 删除插件版本 | `version`、`sha256` |
| `channel.create` | 新建渠道 | `plugin_key`、`plugin_version`、`enabled` |
| `channel.update` | 更新渠道且有字段变化 | `fields`（被改的字段名）；切版本时带 `from_version` / `to_version` |
| `channel.secret` | 设置渠道 Key | 无 |
| `channel.trusted` | `trusted_internal` 变化（创建时为 true 也记一条） | `from`、`to` |
| `channel.credential` | `allow_credentials` 变化（创建时为 true 也记一条） | `from`、`to` |
| `channel.enable` | 渠道启停变化 | `from`、`to` |

## 模型（admin 与 super_admin 都能写）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET / POST | `/models` | 列表 / 新建草稿。列表项：`key / kind / label / channel / enabled / sort / published_revision_id / published_revision_no / draft_revision_no / has_unpublished_draft / updated_at`，不含正文。`label`、`channel`（绑定的渠道 key，即 `channels[0].channel`）优先取已发布版本的正文，没发布过取最新草稿的，正文里没有则为空串 |
| GET / PUT | `/models/:key` | 详情（草稿 + 已发布）/ 更新草稿 |
| POST | `/models/:key/validate`、`/publish`、`/rollback` | 校验 / 发布 / 回滚 |
| GET | `/models/:key/revisions`、`/models/:key/revisions/:rid` | 历史 |
| POST | `/models/:key/dry-run` | `{ "input": {} }` → 渲染出**插件返回的请求描述**（宿主校验后的 method / url / query / headers / body 等），不发送。**不含宿主注入后的鉴权头**（Key 在 dry-run 里不解析，鉴权注入处以 `***` 占位），响应经过脱敏，所以不要把它当成“最终请求” |
| POST | `/models/:key/test-run` | `{ "input": {} }` → 试跑任务视图（is_test，不扣积分） |
| GET | `/test-runs/:id` | 试跑任务视图（只能查自己创建的，查不到 404 / 40004） |
| GET | `/test-runs/:id/trace` | 试跑追踪：`{ "steps": [TraceStep] }`（每次钩子的输入 / 输出 / `utils.log`，每次 HTTP 的请求与响应，均已脱敏）；任务还没有追踪时 `steps: []`；归属规则同上，别人的任务 / 不存在 / 非试跑任务统一 404（40004） |
| PUT | `/models/:key/enabled`、`/models/:key/sort` | 上下架 / 排序：`{ "enabled": true }`、`{ "sort": 5 }`；上架要求已发布过 |
| GET | `/schema/model` | 模型配置的 JSON Schema（`/schema/provider` 已删除） |

`dry-run` / `test-run` 请求里的 `use_provider_draft` 字段已废弃（忽略）。

### 模型配置正文（draft / published 的 body）

```jsonc
{
  "key": "kling-i2v", "kind": "video",          // kind：video / image / audio / text
  // hint 最多 500 字；vendor 可省略，小写字母/数字/连字符（前端据此显示厂商 logo）；tags 可省略，最多 5 个、每个最多 12 字、不能重复
  "label": "可灵 图生视频", "hint": "", "vendor": "kling", "tags": ["推荐"], "deadline": "30m", "enabled": false, "sort": 100,
  "channels": [ { "channel": "newapi-main", "upstream_model": "kling-v2-master" } ],   // 首期必须恰好一个
  "params": { "max_tokens": 2000 },               // 可选，固定参数，原样交给插件
  "capabilities": {                               // 模型能力：由运营手填，画布渲染与下单校验的唯一来源（取代旧的 input_schema）
    "ops": ["t2v", "i2v", "omni"],                // 生成方式：video 取 t2v / i2v / omni，image 取 t2i / i2i；text、audio 不填
    "refs": { "image": { "on": true, "max": 4, "max_mb": 10 }, "audio": { "on": false, "max": 0, "max_mb": 0 }, "video": { "on": false, "max": 0, "max_mb": 0 } },
    "prompt": { "max_length": 2000 },
    "params": { "duration": { "type": "number", "label": "视频时长", "open": true, "min": 4, "max": 12, "step": 1, "default": 5, "unit": "秒" } },   // 有序对象，书写顺序就是画布参数面板的显示顺序；参数名会作为任务输入的键传给插件
    "context": { "window": 128000, "output": 8192 },   // 仅 text
    "system": "…"                                       // 仅 text，固定系统提示，不下发给画布
  },
  "pricing": {                                    // 定价：价格一律是整数积分（取代旧的 credits）
    "billing": "per_second",                      // per_call 按次 / per_second 按秒（× 参数 duration）/ token 按 Token（仅 text，用 token.in / token.out，积分 / 百万 Token）
    "per_second": 2,
    "tiers": [ { "on": true, "when": { "resolution": "1080P" }, "unit": 4 } ],   // 规格价格：键是 spec 参数名、op 或 ref_video；条件最多的一条胜出
    "cost": { "on": true, "per_second": 1 }       // 积分成本，仅管理端，不下发给画布
  }
}
```

保存草稿时的跨对象检查（渠道存在且启用、插件启用、插件版本支持该 kind）以 `issues`（路径 `channels[0].channel`）报出，不阻塞保存。

发布（publish / rollback）的前置检查，不满足分别返回：正文无校验问题（400 / 40010）→ 渠道存在（404 / 50011）且启用（409 / 50014）→ 插件存在且启用（400 / 50013 或 409 / 50004）→ 渠道固定的插件版本存在（400 / 50013）→ 插件版本的 `endpoints` 里有这个模型的 `kind`（400 / 40010）→ 渠道用的插件 `auth.type` 不是 `none` 时渠道 Key 已设置（409 / 50015）。

## 面向画布的接口（变化）

- `GET /api/v1/models?kind=video|image|audio|text`（登录即可，不要求管理员）：`kind` 新增 `text`，其他取值 400；返回字段为 `key / kind / label / hint / vendor / tags / capabilities / pricing`（`capabilities` 不含 `system`，`pricing` 不含 `cost`）（`vendor` 无则为空串，`tags` 无则为 `[]`），仍然不含 params / 渠道 / 插件信息。
- `POST /generation-tasks`：请求新增 `node_ids`（长度等于生成数量，第 i 个任务绑定第 i 个节点；只生成 1 个时可以只传 `node_id`），积分按 `pricing` 计算；响应改为 `{ "items": [ { "node_id", "task" } | { "node_id", "error": { "status", "code", "message" } } ] }`，按节点逐项给出，某个节点失败（402 积分不足、429 并发已满……）不影响其它节点。幂等键覆盖全部任务：第 i 个任务用 `Idempotency-Key#i`（i=0 不加后缀）。任务视图新增 `charged_credits`（实际扣的积分，Token 计费可能小于冻结额）。
- `POST /generation-tasks` 的 `kind` 新增 `text`；文本任务成功后 `outputs` 是 `[{ "media_type": "text", "text": "正文" }]`（没有 `asset_id` 与 `url`）。
- `TaskOutput` 新增 `text` 字段，`asset_id` / `url` 在文本产物里不出现。
- 平台回调 `POST /webhooks/:provider/:secret` 已删除。

## 错误码（本文用到的）

| 码 | HTTP | 含义 |
|---|---|---|
| 10001 | 400 | 参数错误（绑定 / 格式 / 校验） |
| 10003 / 10006 | 401 | 未登录 / 登录过期 |
| 10004 | 403 | 没有权限（角色不够） |
| 10002 | 404 | 路由不存在（如已删除的 `/secrets*`） |
| 40004 | 404 | 试跑任务不存在 |
| 40006 | 400 | 示例输入不合法（dry-run / test-run） |
| 40010 / 40011 / 40012 | 400 / 404 / 409 | 模型配置：校验未通过 / 不存在 / 没有可发布的草稿 |
| 50001 | 404 | 插件（版本）不存在 |
| 50002 | 400 | 插件预检未通过（上传接口里以 `accepted=false` 返回，不用这个码） |
| 50003 | 409 | 插件版本号已存在（上传接口里以 `issues` 返回，不用这个码） |
| 50004 | 409 | 插件已停用 |
| 50005 | 409 | 插件版本仍被渠道或进行中的任务使用，无法删除 |
| 50006 | 409 | 内置插件不能被删除或覆盖 |
| 50007 | 413 | 插件文件超过大小限制 |
| 50011 | 404 | 渠道不存在 |
| 50012 | 409 | 渠道 key 已存在 |
| 50013 | 400 | 渠道配置不合法（原因在 msg） |
| 50014 | 409 | 渠道已停用 |
| 50015 | 409 | 渠道 Key 尚未设置 |
| 50021 | 503 | 插件运行时（plugin-runner）暂不可用 |
| 50022 | 502 | 连通性检查 / 导入失败（原因在 msg，已脱敏） |

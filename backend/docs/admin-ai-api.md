# AI 管理接口（协议插件设计落地后）

> 路径前缀 `/api/v1/admin/ai`，都需要登录（JWT）。角色：`user` / `admin`（运营）/ `super_admin`（运维）。
> 权限：**读、模型相关的写、拉取与导入模型、渠道内模型排序 = `admin` 或 `super_admin`；写插件 / 渠道（上传、启停、删除、批量升级模型、建渠道、改渠道、设 Key / Secret Key、渠道排序）= 仅 `super_admin`**。
> `admin` 调用 super_admin 接口返回 403（错误码 10004）；普通用户调用任何管理接口都是 403；没带有效 token 是 401（10003 / 10006）。
> 首个 `super_admin` 只能用 SQL 提升（`UPDATE users SET role = 'super_admin' WHERE ...`），接口不提供提升角色的途径；角色最多缓存 30 秒。

## 响应格式

```jsonc
{ "code": 0, "msg": "success", "data": {}, "request_id": "..." }
```

- 成功时 `code` 为 0、`msg` 固定为 `"success"`，HTTP 200。**没有返回内容的接口（启停、设 Key、删除版本等）整个 `data` 字段都省略**（不是 `data: null`）。
- 出错时 `code` 非 0，`msg` 是给人看的原因，HTTP 状态码由错误码决定（见文末错误码表）；错误响应没有 `data`。
- 列表接口的 `data` 是数组，没有内容时是 `[]`，不会是 `null`。
- 参数绑定 / 格式错误统一是 400 + `10001`，`msg` 是中文原因。

已经删除的旧接口：`/providers*`、`/import/runninghub`、`/secrets*`、`/schema/provider`、`/webhooks/:provider/:secret`，以及渠道绑协议时代的 `POST /channels/check-draft`、`POST /channels/:key/check`、`POST /channels/:key/import`（渠道不再有协议，连通性在模型的“测试模型”里验证，拉取模型改走 `POST /channels/:key/pull-models`）。渠道 Key 统一走 `PUT /channels/:key/secret`。

## 当前用户

| 方法 | 路径  | 权限  | 说明                                                                                          |
| ---- | ----- | ----- | --------------------------------------------------------------------------------------------- |
| GET  | `/me` | admin | `{ "user_id": 1, "role": "admin" \| "super_admin" }`，前端据此隐藏写操作。`user` 调用得到 403 |

## 插件（super_admin 写，admin 只读）

| 方法   | 路径                              | 说明                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| ------ | --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| GET    | `/plugins`                        | 插件列表（admin），见下 `PluginView`，按 key 升序，版本新到旧                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| POST   | `/plugins`                        | 上传插件：`multipart/form-data`，字段 `file`（.js，≤512KB）。**预检不通过也返回 200**：`{ "accepted": false, "issues": [{ "path": "meta.endpoints.video.mode", "message": "..." }], "version": null }`；通过则登记为新版本 `{ "accepted": true, "issues": [], "version": PluginVersionView }`。**版本号由服务端决定**：代码里的 `meta.version` 合法、没被占用且比已有最高版本高就直接用，否则（没写、不是 semver、被占用、比最高版本低）取已有最高版本补丁位加 1（预发布后缀去掉），新插件没写就是 `1.0.0`；登记的版本号会同步写回库里的 `meta_json`，代码原文和 sha256 不变。版本号被调整时响应多一个 `version_adjusted: { "from": "<代码里写的合法版本号，没写或不合法为空串>", "to": "<登记的版本号>" }`，没调整时没有这个字段。代码与这个插件某个已有版本完全相同（sha256 一样）→ `accepted: false`，`issues` 里一条 `path` 为 `meta`、说明“代码与 vX.Y.Z 完全相同，没有产生新版本”（不是 HTTP 错误）。并发上传撞上唯一索引时服务端重算最多 3 次，仍冲突才报 `meta.version`。没有 `file` 字段 / 不是 multipart / 空文件 400（10001）；文件超限 413（50007，请求体总大小也有上限，超过同样 413）；往内置插件的 key 上传 409（50006）；runner 不可用 503（50021） |
| PUT    | `/plugins/:key/enabled`           | `{ "enabled": true }`，停用后所有协议用了它的模型不再接新任务（进行中的任务按快照继续）；插件不存在 404（50001）                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| DELETE | `/plugins/:key/versions/:version` | 只能删没有模型引用、也没有非终态任务引用的版本（否则 409，50005）；内置插件 409（50006）；插件或版本不存在 404（50001）。删除最后一个版本后插件行一并消失                                                                                                                                                                                                                                                                                                                                                                                        |
| GET    | `/plugins/:key/delete-check`      | 删除整个插件前的预检（super_admin），见下文「删除预检」；kind：`builtin_plugin`、`plugin_models`（refs 是协议用了它任一版本的模型 key + 展示名）、`active_tasks`。插件不存在 404（50001）                                                                                                                                                                                                                                                                                                                                                        |
| POST   | `/plugins/:key/upgrade-models`    | 批量升级模型（super_admin）：把协议固定在该插件旧版本上的模型升到指定版本，请求体 `{ "version": "1.2.0" }`，不传（或没有请求体）升到最新版本。逐个处理，失败不中断：新版本不再声明的协议设置项丢弃，其余按新版本的 `channelSettings` 校验并补默认值（必填项缺失算该模型失败）；已启用的模型保存前会过启用检查，不通过算失败、保持原样。→ `{ "to_version": "1.2.0", "upgraded": ["m1"], "failed": [{ "key": "m2", "reason": "…" }] }`。插件不存在或目标版本不存在 404（50001），插件已停用 409（50004）。写审计 `plugin.upgrade_models` |
| DELETE | `/plugins/:key`                   | 删除整个插件（全部版本 + 插件行，super_admin）：内置插件 409（50008，只能停用）；任一版本仍被模型固定或被非终态任务引用 409（50005，msg 带数量）；不存在 404（50001）。事务内按“插件行 → 版本行”加锁后重新统计，写审计 `plugin.delete`（target_type=plugin）                                                                                                                                                                                                                                                                                     |

```jsonc
// PluginView
{
  "key": "newapi",
  "name": "New API",
  "source": "builtin" /* builtin | uploaded */,
  "enabled": true,
  "updated_at": "...",
  "versions": [
    /* 新到旧 */
    {
      "id": 3,
      "plugin_key": "newapi",
      "version": "1.0.0",
      "sha256": "…",
      "created_at": "...",
      "created_by": 0,
      "model_count": 1, // 协议固定在这个版本上的模型数
      "meta": {
        /* pluginmeta.Meta：apiVersion、key、name、version、vendor（可选，厂商 slug）、auth、endpoints（每项可带展示用的 method、path）、channelSettings（有序对象，现在是模型的协议设置项）、poll；库里 meta 损坏时为 null */
      },
    },
  ],
}
```

响应里永远不含插件代码。`sha256` 是服务端对上传文件自己算的哈希。

内置插件（`backend/plugins/*.js`）在服务启动时按 `key + version` 自动登记为 `builtin`：同一版本重复启动是空操作；版本号没升但代码变了会报错并保持库里的旧代码（版本不可变）；key 已被上传的插件占用会报错、不覆盖。新版本的内置插件随发版登记，模型仍要显式升级（插件页“升级模型”）。

## 总览统计（admin 与 super_admin 都能读）

`GET /stats?days=7|30`：后台总览页的任务量柱形图与调用占比数据。`days` 不传按 7，只接受 7 或 30，其他值 400（10001）。

```jsonc
{
  "days": 7,
  "daily": [{ "date": "2026-10-04", "succeeded": 80, "failed": 4, "other": 3 }],
  "by_model": [{ "model": "kling-v2", "label": "可灵 v2", "count": 250 }],
  "by_kind": [{ "kind": "video", "count": 540 }],
}
```

统计口径（页面上所有数字都用它，能互相对上）：

- 范围：`created_at` 落在区间内的正式任务，`is_test = true` 的运营试跑不算。区间是含今天往前 `days` 天的 0 点到明天 0 点，按 `Asia/Shanghai` 的自然日分桶（与个人中心热力图的默认时区一致）。
- 每天三个桶，之和就是当天任务数：`succeeded`（状态 `succeeded`）、`failed`（`failed` + `expired`）、`other`（`canceled` 与所有进行中状态）。
- `daily` 恒为 `days` 项，日期升序，没有任务的日子补 0，**最后一项是今天**（今天还没过完）。
- `by_model` 给全量，按任务数降序、同数量按 key 升序；`label` 是模型当前展示名，模型已被删除或没有展示名时回退为 `key`。`by_kind` 的 `kind` 为 `video` / `image` / `audio` / `text`。没有任务时两者都是 `[]`。
- 性能：靠 `generation_tasks.created_at` 单列索引 `idx_task_created`（AutoMigrate 建）；每次请求实时聚合，没有缓存。

## 渠道（super_admin 写，admin 只读；拉取与导入模型、渠道内模型排序 admin 也能调）

```jsonc
// ChannelView：渠道只管连接信息（地址、请求头、限流），协议在模型的 channels[0].protocol 里选
{
  "key": "newapi-main",
  "name": "自建 New API",
  "base_url": "https://gw.example.com",
  "headers": { "X-Biz": "canvas" }, // 自定义请求头，明文存储（不要放密钥），只加到发往 base_url 同源的请求
  "rate_limit": { "rps": 5, "max_concurrency": 20, "max_running": 2 },
  "sort": 10, // 渠道顺序，越小越靠前；用户看到的模型顺序 = 渠道顺序 + 渠道内顺序
  "enabled": true,
  "secret_set": true, // API Key 只写不读，这里只告诉有没有设置
  "secret_key_set": false, // Secret Key（可选的第二凭证，AK/SK 签名类协议用）同样只写不读
  "updated_by": 1,
  "updated_at": "...",
  "created_at": "...",
}
```

`rate_limit` 里 0 表示不限：`rps` / `max_concurrency` 限制发往上游的 HTTP 请求（每秒数、同时数）；`max_running` 限制**同时在上游生成的任务数**（跨实例，由 worker 领取任务时在数据库里执行），超出的任务留在 `pending`（用户看到“排队中”），等有空位再提交；排队超过任务的 `deadline_at` 会置为 `expired`（文案“排队超时，积分已退回”）。任务真正提交给上游时，`deadline_at` 会从提交时刻重新计算一个完整的模型超时。

| 方法   | 路径                          | 说明                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ------ | ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/channels`、`/channels/:key` | 列表（按渠道顺序 `sort`、key 升序）/ 详情（admin）；详情渠道不存在 404（50011） |
| GET    | `/channels/loads`             | 各渠道当前的任务负载（admin）→ `[ { "channel": "yswg", "running": 2, "waiting": 5 } ]`。`running` = 同时生成数（`queued` / `running` + 正在提交的 `pending`），与 `rate_limit.max_running` 比较；`waiting` = 还没调用上游、在等名额的 `pending`；没有未完成任务的渠道不返回，没有任何任务时返回 `[]` |
| POST   | `/channels`                   | 新建（super_admin）：`{ key?, name, base_url, headers?, rate_limit?, enabled? }`。**`key` 不传由后端生成**（16 位随机十六进制，撞上已有渠道自动重试），响应里返回；显式传了就按 `^[a-z0-9][a-z0-9-]{0,63}$` 校验、重复 409。后台界面不让运营填 key。`enabled` 不传按 true，新渠道排在现有渠道之后。**保存只做字段校验，不向上游发请求**。key 重复 409（50012）；缺必填字段 400（10001）；业务校验失败 400（50013，所有问题一次报出，原因在 msg）：key 格式（`^[a-z0-9][a-z0-9-]{0,63}$`）、name 非空且 ≤128 字、`base_url` 是 http/https、有主机、**无用户名密码、无查询参数与片段**、`headers`（≤32 条；名称是合法 token、不区分大小写不能重复；**Host / Content-Length / Transfer-Encoding / Connection / Upgrade / Authorization / Proxy-Authorization / Cookie 不能设置**；值 ≤1024 字符、不含换行与控制字符）、`rate_limit` 的 `rps` / `max_concurrency` / `max_running` 非负 |
| PUT    | `/channels/:key`              | 更新（super_admin），字段都可选（不传表示不改）：`name`、`base_url`、`headers`（传了就整体替换，空对象清空）、`rate_limit`（整体替换）、`enabled`；`sort` 只能由 `PUT /channels/sort` 修改。渠道不存在 404（50011）；校验失败 400（50013） |
| PUT    | `/channels/sort`              | 调整渠道顺序（super_admin）：`{ "keys": ["b", "a", "c"] }`，必须恰好是现有全部渠道的 key（不能重复、不能遗漏），服务端按 10、20、30… 重写 `sort`，否则 400（10001）、不改任何数据。写审计 `channel.sort` 并刷新 Registry（渠道顺序决定用户看到的模型顺序） |
| PUT    | `/channels/:key/secret`       | 设置渠道 API Key：`{ "value": "..." }`（去掉首尾空白，≤4096 字节），存 `ai_secrets`，名字 `channel:<key>`，只写、响应无 `data`、不回显。没有配置主密钥 `APP_AI_SECRET_KEY` 时 500 并在 msg 里说明；渠道不存在 404 |
| PUT    | `/channels/:key/secret-key`   | 设置或清除渠道 Secret Key（可选的第二凭证，供 AK/SK 签名类协议用，插件读 `ctx.credentials.secretKey`）：`{ "value": "..." }`，**空串表示清除**；存 `ai_secrets`，名字 `channel:<key>:secret`，只写不读。渠道不存在 404。写审计 `channel.secret_key`（不含值） |
| POST   | `/channels/:key/pull-models`  | 向上游拉取模型清单（admin），**不落库**：宿主内置的通用请求 `GET {base_url}/v1/models`（`base_url` 已以 `/v1` 结尾不重复拼接），渠道 API Key 作 Bearer，带渠道自定义请求头，走 SSRF 防护与渠道限流，不经过协议插件。→ `{ "models": [{ "id": "gpt-image-2", "exists": false }], "total": 8 }`，`exists` 表示这个渠道下已有同名上游模型（导入时会跳过）。限制：响应体 ≤4MB、≤2000 个模型、30 秒超时。渠道不存在 404（50011）；API Key 没设置 409（50015）；上游不通 / 401 / 403 / 非 2xx / 返回不是 OpenAI 形状 / 超限 / 超时 502（50023，原因在 msg，已抹掉 Key） |
| POST   | `/channels/:key/models/import` | 把所选上游模型导入成“待配置”模型（admin）：`{ "ids": ["gpt-image-2", "sd2.0-c1"] }`（去空白与重复；至少 1 个，单次 ≤500 个，超出 400 提示分批）。为渠道下还没有的上游模型创建**未启用、kind 为空、没有协议**的模型（展示名取上游 id，排在渠道内现有模型之后）；渠道下已有同名上游模型的跳过。模型 key 取上游 id（不合法字符换成 `-`），与已有 key 冲突时加渠道前缀 `<渠道>-<id>`，仍冲突再追加 `-2`、`-3`…。→ `{ "created": [{ "key": "gpt-image-2", "upstream_model": "gpt-image-2" }], "skipped": ["sd2.0-c1"] }`。逐个写库（半成品本来就允许保存），最后刷新一次 Registry。渠道不存在 404（50011）。写审计 `model.import` |
| PUT    | `/channels/:key/models/sort`  | 调整渠道内模型的顺序（admin）：`{ "keys": [...] }` 必须恰好是该渠道下全部模型的 key，按 10、20、30… 重写这些模型的 `sort`，不影响其他渠道；不是全集 400（10001）。刷新 Registry |
| GET    | `/channels/:key/delete-check` | 删除预检（super_admin）；kind：`channel_models`（refs 是引用它的模型 key + 展示名 label，没有 label 用 key）、`active_tasks`。渠道不存在 404（50011） |
| DELETE | `/channels/:key`              | 硬删除渠道，连同 ai_secrets 里的 `channel:<key>` 与 `channel:<key>:secret`（super_admin）：仍被模型（`channels[].channel`）或非终态任务的快照引用 409（50016，msg 带数量）；不存在 404（50011）。写审计 `channel.delete` 并刷新 Registry |

### 审计日志

写插件 / 渠道都写 `ai_audit_logs`（操作人 `actor_id`、时间、目标；`detail_json` 里**不含任何凭证与插件代码**）。写入失败不回滚已提交的变更，只记 Error 日志。

| 动作                 | 触发                                                                               | detail                                                                   |
| -------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `plugin.upload`      | 上传插件版本成功；内置插件首次登记（操作人 0，多一个 `source: "builtin"`）         | `version`、`sha256`                                                      |
| `plugin.enable`      | 启停插件                                                                           | `enabled`                                                                |
| `plugin.delete`      | 删除插件版本（target_type=`plugin_version`）；删除整个插件（target_type=`plugin`） | 删版本：`version`、`sha256`；删插件：`versions`（`[{version, sha256}]`） |
| `channel.create`     | 新建渠道                                                                           | `enabled`                                                                |
| `channel.update`     | 更新渠道且有字段变化                                                               | `fields`（被改的字段名；请求头的值不记）                                  |
| `channel.secret`     | 设置渠道 Key                                                                       | 无                                                                       |
| `channel.secret_key` | 设置 / 清除渠道 Secret Key                                                         | 无                                                                       |
| `channel.sort`       | 调整渠道顺序                                                                       | `keys`（新顺序）                                                         |
| `model.import`       | 从渠道拉取并导入模型（target 是渠道）                                              | `created`（新建的模型 key）、`skipped`（跳过的数量）                     |
| `plugin.upgrade_models` | 批量升级模型（target 是插件）                                                  | `to_version`、`upgraded`（成功的模型 key）、`failed`（失败数量）         |
| `channel.trusted`    | 历史记录：已取消的“允许访问内网”开关（`trusted_internal`）变化，不再产生新记录      | `from`、`to`                                                             |
| `channel.credential` | 历史记录：已取消的 `allow_credentials` 开关，不再产生新记录，展示旧审计时要认得    | `from`、`to`                                                             |
| `channel.enable`     | 渠道启停变化                                                                       | `from`、`to`                                                             |
| `channel.delete`     | 删除渠道（连同它的 Key）                                                           | 无                                                                       |

## 模型（admin 与 super_admin 都能写）

| 方法       | 路径                                                    | 说明                                                                                                                                                                                                                                                                                                                        |
| ---------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET / POST | `/models` | 列表 / 新建（保存配置，**默认未启用**，不看正文里的 `enabled`）。列表项：`key / kind / label / vendor / tags / channel / upstream / plugin / plugin_version / enabled / sort / updated_at`，不含正文；`label`、`channel`（绑定的渠道 key，即 `channels[0].channel`）、`upstream`（上游模型名）、`plugin` 与 `plugin_version`（协议选的插件与固定版本，没选为空串）取自配置正文，正文里没有则为空串。目录接口 `/admin/ai/models` 还带 `health.status`（`online` / `broken` / `offline` / `pending`，**待配置 = 没选能力或协议且未启用**）、`channel_name`、`plugin_name`，顺序是用户看到的顺序（渠道顺序，再渠道内顺序）。**新建时正文里的 `key` 可以不传（或传空串），由后端生成**（`m-` 加 12 位随机十六进制，撞上已有模型自动重试，写回正文且不改变字段顺序），后台界面不让运营填 key；显式传了就按字符集 / 长度校验、已存在报 `50013`（`ErrConfigInvalid`）。响应 `{ key, issues: [] }`：`key` 是保存的模型 key（没传时从这里取）；有校验问题也已保存，但启用会被拦 |
| GET / PUT  | `/models/:key` | 详情 `{ key, kind, enabled, sort, body, updated_at }`（没有版本历史，每个模型只有一份配置）/ 更新配置。**已启用的模型保存即生效**，所以保存前要过和启用时一样的检查，不通过 400/409 拒绝、不落库 |
| POST       | `/models/:key/validate` | 校验：不传 body 校验已保存的配置（模型不存在 404 / 40011），传 body 校验传入内容 |
| POST       | `/models/:key/dry-run`                                  | `{ "input": {} }` → 渲染出**插件返回的请求描述**（宿主校验后的 method / url / query / headers / body 等），不发送。**不含宿主注入后的鉴权头**（Key 在 dry-run 里不解析，鉴权注入处以 `***` 占位），响应经过脱敏，所以不要把它当成“最终请求”                                                                                 |
| POST       | `/models/:key/test-run`                                 | `{ "input": {} }` → 试跑任务视图（is_test，不扣积分）                                                                                                                                                                                                                                                                       |
| GET        | `/test-runs/:id`                                        | 试跑任务视图（只能查自己创建的，查不到 404 / 40004）                                                                                                                                                                                                                                                                        |
| GET        | `/test-runs/:id/trace`                                  | 试跑追踪：`{ "steps": [TraceStep] }`（每次钩子的输入 / 输出 / `utils.log`，每次 HTTP 的请求与响应，均已脱敏）；任务还没有追踪时 `steps: []`；归属规则同上，别人的任务 / 不存在 / 非试跑任务统一 404（40004）                                                                                                                |
| PUT        | `/models/:key/enabled`、`/models/:key/sort` | 启用 / 停用、排序：`{ "enabled": true }`、`{ "sort": 5 }`；**启用前检查已保存的配置**（见下），不通过不能启用；用户只能看到并使用已启用的模型 |
| GET        | `/schema/model`                                         | 模型配置的 JSON Schema（`/schema/provider` 已删除）                                                                                                                                                                                                                                                                         |
| GET        | `/models/:key/delete-check`                             | 删除预检；kind：`model_enabled`。模型不存在 404（40011）                                                                                                                                                                                                                                                                    |
| DELETE     | `/models/:key`                                          | **硬删除**：事务内锁住模型行 → 还在上架 409（50031）→ 删掉该模型的配置→ 删掉模型行，不可恢复；不存在 404（40011）。进行中与历史任务不受影响（它们只读自己的配置快照）。之后**同名 key 可以重新新建 / 导入**                                                                    |

### 删除预检

模型、渠道、插件的 `GET .../:key/delete-check` 共用一个结构：

```json
{
  "blockers": [
    {
      "kind": "channel_models",
      "message": "2 个模型在用这个渠道",
      "refs": [{ "key": "m1", "name": "可灵 2" }]
    }
  ]
}
```

`blockers` 与 `refs` 永远是数组；`blockers` 为空表示可以删。预检只给界面展示，真正能否删除以 DELETE 事务内的判断为准。

`dry-run` / `test-run` 请求里的 `use_provider_draft` 字段已废弃（忽略）。

### 模型配置正文（body）

```jsonc
{
  "key": "kling-i2v",
  "kind": "video", // kind：video / image / audio / text / agent（画布 Agent 用的对话大模型，见下）
  // hint 最多 500 字；vendor 可省略，小写字母/数字/连字符（前端据此显示厂商 logo）；tags 可省略，最多 5 个、每个最多 12 字、不能重复
  "label": "可灵 图生视频",
  "hint": "",
  "vendor": "kling",
  "tags": ["推荐"],
  "deadline": "30m",
  "enabled": false,
  "sort": 100,
  "channels": [
    {
      "channel": "newapi-main",
      "upstream_model": "kling-v2-master",
      // 协议：非 Agent 模型必填，Agent 模型不能有。version 是固定的 semver；settings 按该版本的 channelSettings 校验（缺省补默认值）
      "protocol": { "plugin": "newapi", "version": "1.0.0", "settings": { "region": "cn" } },
    },
  ], // 首期必须恰好一个
  "params": { "max_tokens": 2000 }, // 可选，固定参数，原样交给插件
  "capabilities": {
    // 模型能力：由运营手填，画布渲染与下单校验的唯一来源（取代旧的 input_schema）
    "ops": ["t2v", "i2v", "omni"], // 生成方式：video 取 t2v / i2v / omni，image 取 t2i / i2i；text、audio 不填
    "refs": {
      "image": { "on": true, "max": 4, "max_mb": 10 },
      "audio": { "on": false, "max": 0, "max_mb": 0 },
      "video": { "on": false, "max": 0, "max_mb": 0 },
    },
    "prompt": { "max_length": 2000 },
    "params": {
      "duration": {
        "type": "number",
        "label": "视频时长",
        "open": true,
        "min": 4,
        "max": 12,
        "step": 1,
        "default": 5,
        "unit": "秒",
      },
    }, // 有序对象，书写顺序就是画布参数面板的显示顺序；参数名会作为任务输入的键传给插件
    "context": { "window": 128000, "output": 8192 }, // 仅 text
    "system": "…", // 仅 text，固定系统提示，不下发给画布
  },
  "pricing": {
    // 定价：价格一律是整数积分（取代旧的 credits）
    "billing": "per_second", // per_call 按次 / per_second 按秒（× 参数 duration）/ token 按 Token（仅 text，用 token.in / token.out，积分 / 百万 Token）
    "per_second": 2,
    "tiers": [{ "on": true, "when": { "resolution": "1080P" }, "unit": 4 }], // 规格价格：键是 spec 参数名、op 或 ref_video；条件最多的一条胜出
    "cost": { "on": true, "per_second": 1 }, // 积分成本，仅管理端，不下发给画布
  },
}
```

保存时的跨对象检查以 `issues` 报出，不阻塞保存：渠道存在且启用的问题挂在 `channels[0].channel`；协议插件存在且启用、固定的版本存在、版本支持该 kind、协议设置合法的问题挂在 `channels[0].protocol`。没选协议的“待配置”模型照常保存。

启用（`PUT /models/:key/enabled`，以及保存已启用的模型）的前置检查，不满足分别返回：正文无校验问题（400 / 40010，没选能力或协议的“待配置”模型在这里被拦下）→ 渠道存在（404 / 50011）且启用（409 / 50014）→ **Agent 模型**：渠道 API Key 已设置（409 / 50015）即通过，不看协议；其他模型：协议已选（400 / 40010）→ 协议插件存在（400 / 40010）且启用（409 / 50004）→ 模型固定的插件版本存在（400 / 40010）→ 版本的 `endpoints` 里有这个模型的 `kind`（400 / 40010）→ 协议设置按版本的 `channelSettings` 校验（400 / 40010）→ 版本的 `auth.type` 不是 `none` 时渠道 API Key 已设置（409 / 50015）。

## 面向画布的接口（变化）

- `GET /api/v1/models?kind=video|image|audio|text|agent`（登录即可，不要求管理员）：`kind` 取值之外 400；返回字段为 `key / kind / label / hint / vendor / tags / capabilities / pricing`（`capabilities` 不含 `system`，`pricing` 不含 `cost`）（`vendor` 无则为空串，`tags` 无则为 `[]`），仍然不含 params / 渠道 / 插件信息。
- `POST /generation-tasks`：请求新增 `node_ids`（长度等于生成数量，第 i 个任务绑定第 i 个节点；只生成 1 个时可以只传 `node_id`），积分按 `pricing` 计算；响应改为 `{ "items": [ { "node_id", "task" } | { "node_id", "error": { "status", "code", "message" } } ] }`，按节点逐项给出，某个节点失败（402 积分不足、429 并发已满……）不影响其它节点。幂等键覆盖全部任务：第 i 个任务用 `Idempotency-Key#i`（i=0 不加后缀）。任务视图新增 `charged_credits`（实际扣的积分，Token 计费可能小于冻结额）。
- `POST /generation-tasks` 的 `kind` 新增 `text`；文本任务成功后 `outputs` 是 `[{ "media_type": "text", "text": "正文" }]`（没有 `asset_id` 与 `url`）。
- `TaskOutput` 新增 `text` 字段，`asset_id` / `url` 在文本产物里不出现。
- 平台回调 `POST /webhooks/:provider/:secret` 已删除。

## 错误码（本文用到的）

| 码                    | HTTP            | 含义                                                             |
| --------------------- | --------------- | ---------------------------------------------------------------- |
| 10001                 | 400             | 参数错误（绑定 / 格式 / 校验）                                   |
| 10003 / 10006         | 401             | 未登录 / 登录过期                                                |
| 10004                 | 403             | 没有权限（角色不够）                                             |
| 10002                 | 404             | 路由不存在（如已删除的 `/secrets*`）                             |
| 40004                 | 404             | 试跑任务不存在                                                   |
| 40006                 | 400             | 示例输入不合法（dry-run / test-run）                             |
| 40010 / 40011         | 400 / 404       | 模型配置：校验未通过 / 不存在                                    |
| 50001                 | 404             | 插件（版本）不存在                                               |
| 50002                 | 400             | 插件预检未通过（上传接口里以 `accepted=false` 返回，不用这个码） |
| 50003                 | 409             | 插件版本号已存在（上传接口里并发冲突重试 3 次后仍冲突才以 `issues` 返回，不用这个码） |
| 50004                 | 409             | 插件已停用                                                       |
| 50005                 | 409             | 插件版本仍被模型或进行中的任务使用，无法删除                     |
| 50006                 | 409             | 内置插件不能被删除或覆盖                                         |
| 50007                 | 413             | 插件文件超过大小限制                                             |
| 50008                 | 409             | 内置插件不能删除，只能停用                                       |
| 50011                 | 404             | 渠道不存在                                                       |
| 50012                 | 409             | 渠道 key 已存在                                                  |
| 50013                 | 400             | 渠道配置不合法（原因在 msg）                                     |
| 50014                 | 409             | 渠道已停用                                                       |
| 50015                 | 409             | 渠道 Key 尚未设置                                                |
| 50016                 | 409             | 渠道仍被模型或进行中的任务使用，无法删除（msg 带数量）           |
| 50021                 | 503             | 插件运行时（plugin-runner）暂不可用                              |
| 50022                 | 502             | 历史：渠道绑协议时代的连通性检查 / 导入失败，不再产生                      |
| 50023                 | 502             | 拉取上游模型失败（上游不通 / 401 / 返回格式不符 / 超时，原因在 msg，已脱敏） |
| 50031                 | 409             | 模型还在上线，先下线再删除                                       |

## agent 类型模型（画布 Agent 用的对话大模型）

`kind: "agent"` 不走插件钩子（钩子是同步的、不能联网，没法流式），由后端的 LLM 网关直接请求渠道 `base_url` 下的 OpenAI 兼容接口 `/v1/chat/completions`（`stream: true` + `tools`）。所以：

- **不选协议**：Agent 模型的 `channels[0].protocol` 必须为空（带了会校验报错）；网关用 `Authorization: Bearer <渠道 API Key>`，并带上渠道的自定义请求头。启用前只检查渠道存在、启用、API Key 已设置；**不需要**渠道挂任何插件，也不需要声明 `endpoints.agent`。
- **能力**：必须设置 `capabilities.context`（`window`、`output`）；`capabilities.vision` 声明能不能看图（只有 agent 能设）；`capabilities.prompt.max_length` 在这里表示**用户单条消息的字数上限**；没有 `ops`、`refs`、`params`，也不能设 `system`（系统提示词由平台维护）。
- **计费**：只能 `pricing.billing = "token"`，`token.in` / `token.out` 是每百万 Token 的积分价。
- 上架后出现在 `GET /api/v1/models?kind=agent` 和 `GET /api/v1/agent/models`。

# AI 管理接口（协议插件设计落地后）

> 路径前缀 `/api/v1/admin/ai`，都需要登录（JWT）。权限：**读 = `admin` 或 `super_admin`；写插件 / 渠道 = 仅 `super_admin`**，`admin` 调用返回 403（错误码 10004）。
> 响应统一包一层：`{ "code": 0, "msg": "ok", "data": ... }`（见 `response.OK`）。错误时 `code` 非 0，HTTP 状态码对应 `errcode`。
> 取代了旧的 `/providers*`、`/import/runninghub`、`/secrets*` 三组接口（已删除）。

## 当前用户

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/me` | admin | `{ "user_id": 1, "role": "admin" \| "super_admin" }`，前端据此隐藏写操作。`user` 调用得到 403 |

## 插件（super_admin 写）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/plugins` | 插件列表，见下 `PluginView` |
| POST | `/plugins` | 上传插件：`multipart/form-data`，字段 `file`（.js，≤512KB）。**预检不通过也返回 200**：`{ "accepted": false, "issues": [{ "path": "meta.endpoints.video.mode", "message": "..." }], "version": null }`；通过则登记为新版本 `{ "accepted": true, "issues": [], "version": PluginVersionView }`。版本号已存在 → `issues` 里报 `meta.version`（不是 HTTP 错误）。文件超限 413（50007），runner 不可用 503（50021） |
| PUT | `/plugins/:key/enabled` | `{ "enabled": true }`，停用后所有使用它的渠道不再接新任务 |
| DELETE | `/plugins/:key/versions/:version` | 只能删没有渠道引用、也没有非终态任务引用的版本（否则 409，50005）；内置插件 409（50006） |

```jsonc
// PluginView
{ "key": "newapi", "name": "New API", "source": "builtin" /* builtin | uploaded */, "enabled": true, "updated_at": "...",
  "versions": [ /* 新到旧 */
    { "id": 3, "plugin_key": "newapi", "version": "1.0.0", "sha256": "…", "created_at": "...", "created_by": 0,
      "channel_count": 1,                 // 固定在这个版本上的渠道数
      "meta": { /* pluginmeta.Meta：apiVersion、key、name、version、auth、allowedHosts、endpoints、channelSettings（有序对象）、import、poll */ } } ] }
```

## 渠道（super_admin 写，admin 只读；导入 admin 也能调）

```jsonc
// ChannelView
{ "key": "newapi-main", "name": "自建 New API", "plugin_key": "newapi", "plugin_version_id": 3, "plugin_version": "1.0.0",
  "base_url": "https://gw.example.com", "trusted_internal": false, "allow_credentials": false,
  "settings": { "region": "cn" }, "rate_limit": { "rps": 5, "max_concurrency": 20 },
  "enabled": true, "secret_set": true,       // Key 只写不读，这里只告诉有没有设置
  "updated_by": 1, "updated_at": "...", "created_at": "..." }
```

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/channels`、`/channels/:key` | 列表 / 详情（admin） |
| POST | `/channels` | 新建（super_admin）：`{ key, name, plugin_key, plugin_version, base_url, trusted_internal?, allow_credentials?, settings?, rate_limit?, enabled? }`。`plugin_version` 是 semver 字符串（固定到这个版本）；key 重复 409（50012）；校验失败 400（50013，原因在 msg）：key 格式（`^[a-z0-9][a-z0-9-]{0,63}$`）、插件版本存在且插件启用、`base_url` 是 http/https 且无用户名密码、`settings` 符合插件 `channelSettings`（必填项、类型、enum 取值）、`rate_limit` 非负 |
| PUT | `/channels/:key` | 更新（super_admin），字段都可选（不传表示不改）；改 `plugin_version` 即“升级插件后切换渠道”；`trusted_internal` / `allow_credentials` 的变化写审计日志 |
| PUT | `/channels/:key/secret` | 设置渠道 Key：`{ "value": "..." }`，只写，响应 `data: null`，不回显 |
| POST | `/channels/:key/check` | 连通性检查（super_admin）→ `{ "ok": true, "message": "HTTP 200", "duration_ms": 120 }`；插件没实现 `buildCheckRequest` 时 `ok=false, message="插件不支持连通性检查"`；Key 没设置 409（50015）；runner 不可用 503（50021） |
| POST | `/channels/:key/import` | 从渠道导入模型（admin）：`{ "args": { } }`（取值按插件 `meta.import.args`）→ `{ "drafts": [ { "upstream_model": "kling-v2", "kind": "video", "label": "...", "params": { }, "input_schema": { } } ] }`，只预填编辑器，不落库 |

渠道创建 / 修改 / 凭证 / 开关都写 `ai_audit_logs`（操作人、时间、目标；detail 里不含任何凭证）。

## 模型（admin 与 super_admin 都能写）

路径与旧版一致（`providers` 那一组已删除）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET / POST | `/models` | 列表 / 新建草稿 |
| GET / PUT | `/models/:key` | 详情（草稿 + 已发布）/ 更新草稿 |
| POST | `/models/:key/validate`、`/publish`、`/rollback` | 校验 / 发布 / 回滚 |
| GET | `/models/:key/revisions`、`/models/:key/revisions/:rid` | 历史 |
| POST | `/models/:key/dry-run` | `{ "input": {} }` → 渲染出插件返回的请求描述与宿主注入后的最终请求（Key 脱敏），不发送 |
| POST | `/models/:key/test-run` | `{ "input": {} }` → 试跑任务视图（is_test，不扣积分） |
| GET | `/test-runs/:id` | 试跑任务视图 |
| GET | `/test-runs/:id/trace` | 试跑追踪：`{ "steps": [TraceStep] }`（每次钩子的输入 / 输出 / `utils.log`，每次 HTTP 的请求与响应，均已脱敏）；任务还没有追踪时 `steps: []` |
| PUT | `/models/:key/enabled`、`/models/:key/sort` | 上下架 / 排序 |
| GET | `/schema/model` | 模型配置的 JSON Schema（`/schema/provider` 已删除） |

`dry-run` / `test-run` 请求里的 `use_provider_draft` 字段已废弃（忽略）。

### 模型配置正文（draft / published 的 body）

```jsonc
{
  "key": "kling-i2v", "kind": "video",          // kind：video / image / audio / text
  "label": "可灵 图生视频", "hint": "", "credits": 10, "deadline": "30m", "enabled": false, "sort": 100,
  "channels": [ { "channel": "newapi-main", "upstream_model": "kling-v2-master" } ],   // 首期必须恰好一个
  "params": { "max_tokens": 2000 },               // 可选，固定参数，原样交给插件
  "input_schema": { "prompt": { "type": "text", "label": "提示词", "required": true, "port": "text" } }   // 有序对象，书写顺序就是前端渲染顺序
}
```

发布（publish / rollback）的前置检查：正文无校验问题；`channels[0].channel` 对应的渠道存在且启用、插件启用、插件版本的 `endpoints` 里有这个模型的 `kind`；渠道用的插件 `auth.type` 不是 `none` 时渠道 Key 已设置（50015）。

## 面向画布的接口（变化）

- `GET /models?kind=video|image|audio|text` 新增 `text`；返回的字段不变（`key / kind / label / hint / credits / input_schema`），仍然不含 params / 渠道 / 插件信息。
- `POST /generation-tasks` 的 `kind` 新增 `text`；文本任务成功后 `outputs` 是 `[{ "media_type": "text", "text": "正文" }]`（没有 `asset_id` 与 `url`）。
- `TaskOutput` 新增 `text` 字段，`asset_id` / `url` 在文本产物里不出现。
- 平台回调 `POST /webhooks/:provider/:secret` 已删除。

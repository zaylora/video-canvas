# vc 命令全集（参数 · 默认值 · 返回结构）

按 `vc --help` 与站点 `/cli/docs` 对齐，章节顺序与 SKILL.md 的业务主线一致，方便按阶段查。
**这里不写具体模型 key、站点域名、画布编号**：那些每次都不一样，现场用 `model list` / `whoami` 取。

所有命令都吃全局选项：

| 全局选项            | 作用                                                                                          |
| ------------------- | --------------------------------------------------------------------------------------------- |
| `--json`            | 输出一行 `{"ok":true,"data":…}` 或 `{"ok":false,"error":{code,message}}`；进度仍写 stderr     |
| `--base URL`        | API 地址（等价环境变量 `VC_API_BASE`），站点根或完整的 `/api/v1` 都行；令牌只属于登录时的站点 |
| `--no-update-check` | 本次不查 GitHub 上的新版本（等价 `VC_NO_UPDATE_CHECK=1`）                                     |
| `-o, --output 文件` | 仅 `canvas graph` / `canvas export`：写文件；`-o -` 写 stdout                                 |

开关型选项（`--json` / `--wait` / `--watch` / `--active` / `--reject` / `--force` / `--no-open`）不吞后面的位置参数；
`--text` / `--prompt` / `--label` / `--message` 等文本参数的值可以以 `-` 开头。

退出码：`0` 成功 · `1` 业务/网络错误或任务失败 · `2` 用法错误（含「需要交互确认」被拒） ·
`3` 等待超时（任务仍在跑） · `4` 新建项目 / 节点级修改 / 会删节点的整图保存 / 审批被取消。

---

## 一、接入：身份与会话

| 命令            | 参数                                                  | 说明                                                                           |
| --------------- | ----------------------------------------------------- | ------------------------------------------------------------------------------ |
| `login`         | `--base URL`、`--web URL`、`--name 名称`、`--no-open` | 设备码登录；完整授权地址与用户码写 stderr（`--json` 也一样）。登录态默认 30 天 |
| `logout`        | ——                                                    | 只清本机凭据；服务端登录态要到期或改密码后才失效                               |
| `whoami`        | ——                                                    | 实时调 `/me`，可当健康检查                                                     |
| `version`       | `--json`                                              | 也可写 `--version` / `-v`；版本只读 `package.json`                             |
| `skill path`    | `--json`                                              | 打印随包技能目录                                                               |
| `skill install` | `--to <技能目录>`（可重复）`--json`                   | 装进终端助手：默认 `~/.claude/skills`，已有 `~/.codex`、`~/.agents` 的也装     |

`login` 的轮询：服务端给 `slow_down` 时自动加间隔（限频，不是故障）；网络错误与 5xx 退避重试到设备码过期，4xx 不重试；
`denied` / `expired` / `consumed` / 超时都是退出码 1。授权页默认由 API 地址推出，API 与网页不同域时用 `--web`（或 `VC_WEB_URL`）。
凭据记录登录时的站点；`--base` / `VC_API_BASE` 指向别处时命令直接拒绝（退出码 2），提示重新 `login`。

## 二、项目：读取、新建、备份与迁移

| 命令            | 参数                                              | 说明                                                                                           |
| --------------- | ------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `canvas list`   | `--page N`、`--page-size N`                       | 返回 `{list,total,page,page_size}`                                                             |
| `canvas get`    | `<画布编号>`                                      | 节点在 `data.payload_json`                                                                     |
| `canvas graph`  | `<画布编号>`、`-o 文件`                           | 只取 `{nodes, edges, viewport}`，可写回；版本号（`save` 的 `--revision`）见输出                |
| `canvas create` | `--title`（默认「未命名画布」）、`--payload 文件` | **需终端确认**；payload 要 graph 形态                                                          |
| `canvas import` | `<导出文件.json>`                                 | **需终端确认**；只新建，不覆盖                                                                 |
| `canvas export` | `<画布编号>`、`-o 文件`、`--force`                | 导出文档（`format`/`canvas`/`assets`），只给 `import` 用；已有同名文件不覆盖，`--force` 才覆盖 |

新建与导入的确认：先打印标题、节点/连线数量（或素材份数）、来源，等终端输入 `yes`；
`--json` 与非交互终端一律拒绝（退出码 2），取消是退出码 4（输入在回答前就结束，如 Ctrl-D，同样按取消处理）。
没有 `--yes` 旁路。这是约定性的软闸门，不是安全边界。

## 三、素材：模型、积分、生成与等结果

| 命令         | 参数                               | 返回                                                                        |
| ------------ | ---------------------------------- | --------------------------------------------------------------------------- |
| `model list` | `--kind image\|video\|audio\|text` | 数组，元素含 `key` / `kind` / `label` / `hint` / `capabilities` / `pricing` |
| `model show` | `<模型key>`                        | 单个模型；人类可读输出逐条列参数（名、可选值或范围、默认）                  |
| `credits`    | ——                                 | `{balance, frozen, available}`                                              |

`model show` 读的是 `capabilities`：`ops` 是生成方式（如 t2i / i2v / omni），`params` 是「名字 → 字段」的对象
（`type` 为 `enum` / `number` / `boolean`），`refs` 是各类参考素材的开关与上限，`prompt.max` 是提示词字数上限。

本地素材上传：

| 命令           | 参数         | 返回                                                     |
| -------------- | ------------ | -------------------------------------------------------- |
| `asset upload` | `<文件路径>` | 素材视图（`id` / `kind` / `byte_size` / `file_name` 等） |

一次传一个文件；路径不存在、是目录、空文件或超过 200MB 都在本地拒绝（退出码 2）。复用现有 JWT，无额外权限；
服务端按文件头嗅探真实类型、归属到当前账号。返回的 `id` 是纯数字，直接用于 `generate --ref-*` 与首尾帧参数。

上传路径与网页一致：先申请 upload-intent，系统开了直传就直传对象存储再调 complete 登记；未开直传、申请失败或直传失败则降级到 `POST /assets` 中转。登记失败（类型不合法 / 超限）是真错误，不会降级重传。

```
generate <image|video|audio|text> --model <模型key>
         [--canvas <画布编号> --node <节点id>] [--prompt <提示词>]
         [--param 名=值]... [--input '<json>'|@文件|-]
         [--ref-image <素材id>]... [--ref-video <素材id>]... [--ref-audio <素材id>]...
         [--first-frame <素材id> [--last-frame <素材id>]]
                                        锁首帧 / 首尾帧（内部走 keyframe 模式；不能与 --ref-image 混用）
         [--param mode=keyframe]          等价的底层写法：第一张图为首帧，第二张为尾帧
         [--count 1] [--wait] [--timeout 600] [--interval 3]
         [--idempotency-key <值>]
         首个任务提交前，幂等键就写到 stderr；也在结果的 idempotency_key 里
```

| 参数                       | 默认           | 规则                                                                                                        |
| -------------------------- | -------------- | ----------------------------------------------------------------------------------------------------------- |
| `<kind>`                   | 必填（第一项） | 只收 `image` / `video` / `audio` / `text`；`agent` 类模型不能提交生成                                       |
| `--model`                  | 必填           | 从 `model list` 取                                                                                          |
| `--canvas`                 | ——             | 与 `--node` **必须成对**；都不给＝临时任务                                                                  |
| `--node`                   | ——             | 任务绑在这个节点上，用于画布回填与生成记录                                                                  |
| `--prompt`                 | ——             | 等价 `input.prompt`                                                                                         |
| `--param`                  | ——             | 可重复；值先按 JSON 解释，失败按字符串                                                                      |
| `--input`                  | ——             | JSON 字符串 / `@文件` / `-`（stdin），内容必须是 JSON 对象；与 `--param`、`--prompt` 合并（后者覆盖同名键） |
| `--ref-*`                  | ——             | 可重复或逗号分隔；落进 `input.images` / `videos` / `audios`，值须为纯数字素材 id                            |
| `--count`                  | `1`（≤8）      | 拆成 N 个任务，全部绑同一个节点；无 `--node` 时不能用                                                       |
| `--wait`                   | 不加           | 阻塞到终态；不加就只提交并打印任务号                                                                        |
| `--timeout` / `--interval` | `600` / `3`    | 只在 `--wait` 时生效；多个任务共用一个总截止时间，并发地等；提交前就校验                                    |
| `--idempotency-key`        | 随机 UUID      | 调用方给就用调用方的（1–128 字符）；重试时复用它才不会建出两个任务                                          |

返回：`{items: [{node_id, task?, error?}]}`。全部失败时命令直接报错（退出码 1）；部分失败会打印失败项，
成功的照常生成。`--wait` 后 `data.tasks` 是终态任务（含 `outputs[].url`）；超时（退出码 3）、失败、出错时
`--json` 仍是 `{ok:false, error, data:{items, tasks, idempotency_key}}`，`tasks` 列出所有已提交任务的最新状态与冻结积分。
等待中的网络错误与 5xx 会退避重试，4xx 与业务错误不重试。

等结果与查任务：

| 命令        | 参数                                        | 说明                                                                                                      |
| ----------- | ------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `task get`  | `<id\|task_ref>`                            | 纯数字走 `/generation-tasks/:id`，编码串按 `ids=` 查                                                      |
| `task wait` | `<id\|task_ref>`、`--timeout`、`--interval` | 终态 `succeeded` / `failed` / `canceled` / `expired`；失败退出码 1，超时退出码 3；网络错误与 5xx 自动重试 |

`task_ref` 是任务 id 的 32 位十六进制编码，界面与日志里显示的就是它。

## 四、画布：节点级修改与整图写回

| 命令              | 参数                                                                                                       | 说明                                                                                                                           |
| ----------------- | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `canvas node add` | `<画布编号>`、`--kind script\|image\|video\|audio`、`--label`、`--text`、`--prompt`、`--model`、`--at x,y` | **需终端确认**                                                                                                                 |
| `canvas node set` | `<画布编号>`、`<节点id>`、`--label` / `--text` / `--prompt` / `--model`                                    | **需终端确认**；只提交差异                                                                                                     |
| `canvas node rm`  | `<画布编号>`、`<节点id>...`                                                                                | **需终端确认**；连带清连线、分镜挂接、成片顺序；组不能删；重复 id 只算一次                                                     |
| `canvas save`     | `<画布编号>`、`--payload 文件`、`--revision N`                                                             | 整图保存；**`--revision` 必填**（取自 `canvas graph`），严格乐观锁（冲突 409 / `30002`）；新图会让现有节点消失时**需终端确认** |

节点级编辑的确认流程：读当前图 → 逐行列出要改什么 → 等 `yes` → 带读到的 `revision` 提交（期间被改则 409）。
`--json` 与非交互终端一律拒绝；脚本批量改走 `canvas graph` + `canvas save --revision`（只增改、不删节点时无需确认）。

### 实时协作

`vc canvas list --scope shared` 查找受邀画布。`vc canvas status <画布>` 查看权限和确认版本。活跃协作画布的终端改动通过 `vc canvas node` 或 `vc canvas plan/apply` 提交。`plan --input` 接收 `{"changes":[...]}`，计划文件由 `-o` 写出。不要对活跃协作画布调用 `canvas save`。

`vc canvas watch <画布> --jsonl` 输出快照、权限、已确认变更及在线名单变化。通过 `--epoch E --since N` 恢复游标，只用 `snapshot` 或 `change` 事件推进游标。`vc canvas presence <画布> --json` 采样其他在线成员。`watch --follow <用户名或成员ID>` 才会输出该队友明确共享的视角及选区。`vc canvas op status <画布> <操作>` 查询本人收据，`vc canvas resume <画布> --run <操作>` 核对后按需重试原操作。

站点开放 `comments-replay` 时，所有者和编辑者可持续读取 `comment` 和 `comment-cursor`。保存最近的 `comment-cursor.data.cursor` 并使用 `--comments-since <游标> --epoch E` 恢复评论，或用 `--comments-since 0` 从当前代数首条可见评论开始。评论游标独立于图谱版本。

`generation-progress` 只用于显示已确认的节点运行进度。读取任务的最终状态时使用 `change` 或 `snapshot`。

`vc generate … --canvas <画布> --node <节点> --max-credits N` 使用服务端检查的本次积分上限。重试沿用完整参数和原幂等键。`vc asset library list`、`vc canvas asset attach/replace/collect`、`vc canvas output list/select` 可完成素材挂载与结果选择。结构化编辑使用 `canvas character/director/storyboard get/set`，更新时指定观察版本。

多个协作生成与自动拆分镜任务可共用 `--budget-key <编号> --budget-credits N`。上限绑定当前账号、画布和协作代数，并与每次请求的 `--max-credits` 同时生效。服务端统计在途预留及已结算用量。

脚本可通过 `VC_AUTOMATION_CREDENTIALS` 使用用户在终端签发的画布专用凭据文件。`canvas automation issue <画布> --scope read|edit --output <文件> [--hours N]` 签发，`canvas automation list|revoke` 管理本人签发的凭据。凭据用于受限图谱读取、增量回放、收据和普通编辑。脚本需在原站点与画布使用凭据文件，并保护文件和密钥内容。

多批计划的输入使用 `batches`，每批包含 `id`、`changes` 和可选的 `after`。按依赖顺序声明批次，部分成功时从 `error.details.runId` 取得运行编号并传给 `canvas resume --run`。

`vc canvas storyboard shot list/add/set/move/rm` 按稳定 ID 操作镜头，写入需要列表返回的 `--epoch` 和 `--revision`。`vc canvas storyboard split <画布> <节点> --script-file <文件> --mode append --max-credits N` 提交拆分镜，用 `canvas storyboard status/wait <画布> <任务>` 检查状态。角色形象和独立音色使用 `canvas character generate --kind image|voice`，指定模型、提示词和积分上限。

`vc canvas director scene get <画布> <节点>` 读取导演台场景及其版本。`vc canvas director scene apply <画布> <节点> --epoch E --revision N --input edit.json` 提交对象变更。示例 `{"action":"set","collection":"props","id":"wall","fields":{"x":4}}`。集合可用 `characters`、`props` 或 `cameras`，动作可用 `add`、`set` 或 `rm`。删除需要确认，镜头关键帧以对象的 `keys` 字段更新。

`canvas character generate --kind image-and-voice` 依次提交两个任务，分别使用 `--image-model`、`--image-input <JSON文件>`、`--voice-model`、`--voice-input <JSON文件>` 和共享的 `--max-credits`。`--image-max-credits` 指定图像分配额度，余量用于音色。重启后保留原参数和 `--idempotency-key`。`canvas node list/get/move`、`canvas edge add/rm`、`canvas group add/set/rm` 用于图结构操作，写入时携带观察版本和代数。

`vc asset download <素材ID> [--canvas <画布>] -o <文件>` 下载有权访问的媒体。画布素材须指定画布编号，输出路径须为新文件。

`canvas node refs <画布> <节点>` 查看连线和手选参考。替换手选参考时提供 `--input`、`--epoch`、`--revision`，输入如 `{"images":[{"assetId":"123"}]}`。`canvas node duplicate` 复制文本和媒体节点的配置与入边。`canvas node arrange <画布> <节点...> --mode row|column|grid` 整理所选节点。两者都需要观察版本与代数。

`canvas media frame` 用 `--which first|last` 或 `--at-ms <毫秒>` 抽帧为 PNG。`canvas media trim` 用 `--from-ms` 和 `--to-ms` 修剪为视频 MP4 或音频 M4A。两个命令需要本机 FFmpeg、明确的画布和节点、`--epoch`、`--revision` 以及新建的 `-o` 本地路径。结果自带来源素材和版本。

`canvas media grid` 将授权图片切成 PNG 并写入 `-o` 指定的新目录。默认 3×3，可用 `--rows N --columns N` 均分，或用 `--input` 指定 `{"xs":[0.3],"ys":[0.75]}` 格式的比例切线。需要 FFmpeg 和明确的 `--epoch` 与 `--revision`。

`canvas media annotate` 将 `--input` 笔画文件画在已确认的授权图片上。格式为 `{"strokes":[{"color":"#FF0000","width":3,"points":[[0.25,0.5],[0.75,0.5]]}]}`，左上角为 `[0,0]`，右下角为 `[1,1]`，宽度按原图像素计算。需要本机 FFmpeg，传入 `--epoch`、`--revision` 和尚未存在的 `-o` PNG 路径。

`canvas media stage <画布> <来源节点> --file <文件> --source-asset ID --epoch E --revision N --op-id ID` 暂存加工文件。使用加工结果中的来源素材 ID 和观察版本。`canvas media attach <画布> --epoch E --op-id ID` 原子确认素材、节点和来源连线。挂载超时可用同一操作编号重试。未消费的产物一小时后进入回收。

`canvas media outpaint` 先在本机生成透明扩图底片。用 `canvas media stage` 和 `canvas media attach` 确认底片节点，再用 `canvas media outpaint-submit` 按模型能力提交图生图任务。扩图任务仍走协作版本、生成授权、预算和幂等键。

`canvas compose <画布> --nodes <节点1,节点2,...> --epoch E --revision N --op-id ID [--wait]` 以指定顺序合成 1–24 段视频。也可用 `--node <成片节点>` 按当前上游顺序取片段。用 `task get|wait|cancel <任务> --canvas <画布> --epoch E` 恢复查询和控制。提交结果不明确时，保留原节点顺序、版本和操作编号重试。

`canvas task list <画布> [--limit N]` 查看协作编辑成员可见的拆分镜和角色音色摘要。`canvas watch <画布> --tasks --jsonl` 监听 `task-summaries` 事件。个人账单、请求参数、错误详情和运行控制沿用发起人的任务接口。

`vc canvas comment list/add`、`vc canvas mention list` 和 `vc canvas history list/show/copy` 支持审阅。`vc canvas undo/redo --target N` 绑定个人操作版本。共享和授权用 `canvas share`、`canvas member`、`canvas collaboration` 子命令，涉及退出、转移、授权以及撤销时由用户自己确认。

所有者可用 `vc canvas rename <画布> --title <标题>` 改名。`vc canvas archive <画布>` 会预览将失去访问的编辑者和查看者人数，必须由用户在终端确认。成员范围或画布版本变化时，重新读取影响范围再确认。

## 五、画布 Agent（小镜）

```
agent sessions <画布编号>
agent history <会话编号> [--before 序号] [--limit 50]
agent run <画布编号> --message "…" [--selection 节点id,节点id] [--budget 积分] [--attach 素材ID]
          [--session 会话编号] [--watch] [--interval 2] [--timeout 600]
agent events <会话编号> [--after 序号]
agent approve <审批编号> [--session 会话编号] [--items 0,1] [--reject] [--answer 文本]
agent interject <运行编号> --message "…" [--attach 素材ID]
agent resume <运行编号> [--add-budget 积分]
agent cancel <运行编号>
agent undo <运行编号>
```

- `run` 返回 `{session_id, run:{id, session_id, status}}`。不发 `--session` 时复用该画布已有的会话（没有就新建一条，
  标题「命令行」）；网页与命令行看的是同一条会话。给了 `--session` 就先校验它属于这张画布，
  不存在报用法错误（退出码 2），不新建会话、不提交运行。
- `--selection` 是节点 id 列表，圈定这次允许它动的范围；不给由它自己读画布。
- `--budget` 换算成这一轮的花费上限（积分），不给用服务端默认。
- `--watch` 从会话**当前序号之后**开始跟：只打印本轮新事件，不重放历史。收尾条件：出现 `approval.created`（等你批准）；
  `run.status` 进入终态（`succeeded` / `failed` / `timeout` / `interrupted` / `budget_exhausted` / `step_limit` /
  `canceled` / `expired`）；或回了正文之后没有未结束的工具调用、且之后连续两轮拉取都没有新事件。
  `approval.decided`（已处理的审批）不算在等人。拉取遇到网络错误 / 5xx 会退避重试；退出码 3 表示等到 `--timeout` 仍没结束。
- 事件形如 `{session_id, canvas_id, run_id, seq, type, data, created_at}`。`type` 常见值：
  `message.user`、`run.status`、`tool.start` / `tool.end`、`message.done`（`data.text` 是正文）、
  `approval.created`（`data.id` 是审批编号，`data.payload` 是要批准的动作）、`approval.decided`、`run.usage`。
- `approve`：`decision` 为 `approve` / `reject`；`--items` 只批指定序号（不传表示整批通过），
  `--answer` 回答它顺带提出的问题。返回审批视图（`id` / `kind` / `status` / `payload` / `quote_credits` / `expires_at`）。
  批准前会把将批准的条目列到 stderr：后端没有按审批编号查详情的接口，条目只在会话事件里，所以要带 `--session`
  才能展示（缺了会明说看不到）；在终端里再输入 `yes` 才提交，脚本 / `--json` 下只展示不拦（软闸门）；`--reject` 不需要确认。
- `cancel` 只停这一轮；它已经提交的生成任务在服务端继续跑完、产物照常写回节点。
- `history` 用返回的 `before` 继续翻页。`run` 和 `interject` 可重复传入 `--attach`，每条消息最多 12 个素材 ID。`resume` 可追加本轮预算。`undo` 须输入 `yes`，以服务端返回的实际撤销结果为准。

## 六、短剧（只读 + 任务控制）

| 命令               | 参数                                 | 说明                                                                                         |
| ------------------ | ------------------------------------ | -------------------------------------------------------------------------------------------- |
| `drama list`       | `--keyword`、`--page`、`--page-size` | 返回 `{list,total,page,page_size}`                                                           |
| `drama get`        | `<短剧编号>`、`--episodes 10`        | 集多时只列前 N 集（`0` 全部）；含 `canvas_id`、`phase`、暂停原因                             |
| `drama units`      | `<短剧编号>`、`--from`、`--to`       | 单元号区间；服务端返回 `{items:[…]}`，字段 `no` / `title` / `chars`                          |
| `drama unit`       | `<短剧编号>`、`<单元号>`             | 单个单元正文                                                                                 |
| `drama jobs`       | `<短剧编号>`、`--active`             | 字段 `step` / `status` / `progress` / `episodes` / `lane` / `credits`；集号超过 6 个自动压缩 |
| `drama job-cancel` | `<短剧编号>`、`--job`                | 取消未落终态的任务                                                                           |

短剧编号与画布编号同一套编码（32 位十六进制）。建项目、规划、生成剧本、分集出片不在 CLI 范围。

## 贯穿：运行记录

`trace pull` / `trace show` / `trace compare` 原样转发 agent-trace 脚本。npm 包不带该脚本，
而且这个命令会执行脚本，所以**不会**自动去当前目录或仓库里找：必须用 `--script <路径>` 或环境变量 `VC_AGENT_TRACE`
明确指定，执行前会在 stderr 提示「这个脚本会以你的权限运行」。

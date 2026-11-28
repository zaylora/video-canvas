# video-canvas-cli（beta）

连镜（video-canvas）的命令行。用设备码登录，不用把密码或长期令牌交给脚本；画布、模型与积分、生成任务、
画布 Agent（小镜）、短剧只读、运行记录都走已有的 HTTP 接口。

**beta 渠道**：相当一段时间内保持测试状态，命令与参数尽量稳定，但输出格式与子命令仍可能调整。
每次在终端里运行会在后台查一次 GitHub 最新 Release 的版本，有更新时在 stderr 提示升级命令（`VC_NO_UPDATE_CHECK=1` 可关掉）。

> 完整参考（5 分钟上手、输出契约、退出码表、排障）在站点 `/cli/docs`，
> 正文是 `web/src/content/cli-docs.md`，改文档改那一份即可。

## 安装

安装包随每次站点发版挂在 GitHub Release 上（不发布到 npm 仓库），需要 Node 22.18+（随包技能里的脚本直接跑 `.ts`）。站点提供一键脚本（下载、校验 sha256、装 CLI、装技能）：

```bash
curl -fsSL <站点>/api/v1/cli/install.sh | sh          # macOS / Linux
irm <站点>/api/v1/cli/install.ps1 | iex               # Windows PowerShell
```

脚本内嵌在后端（`backend/internal/cliinstall/`），由 `GET /api/v1/cli/install.sh|.ps1` 下发。也可以手动装（不校验 sha256）：

```bash
npm i -g https://github.com/zaylora/video-canvas/releases/latest/download/video-canvas-cli.tgz
vc skill install    # 把技能装进终端助手
vc --version        # 与 video-canvas-cli --version 等价；--json 给脚本读
```

升级就是重跑同一条命令。版本只看 `package.json`，**与站点版本无关**；发版时 `release-docker.yml` 用 `npm pack` 打包，
连同 `.sha256` 与清单 `video-canvas-cli.json` 一起挂到 Release 上。
运行时零第三方依赖。

## 配套技能

安装包里带一份 `skill/`：给终端里的 AI 助手（Claude Code、Codex、WorkBuddy 等）看的操作手册。
它不参与 `vc` 运行，但要装进助手的技能目录才会生效：

```bash
vc skill install                  # 装进 ~/.claude/skills；已有 ~/.codex、~/.agents 的也一并装
vc skill install --to <技能目录>  # 只装到指定目录（可重复给）
vc skill path                     # 第一行是随包技能目录；--json 给脚本
```

升级 CLI 之后重新执行 `vc skill install`，它会整个替换旧版本。`skill/` 是随包发布的正文，改它等于改用户拿到的东西；
`cli-docs.md` 是站点文档，两者分工不同。

## 开发（改源码才需要）

源码是 TypeScript（`src/*.ts`）；发布的是编译产物 `dist/*.js`。

```bash
cd cli
npm ci
npm run build        # tsc -p tsconfig.json → dist/
npm run typecheck    # 源码 + 测试一起做类型检查
npm run lint         # oxlint
npm run format       # oxfmt（只格式化 src 与 test）；format:check 只检查
npm test             # 先 build，再用 node --test 跑全部用例
node dist/cli.js --help
```

- 测试跑的是构建产物 `dist/cli.js`，所以改源码后必须先 build（`npm test` 会自动做）。
- 测试用 Node 的类型擦除直接跑 `.ts`（`import` 里写 `.ts`，`tsc` 产出时改写成 `.js`），开发要 Node 22.18+。
- 版本号只改 `package.json`，`vc --version`、站点页面都读它。
- `npm run prepublishOnly` 会先 typecheck、build、test；发布流程见仓库根的发版说明，不要在本地直接 `npm publish`。

## 登录

```bash
vc login --base https://你的站点
```

1. 终端打印用户码（形如 `ABCD-EFGH`）和完整的授权地址，并尝试自动打开浏览器（`--no-open` 可关掉）。
2. 在网页上**核对页面上的用户码与终端一致**再确认授权（页面在 `/cli/authorize`，需要已登录）。
   不是你本人刚发起的登录，请不要点同意——拿到令牌的一方会获得你账号的完整权限。
3. 命令行拿到 JWT，存到 `~/.config/video-canvas/credentials.json`（连同登录时的站点地址）。

`--base` 只给站点根即可，会自动补 `/api/v1`；也可以写进环境变量 `VC_API_BASE`。
**必须带 `http://` 或 `https://`**——只写域名会直接报错，不做猜测补全。
非 `https` 且不是本机（localhost / 127.0.0.1）的地址会在 stderr 警告：令牌与内容会明文经过网络。

授权页默认由 API 地址推出（去掉 `/api/v1`）。API 和网页不在同一个域名下时，推出的地址打不开，
用 `--web https://网页地址`（或 `VC_WEB_URL`）指定；无论如何，完整的授权地址和用户码都会打印在 stderr。

```bash
vc whoami     # 验证令牌是否仍然有效
vc logout     # 只清本机凭据
```

- **令牌绑定登录时的站点**：`--base` / `VC_API_BASE` 指向别的站点时，命令会直接拒绝并提示重新 `login`，
  保存的令牌不会被发给没登录过的站点。
- **`logout` 只清本地文件**。登录态是自包含的 JWT，服务端没有吊销接口：它要到期（默认 30 天）
  或在网页上改密码后才会失效，输出里也会如实说明。凭据文件泄露了，请直接改密码。
- 令牌与网页登录是同一套 JWT。有效期不同：命令行登录态**默认 30 天**（后台「注册设置 → 命令行登录有效期」可改，
  1–365 天），网页会话按 `jwt.expire_hours`。它与「设备码 10 分钟」「MCP 令牌天数」「--wait 本地等待」都是不同的数。
- 凭据目录权限 `0700`、文件 `0600`（目录已存在也会收紧）。**Windows 不支持 POSIX 权限位，这一条在 Windows 上不生效**，
  靠用户目录本身的访问控制隔离。
- 登录轮询遇到网络错误或服务端 5xx 会按退避重试直到设备码过期；4xx 不重试。

## 画布

```bash
vc canvas list
vc canvas get <code>
vc canvas create --title "新项目" [--payload graph.json]   # 需在终端输入 yes
vc canvas import export.json                               # 需在终端输入 yes
vc canvas export <code> -o export.json [--force]
vc canvas graph <code> -o graph.json                       # 只取 payload_json，并给出 revision
vc canvas save <code> --payload graph.json --revision N
```

`-o` 与 `--output` 等价；`-o -` 把内容写到 stdout（此时不能再加 `--json`）。

**`graph` 和 `export` 不是一回事**：`graph` 输出 `{nodes, edges, viewport}`，是 `save` / `create --payload`
要的形态；`export` 输出带 `format` / `canvas` / `assets` 的导出文档，只能喂给 `import`。
把导出文档喂给 `save` 会被拒绝。

`export` 不给 `-o` 时默认写当前目录的 `canvas-<编号>.json`；**目标文件已存在时不覆盖**，确认要覆盖加 `--force`。

**`canvas save` 的 `--revision` 必填**：它是「你手里这份图取自哪个版本」。`canvas graph` 的输出里会带版本
（写文件时在输出和下一步提示里；`-o -` 或不带 `-o` 时 stdout 只放图，版本写在 stderr；`--json` 时在 `data.revision`）。
整图保存走 `revision` 乐观锁，别人在这期间改过画布会得到 409，不会被覆盖。
如果新图会让现有节点消失，还要在终端里输入 `yes` 确认（规则同 `node rm`，不能与 `--json` 一起用）。

脚本批量改个人画布的推荐流程：`canvas graph <code> -o g.json`（记下版本）→ 改 `g.json` → `canvas save <code> --payload g.json --revision <版本>`。
只增改、不删节点时全程不需要终端确认。

## 实时协作

```bash
vc canvas list --scope shared
vc canvas status <code> --json
vc canvas watch <code> --jsonl
vc canvas presence <code> --json
vc canvas watch <code> --follow <成员用户名或ID> --jsonl
vc canvas watch <code> --epoch 1 --since 120 --once --jsonl
vc canvas watch <code> --epoch 1 --comments-since <评论游标> --jsonl
vc canvas plan <code> --input changes.json -o plan.json
vc canvas apply <code> --plan plan.json --json
vc canvas op status <code> <操作编号> --epoch 1 --json
vc canvas resume <code> --run <操作编号> --json
```

单批计划使用 `{"changes":[{"kind":"node","id":"shot-1","fields":{"data.prompt":"新提示词"}}]}`。`kind` 支持 `node`、`group` 和 `edge`。创建用 `action: "create"` 加 `value`，删除用 `action: "delete"` 并在应用时确认。`plan` 固定当前确认版本、账号、站点和协作代数。冲突后根据当前内容创建新计划。`resume` 先查询收据，缺少收据时使用原编号和载荷重试。局部写入使用 `plan`、`apply` 或交互式 `canvas node`。活跃协作画布拒绝 `canvas save`。

多批计划使用 `{"batches":[{"id":"nodes","changes":[...]},{"id":"links","after":["nodes"],"changes":[...]}]}`。每批单独原子提交，使用同一个观察版本。依赖未确认时，依赖该批的操作暂停，独立批次可继续。部分成功时退出码为 1，`--json` 中的 `error.details` 包含 `runId` 及各批状态。用 `vc canvas resume <画布> --run <runId>` 恢复。

- `canvas watch` 接收共享会话的确认改动及在线名单变化。用 `--since` 恢复时，同时填写事件中的 `--epoch`。历史缺口会输出新快照，权限收回则终止监听。只用 `snapshot` 或 `change` 事件推进已确认的回放游标。`--follow` 只展示指定账号明确共享的选区和视角。
- 支持评论回放的站点会向所有者和编辑者的持续监听输出 `comment` 和 `comment-cursor`。默认从首次查询的评论水位开始。恢复时传入 `--comments-since <游标> --epoch E`，使用 `0` 可读取当前代数的全部可见评论。该选项也支持 `--once`，独立于图谱的 `--since`。
- 持续监听输出当前节点运行的 `generation-progress`，包括节点 ID、运行版本和百分比。任务终态以 `change` 或 `snapshot` 中的确认状态为准。
- `canvas presence` 采样其他在线成员，按账号合并网页与终端。加 `--follow` 才会输出指定队友共享的选中节点和视角。跟随不可用时，终端输出空视角。CLI 心跳和跟随输出均不修改画布。
- `canvas node list/get/move` 用于定位与移动节点。`canvas edge add/rm` 和 `canvas group add/set/rm` 用于连线及分组。写入使用读取结果的 `--epoch` 和 `--revision`。
- `canvas node refs <画布> <节点>` 分别显示入边和手选参考。用 `--input <JSON文件> --epoch E --revision N` 替换手选参考。格式为 `{"images":[{"assetId":"123"}]}`，也支持 `videos`、`audios`。`canvas node duplicate` 复制文本和媒体节点的输入配置与入边。`canvas node arrange <画布> <节点...> --mode row|column|grid` 整理所选节点。复制和布局都要求观察版本及代数。
- `canvas character|director|storyboard get/set` 可读写单个结构化节点。`set` 需要 JSON 文件和 `get` 返回的确认版本。`canvas output list/select` 可查看并选择节点候选。
- `canvas storyboard shot list/add/set/move/rm` 按镜头 ID 操作。写入须带 `list` 返回的 `--epoch E --revision N`，编辑用 `--input <JSON文件>`，排序用 `--index <镜号>`，删除需要终端确认。不同镜头或同镜头的不同字段可以并行提交。同字段被修改、目标已删除或重排基准已改变时，CLI 返回冲突，需要重读。
- `canvas storyboard split --script-file <文件> --mode append|replace --max-credits N` 提交拆分镜，覆盖需要终端确认。用 `canvas storyboard status/wait <画布> <任务>` 对账。请求结果不确定时沿用原参数和幂等键。
- `canvas character generate <画布> <节点> --kind image|voice --model <模型> --prompt <提示词> --max-credits N` 生成角色形象或独立音色，可加 `--wait` 等待结果。
- `canvas character generate <画布> <节点> --kind image-and-voice --image-model <模型> --image-input <JSON文件> --voice-model <模型> --voice-input <JSON文件> --image-max-credits I --max-credits N` 先生成形象，再以余额上限 `N - I` 生成音色。保持参数和 `--idempotency-key` 不变即可续接。按模型需要用 `--voice-from-image` 引用本次角色主图。
- `canvas character/director/storyboard set` 对完整结构字段提交版本锁定。导演台、分镜和角色卡会要求确认版本仍然一致，避免覆盖队友的对象级更新。
- `canvas director scene get <画布> <节点>` 读取场景和观察版本。`canvas director scene apply <画布> <节点> --epoch E --revision N --input edit.json` 按对象 ID 修改，文件示例为 `{"action":"set","collection":"props","id":"wall","fields":{"x":4}}`。支持 `characters`、`props`、`cameras`，同一属性冲突时先重读。关键帧按所属对象的 `keys` 字段提交。删除需要终端确认，仍被引用的角色或活动机位须先解除引用。
- `asset library list` 查看个人资产库。使用 `canvas asset attach/replace/collect` 在媒体节点挂载、替换或收藏授权素材。
- `asset download <素材ID> [--canvas <画布>] -o <文件>` 流式下载个人或画布授权素材。使用 `--canvas` 会复核画布授权和当前引用。目标文件必须未被占用。
- `canvas media frame <画布> <视频节点> --epoch E --revision N --which first|last -o frame.png` 支持首尾帧。用 `--at-ms <毫秒>` 指定时间。`canvas media trim <画布> <视频或音频节点> --epoch E --revision N --from-ms N --to-ms N -o clip.mp4` 修剪视频，音频目标扩展名用 `.m4a`。两条命令需要本机安装 FFmpeg，读取授权素材并输出到新建本地文件。
- `canvas media grid <画布> <图片节点> --epoch E --revision N -o tiles` 默认均分为 3×3，使用 `--rows` 和 `--columns` 指定行列，或传入 `--input cuts.json` 自定义切线，如 `{"xs":[0.3],"ys":[0.75]}`。需要 FFmpeg。结果按行写入新建的本地目录，并返回每张 PNG 的尺寸、路径和来源版本。
- `canvas media annotate <画布> <图片节点> --epoch E --revision N --input strokes.json -o marked.png` 使用本机 FFmpeg。笔画文件示例为 `{"strokes":[{"color":"#FF0000","width":3,"points":[[0.25,0.5],[0.75,0.5]]}]}`。坐标从左上角起按图片宽高归一化到 `[0,1]`，`width` 用原图像素。结果包含来源版本并写入新建的本地 PNG。
- `canvas media outpaint <画布> <图片节点> --epoch E --revision N --left PX --right PX --top PX --bottom PX -o base.png` 生成透明扩图底片，原图像素保持在指定位置，最长边最多 4096 像素。用 `canvas media stage` 和 `canvas media attach` 将底片挂到协作画布，再用 `canvas media outpaint-submit <画布> <底片节点> --source-asset ID --epoch E --revision N --model KEY --max-credits N --idempotency-key ID [--prompt 文本]` 走图生图模型和协作积分确认。模型和比例参数会按站点能力校验。
- `canvas media stage <画布> <来源节点> --file <产物> --source-asset ID --epoch E --revision N --op-id ID` 暂存标注、宫格单片、抽帧或修剪的本地产物。`--source-asset` 和版本使用加工结果中的 `source`。编辑成员再用 `canvas media attach <画布> --epoch E --op-id ID` 确认素材、派生节点及来源连线。放弃挂载的产物一小时后进入回收。挂载请求中断时，使用同一操作编号重试或查询 `canvas op status`。
- `canvas task list <画布> [--limit N]` 查看拆分镜和角色音色的近期任务，进行中的任务优先。`canvas watch <画布> --tasks --jsonl` 轮询并在列表变化时输出 `task-summaries` 事件。两条命令仅对所有者和编辑者开放，摘要包含节点、任务类型、状态、用户名和更新时间。任务详情继续按发起人权限查询。
- `canvas compose <画布> --nodes <节点1,节点2,...> --epoch E --revision N --op-id ID [--wait]` 按给定顺序提交最多 24 段视频。也可用 `--node <成片节点>` 按现有连线和成片顺序取片段。服务端生成首段分辨率、30 fps 的 H.264/AAC MP4，并将结果节点、素材和来源连线一起挂载。使用 `task get|wait|cancel <任务> --canvas <画布> --epoch E` 查询、等待或取消。响应丢失时可用原节点顺序、版本和操作编号恢复任务。产物可用 `asset download <素材> --canvas <画布> -o result.mp4` 下载。
- `generate … --canvas <code> --node <节点> --max-credits N` 为协作生成设置本次候选的总积分上限。CLI 在预先登记确认和扣费意图后提交。遇到不确定结果时，使用相同 `--idempotency-key`、参数和上限。服务端根据实际报价限制冻结额。`task cancel` 单独取消有权操作的任务。
- `--budget-key <编号> --budget-credits N` 为同一账号、画布和协作代数下的自动拆分镜、普通生成、扩图和角色组合生成设置共用累计上限。每次请求仍须提供 `--max-credits`。同一预算编号的上限固定，服务端以在途冻结或预留加实际结算计算用量。重试沿用原编号及金额。
- `vc canvas media separate-voice <画布> <音频节点> --source-asset ID --epoch E --revision N --model KEY --max-credits N --idempotency-key ID` 提交人声分离。选择站点已启用的人声分离模型。节点历史保留原音频和两条产物，产物的 `stem` 区分人声和伴奏。用 `task get|wait <任务>` 查询或等待。
- 用 `vc canvas automation issue <画布> --scope read|edit --hours 24 --output <凭据文件>` 签发可限时使用的画布凭据。脚本将 `VC_AUTOMATION_CREDENTIALS` 指向该文件。`read` 可读快照、图谱回放和收据，`edit` 还可提交授权范围的普通编辑。用 `canvas automation list|revoke` 查看或吊销本人签发的凭据。
- 有生成权限的所有者或编辑者可以签发 `--scope generate --budget-credits N` 的付费凭据。站点按该凭据的累计积分上限计入在途冻结和已结算费用。每次调用指定 `--max-credits`，省略预算组参数。凭据可提交普通生成和查询自身任务。拆分镜、小镜、媒体上传及高影响管理使用普通登录。吊销、撤权、账号会话换代、画布换代或到期会使凭据失效。
- `canvas comment list/add`、`canvas mention list` 和 `canvas history list/show/copy` 复用节点讨论、提及和版本活动。`canvas undo/redo --target N` 需在终端确认目标版本。
- `canvas collaboration enable/disable`、`canvas member list/role/grant` 和 `canvas share invite/revoke/leave/copy/transfer/receipt` 处理共享授权和交付。退出、转移和其他改权限的操作需要终端确认。
- 所有者用 `vc canvas rename <画布> --title <标题>` 修改标题，协作成员通过增量同步看到更新。`vc canvas archive <画布>` 在终端显示将失去访问的编辑者和查看者人数，确认后从作品列表移除原画布。版本、成员范围变化或有在途任务时，站点拒绝归档。

当前命令范围和验收证据见 [协作规格](../docs/design/画布实时协作/01-CLI协作规格.md#实现与验证)。

## 节点级编辑

只提交差异，由服务端按与 Agent 相同的图语义应用，CLI 不自己拼图。**每次都要在终端里确认**：

```bash
vc canvas node add <code> --kind script --label "旁白" --text "第一句"
vc canvas node add <code> --kind image --label "关键帧1" --prompt "草原上的风机" --at 600,100
vc canvas node set <code> <节点id> --label "新标题"
vc canvas node rm <code> <节点id> [更多节点id...]
```

- 能建的种类只有 `script` / `image` / `video` / `audio`；角色卡、分镜表、成片、导演台要在画面上建。
- 文本节点的正文用 `--text`（服务端写进节点正文），其它节点的提示词用 `--prompt`；值以 `-` 开头也可以直接写
  （`--text "- 列表项"`）。
- `rm` 会连带清理连线、分镜里的挂接、成片顺序，由服务端统一处理；重复给同一个 id 只算一次。
- `canvas create` / `canvas import` 同样要确认：它们会在账号下**新建**画布，而 CLI 没有删画布的命令。
- 流程是「读当前图 → 逐行列出要改什么 → 等你输入 yes → 带同一个 revision 提交」。
  确认期间画布被别人改了，服务端返回 409，不会覆盖你没看过的版本。
- 没有 `--yes` 之类的旁路，也不能和 `--json` 一起用。输入在回答前就结束（Ctrl-D、管道关闭）按「取消」处理，退出码 4。

> **这道「终端确认」是约定性的软闸门，不是安全边界。** 它防的是脚本或 AI 助手一不小心批量改了画布，
> 逼着有人在场；但持有令牌的人依然可以直接调接口，伪造伪终端也能绕过它。真正的边界是令牌本身——
> 别把令牌交给你不信任的程序。

## 上传本地素材

```bash
vc asset upload /path/to/shot.png      # 一次一个文件，返回素材 id
```

`--ref-*` 只认纯数字素材 id，本地文件要先上传。上传复用现有登录态，不花积分；服务端按内容嗅探类型，单文件上限 200MB。上传路径跟随站点配置：能直传就直传对象存储，否则自动降级到服务器中转，与网页上传一致。

## 模型、积分与生成

```bash
vc model list [--kind image|video|audio|text]
vc model show agnes-video-2.5        # 参数名、可选值、参考素材上限、计价
vc credits                           # 余额 / 冻结 / 可用

vc generate image --model agnes-image-2.5-flash \
  --prompt "一枚硬币躺在岩面上，侧光" --param aspect_ratio=16:9 \
  --canvas <code> --node <节点id> --wait --timeout 300
```

- **产物由服务端写回节点**：任务落终态时服务端直接写进画布节点并推送，不依赖浏览器在线。
- `--canvas` 与 `--node` 要么都给、要么都不给（都不给是临时任务，产物只能从任务详情取地址）。
- `--param 名=值` 可给多次，值先按 JSON 解释；复杂结构用 `--input '{...}'`（也支持 `@文件`、`-` 读标准输入，
  内容必须是 JSON 对象）。
- 参考素材用 `--ref-image / --ref-video / --ref-audio`，可给多次或逗号分隔。
- `--count N` 拆成 N 个任务，都绑同一个节点（需要 `--node`）。
- **`--wait` 的多个任务共用一个总截止时间**（`--timeout`，默认 600 秒），并发地等；
  超时（退出码 3）、失败或出错时，`--json` 的 `data.tasks` 仍列出所有已提交任务的 id 与冻结积分。
- **幂等键**：每次提交都带 `Idempotency-Key`，并且**在发请求之前**就打印到 stderr、也写进结果的 `idempotency_key`。
  请求结果不确定（响应丢了、进程被杀）时，加 `--idempotency-key <同一个键>` 重跑同一条命令，不会重复扣费。
- 等待过程中遇到网络错误或服务端 5xx 会退避重试到截止时间；4xx 与业务错误立刻失败。

## 画布 Agent（小镜）

```bash
vc agent sessions <画布编号>
vc agent history <会话编号> [--before 序号] [--limit 50]
vc agent run <画布编号> --message "把 S03 重新生成成 10 秒视频" [--attach 素材ID] [--watch]
vc agent events <会话编号> [--after 序号]
vc agent approve <审批编号> --session <会话编号> [--items 0,1] [--reject]
vc agent interject <运行编号> --message "改成夜景" [--attach 素材ID]
vc agent resume <运行编号> [--add-budget 积分]
vc agent cancel <运行编号>
vc agent undo <运行编号>
```

- `--watch` 跟到运行结束（`run.status` 进入终态）或等到需要审批为止；回了一段话不等于结束，
  还有未完成的工具调用时会继续等。`approval.decided` 是已经处理完的审批，不算在等人。
- `--session` 先校验它属于这张画布：不存在就报错，不会顺手新建会话；事件游标从该会话当前位置开始，不重放历史。
- `history` 返回上一页的 `before` 游标。启动和追加指令可重复传入 `--attach`，每条消息最多 12 个素材 ID。`undo` 需要输入 `yes` 确认，返回服务端实际撤销结果。
- `agent approve` 批准前会把将批准的条目内容列出来。后端没有按审批编号查详情的接口，条目只在会话事件里，
  所以要带 `--session`（`agent run --watch` 打印的命令里已经带上）。在终端里还要再输入 `yes`；
  脚本 / `--json` 下只展示、不拦——同样是约定性的软闸门。驳回（`--reject`）不需要确认。

## 短剧（只读 + 任务控制）

```bash
vc drama list [--keyword 关键词]
vc drama get <短剧编号> [--episodes 10]
vc drama units <短剧编号> [--from 1] [--to 20]
vc drama unit <短剧编号> <单元号>
vc drama jobs <短剧编号> [--active]
vc drama job-cancel <短剧编号> --job <任务id>
```

建项目、规划、生成剧本、分集出片仍在网页与 MCP：它们要长上下文、要逐步核对、要花大额积分。

## 生成任务

```bash
vc task get <id|task_ref>
vc task wait <id|task_ref> [--timeout 600] [--interval 3]
```

`task wait` 默认 600 秒超时、3 秒轮询；只在终端打印状态变化，最终结果走 stdout；`--json` 时 stdout 只有一行 JSON，可直接接管道。

- 任务**失败**终态：`ok:false` + 退出码 1。
- **等待超时**：退出码 3，任务可能还在跑，用 `task get` 继续查。别把超时当失败重跑，会白烧积分。
- `task_ref` 是任务 id 的 32 位十六进制编码，画布界面与日志里显示的就是它。

## 运行记录

`trace` 转发到仓库里的 `scripts/agent-trace/agent-trace.mjs`，参数与原来一致。
npm 安装包**不含**这个脚本，而且这个命令会执行一个脚本，所以出于安全**不会**自动去当前目录或仓库里找：
必须用 `--script <路径>` 或环境变量 `VC_AGENT_TRACE` 明确指定，执行前会在 stderr 提示风险。

```bash
vc trace pull --since 7d --feedback down --script <仓库>/scripts/agent-trace/agent-trace.mjs
vc trace show <run_id> --script <路径>
vc trace compare --by harness_key --since 30d --script <路径>
```

拉下来的记录在 `.traces/`，**已在 `.gitignore` 里，不要提交**，也不要贴到 issue 或外部服务。

## 输出与退出码

- 默认人类可读；加 `--json` 输出 `{"ok":true,"data":...}`，失败输出 `{"ok":false,"error":{"code":...,"message":...}}`。
- 开关型选项（`--json`、`--wait`、`--watch`、`--active`、`--reject`、`--force`、`--no-open`）不会吞掉后面的位置参数，
  `vc canvas get --json <code>` 与 `vc canvas get <code> --json` 等价；仍支持 `--json=true`。
- 退出码：`0` 成功，`1` 业务/网络错误或任务失败，`2` 用法错误，`3` `--wait` / `task wait` 等待超时
  （任务仍在跑，不是失败），`4` 新建项目、节点级修改、会删节点的整图保存或审批被取消。

## 权限边界

CLI 用的令牌等同网页登录的权限：能访问该用户自己的画布、素材、生成任务与积分，也能上传本地素材到自己的素材库（与网页端同一个接口，同样的类型嗅探和大小上限）。管理员命令（渠道、模型、用户管理）没有封装进这一版，避免一个脚本令牌顺手改掉平台配置。

**不改节点产物字段**：生成的产物由服务端写回节点，CLI 不去改 `src` / `assetId` / `outputs`，
免得绕过画布的版本归并。**不删画布**，**不做短剧重活**。协作成片可用性由画布的 `compose` 能力声明。
细节见站点 `/cli/docs` 的「边界与限制」。

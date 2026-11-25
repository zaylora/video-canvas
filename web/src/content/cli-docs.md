# 连镜命令行（video-canvas-cli）beta

给人和脚本用的命令行：设备码登录，不用把密码或长期令牌交给脚本；画布、模型与积分、生成任务、短剧只读、
运行记录都走已有的 HTTP 接口。

用 TypeScript 写、运行时零第三方依赖，需要 Node 22.18+（随包的技能脚本直接跑 .ts，低于这个版本要带实验开关）。装好后有两个等价的命令名：`vc` 与 `video-canvas-cli`。

> **beta 渠道**：相当一段时间内保持测试状态。命令名与参数会尽量稳定，但输出格式、提示语、
> 新增子命令仍可能调整。每次运行会在后台查一次最新版本，有新版本时在 stderr 提示一行升级命令
> （`--json` 与非交互终端不打扰脚本，`VC_NO_UPDATE_CHECK=1` 或 `--no-update-check` 可彻底关掉）。

```text
一条主线的顺序：接入 → 项目 → 素材 → 画布 → 短剧

1 接入
  login [--base URL] [--name 名称] [--no-open]    设备授权登录（在浏览器里确认）
  whoami                                           校验令牌并打印当前身份
  logout                                           删除本地凭据（服务端登录态到期或改密码才失效）
  version [--json]                                 打印 CLI 版本（与站点版本独立）

2 项目
  canvas list [--page N] [--page-size N]           我名下的画布，分页
  canvas get <code>                                详情：标题、版本、节点与连线
  canvas graph <code> [-o 文件]                    只要 nodes / edges（可写回的形态）
  canvas create --title <标题> [--payload 文件]    新建画布（需终端确认）；--payload 要 graph 的形态
  canvas export <code> [-o 文件]                   导出文档（含素材引用），备份 / 迁移用
  canvas import <导出文件.json>                     从导出文档新建画布（需终端确认）

3 素材
  model list [--kind image|video|audio|text]       可用模型：key、种类、说明
  model show <模型key>                             这个模型能填哪些参数、参考素材上限、计价
  credits                                         余额 / 冻结 / 可用积分
  generate image|video|audio|text --model <key> [--canvas <code> --node <节点id>]
           [--prompt 提示词] [--param 名=值]... [--input '<json>'|@文件|-]
           [--ref-image 素材id]... [--count 1] [--wait] [--timeout 600] [--interval 3]
                                                   提交生成（产物由画布回填到节点）
  task get <id|task_ref>                           查一次任务
  task wait <id|task_ref> [--timeout 600] [--interval 3]
                                                   阻塞到终态，可安全用于脚本

4 画布
  canvas node add <code> --kind <种类> [--label 标题] [--text 正文] [--prompt 提示词]
                        [--model 模型] [--at x,y]  新增单个节点（需终端确认）
  canvas node set <code> <节点id> [--label 标题] [--text 正文] [--prompt 提示词] [--model 模型]
                                                   修改单个节点（需终端确认）
  canvas node rm <code> <节点id>...                删除节点，连带清理连线与引用（需终端确认）
  canvas save <code> --payload 文件 [--revision N] 整图保存（乐观锁）

5 短剧（另一条业务线：只读 + 任务控制）
  drama list [--keyword 关键词] [--page N] [--page-size N]
  drama get <短剧编号> [--episodes 10]             默认只列前 10 集，0 表示全部
  drama units <短剧编号> [--from 1] [--to 20]
  drama unit <短剧编号> <单元号>
  drama jobs <短剧编号> [--active]
  drama job-cancel <短剧编号> --job 任务id

贯穿全线
  trace pull [--since 7d] [--feedback down] ...    拉线上 Agent 运行记录（转发 scripts/agent-trace）
  trace show <run_id>
  trace compare --by harness_key [--since 30d]

全局选项
  --json               输出一行 JSON（{ok,data} / {ok:false,error}），供脚本与 Agent 解析
  --base URL           覆盖 API 地址（也可用环境变量 VC_API_BASE）；与登录站点不一致会被拒绝
  --no-update-check    本次不检查新版本（也可用 VC_NO_UPDATE_CHECK=1）
  -o, --output 文件     仅 canvas graph / export：写文件；`-o -` 表示写 stdout
```

`-o` 与 `--output` 完全等价。写成 `-o -` 表示把内容输出到 stdout；此时不能再加 `--json`，
因为 stdout 只能放一种内容（会直接报用法错误）。

## 业务主线

这个命令行只干一件事：**把一个项目从「没有」推到「出片」**。所以下面按这条线的阶段排，
命令跟业务阶段对齐，不按「读 / 写」切开：

| 阶段   | 你要做的事                     | 命令                                                      | 门槛                         |
| ------ | ------------------------------ | --------------------------------------------------------- | ---------------------------- |
| 1 接入 | 登录、换站点、看当前身份       | `login`、`whoami`、`logout`                               | 设备码授权，凭据存本机       |
| 2 项目 | 找画布、看结构、新建、备份迁移 | `canvas list/get/graph/create/export/import`              | 新建与导入**要人确认**       |
| 3 素材 | 看模型与积分、提交生成、等结果 | `model list/show`、`credits`、`generate`、`task get/wait` | 花钱；默认提交后不等         |
| 4 画布 | 改节点、整图写回               | `canvas node add/set/rm`、`canvas save`                   | 要人确认 + `revision` 乐观锁 |
| 5 短剧 | 看项目 / 单元 / 任务、取消任务 | `drama list/get/units/unit/jobs/job-cancel`               | 只读 + 任务控制              |
| 6 助手 | 要它判断、要它改、替人收审批   | `agent run/events/approve/cancel`                         | 按模型计费；审批要人决策     |

每一步只依赖前一步的产物：**接入拿令牌 → 项目拿画布编号与节点 id → 素材把产物回填到节点 →
画布决定哪些节点连在一起**。所以顺序本身就是参数来源，跳过一步就得自己填 id。

三条贯穿全线的规矩：

- **读的随便跑**：`canvas list/get/graph`、`model list/show`、`credits`、`drama *`、`task get` 都是只读。
- **写的分三档**：`canvas create`（新建）< `canvas save`（整图覆盖，有乐观锁）<
  `canvas node rm`（删数据，有交互确认 + 乐观锁）。新建项目与节点级修改**必须在终端确认**，没有 `--yes`。
- **花钱的要显式等**：`generate`（按次或按秒冻结积分）默认只提交、不等结果；
  要看结果得加 `--wait`，本机等超时是退出码 3（任务仍在跑），不是失败。

## 安装、升级与配套技能

CLI 的版本号与站点版本**无关**，安装包随每次站点发版挂在 GitHub Release 上（不发布到 npm 仓库），需要 Node 22.18 及以上。
包内是编译后的 JavaScript（`dist/`）与配套技能；源码用 TypeScript 写，仓库内开发时执行 `npm install && npm run build`。

一键安装（装好 CLI，并把配套技能装进终端助手）：

```bash
# macOS / Linux
curl -fsSL <站点>/api/v1/cli/install.sh | sh

# Windows PowerShell
irm <站点>/api/v1/cli/install.ps1 | iex

# Windows CMD
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm <站点>/api/v1/cli/install.ps1 | iex"
```

地址和 `vc login --base` 用的是同一个接口根（`/cli` 页和「复制命令给 Agent」给出的是本站的完整地址）。
脚本是编译进后端的固定内容，不随请求变化，也不含任何站点地址。它从 GitHub 最新 Release 下载 `video-canvas-cli.tgz`
和同名的 `.sha256`，**校验通过才**交给 `npm i -g` 本地安装，最后执行 `vc skill install`。
全局目录没有写权限时（macOS / Linux），脚本改装到 `~/.local` 并提示把 `~/.local/bin` 加进 PATH，不会调用 sudo。

可选环境变量（PowerShell 里写成 `$env:名字 = "值"`）：

| 变量               | 作用                                                                    |
| ------------------ | ----------------------------------------------------------------------- |
| `VC_CLI_VERSION`   | 装指定版本：填 Release 的 tag，如 `v0.7.3`；默认最新                    |
| `VC_DOWNLOAD_BASE` | GitHub 访问不畅时换成镜像目录，目录下要有同名的 `.tgz` 与 `.tgz.sha256` |

也可以手动装：

```bash
npm i -g https://github.com/zaylora/video-canvas/releases/latest/download/video-canvas-cli.tgz
vc skill install
```

手动安装不会校验 sha256；在意这一点就用上面的一键脚本。

装好后有 `vc` 与 `video-canvas-cli` 两个命令。侧栏头像菜单的「在 Agent 里使用」弹窗和 `/cli` 页上都有「复制命令给 Agent」，复制的是一段完整提示
（按系统挑的三条安装命令 + 本站登录命令），粘给终端里的 AI 助手，它会装好并带你完成登录授权。

- 升级：重跑一键脚本，或执行上面的手动安装命令（升级后再 `vc skill install` 一次）。
- 卸载：`npm uninstall -g video-canvas-cli`。
- 固定版本：一键脚本加 `VC_CLI_VERSION=v0.7.3`，或手动装 `https://github.com/zaylora/video-canvas/releases/download/v0.7.3/video-canvas-cli.tgz`。

### 自检

```bash
vc --version          # video-canvas-cli 0.5.0
vc --version --json   # {"ok":true,"data":{...,"node":"v22.22.2","os":"darwin","arch":"x64"}}
```

`cli/package.json` 的 `version` 是唯一来源：Release 附带的清单与 `vc --version` 都读它，不存在两处各写一个数。源码是 TypeScript：仓库里 `cd cli && npm install && npm run build` 之后
`node dist/cli.js --help` 就能跑；Node 22.18+ 也可以直接 `node src/cli.ts --help`（靠类型擦除，不产出文件）。

### 配套技能（给终端里的 AI 助手）

安装包里还带一份 `skill/`：给终端 Agent（Claude Code、Codex、WorkBuddy 之类）看的操作手册——
什么时候该用 vc、模型与参数怎么查、节点 id 怎么挑、哪些操作要人确认。它不参与 `vc` 运行，
一键脚本已经装好；手动装 npm 包时执行一次：

```bash
vc skill install                  # 装进 Claude Code；已装 Codex（~/.codex）的也一并装
vc skill install --to <技能目录>  # 其它助手，例如 ~/.workbuddy/skills
vc skill path                     # 只看随包技能在哪
```

每次升级 CLI 之后再执行一次 `vc skill install`：它会整个替换旧版本，不留过时文件。

技能是给助手看的说明文档；命令由本机的 `vc` 执行。
画布里的 Agent 跑在服务端、没有命令行，所以这份东西不在那边的技能库里。

## 一条主线走完：从登录到出片

```bash
# ① 接入：登录（打印用户码并打开浏览器，在网页上点「允许」）
vc login --base https://你的站点
vc whoami

# ② 项目：看有哪些画布，建一张，取可写回的图
vc canvas list
vc canvas create --title "临时验证"        # 记下返回的画布编号
vc canvas graph <画布编号> -o 画布.json

# ③ 素材：看模型与积分，生成一张图并等它出结果
vc model list --kind image
vc model show <图片模型key>
vc credits
vc generate image --model <图片模型key> --prompt "一枚硬币躺在岩面上，侧光" \
  --param aspect_ratio=16:9 --canvas <画布编号> --node <图片节点id> --wait --timeout 300

# ④ 画布：改一两个节点不用整图（会先把差异摆出来等你确认）
vc canvas node add <画布编号> --kind script --label "旁白" --text "第一句"

#    要整图改，就走 graph → 编辑 → save；带 --revision 才是乐观锁
vc canvas save <画布编号> --payload 画布.json --revision 3

# ⑤ 备份 / 迁移（主线之外的常用操作）：export → import（save 只接受 graph）
vc canvas export <画布编号> -o 备份.json
vc canvas import 备份.json

# 收工
vc logout
```

画布编号是 32 位十六进制串（`idcodec` 编出来的对外编号），不是数据库自增 id。

`graph` 与 `export` 是两种文档：`graph` 是 `{nodes, edges, viewport}`，给 `save` / `create --payload` 用；
`export` 带 `format` / `canvas` / `assets`，给 `import` 用。把导出文档喂给 `save` 会被拒绝。

`export` 不给 `-o` 时默认写当前目录的 `canvas-<编号>.json`。在仓库或项目目录里跑脚本时建议总是显式给 `-o`，
避免多出一个未跟踪文件。

## 一、接入：登录、身份与站点

一切从一枚本机令牌开始：`login` 走设备码，在浏览器点「允许」，之后所有命令都靠这份凭据。

| 命令      | 参数          | 默认                         | 说明                                                                           |
| --------- | ------------- | ---------------------------- | ------------------------------------------------------------------------------ |
| `login`   | `--base URL`  | 已保存的地址或 `VC_API_BASE` | 站点根或完整的 `/api/v1` 都可以                                                |
|           | `--name 名称` | `video-canvas-cli@主机名`    | 授权页上标注为「自报」的设备名，最长 64 字，仅供参考，不能凭它判断是不是你自己 |
|           | `--no-open`   | 自动开浏览器                 | 只打印授权地址，不拉起浏览器                                                   |
| `logout`  | 无            | ——                           | **只删本地凭据**；服务端令牌到期或改密码才失效                                 |
| `whoami`  | 无            | ——                           | 实时调 `/me`，令牌失效会立刻报错，可当健康检查                                 |
| `version` | `--json`      | ——                           | 也可写 `--version` / `-v`                                                      |

- `login` 的授权地址与用户码**始终写 stderr**，`--json` 时也一样；stdout 只留最后一行结果。
- 网页授权页要求先登录，并且**必须手动输入终端里显示的用户码**才能点「同意」：带 `code` 的链接能被别人转发给你，
  但只有发起命令的人看得到终端里的码。页面还会列出授权账号、发起时间、发起 IP 与自报设备名，不是你刚发起的就点「拒绝」。
- `login` 会轮询到用户确认或 10 分钟过期。`denied` / `expired` / `consumed` / 超时都是退出码 1。
- 登录态**默认 30 天**（后台「注册设置 → 命令行登录有效期」可改，1–365 天），与网页登录的 72 小时分开算。
  命令行是「装一次、长期用」，按网页会话的时长会每天都要重登。
- `logout` 只清本地凭据，不会让服务端令牌失效：要等登录态到期，或在网页改密码（改完所有设备一起失效）。
- `version --json` 的 `data` 是 `{name, version, channel, node, os, arch}`，版本号只读 `package.json`。

**几个「有效期 / 超时」是独立设置**：各自管一件事，改一个不影响另一个。

| 名称                   | 管什么                                 | 缺省    | 在哪改                      |
| ---------------------- | -------------------------------------- | ------- | --------------------------- |
| 命令行登录有效期       | `vc` 登录一次能免登录用多久            | 30 天   | 注册设置 → 命令行登录有效期 |
| 设备码有效期           | 这一次授权确认必须在多久内完成         | 10 分钟 | 代码内固定（防猜码）        |
| MCP 令牌默认有效期     | 外部 AI 工具接入令牌签发时的默认天数   | 30 天   | 注册设置 → 令牌默认有效期   |
| MCP 调用超时           | 外部接入**单次调用**的服务端上限（秒） | 60 秒   | 注册设置 → MCP 调用超时     |
| `--wait` / `task wait` | 命令行**本地**愿意等一次生成多久       | 600 秒  | 命令行参数 `--timeout`      |

最后一行尤其容易混：`--timeout` 只是本机愿意等多久，到点退出码 3，任务在服务端照跑；
把它调大或调小**都不影响**登录态，也不影响服务端给外部接入设的调用超时。

### 多站点与凭据

凭据存在 `$XDG_CONFIG_HOME/video-canvas/credentials.json`（没设 XDG 就是 `~/.config/video-canvas/`），
目录 `0700`、文件 `0600`，里面有站点地址、令牌、过期时间、设备名。

| 想做什么       | 怎么做                                                    |
| -------------- | --------------------------------------------------------- |
| 换到另一个站点 | `vc login --base https://另一站点`，覆盖旧凭据            |
| 临时指定地址   | `--base` 或环境变量 `VC_API_BASE`；必须与登录时的站点一致 |
| 看登录的是谁   | `vc whoami`                                               |
| 清掉本地凭据   | `vc logout`                                               |

`--base` 给站点根（`https://host`）或完整的 `https://host/api/v1` 都可以，两种写法等价。
**必须带 `http://` 或 `https://`**：只写 `example.com` 会直接报「API 地址必须以 http:// 或 https:// 开头」，
不做猜测补全。

令牌**绑定登录时的站点**：`--base` 或 `VC_API_BASE` 指向另一个站点会被拒绝，不会把令牌发过去；
想用另一个站点就重新 `vc login --base 新地址`。地址不是 `https://` 且不是本机（`localhost`、`127.0.0.1`）时会给出警告，
因为令牌会明文传输。

## 二、项目：找到、新建、备份与迁移

项目是后面所有参数的来源：画布编号、节点 id、素材去向都在这里定。

### 查看：列表、详情、节点图、导出

| 命令            | 参数                        | 说明                                                       |
| --------------- | --------------------------- | ---------------------------------------------------------- |
| `canvas list`   | `--page N`、`--page-size N` | page ≥ 1（默认 1）；page-size 不传是 10，服务端上限 50     |
| `canvas get`    | `<code>`                    | 详情；`--json` 的节点在 `data.payload_json`                |
| `canvas graph`  | `<code>`、`-o 文件`         | 只要 `{nodes, edges, viewport}`；不给 `-o` 就打印到 stdout |
| `canvas export` | `<code>`、`-o 文件`         | 导出文档；默认写当前目录 `canvas-<编号>.json`              |

- `canvas list --json` 的 `data` 是 `{list,total,page,page_size}`；人类可读输出是 CODE/REV/UPDATED/TITLE 四列。
- `canvas get` **没有** `-o`：它输出的是画布对象，落盘请用 `canvas export` 或 `canvas graph`。
- `canvas export --json` 的 `data` 是 `{file, skipped_assets}`；`skipped_assets` 是导出时读不到内容的素材数，
  导出的文档因此仍可导入，但会少那几个素材。
- 只读命令不会有副作用，可以随便跑；唯一例外是 `export` 不给 `-o` 时会在当前目录写文件。

### 新建与导入：需要人确认（`canvas create` / `canvas import`）

这两条命令会在账号下**新增**一张画布。命令行是能被脚本与 Agent 调用的，一个循环就能堆出几十张没人知道的空项目，
而 CLI 本身没有删除画布的能力，只能事后去前端一张张删。所以这里和节点级修改用同一道门：

1. 先把「要建什么」打出来：标题、节点与连线数量（或素材份数）、来源文件。
2. 等你在终端输入 `yes` 回车；其它输入、回车、Ctrl-C 都算取消，**退出码 4，一个画布都不会建**。
3. `--json` 与「stdin / stderr 不是终端」一律拒绝（退出码 2）——脚本无法在无人确认时新建项目。

```bash
$ vc canvas create --title "客户 A 的宣传片"
将新建画布：
  标题：客户 A 的宣传片
  内容：空白画布
  来源：命令行参数（无 payload）
确认新建？输入 yes 回车，其它任意输入取消：yes
已创建：客户 A 的宣传片（<画布编号>）版本 1
```

确认只走终端交互。需要批量新建时，在终端逐次确认，或在网页新建。

## 三、素材：模型、积分与生成

素材是唯一花积分、也是唯一产出东西的一步。顺序固定：看模型 → 看余额 → 提交 → 等结果。

### 上传本地素材

`generate` 的 `--ref-*` 只认纯数字素材 id。手里有本地图片、视频或音频时，先上传再引用：

```bash
vc asset upload /path/to/shot.png       # 返回素材 id、类型、大小；一次一个文件
vc generate video --model <模型key> --ref-image <返回的素材id> ...
```

上传用现有的 CLI 登录态，不需要单独申请权限，也不花积分、不改画布。路径不存在、是目录、空文件或超过 200MB 会在本地直接拒绝（退出码 2）；服务端按文件头嗅探真实类型（不看扩展名），素材归属到当前账号。返回的 `id` 可直接用于 `generate --ref-image/--ref-video/--ref-audio` 与 `--first-frame/--last-frame`。

上传路径跟随站点配置，与网页上传一致：先申请直传凭证，默认存储开了直传就直接传到对象存储再登记；没开直传、申请失败或直传失败时自动降级到服务器中转，不用手动选。登记失败（类型不合法、超限）会直接报错，不会中途换路重传。

### 模型与积分

| 命令         | 参数                               | 说明                                                    |
| ------------ | ---------------------------------- | ------------------------------------------------------- |
| `model list` | `--kind image\|video\|audio\|text` | 只列已发布、渠道与插件都就绪的模型；不给 kind 就全列    |
| `model show` | `<模型key>`                        | 生成方式（t2i / i2v / omni…）、参数、参考素材上限、计价 |
| `credits`    | 无                                 | `{balance, frozen, available}`；冻结是进行中任务的预留  |

`model list` 的人类可读输出是四列（KEY / KIND / LABEL / HINT）；`model show` 会把**能填的参数名**逐个列出来，
`--param` 填的就是这些名字：

```text
$ vc model show <模型key>          # 示例输出，具体参数以你站点的模型为准
模型显示名（<模型key>）
种类：video
生成方式：t2v / i2v / omni
参数 count：生成数量（可选 1，默认 1）
参数 duration：视频时长（4–12 秒，默认 5）
参数 resolution：清晰度（可选 720P / 1080P / 1K / 2K，默认 "720P"）
参数 aspect_ratio：比例（可选 21:9 / 16:9 / 4:3 / 1:1 / 3:4 / 9:16，默认 "16:9"）
参考素材：image 最多 8 个，单个不超过 15 MB
参考素材：video 最多 1 个，单个不超过 50 MB
计价：{"billing":"per_second","per_second":1}
```

模型清单里有 `agent` 种类的模型，它们不能提交生成任务，`generate` 只收 `image / video / audio / text`。

### 生成

| 参数            | 默认            | 说明                                                                |
| --------------- | --------------- | ------------------------------------------------------------------- |
| `<种类>`        | 必填（第一项）  | `image` / `video` / `audio` / `text`                                |
| `--model`       | 必填            | 模型 key，从 `model list` 取                                        |
| `--canvas`      | ——              | 画布编号；与 `--node` **必须成对出现**                              |
| `--node`        | ——              | 节点 id；产物回填到它，也决定生成记录归属                           |
| `--prompt`      | ——              | 等价于 `--input '{"prompt":"…"}'`                                   |
| `--param 名=值` | ——              | 可给多次；值先按 JSON 解释（`5`→数字、`true`→布尔），解不出按字符串 |
| `--input`       | ——              | 完整 input 对象：JSON 字符串、`@文件`、或 `-` 读标准输入            |
| `--ref-image`   | ——              | 参考图素材 id，可给多次或逗号分隔；落进 `input.images`              |
| `--ref-video`   | ——              | 落进 `input.videos`                                                 |
| `--ref-audio`   | ——              | 落进 `input.audios`                                                 |
| `--count`       | `1`（上限 8）   | 拆成 N 个任务，**都绑在同一个节点上**（需要 `--node`）              |
| `--wait`        | 不加            | 阻塞到终态；不加就只提交并打印任务号                                |
| `--timeout`     | `600`（≤86400） | 只在 `--wait` 时生效                                                |
| `--interval`    | `3`（≤3600）    | 轮询间隔（秒）                                                      |

```bash
# 只提交：拿任务号，后面用 task wait 接
vc generate video --model <视频模型key> --prompt "硬币缓缓旋转" \
  --param duration=5 --canvas <画布编号> --node <视频节点id>

# 提交并等结果
vc generate image --model <图片模型key> --prompt "极简海报" \
  --param aspect_ratio=16:9 --wait --timeout 300

# 带参考图（素材 id 从画布节点的 data.assetId 或产物 URL 旁的 id 取）
vc generate image --model <图片模型key> --prompt "换成夜景" \
  --ref-image 913 --canvas <画布编号> --node <图片节点id>

# 复杂参数直接给 JSON（比如某个模型要求嵌套结构）
vc generate video --model <视频模型key> --input '{"prompt":"…","duration":8}' --wait
```

几条必须知道的：

- **产物由服务端写回节点**。任务落终态时服务端把产物写进节点并推 `canvas.patch`，已打开的编辑器直接合并，
  不需要等谁打开画布。刚提交完节点还是空的，是任务还没跑完——用 `task get` 看状态，不要重投。
- **`--canvas` / `--node` 要么都给、要么都不给**。都不给就是一条不挂画布的临时任务（产物只能从任务详情拿 URL）。
  只给一个会被直接拒绝：那说明你以为它挂上了，实际没有。
- **积分是提交时冻结的**，`generate --wait` 的输出里能看到冻结额与实扣额。余额不够时提交就失败（`402`），
  不会等到执行时才失败。
- 幂等：每次提交自动带一个随机 `Idempotency-Key`；要重放同一次提交（例如脚本重试），用
  `--idempotency-key <同一个值>` 才不会建出两个任务。
- 上游失败会原样透出，例如渠道余额不足：
  `错误：provider_balance {"code":"insufficient_user_quota",…}`。这是上游账号的返回，原样透出。

### 等结果：task get / task wait

| 命令        | 参数                                        | 说明                                                      |
| ----------- | ------------------------------------------- | --------------------------------------------------------- |
| `task get`  | `<id\|task_ref>`                            | 数字走 `/generation-tasks/:id`，其它按 `ids=` 查          |
| `task wait` | `<id\|task_ref>`、`--timeout`、`--interval` | timeout 1–86400（默认 600 秒），interval 1–3600（默认 3） |

- `task_ref` 是任务 id 的 32 位十六进制编码，**画布界面和日志里显示的就是它**，通常比数字 id 更好用。
- 终态是 `succeeded` / `failed` / `canceled` / `expired`。`failed` 会带上游的 `error_code` 与 `error_message`
  （例如 `video queue is full`），这是上游返回的原因。
- `wait` 的状态变化写 stderr，最终结果写 stdout；`--json` 时 stdout 只有一行。
- **超时（退出码 3）不等于失败**：任务可能还在跑，用 `task get` 继续查或加大 `--timeout`。

## 四、画布：节点级修改与整图写回

素材回到节点上之后，这一步决定片子长什么样：改内容、调位置、增删片段。
改一两个节点用 `canvas node`，整图重排用 `canvas save`。

### 整图写回：canvas save

改一大批（重排布局、批量换模型、程序化生成整张图）走这条：`canvas graph` 取图 → 本地改 → `canvas save` 写回。

| 命令          | 参数                                       | 说明                                     |
| ------------- | ------------------------------------------ | ---------------------------------------- |
| `canvas save` | `<code>`、`--payload 文件`、`--revision N` | 整图保存；不给 revision 就先读一次当前值 |

`canvas save` 的冲突语义：没给 `--revision` 时会先读一次当前版本再提交，这是「尽力而为」；
给了 `--revision` 才是严格乐观锁，画布在别处被改过就直接失败（409 / `30002`），**不会覆盖别人的修改**。

### 节点级编辑（`canvas node`）

只改一两个节点时不用整图。CLI 只提交差异，由服务端用与 Agent 相同的图语义应用，自己不拼图。

| 命令              | 参数                                                                       | 说明                                 |
| ----------------- | -------------------------------------------------------------------------- | ------------------------------------ |
| `canvas node add` | `<code>`、`--kind`、`--label`、`--text`、`--prompt`、`--model`、`--at x,y` | 新增一个节点，**需终端确认**         |
| `canvas node set` | `<code>`、`<节点id>`、`--label`、`--text`、`--prompt`、`--model`           | 改一个节点，**需终端确认**           |
| `canvas node rm`  | `<code>`、`<节点id>...`                                                    | 删节点，连带清理引用，**需终端确认** |

能新建的种类：`script`（文本）、`image`（图片）、`video`（视频）、`audio`（音频）。
角色卡、分镜表、成片、导演台要填内嵌结构，请在画面上创建。

```bash
# 加一个文本节点：文本的正文用 --text（服务端写进节点正文 data.text）
vc canvas node add <画布编号> --kind script --label "旁白" --text "第一句"

# 加一个图片节点：生成用提示词走 --prompt，还可以指定模型与位置
vc canvas node add <画布编号> --kind image --label "关键帧1" --prompt "草原上的风机" \
  --model <图片模型key> --at 600,100

# 改标题 / 改正文 / 改提示词
vc canvas node set <画布编号> n_ab12cd34 --label "新标题"
vc canvas node set <画布编号> n_ab12cd34 --text "改过的正文"

# 删节点：连着它的线、分镜里的挂接、成片顺序会一起清掉
vc canvas node rm <画布编号> n_ab12cd34
```

**每一次都要人工确认**，这是硬约束：

1. 先读当前画布，把「要改什么」逐行打到终端（新建什么节点、改哪些字段、删几个节点连带几条连线）。
2. 等你在终端输入 `yes` 回车。输入其它内容、直接回车、按 Ctrl-C 都算取消，退出码 `4`，画布一个字节都不会改。
3. 确认后带着**刚才读到的那个 `revision`** 提交。如果这期间画布被别人改了，服务端返回 409，
   未确认过的版本不会被覆盖。
4. 服务端返回的 `changes` 才是实际落库的差异，CLI 会把它打出来。

没有 `--yes`、没有 `--force`、也不能和 `--json` 一起用：**脚本无法在无人确认的情况下做节点级修改**。
脚本要批量改画布，用 `canvas graph` + `canvas save`（整图，带乐观锁）。

服务端会拒绝的情况（`10001`，消息里说明原因）：

- 节点 id 不存在，或想删的是组（组里还有成员，一次删除会静默拆散整组）。
- 按 `assetId` 建节点：素材归属不在这里校验，需要参考素材请用连线。
- 操作不合法（例如给导演台节点写提示词、给文本节点设模型）——与 Agent 的规则完全一致。

## 五、助手：交给画布里的 Agent

命令行擅长「参数已知、要确定性」的动作。需要**判断**的时候——挑哪个模型、把哪张关键帧接成参考图、
一句话改三四个节点、这版画面到底能不能用——交给画布里的助手（小镜）。它在服务端跑，
用的是与网页同一套画布语义，产物一样落在节点上。

```bash
vc agent run <画布编号> --message "把 S03 重做成 10 秒视频，参考现有 S03 关键帧" --watch
vc agent events <会话编号> [--after 序号]
vc agent approve <审批编号> [--items 0,1]
vc agent cancel <运行编号>
```

- `--selection 节点id,节点id` 圈定范围；`--budget 积分` 限这一轮的花费。
- 不加 `--watch` 只提交并打印运行编号；加了就跟到**它有回复**或**停下来要你确认**为止。
  只跟本轮，不重放历史——从会话当前序号往后看。
- 需要花钱或不可逆的动作，它会停下来列条目等你决定。命令行不猜含义：批准 `--items 0,1`，驳回 `--reject`。
- 同一画布复用同一条会话，网页里能看到同样的上下文；`--session` 指定看哪一条。
- `cancel` 只停这一轮，它已经提交的生成任务在服务端继续跑完。

**该用哪个**：明确的批量（6 张图、6 段视频）用 `generate`，参数确定、可复现、便宜；
要它想、要它改、要它判断，用 `agent`，按模型 token 计费，再叠加它提交的生成积分。

## 六、短剧：另一条业务线

短剧在 CLI 里是**只读 + 任务控制**：看项目、看原文单元、看/取消任务。建项目、改写剧本、跑规划与分集任务
仍然在网页的短剧工作室里做——那些操作要读长上下文、逐步确认，命令行做不合适。

| 命令               | 参数                                 | 说明                                                    |
| ------------------ | ------------------------------------ | ------------------------------------------------------- |
| `drama list`       | `--keyword`、`--page`、`--page-size` | 只要自己的项目；输出 CODE/STATUS/PHASE/集数/锁定        |
| `drama get`        | `<短剧编号>`、`--episodes 10`        | 项目设置、大纲、集摘要、进行中的任务；集多时只列前 N 集 |
| `drama units`      | `<短剧编号>`、`--from`、`--to`       | 原文单元（按单元号区间），输出 单元号/标题/字数         |
| `drama unit`       | `<短剧编号>`、`<单元号>`             | 单个单元的正文                                          |
| `drama jobs`       | `<短剧编号>`、`--active`             | 进行中/历史任务：步骤、状态、进度、集号                 |
| `drama job-cancel` | `<短剧编号>`、`--job <任务id>`       | 取消未落终态的任务                                      |

```bash
vc drama list
vc drama get <短剧编号>
vc drama units <短剧编号> --from 1 --to 20
vc drama unit <短剧编号> 3
vc drama jobs <短剧编号> --active
vc drama job-cancel <短剧编号> --job <任务id>
```

- 短剧编号与画布编号同一套编码（32 位十六进制），`drama get` 里能看到它绑定的 `canvas_id`。
- `phase` 是自动规划阶段（`running` / `paused` / `done`）；暂停原因在 `drama get` 的 `pause_reason` / `pause_code`。
- 没覆盖的短剧能力（建项目、规划、生成剧本、分集合成、改设定）用网页或 MCP 的短剧工具族。
  命令行不做这些，是因为它们要么要花大量积分、要么需要逐步核对文本，不适合一条命令直接落地。

## 贯穿全线的约定

### stdout 与 stderr 的分工

| 流     | 内容                                                     |
| ------ | -------------------------------------------------------- |
| stdout | 只有结果：人类可读的表格，或 `--json` 时的一行 JSON      |
| stderr | 进度、提示、等待状态。`task wait` 的每次状态变化都写这里 |

所以 `vc canvas list --json | jq '.data.list'` 可以直接用，不会被进度日志污染。

`login` 的**授权地址与用户码始终写在 stderr**，加了 `--json` 也一样：stdout 只留最后那一行结果 JSON，
但脚本或人必须知道去哪个地址点确认。早期版本把这段提示关在非 JSON 分支里，导致 `login --json` 只能干等到超时。

### JSON 输出

成功：

```json
{ "ok": true, "data": { "...": "..." } }
```

失败：

```json
{ "ok": false, "error": { "code": 40004, "message": "任务不存在" } }
```

`code` 是服务端错误码原样透出；网络层与本地参数错误用 `-1`，具体区别看退出码。

`task wait` 到失败终态时，`ok` 与退出码保持一致：

```json
{
  "ok": false,
  "error": { "code": -1, "message": "video queue is full" },
  "data": { "status": "failed" }
}
```

早期版本这里输出 `ok: true` 却返回退出码 1，用 JSON 判断成败的脚本会误判成成功。

### 退出码

| 退出码 | 含义                                                                      | 脚本里的处置                                  |
| ------ | ------------------------------------------------------------------------- | --------------------------------------------- |
| 0      | 成功                                                                      | 继续                                          |
| 1      | 业务错误、网络错误，或 `task wait` 等到了失败终态                         | 读 stderr / `--json` 的 `error`，不要盲目重试 |
| 2      | 用法错误：参数缺失或非法、文件名写错、命令不存在、`--base` 写法错         | 修命令行本身                                  |
| 3      | 等待超时：`--wait` / `task wait` 到点还没结束，**任务仍在跑，不代表失败** | 用 `task get` 继续查，或加大 `--timeout`      |
| 4      | 新建项目 / 节点级修改被取消（没输入 `yes`，或非交互终端被拒绝）           | 想要就重跑并在终端里确认                      |

**超时与失败必须分开**：把超时当失败会导致「任务其实成功了，脚本却重跑一遍，白烧积分」。
这是实测踩过的坑，所以单独给了退出码 3。

### 等待与轮询

`task wait` 默认每 3 秒查一次、600 秒超时；`generate --wait` 同样的默认值。都可用 `--interval`（1–3600 秒）、`--timeout`（1–86400 秒）调。
数值写错（如 `--timeout abc`）会直接报用法错误，不会静默退回默认值。
登录轮询按服务端给的 `interval` 走；服务端返回 `slow_down` 时轮询间隔自动加 5 秒（限频信号）。

`--wait` 只影响「本机等多久」，不影响任务本身：到点退出码 3，任务在服务端继续跑。
这也是它和登录有效期、服务端调用超时完全无关的原因。

### 环境变量

运行时：

| 变量                 | 作用                                            |
| -------------------- | ----------------------------------------------- |
| `VC_API_BASE`        | API 地址，优先级低于 `--base`、高于已保存的凭据 |
| `VC_NO_UPDATE_CHECK` | 设为 `1` 彻底关掉升级检查                       |
| `VC_AGENT_TRACE`     | `agent-trace.mjs` 的路径，等价于 `--script`     |
| `XDG_CONFIG_HOME`    | 改凭据目录（默认 `~/.config`）                  |

### 运行记录

| 命令            | 说明                                                       |
| --------------- | ---------------------------------------------------------- |
| `trace pull`    | 拉线上运行记录到 `.traces/`（已在 `.gitignore`，不要提交） |
| `trace show`    | 看单次运行的完整记录                                       |
| `trace compare` | 按 `harness_key` 等维度对比多次运行                        |

`trace` 把参数原样转发给 `scripts/agent-trace/agent-trace.mjs`，所以它与那份脚本的能力完全一致；
`--json`、`--base`、`--script` 是 CLI 自己的选项，不会透传。

- 安装包不带 `scripts/`，需要用 `--script <仓库>/scripts/agent-trace/agent-trace.mjs`
  或设置 `VC_AGENT_TRACE`；都没给会得到一条可操作的提示（退出码 2），而不是一句「请在仓库内运行」。
- `vc trace --help` 会被 CLI 拦下来打印 CLI 自己的用法；要看 agent-trace 的帮助，
  直接 `node <仓库>/scripts/agent-trace/agent-trace.mjs --help`。

## 边界与限制

本版覆盖「画布读写 + 生成提交 + 短剧只读」。范围之外的能力与原因：

- **不写节点的产物字段**。`src`、`assetId`、`outputs` 由服务端与画布统一写回，CLI 不参与，以免与画布的版本归并不一致。
- **不删除画布**。没有 `canvas delete`。建错了的画布只能在前端删除，CLI 不会替你做不可逆操作。
- **不做批量节点编辑**。`canvas node` 一次改一个节点（`rm` 可一次给多个 id），且必须由人在终端确认；
  程序化批量改写走 `canvas graph` + `canvas save` 整图写回（同样有 `revision` 乐观锁）。
- **不做节点级扩展字段**。角色卡、分镜表、成片、导演台要填内嵌结构（人物设定、镜头行、段落顺序），
  CLI 不支持创建它们；能建的只有文本 / 图片 / 视频 / 音频四类普通节点，改的也只有标题、正文/提示词、模型。
- **不跑短剧重活**。短剧的建项目、规划、生成剧本、分集出片都留在网页和 MCP：它们要长上下文、要逐步核对、
  要花大额积分，一条命令直接触发不合适。CLI 只读 + 取消任务。
- **不封装管理端接口**。渠道、模型、用户管理这些管理员操作没有做进 CLI——脚本令牌不应该顺手改平台配置。
- **令牌等同网页登录态**。它和你浏览器里的登录是同一套 JWT：改密码会一起失效，服务端没有单独的吊销列表。
  区别只有有效期：命令行默认 30 天（后台可调），网页会话按 `jwt.expire_hours`。令牌只绑定登录时的站点，不会发给别的站点。

## 排障

| 现象                                      | 原因                                | 处置                                                                           |
| ----------------------------------------- | ----------------------------------- | ------------------------------------------------------------------------------ |
| `登录已过期或被撤销，请重新执行 login`    | 令牌过期，或改过密码                | `vc login` 重登                                                                |
| `授权已过期，请重新执行登录`              | 用户码超过 10 分钟                  | 重跑 `vc login`，用户码一次一换                                                |
| `这个设备码已经被使用`                    | 同一个码被确认了两次                | 重跑 `vc login`                                                                |
| `画布已在别处修改`（409）                 | 保存时 `revision` 落后              | 重新 `canvas graph` 取最新内容，合并后用新版本号再 save                        |
| `缺少 API 地址`                           | 既没 `--base`，也没登录过           | 首次使用就带 `--base`                                                          |
| `API 地址必须以 http:// 或 https:// 开头` | `--base` 只写了域名                 | 补上协议；不做猜测补全                                                         |
| `--base` 与登录站点不一致被拒绝           | 令牌绑定登录时的站点                | 用登录时的站点，或重新 `vc login --base 新地址`                                |
| `收到的是 canvas export 的导出文档`       | 把 `export` 的输出喂给了 `save`     | 写回用 `canvas graph`；迁移用 `canvas import`                                  |
| `--timeout 需要 1–86400 的整数`           | 数值参数写了非数字或越界            | 改成整数；不再静默退回默认值                                                   |
| `找不到 agent-trace`                      | 独立安装包不带运行记录脚本          | 用 `--script <仓库>/scripts/agent-trace/agent-trace.mjs` 或设 `VC_AGENT_TRACE` |
| 等待超时（退出码 3）                      | 任务仍在跑，不是失败                | 加大 `--timeout`，或 `task get <id>` 继续查                                    |
| `节点级修改需要交互确认`                  | stdout/stdin 不是终端（脚本、管道） | 在终端里手动执行；批量改走 `canvas graph` + `canvas save`                      |
| 节点级修改退出码 4                        | 没输入 `yes`，或按了 Ctrl-C         | 重新执行并输入 `yes`；取消是预期行为，画布没动                                 |
| 节点级修改返回 409（`30002`）             | 确认期间画布被别处改了              | 重新执行，让 CLI 读到新版本后重新展示差异                                      |
| `不支持按 assetId 建节点`                 | 想用已有素材直接建图片节点          | 改成先建节点再连线，或在画面上传素材                                           |
| 生成提交返回 402                          | 可用积分不够冻结                    | `vc credits` 看余额，充值或减少 `--count`                                      |
| `provider_balance` / 上游 429             | 渠道 Key 余额不足或并发满           | 换模型 / 等队列；这是上游问题，CLI 与画布都改不了                              |
| `--canvas 与 --node 要一起给`             | 只给了一个                          | 两个都给（产物落回节点），或都不给（临时任务）                                 |

## 与 MCP 接入的区别

| 对比项   | MCP 接入                               | 命令行（CLI）                   |
| -------- | -------------------------------------- | ------------------------------- |
| 谁在用   | AI 工具（Claude、Cursor 等）           | 人和脚本                        |
| 凭据     | 项目令牌 `vcm_…`，有有效期，可随时注销 | 设备授权换来的登录态，存本机    |
| 怎么授权 | 在「MCP 接入」页签发令牌               | 执行 `vc login`，网页确认用户码 |
| 能力范围 | 按项目勾选的权限档位                   | 与网页登录同等，限本人名下数据  |

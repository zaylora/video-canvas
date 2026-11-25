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

脚本批量改画布的推荐流程：`canvas graph <code> -o g.json`（记下版本）→ 改 `g.json` → `canvas save <code> --payload g.json --revision <版本>`。
只增改、不删节点时全程不需要终端确认。

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
vc agent run <画布编号> --message "把 S03 重新生成成 10 秒视频" [--watch]
vc agent events <会话编号> [--after 序号]
vc agent approve <审批编号> --session <会话编号> [--items 0,1] [--reject]
vc agent cancel <运行编号>
```

- `--watch` 跟到运行结束（`run.status` 进入终态）或等到需要审批为止；回了一段话不等于结束，
  还有未完成的工具调用时会继续等。`approval.decided` 是已经处理完的审批，不算在等人。
- `--session` 先校验它属于这张画布：不存在就报错，不会顺手新建会话；事件游标从该会话当前位置开始，不重放历史。
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
免得绕过画布的版本归并。**不删画布**，**不做短剧重活**。**暂不支持成片合成**：服务端的合成接口就绪后再恢复相关命令。
细节见站点 `/cli/docs` 的「边界与限制」。

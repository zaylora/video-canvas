---
name: video-canvas-cli
description: 用命令行 vc（video-canvas-cli）操作连镜画布上的一个项目：接入、找/建项目、生成图片视频音频文本、改节点与整图写回、交给画布 Agent，另含短剧只读与任务取消。适用于批量生成素材、脚本化出片、把画布接进自动化流程，以及在不开浏览器时核对任务与素材。触发词：vc 命令、命令行生成、批量出图、批量视频、查生成任务、短剧任务、canvas cli。
agent_created: true
---

# 连镜命令行（vc）操作手册

用 `vc` 把一个项目从「没有」推到「有素材」。按业务主线走，每步只依赖上一步的产物：

**接入（拿令牌）→ 项目（拿画布编号与节点 id）→ 素材（产物回填到节点）→ 画布（决定片子长什么样）**

协作画布声明 `compose` 能力时，可以按已确认的视频节点顺序提交成片，并在终端恢复等待或下载结果。

短剧是另一条线，放在最后一节。所有能力都在已有命令里：**不自己拼 HTTP 请求，不绕过命令的确认环节**。

需要判断的事（挑模型、接参考图、按语义改多个节点、看画面能不能用）交给画布里的 Agent，见「五、复杂操作：交给小镜」。

## 什么时候用、什么时候不用

用：批量提交生成并等结果、按节点改动画布、整图导出导入备份、
在不开浏览器时核对任务与积分、把拿不准的改动交给画布 Agent 并替人收审批。

不用：画布里的拖拽排版、合成成片（回网页）；
短剧的建项目、规划、生成剧本、分集出片（回网页或 MCP 的短剧能力族）；删除画布（CLI 没有 `canvas delete`）。

---

## 一、接入：拿令牌

每轮会话先做这三步，30 秒：

```bash
vc --version          # 需要 Node 22.18+；没有这个命令就先装（见下）
vc whoami             # 令牌是否有效，失效会立刻报错
vc credits            # 余额 / 冻结 / 可用：花钱之前先看
```

没装时从 GitHub Release 装（需要 Node 22.18+），再把这份技能装进助手：

```bash
npm i -g https://github.com/zaylora/video-canvas/releases/latest/download/video-canvas-cli.tgz
vc skill install
vc login --base <站点>        # 浏览器里确认一次；确认前核对页面上的用户码与终端一致
```

站点地址从用户给的信息里取，**没给就问，不要猜域名**。登录要**人**在浏览器里点同意，
**不要替用户点，也不要把别人发来的授权链接交给用户去点**——点同意的人会把自己账号的完整权限交给发起登录的那一方。
凭据在 `$XDG_CONFIG_HOME/video-canvas/credentials.json`（没设 XDG 就是 `~/.config/video-canvas/`），
**令牌只属于登录时的站点**：`--base` 指向别处会被直接拒绝，要换站点用 `vc login --base <新站点>`。
`vc logout` 只清本机凭据，服务端登录态要到期或改密码后才失效，不要对用户说「已经退出/已吊销」。
API 与网页不在同一个域名时，登录打印的授权地址可能打不开，加 `--web <网页地址>`。

这份技能自己的位置用 `vc skill path` 查；升级 CLI 后执行 `vc skill install` 更新它。

## 二、项目：拿到画布编号与节点 id

后面每一步都要填 id，都从这里来：

```bash
vc canvas list                          # 我名下的画布（CODE/REV/UPDATED/TITLE）
vc canvas get <画布编号>                 # 详情：标题、版本、节点与连线
vc canvas graph <画布编号> -o /tmp/graph.json   # 只要 nodes/edges：这是可以写回的形态
```

**新建与导入要人确认**：`canvas create`、`canvas import` 会在账号下新增画布，而 CLI 不能删画布。
命令会先打印要建什么，再等终端输入 `yes`；`--json` 与非交互终端一律拒绝（退出码 2），取消是退出码 4。
**不要伪造 pty 去骗过确认，也不要把这两条写进无人值守脚本**——那等于替用户做不可逆的事。
这道确认是约定性的软闸门，不是安全边界（持令牌的人仍可直接调接口），它靠的是你守约。

`graph` 与 `export` 别混：`graph` 是 `{nodes, edges, viewport}`，给 `save` / `create --payload` 用；
`export` 是带 `format` / `canvas` / `assets` 的导出文档，给 `import` 用。把导出文档喂给 `save` 会被拒绝。
`canvas export` 不覆盖已有文件，要覆盖才加 `--force`。

活跃协作画布先用 `vc canvas status <画布编号>` 核对角色、代数和版本。用 `canvas watch --jsonl` 跟进队友修改，恢复游标时同时提供 `--since` 和 `--epoch`。用 `canvas presence` 查看在线队友，收到跟随请求时用 `canvas watch --follow <用户名或成员ID>`。协作画布用 `canvas plan` 与 `canvas apply` 写入差异。遇到未知结果先用 `canvas op status` 查询，再按需 `canvas resume`。

协作生成须给出团队本次授权的 `--max-credits`。多个请求共用额度时同时使用 `--budget-key` 和 `--budget-credits`，恢复时保留原参数、上限和 `--idempotency-key`。高影响共享操作和撤销由用户在终端确认。命令组合和受限自动化凭据见 [CLI 参考](references/commands.md#实时协作)。

## 三、素材：唯一花钱、也唯一产出东西的一步

顺序固定：**查模型 → 看余额 → 提交 → 等结果**。模型 key、参数名、可选值、计价每个站点都不同，
且随渠道上下架变化，所以**一律现场查，不要凭印象写模型名**：

```bash
vc model list --kind image|video|audio|text   # key 才是要填的东西
vc model show <模型key>                        # 生成方式、参数名/可选值/范围、参考素材上限、计价
vc credits                                     # 余额 / 冻结 / 可用
```

用户提到的模型不在清单里，就如实说「当前站点没有这个模型」，并给出 `model list` 里的候选。

**本地文件先上传再引用**：`generate` 的 `--ref-*` 只认纯数字素材 id，不认本地路径。手里有图片 / 视频 / 音频文件时先传：

```bash
vc asset upload /path/to/shot.png          # 返回素材 id、类型、大小；一次一个文件
vc generate video --model <key> --ref-image <上传返回的素材id> ...
```

上传复用现有登录态，没有额外权限；文件类型由服务端按内容嗅探（不是看扩展名），单文件上限 200MB。上传不花钱，也不改画布；传完的素材 id 直接给 `generate --ref-*` 或首尾帧参数。

上传路径跟随系统配置：先申请直传凭证，开了直传就直传对象存储再登记；没开、凭证申请失败或直传失败时自动降级到服务器中转。和网页上传是同一套规则，不用手动选。

提交生成（`--canvas` 与 `--node` 要么都给、要么都不给；只给一个会被拒绝）：

```bash
vc generate image --model <模型key> --prompt "<提示词>" \
  --param <参数名>=<值> --canvas <画布编号> --node <节点id> --wait --timeout 300
```

- `--param` 只能填 `model show` 列出的名字；值先按 JSON 解释（`5` → 数字，`true` → 布尔），解不出按字符串。
- `--count N` 一次多个结果，都绑同一个节点（需要 `--node`）；`--wait` 时这些任务共用一个总超时。
- `--ref-image` / `--ref-video` / `--ref-audio` 给素材 id（纯数字），可给多次。

**首帧 / 尾帧用专用参数**：只给 `--ref-image` 时走的是「参考生成」——图片影响风格与内容，但不锁首帧。
要锁帧用 `--first-frame` / `--last-frame`（内部就是 keyframe 模式，第一张为首帧、第二张为尾帧）：

```bash
# 只锁首帧
vc generate video --model <模型> --prompt "..." --canvas <画布> --node <节点> \
  --first-frame <首帧素材id>
# 首尾帧都锁
vc generate video --model <模型> --prompt "..." --canvas <画布> --node <节点> \
  --first-frame <首帧素材id> --last-frame <尾帧素材id>
```

等价写法是 `--param mode=keyframe --ref-image A [--ref-image B]`，两套不要混用；
`--last-frame` 不能单独给——模型把第一张图当首帧，单独给尾帧会顶到首帧位置上。
也不要再叠 `--param mode=reference` / `--param op=t2v`（会把锁帧顶掉），或用 `--ref-audio`、`--ref-video`
（keyframe 模式只吃首尾两张图）——这些组合 CLI 会直接报错，删掉冲突项即可。

做连续多镜（含首尾循环的短片）时，让相邻两段共用一张图：上段的尾帧就是下段的头帧，
接缝才是同一帧而不是「看起来像」；最后一段的尾帧回到第一段的首帧，循环点才闭得死。
能否真的复刻取决于模型能力：`agnes-*-flash` 只做到构图一致，人物会被重绘，要严格锁帧得换更强的模型。

- 复杂参数用 `--input '<json>'`、`--input @file.json` 或 `--input -`（读标准输入）。

**产物由服务端写回节点**：任务进入终态后，服务端把产物写进节点并推 `canvas.patch`，
已打开的编辑器直接合并。所以提交成功后节点会自己长出结果，**不需要**也用不着 `canvas node set`
去改 `src` / `assetId` / `outputs`（CLI 也不提供这些字段）。刚提交完节点还是空的，是任务还没跑完，
用 `task get` 看状态，**不要重投**——重投会再冻结一次积分。

**超时不等于失败**：`--wait` / `task wait` 到点返回退出码 3，任务在服务端照跑。遇到 3 用
`task get <id|task_ref>` 继续查，**不要重投**——重投会再冻结一次积分。`--json` 的 `data.tasks` 里有所有已提交任务的 id。

**幂等键**：每次提交都会带 `Idempotency-Key`，并在发请求**之前**打印到 stderr、写进结果的 `idempotency_key`。
响应丢了、进程被杀、不确定有没有提交成功时，**用同一个键重跑同一条命令**：`--idempotency-key <键>`，不会重复扣费。

## 四、画布：决定片子长什么样

素材回到节点上之后，这一步改内容、调位置、增删片段。两条路：

要改一两个节点，用节点级编辑（只提交差异，服务端用与 Agent 相同的图语义执行）：

```bash
vc canvas node add <画布编号> --kind script --label "旁白" --text "第一句"
vc canvas node set <画布编号> <节点id> --label "新标题"
vc canvas node rm <画布编号> <节点id>          # 连带清理连线、分镜挂接、成片顺序
```

同样**必须由人在终端确认**，没有 `--yes`、不能与 `--json` 同用。能建的种类只有 `script` / `image` /
`video` / `audio`；角色卡、分镜表、成片、导演台要填内嵌结构，回网页建。

要整图重排（批量换模型、程序化布局），走 `graph` → 本地改 → `save`。**`--revision` 必填**，
它就是 `canvas graph` 输出里的版本——「我这份图取自哪一版」：

```bash
vc canvas graph <画布编号> -o /tmp/graph.json     # 输出里有「版本 N」；--json 时在 data.revision
# 本地改 /tmp/graph.json
vc canvas save <画布编号> --payload /tmp/graph.json --revision N
```

乐观锁保证：期间画布被别人改过会返回 409（`30002`），**不覆盖别人的修改**。
看到 409 就重新 `canvas graph` 取最新版，合并后用新 revision 再存。
只增改、不删节点时脚本可以直接跑；**新图会让现有节点消失时，`save` 同样要人在终端确认**（规则同 `node rm`）。
也就是说，「批量改」可以脚本化，「批量删」不行：这是约定性的软闸门，别想办法绕过它。

节点 id 不用手抄——`scripts/node-pick.ts` 从 graph 里挑：

```bash
node scripts/node-pick.ts /tmp/graph.json --kind image          # 按种类
node scripts/node-pick.ts /tmp/graph.json --kind video --has-asset   # 只要已有产物的
node scripts/node-pick.ts /tmp/graph.json --label "旁白"          # 按标题
```

## 五、复杂操作：交给小镜

命令行只擅长「参数已知、要确定性」的动作。一旦需要**判断**——挑哪个模型、把哪张关键帧接成参考图、
一句话描述改三四个节点、看看这版画面能不能用——交给画布里的 Agent（小镜）。它在服务端跑，
用的是与网页同一套画布语义，产物一样落在节点上。

```bash
vc agent sessions <画布编号>
vc agent history <会话编号> [--before 序号]
vc agent run <画布编号> --message "把 S03 这一镜重做成 10 秒视频，参考现有 S03 关键帧" --watch
vc agent events <会话编号> [--after 序号]      # 单独回看，含审批条目
vc agent approve <审批编号> --session <会话编号> [--items 0,1]   # 按编号批准；--reject 整体驳回
vc agent cancel <运行编号>                     # 停这一轮，已提交的生成任务照常跑完
vc agent interject <运行编号> --message "改成夜景"
vc agent resume <运行编号> [--add-budget 积分]
vc agent undo <运行编号>                       # 输入 yes 确认撤销检查
```

- `--selection 节点id,节点id` 圈定范围，不说就由它自己读画布找。
- `--budget 积分` 是这一轮的花费上限，不给就用默认。
- 不加 `--watch` 只提交并打印运行编号；加了就跟到**运行结束**或**停下来要你确认**为止，不会干等到超时。
  回了一段话不等于结束，它后面常常还有工具调用，所以会继续等；`approval.decided` 是已处理完的审批，不算在等人。
- 一轮可能有好几个 `message.done`（工具调用之间的过渡），最后一句话才是结论。
- **需要花钱或不可逆的动作它会停下来等审批**，此时事件里会列出编号。人看编号决定，
  命令行不替你猜含义：批准 `--items 0,1`，驳回 `--reject`。
  批准前 `agent approve` 会把将批准的条目内容列出来——后端没有按审批编号查详情的接口，
  所以要带 `--session`（`agent run --watch` 打印的命令里已经带上）。**把这些条目念给用户，由用户决定**，
  不要自己替用户批准花钱的动作。在终端里它还会再要一次 `yes`；脚本下只展示不拦，这同样是软闸门。
- 同一画布复用同一条会话，网页里能看到同样的上下文；`--session` 可以指定用哪一条，
  先校验它属于这张画布，不存在就报错，不会新建。

代价要说清：小镜一轮按模型 token 计费，再叠加它提交的生成积分，比直接 `generate` 贵。
**明确的批量（6 张图、6 段视频）用 `generate`；要它想、要它改、要它判断，用 `agent`。**

## 六、短剧（另一条业务线：只读 + 任务控制）

```bash
vc drama list [--keyword <词>]
vc drama get <短剧编号> [--episodes 10]     # 默认只列前 10 集，0 看全部
vc drama units <短剧编号> --from 1 --to 20
vc drama unit <短剧编号> <单元号>
vc drama jobs <短剧编号> [--active]
vc drama job-cancel <短剧编号> --job <任务id>   # 取消不退已花积分
```

建项目、规划、生成剧本、分集出片都不在 CLI 里：那些要长上下文、要逐步核对文本、要花大额积分，
一条命令直接触发不合适。需要时回网页或走 MCP 的短剧工具族。

---

## 贯穿全线的约定

**脚本化**：一律加 `--json` 拿一行 `{ok,data}` / `{ok:false,error}`，进度写 stderr，stdout 只有结果。
按退出码分支：0 成功、1 业务或任务失败、2 用法错误、3 等待超时（任务还在跑）、4 人取消了写操作。
布尔开关（`--json`、`--wait`、`--watch`、`--active`、`--reject`、`--force`）不会吞掉后面的参数，放前放后都行。
等待类命令遇到网络抖动或服务端 5xx 会自己退避重试，4xx 才直接失败。

**重试要带幂等键**：提交是幂等的（自动带 `Idempotency-Key`，并在 stderr 与结果里给出）；自己的重试要复用
`--idempotency-key <同一个值>`，否则会建出两个任务、冻结两次积分。

**花钱纪律**：不要放进无上限循环；先 `credits`，再按 `model show` 的计价 × 次数/秒数估成本。

**排障**：

| 现象                          | 含义                        | 处置                                            |
| ----------------------------- | --------------------------- | ----------------------------------------------- |
| 退出码 3                      | 等待超时，任务仍在跑        | `task get` 继续查，别重投                       |
| `402` / 积分不足              | 可用积分不够冻结            | `credits` 核对，减少 `--count` 或充值           |
| `provider_balance` / 上游 429 | 渠道余额不足或并发满        | 上游账号问题，换模型或等；如实告诉用户          |
| `画布已在别处修改`（409）     | 整图保存时 revision 落后    | 重新 `canvas graph`，合并后用新 revision 存     |
| `需要交互确认`                | 非交互终端跑了要确认的命令  | 交给人在终端执行；不要伪造 pty                  |
| `必须显式给 --revision`       | `canvas save` 没给基准版本  | 先 `canvas graph` 取图和版本，再带 `--revision` |
| `不属于…的登录态`             | `--base` 与登录时的站点不同 | `vc login --base <站点>` 重新登录               |
| 找不到模型                    | key 写错或站点没上架        | `model list` 重新取                             |

全量参数、默认值、返回结构见 `references/commands.md`。

## 本仓库内的落点（改 CLI 时用）

- 源码是 **TypeScript**：命令实现在 `cli/src/commands/`（按领域分文件），入口与用法 `cli/src/cli.ts`，
  版本唯一来源 `cli/package.json`；发布的是 `cli/dist/*.js`（`tsc` 产物，不入库）。
- 改完 `cd cli && npm ci && npm run build`，再 `npm run typecheck && npm run lint && npm run format:check && npm test`。
- 本体就是这份技能：`cli/skill/` 随 npm 包发布（`vc skill path` 查位置）；站点文档正文
  `web/src/content/cli-docs.md`（线上 `/cli/docs`）。
- CLI 通过 npm 发布（包名 `video-canvas-cli`），发版时由流水线按版本号发布，不在本地手动 `npm publish`。

不在本文档里写具体站点、具体模型、具体画布编号：这些每次都不一样，一律现场查。

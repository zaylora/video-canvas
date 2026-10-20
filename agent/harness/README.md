# Agent 调优 Harness

给 AI 编码助手（Claude Code / Codex）调优画布 Agent 用的命令行工具：用 JSON 写用例，走**真实服务**跑 Agent，Agent 的提问和审批按用例里预写的应答处理，跑完对画布做结构断言，输出一份先给结论、再给证据的报告。

设计依据：[`docs/design/Agent调优Harness/Agent调优Harness.md`](../../docs/design/Agent调优Harness/Agent调优Harness.md) 的 6.B.3、6.B.4、第 7 节和第 8.4 节（接口契约）。

- 运行环境：Node ≥ 22.19，**零依赖**，ESM，没有构建步骤。
- 不调用浏览器，只走 HTTP 接口（项目约定不用 Playwright 做端到端）。
- 在 `agent/` 目录下执行：`npm run harness -- <命令>` 或 `node harness/cli.mjs <命令>`。

## 调优循环

1. **跑基线**：`node harness/cli.mjs run harness/cases/ --out ../.harness/base`
2. **读报告**：先看 `<运行目录>/report.md` 开头的「断言结论」「失败断言」，证据里有节点 id、相交的两个矩形坐标、越界的量；再看「可疑信号」，最后到时间线里按序号去 `trace.jsonl` 找原文（模型请求 / 响应、工具参数 / 返回）。
3. **改提示词或工具**：改 `backend/internal/agent/prompts/system.md` 或工具声明，**同时提升 `prompts.Version`**。
4. **重跑**：`... run harness/cases/ --out ../.harness/after`。有随机性的用例加 `--repeat 3` 看通过率。
5. **对比**：`node harness/cli.mjs diff ../.harness/base ../.harness/after`。两边都是整批目录（含 `summary.json`）时按用例 id 配对，给通过数变化的总览，再逐个用例对比；有退化（A 过 B 没过的断言、B 新增的失败断言、用例通过率下降）退出码为 `1`，改进不影响。输出是 Markdown，写到 stdout，加 `--out diff.md` 同时存文件。

改了断言（或发现断言写得不对）不需要重新花钱跑：修改用例文件后执行
`node harness/cli.mjs report <运行目录> --case <用例文件>`，会根据目录里保存的最终画布和事件重新执行断言并刷新 `report.md`。

## 命令

| 命令 | 期次 | 作用 |
| --- | --- | --- |
| `run <用例文件或目录> [--repeat N] [--max-credits N] [--keep] [--out DIR]` | 一期 | 跑用例；目录会递归找 `*.case.json` |
| `report <运行目录> [--case 用例文件]` | 一期 | 重新执行断言并刷新报告的断言部分 |
| `diff <目录A> <目录B> [--out 文件]` | 二期 | 对比两次运行或两个整批目录 |
| `to-case <轨迹包目录> [--out 文件] [--id 用例id]` | 二期 | 把线上轨迹包转成用例 |

选项：

- `--repeat N`：每个用例跑 N 次（默认 1，每次都花钱），每次一个输出目录，摘要里给通过率。
- `--max-credits N`：**预算闸门**。开跑前把所有用例的 `budget × repeat` 求和，超过 N（默认 200）就拒绝开跑并打印每个用例的明细。后端另按每个 run 的 `budget_credits` 强制执行。
- `--keep`：保留测试画布（默认跑完删除）。
- `--out DIR`：输出根目录。默认是**仓库根**的 `.harness/runs`（根 `.gitignore` 只忽略 `/.harness/`，不会跟着 cwd 落到 `agent/` 下）。`diff` 和 `to-case` 的 `--out` 含义不同，见下。
- `--id`：仅 `to-case`，指定生成的用例 id。

退出码：`0` 全部通过（`diff`：无退化）；`1` 有用例失败（`diff`：有退化）；`2` 用法 / 配置错误、环境变量缺失、被预算闸门拒绝、目录或文件有问题（不存在、缺关键文件、JSON 坏了；这类错误只打印一行说明，不带堆栈）。

## 环境变量

认证只从环境变量读，密钥不写进仓库，也不写进输出目录和日志（输出文件和日志在落盘前会把密码和令牌替换成 `***`）。

| 变量 | 必需 | 说明 |
| --- | --- | --- |
| `HARNESS_BASE_URL` | 是 | 后端地址，如 `http://localhost:8080`；末尾的 `/` 和 `/api/v1` 会被去掉，地址里的用户名密码会被忽略 |
| `HARNESS_USERNAME`（或 `HARNESS_EMAIL`，二选一） | 是 | 跑用例的专用测试账号的**用户名**（登录接口只认用户名）；`HARNESS_EMAIL` 是旧名，值同样会当作用户名发送 |
| `HARNESS_PASSWORD` | 是 | 该账号的密码 |
| `HARNESS_ADMIN_EMAIL` / `HARNESS_ADMIN_PASSWORD` | 否 | **同一环境**的管理员账号，用来取轨迹包。缺少或登录失败时 `run` 仍然执行，只是报告标「无轨迹（缺管理员凭据 / 管理员登录失败）」 |
| `HARNESS_USERNAME` / `HARNESS_ADMIN_USERNAME` | 否 | 见下 |

> 后端登录接口 `POST /api/v1/auth/login` 的请求体字段是 `username`（`model.LoginUserReq`），不是邮箱。harness 默认把 `HARNESS_EMAIL` 的值当作登录用户名发送；账号的用户名与邮箱不同时，用 `HARNESS_USERNAME`（管理员用 `HARNESS_ADMIN_USERNAME`）单独指定。

建议用专用测试账号，并给它足够的积分；用例写的 `budget` 就是它的花费上限。

## 用例格式

文件名 `<id>.case.json`，放在 `agent/harness/cases/`。示例见 `cases/` 下的 3 个用例（都不花积分），初始画布见 `fixtures/`。**会真实花积分的用例放在 `cases-paid/`**，默认的 `run harness/cases/` 不会碰到它们；要跑时显式指定该目录，并确认预算：`run harness/cases-paid/ --max-credits 50`。

```json
{
  "id": "storyboard-9x16-3shots",
  "title": "三镜头 9:16 分镜搭建",
  "tags": ["storyboard", "layout"],
  "fixture": "fixtures/script-only.canvas.json",
  "mode": "storyboard",
  "agent_model_key": "",
  "message": "把剧本拆成 3 个镜头，竖屏 9:16，每个镜头一组：镜头文本→关键帧→视频",
  "selection": ["n_script"],
  "budget": 0,
  "timeout_sec": 600,
  "wait_tasks": false,
  "answers": [
    { "match": { "kind": "ask", "contains": "画幅" }, "answer": { "answer": "9:16" } },
    { "match": { "kind": "ask", "ask_kind": "model" }, "answer": { "answer": "$first_model" } },
    { "match": { "kind": "generate" }, "answer": { "decision": "reject" } },
    { "match": { "kind": "delete" }, "answer": { "decision": "reject" } }
  ],
  "expect": [
    { "type": "run_status", "in": ["succeeded"] },
    { "type": "no_overlap" },
    { "type": "members_inside_group" }
  ]
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `id` | 是 | 小写字母、数字、`-`、`_`，不超过 128 字符；进输出目录名和请求头 `X-Agent-Harness` |
| `message` | 是 | 发给 Agent 的用户原话 |
| `budget` | 是 | 本轮积分预算，作为 `budget_credits` 发给后端；不花钱的用例写 `0`（缺失会报错） |
| `expect` | 是 | 断言列表，至少一条 |
| `fixture` | 否 | 初始画布 JSON（`{nodes, edges}`），相对路径依次在用例所在目录、其上一级、`agent/harness/` 里找；不写就是空画布 |
| `mode` | 否 | `all` / `script` / `storyboard` / `prompt`，默认 `all` |
| `agent_model_key` | 否 | 指定 Agent 模型；空表示后端取第一个可用的 |
| `selection` | 否 | 选中的节点 id |
| `timeout_sec` | 否 | 默认 900，超时取消运行并判失败（等生成任务也算在内） |
| `wait_tasks` | 否 | 运行结束后等本轮绑定的生成任务结束（见下） |
| `answers` | 否 | 应答剧本 |
| `title` / `tags` | 否 | 给人看的 |
| `source` | 否 | 来源记录 `{run_id, exported_at, prompt_version, status}`，`to-case` 写入，只给人看，不影响执行 |

### 应答剧本 `answers`

Agent 运行中遇到需要用户决定的事会暂停（`approval.created`）。harness 没有人，只能按 `answers` 应答：

- **按顺序匹配，第一条命中的生效。** `match` 可省略（命中全部）。
- **没命中任何一条：** 立即取消运行，用例判失败，原因「未预写的提问：<原文>」，报告「失败断言」里附上**建议补的 answers 条目**，把它粘进用例（回答按需要改）再跑。

`match` 字段：

| 字段 | 取值 | 说明 |
| --- | --- | --- |
| `kind` | `generate` / `delete` / `ask` | 审批种类（`model.ApprovalGenerate` 等，只有这三种） |
| `ask_kind` | `choice` / `model` | 仅 `ask`：`ask_user` 工具的 `kind` 参数。`choice` 是选项题，`model` 是选模型题 |
| `model_kind` | `image` / `video` | 仅选模型题：要选哪类模型（`ask_user` 的 `modelKind` 参数） |
| `contains` | 字符串 | 提问原文包含这段文字；`generate` / `delete` 则匹配 Agent 给的理由和节点标题 |

`answer` 的字段与后端 `DecideAgentApprovalReq` 一致：`decision`（`approve` / `reject`，不写默认 `approve`）、`items`（逐项决定 `[{index, approve, count}]`）、`answer`（回答文字，**回答提问时必须写**）、`add_budget`。拒绝时可以附 `answer` 作为理由。

`answer.answer` 支持两个占位符：`$first_model`（选模型题里的第一个可选模型 key）、`$first_option`（选项题的第一个选项）。选模型题的 key 因环境而异，用占位符比写死 key 稳。

#### ask 子类怎么区分（已核对后端代码）

`backend/internal/service/agent/bridge_tools.go` 的 `toolAsk`：`ask_user` 的参数 `kind` 只能是 `choice` 或 `model`（其他值报错），原样存进 `AskPayload.Kind`，序列化为 `approval.created` 事件里 `data.payload.kind`。选模型题另有 `payload.model_kind`（`image` / `video`）和 `payload.models[]`（`{key,name,hint}`）。所以 `ask_kind` 就是 `payload.kind`，`model_kind` 是 `payload.model_kind`，提问原文是 `payload.question`。后端生成轨迹包时，`approval` span 的 `ask_kind` 字段也应取 `payload.kind`。

### 断言清单（一期）

每条断言返回 `{type, ok, expected, got, evidence}`，写进 `assertions.json`。

| 类型 | 参数 | 判定 | 证据 |
| --- | --- | --- | --- |
| `run_status` | `in`: 状态数组 | 终态在列表里。**状态值是后端真实的 Run 状态常量**：`queued`、`running`、`waiting_approval`、`waiting_input`、`succeeded`、`failed`、`canceled`、`budget_exhausted`、`step_limit`、`timeout`、`interrupted`、`expired`（没有 `done` / `error`，写了会在载入用例时报错并提示改成什么） | 终态和错误文案 |
| `no_overlap` | — | 顶层对象两两、同组成员两两之间矩形不相交（边贴边不算）；尺寸用 `size.mjs` | 相交的两项及其矩形坐标、重叠面积 |
| `members_inside_group` | — | 每个成员矩形完全落在组框内；成员 `parentId` 指向不存在的组也判失败 | 越界的成员、哪一边、越界多少 px |
| `count` | `of`: `group` / `node`（全部普通节点）/ `edge` / 节点种类；`min`、`max` 至少一个 | 数量在 [min, max] 内 | 实际数量 |
| `every_group_has` | `kinds` | 每个组都包含所列种类 | 缺了哪种的组 |
| `edge_exists` | `from`、`to`（节点种类或节点 id）；`scope`: `"group"` 可选 | 存在 from→to 的连线；`scope=group` 表示每个组内（两端都是该组成员）都要有 | 缺线的组 |
| `nodes_have_prompt` | `kinds` | 指定种类的节点 `data.prompt` 或 `data.params.prompt` 非空白 | 不满足的节点 |
| `nodes_have_output` | `kinds` | 指定种类的节点已有产物（`data.outputs` 非空、`data.src` 非空，或见下的任务产物） | 不满足的节点 |
| `asked` / `not_asked` | 可选 `kind`、`ask_kind`、`model_kind`、`contains`（含义同 `match`） | 本轮是否出现过某类提问 / 审批 | 提问原文 |
| `max_tool_errors` / `max_steps` / `max_credits` | `max` | 过程指标上限 | 实际值 |

**防空转：** 目标对象不存在时判失败而不是通过——画布里一个组都没有时，`every_group_has` 和 `scope=group` 的 `edge_exists` 失败；指定种类一个节点都没有时，`nodes_have_prompt` / `nodes_have_output` 失败。

**过程指标的口径（从事件推出，不依赖轨迹包）：** `max_steps` 数不带 `parent_id` 的 `tool.end`（codemode 嵌套调用不计步，与后端一致）；`max_tool_errors` 数 `is_error` 的 `tool.end`；`max_credits` 取最后一条 `run.usage` 的 `spent_credits`。

**不做 LLM 裁判**：主观质量由人看报告判断。

**除用例里写的断言外，harness 还会自动加入运行时问题**（`type` 以 `harness.` 开头，`ok:false`，用例因此判失败）：`harness.unscripted_question`（未预写的提问）、`harness.timeout`（超时）、`harness.run_error`（流程出错：应答被后端拒绝、事件接口连续失败、开跑被拒绝等）。

### `wait_tasks`

前端负责把生成产物回填进画布，harness 没有浏览器，所以画布里的节点永远看不到 `outputs`。`wait_tasks: true` 时，运行结束后 harness 每 5 秒读一次画布，对节点上带 `data.taskId` 的节点查 `GET /generation-tasks/:id`，直到任务不再处于 `pending / queued / running / finalizing`，或超时。任务的产物数量记在 `tasks.json`，`nodes_have_output` 把它算作「已有产物」。

## 输出目录

`.harness/runs/<YYYYMMDD-HHmmss>-<用例id>[-<第几次>]/`，内容就是轨迹包（设计文档 8.4）再加几样：

| 文件 | 内容 |
| --- | --- |
| `report.md` | 报告。标题和概要之后是 harness 插入的「断言结论」「失败断言」「运行备注」（包在 `<!-- harness:assertions:start/end -->` 标记里，重复生成会整段替换），后面是后端生成的「可疑信号」「时间线」「最终画布概要」 |
| `trace.jsonl` | 一行一个 span，带请求 / 响应 / 工具参数 / 返回原文。**无轨迹时**是 harness 用事件拼的简版（第一行 `system` 带说明，没有原文） |
| `events.jsonl` / `mutations.jsonl` / `canvas.before.json` / `canvas.after.json` | 后端轨迹包里的原文件；无轨迹时 harness 用自己补拉的事件、fixture 和最终画布补上（没有 `mutations.jsonl`） |
| `meta.json` | 后端的 meta 再补 `case_id`、`case_file`、`base_url`；无轨迹时是 harness 自己写的简版（`trace_complete:false`、`no_trace_reason`） |
| `assertions.json` | `{case_id, passed, summary, run, notes, expect, results[]}`，`report` 命令靠它里面的 `expect` 重新断言 |
| `canvas.final.json` | harness 在运行结束（和等完任务）之后读到的最终画布，断言的依据 |
| `events.pulled.jsonl` | harness 补拉到的事件（后端 `events.jsonl` 的行格式以后端为准，重新断言用这份） |
| `tasks.json` | 仅 `wait_tasks` 时有：节点 id → 任务 id、状态、产物数 |

整批运行在 `--out` 根目录另有 `summary.md` 和 `summary.json`：每个用例一行（是否通过、通过率、步数、积分、耗时），失败的列出原因和目录。

## 无轨迹的情况

缺少 `HARNESS_ADMIN_*`、管理员登录失败、或后端拒绝给轨迹包（已过期 / 已关闭 / 接口还不存在）时，`run` 仍然执行：用补拉到的事件拼简版 `trace.jsonl` 和报告，并标「无轨迹（原因）」。此时报告里没有模型请求和响应原文，只能看到工具调用和审批的时间线。

## 线上轨迹包的使用约定

开发环境连不到生产环境，线上问题由管理员在生产后台「日志管理 → Agent 运行」点「下载轨迹包」，得到 `agent-trace-<runId>.zip`。把它拷到开发机，**解压到仓库根的 `.harness/imports/<runId>/`**，然后让编码助手读：

```
.harness/imports/<runId>/
├─ report.md        先读这个：概要、可疑信号、时间线、最终画布概要
├─ trace.jsonl      按序号找原文
├─ events.jsonl  mutations.jsonl
├─ canvas.before.json  canvas.after.json
└─ meta.json        prompt_version、模型、tools_hash、trace_complete ...
```

`.harness/` 已加进根 `.gitignore`，轨迹包含用户数据，不会进仓库。

## diff：对比两次运行

```bash
node harness/cli.mjs diff <目录A> <目录B> [--out diff.md]
```

目录可以是 `run` 的输出目录，也可以是线上轨迹包目录（`.harness/imports/<runId>/`），所以既能比「改提示词前后各跑一遍」，也能拿线上的一次失败和本地修复后的回放比。至少要有 `meta.json`，以及 `events.jsonl`（或 `events.pulled.jsonl`）、`trace.jsonl` 之一。

输出的 Markdown 依次是：

1. **概要**：run id、`prompt_version`、模型、终态、步数、工具报错数、积分、token 入 / 出、耗时，各给 A、B 和变化量。口径与已有的一致：步数 / 报错 / 积分 / 终态来自事件（与 `report`、断言同一套，不数嵌套的脚本调用）；token 是 `trace.jsonl` 里所有 `chat` span 的 `usage` 之和（与后端 `report.md` 概要行一致）；耗时取 `meta.json` 的开始与结束时间。
2. **断言对比**：按断言类型（同一类型出现多次按出现顺序配对）逐条列 A、B 的结果和结论——`退化`（A 过 B 没过）、`改进`（A 没过 B 过）、`持平`、`同样失败`、`仅 A` / `仅 B`（B 新增的失败断言，如 `harness.unscripted_question`，计入退化）。退化的断言附 B 的实际值和证据。**线上轨迹包没有 `assertions.json`**，任何一边没有就跳过这一节并注明，此时只比运行指标，不会有退化。
3. **工具调用**：每个工具 A、B 各调用几次，顺序是否相同、第几步起分叉，以及折叠后的调用序列。

**整批目录**：两边都含 `summary.json`（`run` 的 `--out` 根目录）时，按用例 id 配对，先给总览（每个用例 A、B 的通过数、变化、平均步数 / 积分 / 耗时，以及「通过用例数」变化），再逐个用例给上面三节。`--repeat N` 的用例按第几次逐次配对。`summary.json` 里记的是运行时的绝对路径，批次目录被挪走后会回退到批次目录下同名子目录。一边是整批目录、一边是单次目录属于用法错误（退出 `2`）。

退化的定义：A 通过而 B 没过的断言、B 新增的失败断言、整批对比时用例通过率下降。**改进不影响退出码。**

## to-case：线上问题转成回归用例

线上出了问题：管理员在后台下载轨迹包，解压到 `.harness/imports/<runId>/`，先读 `report.md` 定位，然后：

```bash
node harness/cli.mjs to-case ../.harness/imports/<runId> [--id 用例id] [--out 用例文件]
```

会生成：

| 产物 | 位置 | 内容 |
| --- | --- | --- |
| 用例 | `harness/cases-imported/<id>.case.json`（`--out` 可改） | `id` 默认 `imported-<run id 前 8 位>`；`message` / `mode` / `selection` 取第一条 `message.user` 事件；带 `source: {run_id, exported_at, prompt_version, status}` 记来源 |
| 初始画布 | `harness/fixtures/imported/<id>.canvas.json` | 就是 `canvas.before.json`，用例的 `fixture` 引用它 |

- **应答剧本 `answers`**：由 `approval.created` / `approval.decided` 还原，每个审批一条，回答的是**用户当时实际的决定**。选项题用 `ask_kind: choice` 加提问开头几个字作 `contains`；选模型题用 `ask_kind: model` 加 `model_kind`（回答是线上的模型 key，本地没有就改成 `$first_model`）；生成审批部分批准时还原成逐项 `items`（含减少的张数）；删除审批同理；拒绝带上用户写的理由。按顺序出现的同种审批如果回答不同，后面的会被前面的盖住（`answers` 第一条命中的生效），命令会提醒。没有等到用户决定的审批（运行被中断）不生成应答。
- **`budget`**：运行里已批准生成的报价总和，没有则 `0`。**批准了生成的用例回放时会真实花积分**，命令会单独提醒；不想花钱就把 `generate` 的应答改成 `reject`、`budget` 改 `0`。
- **`expect`（保守默认）**：`run_status in ["succeeded"]`、`no_overlap`、`members_inside_group`、`max_tool_errors`（取原运行的报错数，至少 2）。初始画布本身就违反 `no_overlap` / `members_inside_group` 时会警告，因为回放必然失败。线上问题具体是什么，要自己加断言（比如 `count`、`every_group_has`）。
- 缺 `events.jsonl`、`canvas.before.json`，或没有 `message.user` 事件、文本为空，会报明确错误（退出 `2`），不留半截文件。同 id 重复执行会覆盖。

**隐私**：生成物含线上用户的剧本和提示词。`harness/cases-imported/` 与 `harness/fixtures/imported/` 已加进根 `.gitignore`，命令每次都会提醒「这是线上用户内容，提交到仓库前请脱敏确认」。要把它变成长期回归用例，脱敏后把用例挪进 `cases/`、画布挪进 `fixtures/`（并改 `fixture` 路径和 `source`），再提交。`--out` 指到别处则不受这条 `.gitignore` 保护。

典型流程：`to-case` → `run harness/cases-imported/<id>.case.json` 复现失败 → 改提示词或工具并提升 `prompts.Version` → 再 `run` → `diff` 两次的输出目录确认没有退化。

## 测试

`agent/test/harness/` 下用假后端（注入的 fetch + 假时钟）端到端覆盖：通过、断言失败、未预写提问、超时取消、预算闸门、缺管理员凭据、画布删除失败、wait_tasks、密钥不泄露等。**不调用任何真实服务，不依赖真实凭据。**

```bash
cd agent
node --test "test/harness/*.test.mjs"
```

节点尺寸规则 `size.mjs` 直接读后端和前端共用的测试向量 `backend/internal/tests/testdata/canvasgraph/node-size-cases.json`；规则改动时三处要一起改。

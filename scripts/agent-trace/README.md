# agent-trace

把线上画布 Agent 的运行记录拉到本地 `.traces/`，供 `/agent-review` skill 分析。设计见 `docs/design/Agent运行观测/Agent运行观测.md` 的 6.4–6.7、8.7 节。

- Node 18+，零第三方依赖。
- 导出内容是**用户原文**，CLI 不做脱敏。`.traces/` 已被 `.gitignore`（首次 `pull` 时若缺会自动追加），**禁止提交**。

## 配置

| 变量 | 说明 |
| --- | --- |
| `VC_API_BASE` | API 前缀，如 `https://host/api/v1`，末尾不带斜杠（带了也会去掉） |
| `VC_TRACE_TOKEN` | 后台签发的分析令牌（`vca_` 开头，scope `agent:read`，默认 30 天有效） |

进程环境变量优先；没设时读 `.traces/.env`（每行 `KEY=VALUE`，支持 `#` 注释）。缺任何一个都会以退出码 2 退出并提示。

```
# .traces/.env
VC_API_BASE=https://canvas.example.com/api/v1
VC_TRACE_TOKEN=vca_xxxxxxxx
```

在仓库根目录运行（`.traces/` 建在当前目录下）：

```bash
node scripts/agent-trace/agent-trace.mjs <命令>
```

## 命令

### pull

```bash
node scripts/agent-trace/agent-trace.mjs pull --since 7d --sample 60
node scripts/agent-trace/agent-trace.mjs pull --since 2026-10-01 --feedback down --undone
```

| 选项 | 说明 |
| --- | --- |
| `--since` / `--until` | `7d`、`24h`、`30m`（距现在），`2026-10-01`（本地时区当天 0 点；`--until` 的日期取次日 0 点，即包含当天），或 ISO 时间 |
| `--feedback` | `down` / `up` / `any` / `none` |
| `--undone` | 只要被撤销过的运行 |
| `--status a,b` | 按运行状态过滤，如 `failed,canceled` |
| `--harness` / `--model` / `--prompt-version` / `--user` | 按 `harness_key` / `model_key` / `prompt_version` / `user_id` 过滤 |
| `--limit` | 最多命中多少条，默认 200 |
| `--sample N` | 在命中的运行里抽 N 条，见下 |
| `--with-requests` | 同时拉每次模型调用**实际发给上游的完整请求体**（system 提示词、发送前裁剪后的 messages、tools、max_tokens 等），写成 `runs/<日期>_r<id>.requests.json`。服务端只留 7 天，想留着就要在这之内拉下来 |

行为：

1. 分页拉摘要（尊重 `--limit`）。
2. `--sample N`：分层抽样。信号样本优先，最多取 `ceil(N*2/3)` 条，其余随机补满 N，总数不超过 N。信号样本 = 点踩、被撤销、有被拒绝的审批、状态不是 `succeeded` / `running` / `queued` / `waiting_*` 的运行。
3. 对抽中的运行逐条拉 `format=both` 导出（每条运行 1 次请求：`content` 写 `.md`，`data` 写 `.json`）。本地已有 `runs/*_r<id>.md/.json`，且 `index.jsonl` 里该运行的 `updated_at` 没变的，跳过。
4. 带 `--with-requests` 时，每条运行再请求一次 `/requests`；运行没变但本地缺请求体时只补取这一项，不重复导出。服务端返回空（过了 7 天或没留存）不写空文件；重拉运行时本地已有的请求体文件会跟着新日期改名保留，不会被删掉。
5. 结束打印：`命中 X 条，抽样 Y 条，新写 Z 条，跳过 W 条`（带 `--with-requests` 时末尾多一段 `，请求体 K 条`）。

请求体里**图片地址和图片数据已被服务端换成占位文字**（`[已省略 N 字符]`），其余内容是原文。超过 8MB 的请求只留摘要。`.json` 里每次模型调用的 `request` 字段是摘要（消息数、工具数、字节数、参数），`id` 与 `.requests.json` 里的 `items[].id` 对应。

错误处理：401 提示「令牌失效，请在后台重新签发」；429 按 1s / 2s / 4s 退避重试 3 次（响应带 `Retry-After` 头时优先用它），仍失败就停止（已写的文件和 index 保留，再次运行会续拉）；HTTP 非 2xx 或响应 `code≠0` 都报错，退出码 1。用法、配置错误退出码 2。

### show

```bash
node scripts/agent-trace/agent-trace.mjs show <run_id>
```

直接在终端打印该运行的 Markdown，不落盘。

### compare

```bash
node scripts/agent-trace/agent-trace.mjs compare --by prompt_version --since 30d
```

`--by` 取 `harness_key` / `prompt_version` / `model_key`（`harness`、`model` 也认）。调用 `/admin/agent/stats` 拿服务端聚合指标，再合并本地 `.traces/verdicts.jsonl` 算「AI 判定失败率」：

- AI 判定失败率 = (failed + partial) / 已判定数；`unclear` 不计入已判定。同一 `run_id` 有多条判定时取最后一条。
- 分组依据：`harness_key` 优先用判定里的字段；其余维度通过本地 `index.jsonl` 把 `run_id` 映射回摘要字段，所以要先 `pull`。找不到的判定会提示并忽略。
- **任一组运行数 < 30 时，所有行的结论都写「样本不足」**，不给改善/变差。
- 样本都够时，以运行数最多的组为基线，在失败结束率、点踩率、AI 判定失败率上比较，差 ≥ 5 个百分点才算明显：`改善` / `变差` / `有升有降` / `无明显差异`。
- verdicts 不带时间，合并时不按 `--since` 过滤；做版本对比时建议用最近一次 pull 的样本判定。

## 目录结构

```
.traces/                         # 整个目录 gitignore
  .env                           # 可选：VC_API_BASE / VC_TRACE_TOKEN
  index.jsonl                    # 每行一条运行摘要，按 run_id 去重
  runs/2026-10-20_r1234.md       # AI 读的单次运行文档（后端生成，原样保存）
  runs/2026-10-20_r1234.json     # 同一运行的完整结构化数据
  runs/2026-10-20_r1234.requests.json  # 可选（--with-requests）：每次模型调用实际发给上游的完整请求体
  verdicts.jsonl                 # /agent-review 写入的逐条判定
  reports/2026-10-21.md          # /agent-review 的分析报告
```

文件名里的日期取 `ended_at`（没有则 `updated_at`）的本地日期；`<id>` 是 `run_id` 去掉非字母数字下划线字符后的结果。

## 样本示例

`index.jsonl` 一行（已折行）：

```json
{"run_id":"r_8Kd2","session_id":"s_91","user_id":"u_17","canvas_id":"c_3","mode":"agent",
 "model_key":"deepseek-v4","prompt_version":9,"tools_hash":"a1b2c3","build_sha":"7baa1a3",
 "harness_key":"p9-a1b2c3","skills":["storyboard"],"status":"failed","error_code":"step_limit",
 "request_head":"把这 6 张图排成分镜并加转场","steps":31,"tool_calls":24,"tool_errors":3,
 "model_calls":12,"input_tokens":88123,"output_tokens":4012,"spent_credits":12.5,"duration_ms":186000,
 "approvals_total":1,"approvals_rejected":1,"steer_count":0,"mutation_count":5,"undone":true,
 "feedback":-1,"ended_at":"2026-10-20T06:03:11Z","updated_at":"2026-10-20T06:03:12Z"}
```

`verdicts.jsonl` 一行（由 `/agent-review` 写入）：

```json
{"run_id":"r_8Kd2","harness_key":"p9-a1b2c3","goal":"把 6 张图排成分镜并加转场","outcome":"failed",
 "failure_codes":["loop_waste","canvas_mess"],"evidence":"#7-#19 反复调整同一批节点位置；#24 用户拒绝审批；#28 画布改动被撤销",
 "component":"prompt","suggestion":"system.md 增加：一次布局调整后先核对结果再继续","confidence":0.7}
```

## 测试

```bash
cd scripts/agent-trace && node --test
```

测试用假 fetch 和注入的等待函数，不访问网络。

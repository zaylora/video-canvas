# 任务队列改用 River（可配置并发 + 后台监控）

> - **范围**：把异步生成任务的调度从自研“任务表 + SKIP LOCKED + 租约 + next_poll_at 轮询”换成开源任务队列 River；后台可以调整三个队列的并发，并能看到队列、渠道、吞吐和实例的运行情况。
> - **日期 / 状态**：2026-10-20，**已实现**，待浏览器验证与多实例联调。实现与最初提案有出入的地方见 §13，正文里已按实际实现改写（标 ★ 的是改过的）。
> - **证据约定**：**事实**附文件路径或来源链接；**推断**是基于事实的判断；**提案**是尚未实现的设计。
> - **方案演进**：
>   1. 起点是“改成 MQ”。用户确认真实诉求是“用业界成熟方案，不想自己维护一套调度”。
>   2. 比较过 RabbitMQ / RocketMQ / Kafka / Redis Streams，用户一度选择 RabbitMQ。后来改为 River，原因是 MQ 只负责送消息，任务状态、重试、延迟轮询、积分原子性和任务级监控仍要自研，见 §4.3。RabbitMQ 方案已搁置，复用条件见 §11。
>   3. 访谈中途追加了“单用户并发也用队列控制”，结论是超限后改为排队，见 §6.4。
> - **2026-10-20 修订**：用户评审原型后指出，默认单用户并发已经在「注册设置」里配置，单个用户的覆盖值在「用户管理」里配置，任务队列页不应重复。因此删掉了页面上的「限流参数」区块；排队上限从全局固定值 20 改为“用户生效并发 × 倍数”，倍数和排队超时放进「注册设置」。同时用户确认保持三个队列，不合并。
> - **配套演示**：[任务队列River-演示.html](任务队列River-演示.html)

## 1. 设计摘要

- **问题**：现在的调度是自研的。领取 SQL、租约、心跳、重试和延迟轮询都要自己维护。并发写在 `config.yaml`，改了要重启。后台看不到积压、重试、worker 运行状态。
- **目标**：
  1. 调度交给成熟开源组件。
  2. 后台能调并发，并且立即生效。
  3. 后台能看到队列健康状况。
  4. 单用户超出并发时从“拒绝”改为“排队”。
- **推荐结论**：
  - 引入 **River 开源版**（MPL-2.0，PostgreSQL 原生，不新增中间件）。
  - 按阶段拆成 `gen_submit` / `gen_poll` / `gen_finalize` 三个队列。
  - 渠道上限和用户上限统一放在 submit 入口的“名额”薄层里检查，拿不到名额就 `JobSnooze`。
  - 队列的实际并发由进程内可调信号量控制，后台修改后几秒内生效。
  - 后台新增「任务队列」页，内容包括队列卡片、渠道占用、近 1 小时曲线和实例列表。单个 job 的排查、重试、取消交给嵌入的 River UI。
  - `generation_tasks` 继续作为业务事实来源，前端、WS、积分、使用记录的契约都不变。

## 2. 项目现状

### 事实

| 项 | 现状 | 证据 |
| --- | --- | --- |
| 队列载体 | PG 表 `generation_tasks`，`pg_advisory_xact_lock` + MATERIALIZED CTE + `FOR UPDATE SKIP LOCKED` 领取并写 `lease_until` | `backend/internal/repository/generation_task.go:350-427` |
| 唤醒 | 容量 1 的 chan `Kick`，配合 1s ticker | `backend/internal/service/generation_task.go:111-157`；`backend/internal/provider/worker/worker.go:146-176` |
| 执行 | `handle` 按状态分发：pending→submit，queued/running→poll，finalizing→finalize | `worker.go:233-285`，`worker_submit.go` / `worker_poll.go` / `worker_finalize.go` |
| 状态迁移 | 由 `provider.TaskStore` 接口抽象，`GenerationTaskService` 实现，CAS + 事件表 + 迁移后 publish | `backend/internal/provider/taskstore.go:15-50`；`backend/internal/service/generation_task_store.go` |
| 长任务 | 提交后释放槽位，按 `next_poll_at` 退避再查（5s 起，指数到 15s，±20% 抖动） | `worker_poll.go:13-50`；`policy.go:51-104` |
| 重试 | 提交 5 次（2s→60s）；轮询出错不限次数（deadline 兜底）；转存 10 次（10s→10min）；`submit_unknown` 直接失败退款；runner 连不上时 10s 后再试，不计次数 | `worker_errors.go`、`policy.go` |
| 全局并发（改造前） | `ai.worker.concurrency` 默认 8，`batch_size` 50，`tick` 1s，`lease` 60s；改了要重启 | `backend/configs/config.yaml:54-59`；`config/config.go:167-174` |
| 渠道并发 | `ai_channels.rate_limit_json.max_running`。占用数 = queued/running 加上提交中（pending 且租约有效）；按 `created_at` 先到先得；改了立即生效 | `repository/generation_task.go:355-391` |
| 用户并发 | 创建时 `CountActive`（全部非终态）与上限比较，超出返回 **429**；上限取值顺序：`users.max_active_tasks` → `system_settings.default_max_active_tasks` → 常量 4（范围 1..64） | `service/generation_task_create.go:287-340`；`service/task_limits.go:42-51`；`service/settings.go:21-30,60` |
| 系统设置 | `system_settings` KV 表，每次读都直接查库，没有缓存，所以天然是热更新 | `model/system_setting.go`；`service/settings.go:46-64` |
| 部署 | worker 和 API 同进程；compose 里只有 postgres:16 和 redis:7；plugin-runner 是独立容器 | `initialize/app.go:403-424`；`docker-compose.yml` |
| DB 驱动 | GORM + `gorm.io/driver/postgres`（底层 pgx v5） | `backend/go.mod:28,53` |
| 后台鉴权 | 只认 `Authorization: Bearer`；角色有 admin / super_admin，插件和渠道的写操作仅限 super_admin | `middleware/auth.go:22-27`；`router/router.go:159-161` |
| 已有监控 | `/admin/ai/stats`（按日统计）、`/admin/ai/channels/loads`（渠道 running/waiting）、使用记录与事件时间线 | `handler/admin_stats.go`；`handler/admin_channel.go:58-66`；`service/admin_usage.go` |
| WS | 进程内 Hub；多实例需要换成 Redis Pub/Sub（接口已预留，尚未实现） | `pkg/ws/hub.go`；`pkg/ws/types.go:34-37` |

### 约束

- 任务状态、积分冻结/结算/退回、对账都以 `generation_tasks` 为准（`docs/design/长任务生成设计/长任务生成设计.md` §5、§8）。
- 创建任务时扣积分和入队必须原子：不能出现“扣了积分没有任务”或“有任务没扣积分”。
- 长任务的本质是**延迟调度**：每隔 5–15s 查一次上游。
- 渠道上限必须跨实例生效，并且按先到先得。

### 缺口

- 调度、租约、心跳、救回都是自研代码，维护成本由项目自己承担。
- 并发不能热更新；后台看不到队列积压、重试中的任务、worker 实例。
- 用户超出并发时直接被拒，只能自己等前面的任务跑完再点。

## 3. 用户与场景

| 角色 | 场景 | 诉求 |
| --- | --- | --- |
| 运维 / super_admin | 上游扩容或限流、高峰期积压、转存变慢 | 不重启就能调整各阶段并发；临时暂停某阶段；第一时间看到积压和失败率 |
| admin | 用户反馈“一直排队” | 看清卡在哪：用户名额、渠道名额，还是队列繁忙；能定位到具体 job |
| 创作者 | 一次点出 8 个视频，自己的上限是 4 | 不被 429 拒绝，多出来的自动排队，节点上显示“排队中” |

**成功指标**：
- 删掉自研的领取、租约、心跳、kick 代码。
- 修改并发后 ≤5s 生效。
- 用户超出并发时不再收到 429（只在超过排队上限时才会收到）。
- 后台一屏能回答“现在健康吗、卡在哪”。

## 4. 调研

访问日期均为 2026-10-20。版本号由 `gh api` 实时查得。

### 4.1 任务队列 / MQ 候选

| | River 开源版 | River Pro | asynq | Temporal | RabbitMQ | NATS JetStream |
| --- | --- | --- | --- | --- | --- | --- |
| 版本 | v0.49.0（2026-10-05），UI v0.21.0 | 同左 | v0.26.0（2026-02），asynqmon 2022 年起停更 | Server v1.32.1 / SDK v1.50.0 | 4.3；amqp091-go v1.16.0 | v2.15.1 |
| 新增中间件 | 无（PG） | 无 | 无（Redis） | Temporal 集群 | RabbitMQ | NATS |
| 业务事务内原子入队 | ✅ `InsertTx` | ✅ | ❌ 需 outbox | ❌ 需 outbox | ❌ 需 outbox | ❌ 需 outbox |
| 延迟 / snooze | ✅ `JobSnooze`，不消耗 attempt | ✅ | 变通实现 | ✅ Timer | 4.3 quorum queue 延迟重试；官方延迟插件已归档 | `NakWithDelay`，会计入投递次数 |
| 按渠道/用户限并发 | ❌ 需自研薄层 | ✅ `GlobalLimit` + `ByArgs` | 需自研 | 只能限 RPS | 只能每渠道一个队列 | 每渠道一个 consumer |
| 运行时调并发 | 未验证（可以暂停/恢复队列） | ✅ 在 UI 中覆盖 | ❌ | RPS 可调 | ❌ | ✅ |
| 任务级 UI，可嵌入成 `http.Handler` | ✅ River UI | ✅ | asynqmon（停更） | 独立服务 | 只能看消息 | 无官方免费 UI |
| 费用 | 免费 MPL-2.0 | 125 美元/月 | 免费 | 免费 | 免费 | 免费 |

### 4.2 关键能力核对

- **事实（已核对官方文档）**：River 有 `riverdatabasesql` 驱动，可以和 GORM 共享 `database/sql` 连接池和事务，`InsertTx` 的事务类型是 `*sql.Tx`。这个驱动本身只能轮询；配合 `NewWithPgxListener(sqlDB, pgxPool)` 才能用 LISTEN 及时拿到新任务。https://riverqueue.com/docs/database-drivers
- **事实（调研检索）**：`JobSnooze(d)` 不消耗重试次数，可以无限次 snooze。https://riverqueue.com/docs/snoozing-jobs
- **事实（调研检索）**：River UI 可以用 `riverui.NewHandler` 挂到现有 mux 上，鉴权由宿主负责。https://riverqueue.com/docs/river-ui
- **事实（已核对官方博客）**：RabbitMQ 4.3 的 quorum queue 新增 `delayed-retry-*`（线性退避）；AMQP 1.0 可以用 `x-opt-delivery-time` 给每条消息单独指定投递时间；delayed-message-exchange 插件已归档。https://www.rabbitmq.com/blog/2026/04/23/rabbitmq-4.3-release
- **未验证**：River 开源版能否在运行时修改 `MaxWorkers`。因此本设计不依赖这项能力，见 §6.3。
- **未验证**：River 的 `RescueStuckJobsAfter` 是不是全局单值（不能按 kind 分别设置）。这会影响崩溃后救回的速度，见 §7。

### 4.3 为什么从 RabbitMQ 改为 River（依赖框架调研结论）

- **原子性**：RabbitMQ 发消息不能参与 PG 事务，冻结积分和入队需要 outbox 表加投递器，这又是一段自研代码。River 用 `InsertTx` 直接解决。
- **延迟轮询**：RabbitMQ 4.3 的延迟重试只能做线性退避；要逐条指定延迟必须改用 AMQP 1.0 客户端；旧的延迟插件已归档。River 只要 `return river.JobSnooze(d)`。
- **监控**：RabbitMQ 管理台只能看消息堆积，看不到“某个任务重试了几次、为什么失败”。River UI 是任务级的。
- **运维**：River 只多几张 `river_*` 表，不需要新容器。
- **用户决定**：**River 开源版**。Pro 的分区限流省下的是一层薄逻辑，不值得每月 125 美元，并且自托管分发场景下的许可条款也没有核实。

## 5. 设计原则

1. **业务状态只有一份**：`generation_tasks` 是事实来源，River job 只负责“何时、由谁执行下一步”。（约束 §2）
2. **状态迁移和下一阶段入队放在同一事务**：不会出现“状态变了但没有后续 job”的悬空任务。（`InsertTx`）
3. **限流统一放在一处**：用户名额和渠道名额都在 submit 入口用同一个事务判定，规则沿用现有的先到先得。
4. **配置可热改，硬上限在配置文件**：后台只能在 `1..硬上限` 范围内调整，防止误操作把实例压垮。
5. **监控讲业务语言**：自建页面回答“卡在哪”，River UI 负责查单个 job。

## 6. 推荐方案（提案）

### 6.1 整体链路 ★

**一个任务对应一个 job，job 随任务所处的阶段在三个队列之间交接，直到任务进入终态。**任务行（`generation_tasks`）仍是唯一的业务事实来源：状态、积分、名额、“下一次什么时候处理”（`next_poll_at`）、重试次数（`poll_attempts`）都在它上面；River 只负责“什么时候执行、哪个实例执行、同时执行几个、实例挂了怎么救回、怎么监控”。业务逻辑（提交、轮询、转存、失败、退款）原样留在 `provider/worker`，队列只在合适的时候调用它的 `Handle`。

```
用户点生成
  └─ POST /generation-tasks
       事务：EnsureCredit → LockCredit → 排队上限检查(429) → InsertTask(pending)
             → 冻结积分 → river.InsertTx(SubmitArgs{task_id})        ← 原子
  一个 job 被取出后（三个阶段共用同一套流程）：
    1. 读任务行：终态 / 不存在 → 结束；任务已进入别的阶段 → 把它交给那个阶段的队列，结束
    2. next_poll_at 还没到 → JobSnooze 睡到点（不消耗重试次数）
    3. 抢本阶段的并发闸门（后台可调，按实例计）：满了 → JobSnooze(1–3s)
    4. 提交阶段且还没拿到名额：
         排队超过截止时间 → 走超时流程（expired + 退款，不请求上游）
         申请“用户名额 + 渠道名额”：拿不到 → JobSnooze(2–6s)；拿到 → 重读任务行（deadline 已重置）
    5. 调用 worker.Handle 处理这一步（提交 / 查询一次 / 转存），结果写在任务行里
    6. 再读任务行：终态 → 结束；进入下一阶段 → 入队下一阶段的 job（按 next_poll_at 排期），结束；
       还在本阶段 → JobSnooze 到新的 next_poll_at（任务行没有任何变化则睡 5 秒，避免空转）
每次状态迁移后照旧 publish task.updated → WS → 前端（不变）
```

- **阶段交接不在业务事务里**：任务状态迁移和“入队下一阶段 job”是两步。入队失败时当前 job 由 River 重试，不会丢；只有“状态已变、进程恰好在入队前崩溃”会留下没有 job 的任务，由 §6.7 的定时对账补上。这样做的原因：状态迁移分散在 8 个 store 方法里，每个都塞进入队事务会大面积改动已经测过的积分与状态逻辑，收益只是缩短一个极小的窗口。
- 下一次查询的间隔（5s → 15s ±20%）、提交重试的退避（2s → 60s）、转存重试（10s → 10min）仍由 worker 的 `policy.go` 算好写进 `next_poll_at`，队列只负责睡到点。

### 6.2 三个队列与 job 定义 ★

| 阶段（界面显示） | River 队列 / job kind | args | 单个 job 的执行时限 | 默认并发 / 硬上限（每实例） |
| --- | --- | --- | --- | --- |
| 提交上游 | `gen_submit` / `gen.submit` | `{task_id}` | 45 分钟（同步接口要等上游生成完，取模型任务超时，缺省 30 分钟） | 8 / 64 |
| 查询进度 | `gen_poll` / `gen.poll` | `{task_id}` | 5 分钟（只是一次短请求） | 16 / 64 |
| 转存产物 | `gen_finalize` / `gen.finalize` | `{task_id}` | 45 分钟（单次转存最长 30 分钟） | 4 / 16 |

- **业务上的重试不用 River 的重试**：提交最多 5 次、转存最多 10 次、`submit_unknown` 直接失败退款、runner 连不上 10 秒后再试且不计次数——这些全部沿用 worker 现有逻辑，次数记在任务行的 `poll_attempts` 里。River 的重试只在读写数据库这类基础设施故障时触发（`MaxAttempts=100`，避免偶发故障把 job 丢弃变成孤儿）。
- 每个 kind 用 `UniqueOpts{ByArgs: true, ByState: 存活状态}`：同一任务同一阶段同时只存在一个活着的 job；已完成 / 取消 / 丢弃的 job 不挡后来的同任务 job。
- 对外（接口、界面、设置键）只用短名 `submit` / `poll` / `finalize` 和中文名，**不暴露 River 的队列名**。

### 6.3 后台可调并发（立即生效，按实例计）★

- River 开源版**没有运行时修改队列 `MaxWorkers` 的接口**（已核对 v0.49 源码：`QueueConfig.MaxWorkers` 只在启动时读取，`QueueUpdate` 只改 metadata），所以每个队列的 `MaxWorkers` = 配置文件里的**硬上限**（`ai.queue.max_submit` / `max_poll` / `max_finalize`），启动时固定。
- 实际并发由进程内的**可调闸门**控制。值来自 `system_settings`：`queue_concurrency_submit` / `queue_concurrency_poll` / `queue_concurrency_finalize`，缺省 8 / 16 / 4。
- 刷新方式：保存后本进程立即刷新并写一次心跳；所有实例另外每 5 秒重读一次（多实例时 ≤5 秒内生效）。
- job 取出后先抢闸门；满了就 `JobSnooze(1–3s)`。**取舍**：并发调低时，River 仍会领取到硬上限那么多个 job，多出来的反复睡眠，会产生少量数据库写入（几十次/秒量级，PG 可以承受）。
- 调低并发**不会中断**正在运行的 job，只影响新的获取。
- **暂停 / 恢复**使用 River 的 `QueuePause` / `QueueResume`（持久化，对所有实例生效）。暂停期间已入队的 job 保留，恢复后继续执行。生产环境用 pgx 连接池做 LISTEN，暂停立即传播；拿不到监听连接时退化成轮询，各取任务循环要几秒才会发现。
- 数字的含义是“**每个实例**”。目前是单实例部署，所以等于全局值。

### 6.4 名额：用户上限 + 渠道上限（超限排队）

**新列** `generation_tasks.slot_acquired_at timestamptz NULL`：非空表示该任务已经占用名额。名额的定义如下：

| 名额 | 占用范围 | 上限来源 | 说明 |
| --- | --- | --- | --- |
| 渠道名额 | `slot_acquired_at` 非空 且 status ∈ {pending（提交中）, queued, running} | `ai_channels.rate_limit_json.max_running`（0 = 不限） | 与现状语义一致：转存不占渠道名额 |
| 用户名额 | `slot_acquired_at` 非空 且 非终态（含 finalizing） | `users.max_active_tasks` → `default_max_active_tasks` → 4 | 与现在的“进行中任务”口径一致，只是不再把排队中的任务算进去 |

**获取名额的事务**（`repository.AcquireSlot`，提交阶段拿到并发闸门之后调用）★：

1. 读任务行并 `FOR UPDATE`：同一个任务的重复申请串行；任务不存在 / 已不是 pending → `gone`；已持有名额 → 直接成功（幂等）。
2. 先拿用户的事务级建议锁（`pg_advisory_xact_lock(hashtextextended('vc_slot_user:<id>'))`，试跑任务跳过），再拿渠道的建议锁（`vc_slot_channel:<key>`）。加锁顺序固定为先用户后渠道，避免死锁。**没有复用积分行锁**：名额申请不该和积分结算互相排队。
3. 用户名额：该用户 `slot_acquired_at` 非空且非终态（含 finalizing）的正式任务数 < 用户生效并发；满了 → `wait_user`。**用户维度只数数，不做先后排名**：同一用户自己的排队任务谁先重试到谁先拿。
4. 渠道名额（`max_running` 为 0 或渠道记录不存在表示不限）：已占名额（pending / queued / running）数 **+ 排在前面的有效等待者数** < `max_running`，否则 `wait_channel`。“有效等待者”= 同渠道、pending 且没拿到名额、没超时、`(created_at, id)` 更小、**并且它自己的用户还有空闲名额**。最后一个条件是为了不让“排在前面但自己用户已满的任务”占着渠道名额不放、挡住别的用户。
5. 两个都够：写 `slot_acquired_at = now` 与 `deadline_at = now + 模型超时`，生成时间从拿到名额的这一刻算起。
6. 没拿到：什么都不改，队列 `JobSnooze(2–6s)` 后再试。

**排队边界**：

- **排队上限**：创建时统计“该用户全部非终态任务数”（含排队中），如果 ≥ **该用户生效并发 × `queue_multiplier`**，返回 429，文案改为“排队任务已达上限”。
  - 生效并发沿用现有的取值顺序：`users.max_active_tasks` → `default_max_active_tasks` → 4。
  - 倍数默认 5，比如并发 4 时最多排 20 个，并发 10 时最多排 50 个。
  - 跟着用户并发走，是因为每个用户的并发不一样，全局固定值会出现“上限比并发还小”的矛盾。这一条是用户确认的。
- **排队超时**：pending 且未占名额的时间超过 `queue_max_wait_minutes`（默认 60）→ `Expire`，退款，原因写“排队超时”。这样排队时间不会挤占生成时间，也不会让任务无限期地排下去。
- 用户取消排队中的任务：状态置为 canceled 并退款（沿用现有取消流程）。队列里的 job 不需要单独取消：它下次醒来读到终态会直接结束。
- 管理员调低用户或渠道上限：不中断已经占用的名额，只影响之后的获取。

### 6.5 后台「任务队列」页

导航位置：AI 配置 → 总览、**任务队列**、模型渠道……。读权限给 admin，写权限（并发、暂停、排队参数）仅限 super_admin，沿用插件/渠道写操作的规则。页面在可见时每 5s 刷新一次，复用 `use-visible-interval`。

1. **队列卡片 ×3**：
   - 排队（available）、延迟中（scheduled，含 snooze）、运行中、重试中（retryable）、近 1h 丢弃（discarded）。
   - 当前并发 / 硬上限，可用滑杆或输入框调整，保存后会显示“已生效于 N 个实例”。
   - 暂停 / 恢复按钮。
2. **不放限流参数**：默认单用户并发、排队倍数、排队超时都在「系统设置 → 注册设置」里配置，单个用户的覆盖值在「用户管理」里配置，这一页既不重复提供入口，也不放提示文字（用户确认）。
3. **渠道占用**：每个渠道显示“占用 / 最大同时生成数”。上限就是渠道弹框里的“最大同时生成数”字段（`max_running`，见 `web/src/pages/admin/ai/channels/channel-dialog.tsx:325`），这里只读；“最大同时请求数”（`max_concurrency`）限制的是 HTTP 请求，不展示、等待名额数、最早等待时长。`/admin/ai/channels/loads` 扩展为基于 `slot_acquired_at` 统计。
4. **近 1 小时曲线**：按分钟统计完成数、失败数、平均排队时长（`slot_acquired_at - created_at`）。数据来自 `generation_tasks`，业务口径，不依赖 River 的保留期。
5. **实例列表**：实例 ID、主机名、版本、启动时间、最后心跳、是否 leader、各队列在途数 / 生效并发。心跳超过 30s 显示“失联”。
6. **「打开 River UI」**：新标签页打开 `/admin/river/`，用于查单个 job、手动重试、取消、删除。

### 6.6 River UI 嵌入与鉴权

- 后端用 `riverui.NewHandler` 挂载到 `/api/v1/admin/river/`。
- 浏览器直接打开这个页面时带不上 Bearer 头，所以：
  1. 前端调 `POST /admin/queue/river-ticket`（Bearer 鉴权，仅 super_admin），拿到一次性 ticket，有效期 30 秒（★ 复用 `pkg/ws.TicketStore`，Redis 和内存都支持，有效期是它固定的 30 秒）。
  2. 新标签页打开 `/api/v1/admin/river/?ticket=…`。后端核销 ticket 后下发 HttpOnly、SameSite=Strict、`Path=/api/v1/admin/river` 的会话 cookie（30 分钟，滑动续期），再 302 到去掉 ticket 的地址。
  3. 这个路径只认这个 cookie（值是 `<用户id>.<过期时间>.<HMAC 签名>`，用 JWT 密钥签名，无状态），并且每次请求都重新校验用户当前是**启用状态的 super_admin**（复用用户状态缓存）：降级、停用立即失效；会话剩余不足 15 分钟时顺带续期。
- 角色：admin 只读还是可操作，River UI 本身不区分。**MVP 只允许 super_admin 打开 River UI**，因为它能重试和删除 job；admin 只看自建页面。这是默认值，可以调整。

### 6.7 前端任务侧

- 节点状态不变：pending 显示“排队中”。
- 429 的含义从“超过并发”变为“超过排队上限”，`toast-policy` 的文案要同步修改。
- 积分在创建时冻结，排队期间保持冻结，取消或超时会退回，与现状一致。

## 7. 状态与异常

| 情形 | 行为 |
| --- | --- |
| 创建时事务失败 | 任务、积分、job 一起回滚（原子） |
| 超过排队上限 | 429，任务不创建 |
| 拿不到名额 | snooze 等待；超过排队超时 → expired + 退款 |
| 队列被暂停 | job 保留在队列里；用户侧仍显示“排队中”；排队超时照常计算（**边界**：暂停太久会导致批量超时退款，后台暂停按钮旁要提示这一点） |
| 上游提交 retryable 错误 | River 重试，最多 5 次，用尽后失败并退款 |
| `submit_unknown`（读超时） | 不重试，直接失败并退款（沿用现有规则，防止重复提交） |
| runner 不可用 | worker 把任务 10 秒后再试且不计次数，**名额保持占用**（★ 不回滚：回滚会让排队的任务在 runner 恢复前白白争抢；代价是 runner 长时间不可用时名额被占着，任务最终按 deadline 过期并退款） |
| 任务超过 deadline | poll/submit `Work` 检查到后，尽力调一次上游 Cancel，然后 expired + 退款 |
| 转存失败 | 重试 10 次，10s→10min；超出 deadline 2h 后失败 |
| 进程崩溃 ★ | 实例每 10 秒写心跳，30 秒没有心跳算失联；任何在线实例的定时对账（每 30 秒）会把“由失联实例正在执行”的 running job **立即放回队列**，所以崩溃后约 30–60 秒内被接手（和改造前的租约 60 秒持平）。River 自带的 rescuer（阈值 1 小时，必须大于最长执行时限 45 分钟）只是兜底 |
| 优雅停机 | `client.Stop`（软停）等 ShutdownTimeout，再 `StopAndCancel` |
| 保存并发失败 / 越界 | 接口返回 400（超出 1..硬上限），前端保留原值并提示原因 |
| 实例失联 | 实例列表标红；它的 running job 由对账放回队列（见上一行） |
| 无权限 | admin 调写接口 → 403；非管理员打开 River UI → 401 页面 |

## 8. 数据与工程影响（提案）

### 8.1 依赖与迁移

- 新增依赖：`github.com/riverqueue/river`、`riverdriver/riverdatabasesql`、`riverdriver/riverpgxv5`（只给 listener 用）、`riverqueue.com/riverui`。版本锁定为 River v0.49.0、UI v0.21.0。**用户已确认引用。**
- River 自身的表用 `rivermigrate` 执行（★ 服务启动时在项目迁移之后自动执行，`cmd/server migrate up` 也会执行）；升级 River 时要看 CHANGELOG（仍是 v0.x）。
- 项目迁移 `0007_river_queue.sql`：
  - `generation_tasks` 加 `slot_acquired_at timestamptz NULL`，并回填升级前已在上游的任务（queued / running / finalizing，以及已提交的 pending）；★ 不加 `current_job_id`（取消不需要它，River UI 里按任务 id 搜索即可）；
  - 三个部分索引：`idx_task_slot_user`、`idx_task_slot_provider`、`idx_task_waiting`（排队等名额的 pending）；
  - 新建 `worker_instances` 表；
  - `system_settings` 新增 key（不需要 DDL）。
- **在途任务迁移** ★：不需要专门的迁移步骤——队列启动后立即对账一次，没有活着 job 的非终态任务会按当前状态补入对应阶段的队列（pending → 提交，queued / running → 查询并按 `next_poll_at` 排期，finalizing → 转存）。
- `next_poll_at` 继续使用（就是“下一次什么时候处理”）；`lease_until` 列保留但不再读写，按“先加后删”的迁移规则留待后续版本删除。

### 8.2 代码改动 ★

| 删除 | 保留 / 改造 | 新增 |
| --- | --- | --- |
| `worker.go` 的调度循环（`Run` / `dispatch` / `claimAndRun`）、租约心跳；`ClaimDue`、`claimDueSQL`、`ExtendLease`；`Kick` 通道；配置 `ai.worker.{concurrency,tick,batch_size,lease}` | `worker` 包的业务逻辑原样保留，入口改成 `Handle(ctx, task)`；`TaskStore` 契约去掉 `ClaimDue/ExtendLease`，加 `GetTask`、`AcquireSlot`；`policy.go` 的退避函数复用；创建任务的事务里多一步 `EnqueueTask` | `provider/queue`（River 客户端、可调闸门、job 处理流程、对账救回、实例心跳）；`repository.AcquireSlot` / `QueueRepository`；`service.QueueAdminService`；`handler.AdminQueueHandler` / `RiverUIHandler`；前端「任务队列」页 |

- GORM 事务转 `*sql.Tx`：`tx.Statement.ConnPool.(*sql.Tx)`。River client 用 `riverdatabasesql.NewWithPgxListener(sqlDB, pgxPool)`，pgxPool 使用同一个 DSN（最多 2 个连接），只用于 LISTEN；拿不到时退化成轮询。
- 配置：`ai.worker.enabled` 保留，含义是“本实例是否执行任务”（API-only 实例关闭它，入队和后台操作照常可用）；新增 `ai.queue.max_submit` / `max_poll` / `max_finalize`（并发硬上限，缺省 64 / 64 / 16）。

### 8.3 数据模型

**`generation_tasks` 新增列**

| 列 | 类型 | 含义 |
| --- | --- | --- |
| `slot_acquired_at` | timestamptz NULL | 占用名额的时间；非空且非终态表示正在占用；部分索引 `(provider)` / `(user_id)`，另有 `(provider, created_at, id) WHERE status='pending' AND slot_acquired_at IS NULL` 给排队排名用 |

**`worker_instances`**

| 列 | 类型 | 含义 |
| --- | --- | --- |
| `id` | varchar(64) PK | River client ID（★ 主机名 + 随机后缀，每次启动换新的） |
| `hostname` / `version` | varchar | 主机名 / 应用版本 |
| `started_at` / `heartbeat_at` | timestamptz | 每 10s 更新心跳；超过 30s 视为失联；超过 1 天的行由清理循环删除 |
| `is_leader` | bool | 从 River leader 状态同步 |
| `stats_json` | jsonb | `{submit:{inflight,limit}, poll:{…}, finalize:{…}}` |

**`system_settings` 新增 key**（范围在 service 层校验）

| key | 默认 | 范围 |
| --- | --- | --- |
| `queue_concurrency_submit` ★ | 8 | 1..`ai.queue.max_submit`（64） |
| `queue_concurrency_poll` ★ | 16 | 1..`ai.queue.max_poll`（64） |
| `queue_concurrency_finalize` ★ | 4 | 1..`ai.queue.max_finalize`（16） |
| `queue_multiplier` | 5 | 1..20；排队上限 = 用户生效并发 × 倍数 |
| `queue_max_wait_minutes` | 60 | 5..1440 |

`queue_multiplier`、`queue_max_wait_minutes` 并入现有的 `RegisterSettingsView`（`model/system_setting.go:24`），通过 `GET/PUT /admin/settings/register`（`router.go:299-300`，写操作仅 super_admin）读写。前端放在 `pages/admin/settings/register-settings.tsx`，紧挨着“默认并发上限”。
| `default_max_active_tasks` | 4（已有） | 1..64（已有） |

### 8.4 接口

前缀 `/api/v1/admin/queue`。读接口给 admin，写接口仅限 super_admin。

| 方法与路径 | 说明 | 业务错误 |
| --- | --- | --- |
| `GET /overview` | 返回 `{queues:[{name,paused,concurrency,hard_cap,counts:{available,scheduled,running,retryable,discarded_1h}}], channels:[{key,name,occupied,max_running,waiting,oldest_wait_sec}], instances:[{id,hostname,version,started_at,heartbeat_at,online,is_leader,stats}]}` | — |
| `GET /throughput?minutes=60` | 返回 `[{minute,succeeded,failed,avg_wait_ms}]`，minutes 取值 15..1440 | 400 参数越界 |
| `PUT /queues/:name` | 请求 `{concurrency}`，返回更新后的队列 | 404 队列不存在；400 超出 1..hard_cap；403 |
| `POST /queues/:name/pause` / `resume` | 暂停 / 恢复（幂等） | 404；403 |
| `PUT /admin/settings/register`（已有，扩展） | 新增 `queue_multiplier`、`queue_max_wait_minutes` 两个字段 | 400 越界；403 |
| `POST /river-ticket` | 返回 `{url}`，ticket 有效期 30 秒、只能用一次 | 403 非 super_admin |
| `GET /api/v1/admin/river/*` | River UI（cookie 会话） | 401 |

- 计数来源：`SELECT queue, state, count(*) FROM river_job WHERE queue LIKE 'gen_%' GROUP BY 1,2`；`discarded_1h` 按 `finalized_at` 过滤。
- 统计直接查 `river_job` / `river_leader`（只读、只用稳定的列）；提交阶段里“因等名额而睡着”的 job 会从“延迟中”挪到“排队”（`QueueCounts` 里 join 任务表）。River 没有现成的按状态计数 API。
- `/admin/ai/channels/loads` 改用 `slot_acquired_at` 口径，总览页的渠道负载组件继续可用。

**崩溃快速救回** ★：已实现，见 §7“进程崩溃”。做法是对账里一条 SQL：把 `river_job.attempted_by` 最后一个元素（当前这次尝试的实例）不在“在线实例”集合里的 running job 置为 retryable、立即可取。在线实例列表为空时什么都不动，避免调用方出错时把所有 running 的 job 都放回去。

### 8.5 测试（TDD） ★

- `tests/repository/generation_task_slot_test.go`：名额申请（用户维度、渠道维度、先到先得、前面的等待者自己用户已满不挡路、超时的等待者不参与排名、试跑任务不看用户上限、终态任务 `gone`、**并发申请不超限**）。
- `tests/provider/queue/`（真实 PostgreSQL + 真实 River）：任务自己走完三个阶段并交接、事务回滚时队列里没有 job、不在事务里入队返回错误、渠道 / 用户上限下排队并最终全部完成且观察到的占用不超限、并发闸门可调（含超过硬上限被裁剪）、暂停 / 恢复、排队超时直接过期不提交、已取消的任务跳过、对账补入队、实例心跳与退出清理、失联实例的 job 救回、队列统计（等名额的 job 算排队、吞吐、渠道占用）。
- `tests/service/`：队列后台服务（总览、吞吐补零、调并发的范围与审计、暂停恢复）；创建任务：排队上限 = 并发 × 倍数（含单用户覆盖）、入队失败整体回滚；`GetTask` / `AcquireSlot`；设置服务的新字段与各阶段并发。
- `tests/handler/admin_queue_test.go`：接口权限与参数校验、River UI 的 ticket 一次性、cookie 签名 / 过期 / 续期 / 实时角色校验。
- `tests/provider/worker/`：`Handle` 分发、排队超时 `Expire`、panic 隔离。
- 前端：纯逻辑单测（并发校验、草稿、汇总、实例失联、失败率、注册设置新字段、429 文案）。
- 异步测试采用轮询加超时（仓库约定）。

## 9. 方案取舍

决策矩阵权重：**少维护** 35%（用户的首要诉求）、正确性与恢复 30%、可配置与监控 20%、引入成本 15%。

| 方案 | 少维护 | 正确性 | 配置监控 | 引入成本 | 加权 |
| --- | --- | --- | --- | --- | --- |
| **River 开源版 + 名额薄层** | 4 | 5 | 4 | 4 | **4.30** |
| River Pro | 5 | 5 | 5 | 3 | 4.75（扣除 125 美元/月和许可不确定后，用户放弃） |
| RabbitMQ + outbox | 2 | 3 | 3 | 2 | 2.50 |
| 保留自研，只加配置和监控 | 1 | 5 | 4 | 5 | 3.40 |

**明确放弃**：

- River Pro 自带的分区限流和在 UI 里改并发，改为自研薄层。
- 集群维度的“总并发”，改为按实例计。
- ~~崩溃后 60s 救回~~ ★ 实现时没有放弃：改用心跳 + 对账，约 30–60 秒（见 §7、§13）。
- 本期不拆独立 worker 进程，也不做 Redis 广播。

## 10. MVP 与后续

### 阶段 1（MVP）

- **后端**：
  - 接入 River、三个队列、名额事务（用户 + 渠道，超限排队）、排队上限与排队超时、可调信号量、在途任务迁移、实例心跳；
  - `admin/queue` 接口、River UI 挂载与 ticket。
- **前端**：「任务队列」页（队列卡片 + 并发调节 + 暂停、渠道占用、1h 曲线、实例列表、打开 River UI）；「注册设置」新增排队倍数和排队超时；429 文案。
- **验收**：
  1. 自研的领取、租约、心跳、kick 代码已删除，相关测试已迁移到 River；
  2. 后台改并发后 5s 内，实例列表里的“生效并发”随之变化；
  3. 用户上限 2 时一次创建 5 个任务：全部 202，同时最多 2 个在跑，其余显示“排队中”并按创建顺序依次开跑；
  4. 用户并发 4、倍数 5 时，第 21 个任务收到 429；把该用户的覆盖值改成 10 之后，可以排到 50 个；
  5. 渠道 `max_running=1` 时跨用户先到先得；
  6. 暂停 submit 队列后不再有新提交，恢复后继续；
  7. 升级时在途任务不丢，迁移后都能跑完；
  8. 非 super_admin 无法打开 River UI。

### 阶段 2

- 名额释放时调 `JobRetry` 唤醒最早等待的 job（降低 snooze 延迟）；
- 节点显示等待原因（“等待你的并发名额 / 等待渠道”）；
- 崩溃快速救回；
- 失败率阈值告警。

### 阶段 3

- 删除 `next_poll_at` / `lease_until`；
- 多实例：WS 改为 Redis Pub/Sub；
- 拆出 `cmd/worker`。

## 11. 待确认问题与已确认事项

### 已确认（用户回答）

1. 动机：用业界成熟方案替换自研调度，不想自己维护。
2. 选型：**River 开源版**，渠道限流用薄层自研（中途选过 RabbitMQ，后改为 River）。
3. 监控：自建概览页加嵌入 River UI。
4. 队列按阶段拆分：submit / poll / finalize。
5. 并发修改立即生效，数字按实例计。
6. 监控 MVP 包含：队列卡片 + 并发调节、渠道占用、近 1h 吞吐与失败率、实例列表。
7. 用户超出并发时改为排队，并保留排队上限（超过才 429）。
8. 排队上限 = 用户生效并发 × 倍数；倍数和排队超时放在「注册设置」里，可配置。
9. 任务队列页不放限流参数，避免和「注册设置」「用户管理」重复。
10. 评审原型后再次确认：保持三个队列，不合并。

### 已搁置的决定

- **RabbitMQ**：将来如果出现“同一任务事件有多个独立消费方”或跨语言消费的需求，再引入 MQ 做事件分发（outbox → MQ）。执行调度仍然用 River。
- **River Pro**：如果按实例计数不够用（需要集群级总并发），或者薄层维护成本上升，再评估。
- 独立 worker 进程、Redis 广播：放在阶段 3。

### 默认值（不需要用户决定）

- poll / finalize 默认并发 16 / 4；硬上限 64 / 64 / 16；
- 排队倍数默认 5、排队超时默认 60 min；
- 用户名额包含转存阶段，渠道名额不包含；
- 生成 deadline 从拿到名额时开始计算；
- River UI 仅 super_admin 可打开；
- 页面每 5s 刷新；实例心跳 10s，30s 判定失联。

### 仍待确认

（原来的两个问题已经有结论，见 §13：崩溃救回改用对账，约 30–60 秒；River 开源版确认没有运行时改 `MaxWorkers` 的接口，用闸门实现。）

目前没有会影响实现或验收的待确认问题。上线前需要在浏览器里和多实例下验证的事项见 §13“未验证”。

## 12. 来源与假设

- River releases https://github.com/riverqueue/river/releases （v0.49.0，2026-10-05）
- River 数据库驱动 https://riverqueue.com/docs/database-drivers （已核对，2026-10-20）
- River snooze https://riverqueue.com/docs/snoozing-jobs
- River UI https://riverqueue.com/docs/river-ui
- River Pro 及并发限制 https://riverqueue.com/pro ，https://riverqueue.com/docs/pro/concurrency-limits
- asynq https://github.com/hibiken/asynq ；asynqmon https://github.com/hibiken/asynqmon
- Temporal 公平性 https://docs.temporal.io/develop/task-queue-priority-fairness
- RabbitMQ 4.3 发布说明 https://www.rabbitmq.com/blog/2026/04/23/rabbitmq-4.3-release （已核对）
- NATS 2.12 https://nats.io/blog/nats-server-2.12-release/
- 项目文件：见 §2 表格中的路径；原设计 `docs/design/长任务生成设计/长任务生成设计.md` §8（不引入 MQ 的理由与引入信号）；`docs/design/协议插件设计/并发控制与沙箱池.md`（其中 L111“SKIP LOCKED 未核实”已过时）。
- **假设**：单实例部署；任务量为每秒几十次状态迁移量级（沿用原设计的估算）。
- **已核实（实现时）**：River v0.49 开源版没有运行时修改 `MaxWorkers` 的接口；没有按状态计数的现成 API；`RescueStuckJobsAfter` 是全局单值（所以不能指望它做快速救回，改用心跳 + 对账）。
- **未验证**：River UI 的具体操作清单（重试 / 取消 / 删除是否都可用）；多实例并行时的行为（只在单进程里测过）。

## 13. 实现记录

2026-10-20 实现完成（未提交）。

### 已实现

- **后端**：
  - 迁移 `0007_task_queue.sql`（`slot_acquired_at`、三个部分索引、`worker_instances`、升级时回填在途任务的名额）；River 自己的表由 `rivermigrate` 在启动时迁移。
  - `repository.AcquireSlot`（用户 + 渠道名额，先到先得）、`QueueRepository`（实例心跳、对账、统计）、创建任务事务内入队（`EnqueueTask`）。
  - `provider/queue`（job 处理流程、可调闸门、对账救回、实例心跳、暂停 / 恢复、River UI 托管）；`provider/worker` 去掉调度循环，入口改为 `Handle` / `Expire`。
  - 后台接口 `/admin/queue/*`、River UI `/admin/river/*`（ticket → cookie）；设置新增排队倍数、排队超时、三个阶段的并发；配置 `ai.queue.max_*`。
  - 删除：`ClaimDue`、`claimDueSQL`、`ExtendLease`、`Kick`、租约心跳、`ai.worker.{concurrency,tick,batch_size,lease}`。
- **前端**：后台「任务队列」页（队列卡片 + 并发调节 + 暂停、渠道占用、近 1 小时曲线、实例列表、打开 River UI）；「注册设置」新增排队倍数与排队超时；429 文案改成“排队任务已达上限”。

### 与最初提案的出入

| 项 | 提案 | 实际 | 原因 |
| --- | --- | --- | --- |
| job 的组织方式 | 每次状态迁移都在同一事务里 `InsertTx` 下一阶段 job | 一个任务一个 job，`Handle` 返回后读任务行再交接（非事务），崩溃缝隙由对账补 | 状态迁移分散在 8 个 store 方法里，改成事务内入队会大面积改动已经测过的积分与状态逻辑 |
| 重试与退避 | River 的 `NextRetry` / `MaxAttempts` | 仍记在任务行（`poll_attempts` / `next_poll_at`），队列 `JobSnooze` 睡到点 | 复用 worker 现有逻辑，几乎零回归；River 重试只管基础设施故障 |
| 崩溃救回 | 可能退化到约 35 分钟，待确认 | 心跳 10s、失联 30s、对账 30s：约 30–60 秒 | 对账里一条 SQL 即可，用户不必承担退化 |
| 并发热更新 | 待验证 River 是否支持 | 确认不支持，用进程内闸门 | 见 §6.3 |
| 名额申请的锁 | 复用积分行锁 `LockCredit` | 用户 / 渠道各一把事务级建议锁 | 名额申请不该和积分结算互相排队 |
| 渠道排名 | 同渠道先到先得 | 排名只计“自己用户还有空闲名额”的更早等待者 | 避免一个用户的积压任务占着渠道名额挡住别人 |
| runner 不可用 | 不占名额 / 回滚名额 | 名额保持占用 | 回滚会让排队任务在 runner 恢复前白白争抢；按 deadline 过期兜底 |
| `current_job_id` 列 | 新增 | 不新增 | 取消靠 job 醒来读终态，不需要它 |
| 设置键 | `queue_submit_concurrency` 等 | `queue_concurrency_submit` 等 | 与阶段短名一致，便于按前缀读取 |
| River UI ticket 有效期 | 60 秒 | 30 秒 | 复用 `ws.TicketStore`，有效期是它固定的 |
| 配置 | `ai.queue.<name>.max_workers` | `ai.queue.max_submit` / `max_poll` / `max_finalize` | 与现有配置的扁平风格一致 |

### 验证

- 后端 `go test ./...`：除 4 个 `TestE2E_*`（Agent 端到端，`StartRun` 返回 60005）外全部通过；**这 4 个在改动前的干净代码上同样失败**（用 `git archive HEAD` 导出到临时目录复现），与本次无关。
- 队列运行时用**真实 PostgreSQL + 真实 River** 测试（13 个用例）；`make lint`（相对 master）0 issues。
- 冒烟：在一次性数据库上启动真实服务，7 个迁移与 River 表迁移成功、队列启动、实例心跳写入、三个队列建出、未登录访问后台接口 / River UI 都返回 401。
- 前端：`tsc`、`oxfmt --check`、`oxlint`（只有存量同类警告）、全部 2646 个单测通过。

### 未验证

- 前端页面没有在浏览器里看过（深浅色、窄屏、弹窗被拦截时的提示）。
- River UI 真实打开后的操作（重试 / 取消 / 删除）与跳转。
- 多实例并行（对账救回、闸门、名额竞争只在单进程里测过）；生产用的 LISTEN 通道（测试用的是轮询模式）。
- Windows 下 `kill` 是强杀，优雅退出的清理只由单元测试覆盖。

### 遗留

- `generation_tasks.lease_until` 列已不再使用，按“先加后删”的规则留待后续版本删除；`TouchByProviderTask` 仍然没有调用方。
- 阶段 2：名额释放时主动唤醒最早的等待者（现在靠 2–6 秒轮询）、节点上显示等待原因（用户名额 / 渠道名额）、失败率告警。
- 阶段 3：多实例下 WebSocket 广播改用 Redis Pub/Sub，拆出独立的 worker 进程。

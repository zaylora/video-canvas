# 阶段 0 验证脚本

验证 Pi Durable 1.4.0 能不能承担画布 Agent 的几个关键行为。结论和取舍见上一级目录设计文档的第 17 节。**这些不是产品代码**，只用 Pi 自带的假模型（`faux`），不联网、不需要任何密钥，也不碰仓库其他部分。

```bash
cd docs/design/Agent服务拆分/阶段0验证
npm ci          # 依赖版本由 package-lock.json 锁定；node_modules 已被 .gitignore 忽略
node --disable-warning=ExperimentalWarning exp1-approval.ts
```

| 脚本 | 验证什么 | 结论 |
|---|---|---|
| `exp1-approval.ts` | 审批怎么建模：工具返回 `terminate` 结束这一轮，用户决定后用带 `edits` 的写入替换占位的工具结果，再提交一条输入让模型继续 | 可行：等审批期间不请求模型，模型看到的是真实结果 |
| `exp2-crash.ts` | 工具执行中被 `SIGKILL`，用 SQLite 重开后 `resume()`；对比 `replay: "never"` 和 `"safe"` | `never`：不重跑，模型收到「被中断，可能已经执行」；`safe`：用同一个调用 id 重跑 |
| `exp3-steer.ts` | 官方插话（A）与「中止后重提」（B）的差别 | A：插话等回答写完才处理，用户会看到两段回答；B 能还原现在的打断重答。**已决定用官方效果（A）** |
| `exp4-compaction.ts` | `beforeCompact` 钩子在自动、手动压缩时的参数，以及第二次压缩能不能拿到上一份摘要 | 钩子都会被调用；第二次的 `entries[0]` 是上一份摘要 |
| `exp5-hook-model.ts` | 在钩子里用 `api.models` 调模型，按自己的提示词写摘要并返回 `{summary}` | 可行 |
| `exp6-many.ts` | 一个 Harness（一个 SQLite 文件）同时托管很多会话 | 200 个会话 × 3 轮同时进行，全部完成，没有串内容 |

`exp6-many.ts` 可以带参数：`node … exp6-many.ts 500`。

实验里踩过的坑，实现阶段 3 时会再遇到：
- 压缩任务在「没有可压缩的内容」时会直接完成，**不会调用钩子**；测试压缩要让上下文超过 `keepRecentTokens`。
- `faux` 的回答按调用顺序消费，钩子里再调一次模型也会消费一个。

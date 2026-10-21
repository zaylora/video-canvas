<!-- PR 标题请沿用提交信息格式：feat(canvas): 支持节点分组。详见 CONTRIBUTING.md -->

## 为什么改

<!-- 关联的 Issue 或要解决的问题。合并后自动关闭 Issue：Closes #123 -->

## 改了什么

<!-- 关键改动和取舍，不必复述每一行 diff -->

-

## 怎么验证

<!-- 跑了哪些命令、手动验证了什么，结果如何。界面改动请附前后截图或录屏 -->

-

## 风险与兼容性

<!-- 没有就写“无”。迁移、接口/配置变更、对已有数据的影响都要写 -->

## 自检清单

- [ ] 一个 PR 只做一件事，提交信息符合 Conventional Commits（`type(scope): 中文描述`）
- [ ] 先写了能复现问题的失败测试，新增/修改的行为都有测试覆盖
- [ ] 后端：`make fmt && make lint && make test` 通过
- [ ] 前端：`bun run typecheck && bun run lint && bun run format:check && bun test` 通过
- [ ] Agent 运行时（仅改动时）：`npm test` 通过
- [ ] 改了表结构：已新增迁移文件，且没有修改已发布的迁移
- [ ] 改了接口、配置或用户可见行为：已同步更新文档
- [ ] 没有提交密钥、令牌、真实连接串或运行时数据
- [ ] 没有手动修改 `CHANGELOG.md` 和 `VERSION`

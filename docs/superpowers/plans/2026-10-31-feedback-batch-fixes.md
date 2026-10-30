# 体验问题修复计划（2026-10-31 反馈批次）

> 来源：飞书文档 <https://fcnmm88165eo.feishu.cn/wiki/YX4ZwYMdniOiUQkTmb4cMINknF6>，共 8 条，其中「短剧开始时选模型时长与类型」已划掉，本次不做。
> 代码定位来自只读扫描 + 抽查核对，行号以 2026-10-31 的 master 为准。

## 总览

| # | 问题 | 范围 | 改动量 | 需后端 |
|---|------|------|--------|--------|
| 1 | 后台渠道模型列表：已启用的排前面 | 后台 | 小 | 是 |
| 2 | 短剧加载动画播完重来、不连贯 | 短剧 | 小 | 否 |
| 3 | 角色卡详情退出后，AI 生成音频记录没了 | 画布 / 短剧 | 中 | 是 |
| 4 | 分集栏切页签时会动（做成真正"公共"的一条栏） | 短剧 | 小 | 否 |
| 5 | 主侧边栏限制宽度（画布区出现横向滚动条） | 首页 | 小 | 否 |
| 6 | 人物传记改成卡片式 | 短剧 | 中 | 否 |
| 7 | 资产库切换 图片/视频/音频 时回到顶部 | 资产 | 小 | 否 |
| 8 | 比例图标形状不准 | 画布 / 后台 | 小 | 否 |

通用规则（用户补充）：**不能点的选项一律用置灰的禁用样式**（降低不透明度、`cursor-not-allowed`、`aria-disabled`，悬停 `title` 说明原因），不能和可点项长得一样。

建议顺序：2 → 5 → 7 → 8 → 4 → 1 → 6 → 3（先小后大，3 依赖新增后端接口放最后）。每条单独提交，先写失败测试再实现。

---

## 1. 渠道模型列表：已启用的排前面

**现状**
- 列表顺序在 Go 内存里排：`backend/internal/service/ai_catalog_list.go` 约 326-340 行，按 渠道顺序 → `sort` → `key`，没有看启用状态。
- 拖动排序保存时把渠道内全部模型重写成 `sort = 10, 20, 30…`（`ai_config_channel_models.go:169-194`）；启停开关只改 `enabled`，行不会移动。
- 前端 `web/src/pages/admin/ai/models/model-rows.tsx` 直接按服务端顺序渲染，`channel-models-panel.tsx:151-173` 处理拖动。

**方案**（默认采用，如需改再说）
- 排序改为：渠道顺序 → **已启用在前** → `sort` → `key`。即「启用组」和「未启用组」两组，组内仍是手动拖动的顺序。
- 判断依据用 `enabled` 字段，不用「在线」健康状态（健康状态会自己变，列表会跳）。
- 拖动只在同组内有效：跨组拖动在前端拦下（松手回弹），保存时仍提交整个渠道的完整 keys，后端校验不变。
- 打开/关闭启用开关后列表刷新，行会移到对应组——这是预期行为。

**测试**
- `backend/internal/tests/service/ai_catalog_list_test.go` 的「顺序是用户看到的顺序」用例改期望，新增「启用的排前、组内按 sort」用例。
- 前端补一个纯函数测试：跨组拖动被拒绝（把判断抽到 `web/src/utils/admin/` 下）。

---

## 2. 短剧加载动画播完重来

**根因**（已核对代码）
- `web/src/pages/drama/workspace/index.tsx:118`：数据没到时 `return loader`，根节点是 `<FocusLoader>`；数据到了以后 `return <>…{loader}</>`（第 192 行），加载层换了位置。React 会卸载旧的、挂一个新的，动画从第 0 帧重播，900ms 最短展示时间也重新计时。画布页的 `CanvasLoader` 一直在同一棵树里，所以没这个问题。
- 次要：退场时 `.canvas-loader[data-leaving] .logo-anim [data-part]` 整条替换了 animation，退场关键帧只写了 `to`，从"对焦循环"当前位置跳回原位再退场，有一帧跳变（`web/src/index.css:666-673`）。

**方案**
- 让加载层在两种状态下处于同一位置：始终返回 `<>{value && 工作台}{loader}</>`，不再在 `!value` 分支单独返回 loader。
- 退场跳变：把退场动画放到 logo 外面包一层的元素上（外层做散开/淡出，内层继续对焦循环），两层 transform 叠加，就不会跳回原位。
- 不改动画设计本身（进场 0.9s + 1.6s 对焦循环 + 退场）。

**测试**
- `web/src/tests/components/focus-loader.test.tsx` 旁新增工作台加载测试：数据从无到有时，加载层 DOM 节点是同一个（没有被重新挂载）。

---

## 3. 角色卡详情：保留历次 AI 生成的音频

**现状**
- 每个角色节点只有一个 `voiceTaskId` 槽位（`web/src/types/canvas.ts:52-55`），成功后音频绑成 `character.voice` 并清掉任务 id（`utils/characters/voice-task.ts:118-124`）。新结果直接覆盖旧的，没有历史。
- 声音任务的 `node_id` 是 `voice:{角色节点id}`（`voice-task.ts:14-17`），后端表里有这一列，但查询接口只支持 `?ids=` 和 `?status=active`（`backend/internal/model/generation_task.go:148-152`）。
- 模型选择、错误信息放在 `use-voice-generation.ts` 的本地 state 里，切工序时节点卸载就丢了。

**方案**
- 后端：`GET /generation-tasks` 增加 `node_id` 查询（必须带 `canvas_project_id`），只查当前用户、非测试任务，按创建时间倒序，最多 20 条。按 `router → handler → service → repository` 分层加，`(user_id, canvas_project_id, node_id)` 视数据量考虑加索引。
- 前端：角色卡详情的「AI 生成声音」下面加「生成记录」列表：每条显示提示词摘要、模型、时间、状态，成功的可试听、可「设为音色」，当前音色标出来。失败的显示原因。
- 打开详情时按 `voice:{nodeId}` 拉一次；新任务完成后追加到列表顶部。
- 模型选择持久化到节点数据（和 `voicePrompt` 一样），切工序再回来不丢。
- 顺带核实：`utils/canvas/history.ts:17-32` 的撤销字段里有 `voiceTaskId` 没有 `character`，撤销可能把音色退回去而任务 id 还在——写测试确认，确有问题一并修。

**测试**
- 后端：`backend/internal/tests/` 下 repository / service / handler 各补按 `node_id` 查询的用例（只返回本人、非测试、按时间倒序、限条数、缺 `canvas_project_id` 报参数错误）。
- 前端：`web/src/tests/utils/characters/` 补「历史记录转列表项」「设为音色」的纯函数测试。

---

## 4. 分集栏固定成一条公共栏

**现状**
- 分集栏在 剧本/资产管理/片场/成片 已经是同一个组件实例（`index.tsx:144-152`），但内容随页签变：只有剧本页顶上有「大纲」「人物」+ 分隔线，只有资产页有「全集」（`episode-rail.tsx:116-138`），所以集号列表会上下跳。
- 总览页不渲染分集栏（`stage !== "overview"`），切进切出主内容左右平移 78px。

**方案**（用户已确认）
- 「大纲」「人物」在所有页签都显示；在非剧本页点击，跳到剧本页并打开对应文档。
- 「全集」固定占位，只在资产管理页可点；其他页签显示为**置灰禁用**（不可点、`title` 说明「仅资产管理可按全集筛选」）。
- 总览页也显示分集栏：点某一集跳到该集的剧本页。
- 「新增一集」在列表最底部，不影响上面位置，保持只在剧本页显示。
- 标题文字「分集 / 筛选」不影响布局，统一成「分集」。

**测试**
- `web/src/tests/pages/drama/episode-rail.test.tsx`：各页签下顶部条目数量和顺序一致；非资产页「全集」为 disabled；总览页渲染分集栏。

---

## 5. 主侧边栏横向溢出

**根因**（推断，待本地复现确认）
- 对话、画布列表放在 `overflow-y-auto` 的容器里（`web/src/components/home/app-sidebar.tsx:129`），`overflow-x` 也会变成 `auto`。
- 列表容器 `grid gap-0.5`（`sidebar-list.tsx:25`）没写列宽，隐式列按内容最小宽度撑开；链接是 `whitespace-nowrap`，长标题把整列撑宽，`truncate` 没机会生效，于是底部出现横向滚动条。

**方案**
- 列表容器改成 `grid grid-cols-[minmax(0,1fr)]`，`ListItem` 外层加 `min-w-0`，长标题正常省略号截断。
- 滚动容器加 `overflow-x-hidden` 兜底。
- 侧栏宽度保持 15rem 固定，不做拖拽调宽。

**测试**
- `web/src/tests/components/home/sidebar-list.test.tsx` 断言列表容器带 `minmax(0,1fr)` 列定义、项目带 `min-w-0`（jsdom 测不了真实布局，用 class 断言兜住回归）。

---

## 6. 人物传记改成卡片

**现状**
- `web/src/pages/drama/workspace/project-doc.tsx` 把 `drama.charactersMd`（AI 写的 Markdown）放进富文本编辑器。
- 常见结构：分组标题（如「主要人物」）→ 一级列表项是人名（可带「（外号：…）」）→ 二级列表项是 `**身份**：…`、`**动机**：…`、`**能力/根基**：…`、`**关系网络**：…`、`**角色弧线**：…`。

**方案**（用户已确认：纯前端解析 Markdown，不复用资产区数据）
- 新增 `web/src/utils/drama/parse-characters.ts`：Markdown → `{ group, name, alias, fields: [{label, value}], extra }[]`。
  - 一级列表项 = 一个人物；括号里的外号拆成 alias。
  - 二级列表项按「**标签**：内容」拆字段；不认识的标签照样显示，不丢内容；没有标签的行放进 extra。
  - 分组标题（`#` 标题或单独一行加粗）作为分组。
- 所有人物用**同一种卡片样式**，只是名字和字段内容不同：卡片头是名字 + 外号 + 分组标签，下面是字段，标签与内容分开排版，长内容可展开。
- 卡片是查看态；保留「编辑原文」入口切回现在的富文本编辑器，保存逻辑不变。
- 解析不出任何人物时（AI 格式变了），直接回退显示原来的富文本，不显示空白。
- 视觉按 `web/docs/coding-standards.md` 6.1 的清透胶囊风格，不加操作说明小字。

**测试**
- `web/src/tests/utils/drama/parse-characters.test.ts`：截图里这种格式、带外号、多分组、未知标签、无标签行、空字符串、完全不是列表的 Markdown（回退）。

---

## 7. 资产库切换类型回到顶部

**现状**
- 图片/视频/音频是 `web/src/pages/assets/history-view.tsx:53` 的本地 state，切换时没有任何滚动处理；滚动容器是 `window`。

**方案**
- 切换类型时 `window.scrollTo({ top: 0 })`（不用平滑滚动，避免和骨架屏切换叠在一起抖）。
- 来源/时间/排序筛选变化同样回顶（内容整体换了），搜索输入不回顶。

**测试**
- 新增 `web/src/tests/pages/assets/history-view.test.tsx`：点击「视频」后 `window.scrollTo` 被以 `top: 0` 调用。

---

## 8. 比例图标形状不准

**现状**
- `web/src/components/canvas/video-param-panel.tsx` 的 `RatioShape`：最长边 16px、1.5px 边框、圆角 3px 的 `span`。扁的比例（21:9 高约 7px）扣掉上下边框只剩约 4px，又带 3px 圆角，看起来像胶囊，比例失真；Auto 和 1:1 是同样大小的方框。
- 短剧选择器 `web/src/components/drama/pickers.tsx:231-240` 另有一份 18px 的 `RatioShape`，两处画法不一致。
- 该网格和画布节点生成面板是同一个组件，改了两处一起生效（用户已知晓）。

**方案**
- 抽一个共用的比例图标（SVG，`viewBox` 内按真实宽高比画矩形，描边在内侧，圆角按短边比例取、最大 2px），两处都用它。
- 所有比例放进同一个固定的外框里居中，外框高度一致，不同比例之间可以直接对比形状。
- Auto 换成区别于 1:1 的图形（如四角括号），不再是同尺寸虚线方框。
- 网格里不支持的选项（若有）按通用规则置灰禁用。

**测试**
- `web/src/tests/components/` 新增比例图标测试：16:9、21:9、9:16 输出的矩形宽高比与标称一致（误差 < 1%），Auto 与 1:1 图形不同。

---

## 验证

- 前端：`cd web && bun run test`（定向文件先跑）、`bun node_modules/oxlint/bin/oxlint src`、`bun node_modules/oxfmt/bin/oxfmt <改动文件>`、`bun run build`。
- 后端（1、3）：`cd backend && go test ./internal/tests/...`，测试库按本机测试环境配置。
- 不做 Playwright 端到端；2、4、5、8 这类视觉问题改完后请人工在浏览器里看一眼。

## 待确认 / 默认假设

- 第 1 条采用「启用组在前、组内手动顺序、禁止跨组拖动」，判断用 `enabled` 而非在线状态。
- 第 4 条总览页点某一集跳到该集剧本页。
- 第 5 条根因是读代码推断，动手前先本地复现。
- 「禁用置灰」规则理解为针对分集栏「全集」等不可点项，并推广到本次涉及的所有不可点选项。

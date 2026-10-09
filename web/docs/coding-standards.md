# 前端代码规范

## 1. 注释规范

- 一律使用多行 JSDoc 块注释，不用 `//` 写说明：

  ```ts
  /**
   * 说明
   * @param xxx 参数说明
   * @returns 返回值说明
   */
  ```

- **API 函数**：`api/*/index.ts` 里每个导出函数都要有 JSDoc，写清 `@param` 和 `@returns`；没有返回值的只写 `@param`。
- **注释内容**：写「是什么、为什么」，不写「怎么做」。已有的约束要保留，例如幂等键必须复用、凭证只写不读。
- **类型注释**：每个 `type`、`interface`、联合类型的每个分支，以及每个字段，都要有 `/** */` 注释。
- **字段注释**：一行即可。字段有单位、取值含义或特殊约定时必须写明，例如「毫秒」「0 表示没有该维度」「null 表示未完成」。

## 2. API 层结构

- 目录：`api/<模块>/index.ts` 放请求函数，`type.d.ts` 放类型。
- 路径集中管理：接口路径多、易变的模块（如 admin-ai）把路径放进 `endpoints.ts`，请求函数不写死 URL。
- 请求统一走 `@/utils/requests/service`，不直接使用 axios。
- 列表接口对后端可能返回的 `null` 做兜底（`?? []`）。

## 3. 错误处理与全局 Toast

- **全局错误捕获封装在 `utils/requests/request.ts` 的响应拦截器里**：业务错误码非 0、HTTP 错误、超时、网络异常都会统一包装成 `ApiError`，并通过全局提示（toast）展示给用户。
- 错误提示直接在 `request.ts` 里调用 shadcn 的 Sonner（`toast.error`，同一错误码的提示按 `id` 去重），业务代码和页面**不要**自己重复弹请求错误的 toast。
- Toast 统一使用 shadcn/ui 的 `sonner` 组件（`@/components/ui/sonner` 的 `<Toaster />` 挂在 `App.tsx`，`toast` 从 `sonner` 导入），不要再自己实现 toast 组件或 store。
- 所有请求失败都会走全局 toast，**没有**跳过提示的开关。因此调用方**不要**为了弹错误提示而 `catch`，也不要再写页面内的重复错误文案。
- 只有失败后需要做**额外逻辑**时才 `catch`，例如 409 冲突后重新拉取详情、失败后回退到兜底状态（加载失败显示「画布不存在」页、保留上一次成功的清单）、把错误翻译成面板内的就地提示。这时全局 toast 照常弹出，`catch` 里只处理状态，不再重复提示。
- 只需要恢复 loading 等状态时用 `try/finally`，不写空 `catch`。

## 4. 前后端字段命名与转换

- 后端原始结构命名为 `Backend*Dto`，保持 snake_case 和数字 id。
- 前端使用的结构命名为 `*Dto`，使用 camelCase 和字符串 id。
- 转换只在 API 层做（如 `mapAsset`、`mapCanvasProject`），页面和组件不接触后端原始结构。
- 后端用 0 表示「没有该维度」的字段，前端统一转成 `null`。
- **例外**：`api/admin/ai`（管理端 AI 配置）直接沿用后端 snake_case 字段与数字 id，不做映射：字段多、与 `backend/docs/admin-ai-api.md` 一一对应，映射层只会增加漂移风险。

## 5. 类型组织

- API 相关类型放在 `api/<模块>/type.d.ts`。
- 画布和组件共用的类型放在 `types/canvas.ts`，通过 `types/index.ts` 统一导出。
- 纯类型导入使用 `import type`。

## 6. UI 组件（shadcn/ui）

- `src/components/ui` 下的组件由 shadcn/ui 命令行生成，**不要手写**。新增组件先用命令行添加，例如：

  ```bash
  bunx shadcn@latest add <component>
  ```

- 生成之后，允许在原文件上做优化修改（样式、交互、封装等）。
- 项目自己的业务组件放在 `src/components` 其他目录（如 `canvas`、`setting`），不要放进 `ui`。
- 配置见 `components.json`（style: base-nova，图标库 lucide）。

## 7. 生成任务：统一入口

所有入口（画布节点、首页对话、以后新增的任何地方）提交和取消生成任务，**只经过 `utils/tasks/gateway.ts`**，共用同一套逻辑：

- 幂等键与重试：每次点击一个新的 `Idempotency-Key`，可重试的失败沿用同一个 key。
- 提交 / 取消成功后：任务快照交给 `handleTaskView` 合并进任务库，并刷新余额（`acceptTasks`）。之后进度只由任务库驱动（WebSocket 推送 + 断线对账），入口只订阅任务库，不自己轮询。
- 状态怎么理解：进行中 / 终态 / 可取消看 `utils/tasks/status.ts`，排队中 / 生成中 / 即将完成看 `utils/tasks/node-view.ts` 的 `derivePendingPhase`。入口里不要再写自己的状态集合或文案判断。
- 入口之间只允许在「提交到哪里、结果往哪放」上不同：画布放进节点，对话放进记录。
- 新增入口时：在 gateway 里加一个 `submitXxxTasks`，内部复用 `submitWithRetry` 和 `acceptTasks`。
- `src/tests/utils/tasks/gateway-guard.test.ts` 会检查：除 gateway 外没有代码直接调用 `createGenerationTask`、`cancelGenerationTask`、`submitConversationRecord`、`submitWithRetry`，`handleTaskView` 只有 gateway 和 `utils/ws` 能调用。

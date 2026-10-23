# Agent 看到并选择节点生成方式

> **范围**：视频、图片节点生成面板顶部的「生成方式」页签（文生视频 / 图生视频 / 全能参考，图片是文生图 / 图生图），让画布 Agent 能看到、能设置，并在创建节点时选对。
> **日期与状态**：2026-10-23，已实现、未提交、未浏览器验证（见 §5）。
> **证据约定**：**事实**附文件路径；**提案**是尚未实现的设计。

## 1. 问题

画布上视频节点有三个页签（文生视频 / 图生视频 / 全能参考）。Agent 在这件事上有三个缺口：

1. **看不到**：`canvas_get_state` 只在节点已保存了方式时才带出 `params.op`，没保存时完全看不出会按哪种方式生成，目录里更没有。
2. **选不了**：`params.op` 技术上能写（`checkParams` 只拦 images/videos/audios），但提示词和工具说明都没提，写错值也不报错（生成时 `pickOp` 悄悄回退到别的方式）。
3. **没人把关**：前端在用户连线时会自动切换方式（`opToAcceptSource`），Agent 的连线不会；后端也不拦「方式和连的素材对不上」（图生没连图、文生却连了参考都放行），所以会出现「Agent 建了文生视频节点又给它连了参考图，参考被悄悄忽略」。

## 2. 事实

| 事实 | 证据 |
| --- | --- |
| 视频节点的方式存在 `data.params.op`，取值 t2v / i2v / omni；图片是 t2i / i2i | `web/src/hooks/use-task-node.ts` 的 `setOp`、`web/src/utils/tasks/capabilities.ts` 的 `currentOp` |
| 同时支持 t2i 和 i2i 的图片模型自动判断（有参考图就是图生图），不存方式、前端不显示页签 | `capabilities.ts` 的 `isAutoOp` |
| 选中的方式不在模型 `capabilities.ops` 里时，前端取第一个，后端 `pickOp` 在有参考图且支持图生时取 i2v，否则取第一个 | `capabilities.ts` 的 `currentOp`；`backend/internal/service/agent/generate.go` 的 `pickOp` |
| 后端不校验「方式要求有参考素材」，测试明确放行（上游可能还没出图） | `backend/internal/provider/modelcfg/input.go`、`internal/tests/provider/modelcfg/input_test.go` |
| `model_list` 已经输出每个模型的 `ops` 和 `refs` | `backend/internal/service/agent/bridge_tools.go` 的 `modelSummary` |

## 3. 设计（已实现）

- **设置**：`canvas_apply_ops` 的 `create_node` / `update_node` 新增顶层字段 `mode`，写入 `params.op`。
  - 种类要对：视频节点只能 t2v / i2v / omni，图片节点只能 t2i / i2i，其他节点报错；
  - 要先有 `model`（方式由模型决定），同一项里一起写 `model` 和 `mode` 即可；
  - 模型在可用清单里时，必须是它支持的方式，否则报错并列出可选项；自动判断的图片模型不允许设置；
  - 模型不在清单里（可能已下架）不校验是否支持，生成时再兜底；
  - 直接往 `params` 里写 `op` 被拒绝并指向 `mode`，避免绕过校验。
- **看到**：`canvas_get_state` 的节点详情新增 `mode`：`op`（已保存且模型仍支持的方式，空表示没选）、`options`（该模型可选的方式）、`auto`（自动判断，不用设）。节点没有模型或不是图片、视频节点时没有这个字段。
- **选对**：
  - 系统提示词（版本 14）新增第 14 条：三种方式的含义、什么素材选什么方式、写 `mode` 要和 `model` 一起；
  - 批量修改后检查涉及的视频节点，在结果里返回 `warnings`（不拦截）：文生视频却连了参考素材（参考会被忽略，并只建议该模型确实支持的方式）、图生视频却连了视频或音频（只引用图片）。没连参考素材不警告，因为上游可能稍后才连或还没出图，这和前端、后端提交时放行的规则一致。
- **一条规则**：生成时选方式的 `pickOp` 与警告里判断「当前是哪种方式」用的是同一个函数 `canvasgraph.PickOp`，不会出现提醒和实际生成不一致。

## 4. 放弃的做法

- **连线时像前端一样自动切换方式**：要把前端的 `opToAcceptSource` / `acceptsSourceKind`（依赖 `refs` 开关）整套搬到 Go，而且会悄悄改掉 Agent 自己选的方式。选择交给 Agent、用提醒兜底更透明；若以后发现 Agent 经常忽略提醒，再考虑自动切换。
- **目录里显示方式**：目录只给概要，且要为每个节点查模型能力；方式只在详情里给。
- **后端强校验「方式需要参考素材」**：会破坏「上游还没出图也能先申请」的现有行为。

## 5. 实现记录

- `canvasgraph/mode.go`：`PickOp`、`ModeInfo`、`AnnotateModes`、`ModeWarnings`、`applier.setMode`；`Op.Mode`、`Options.ModelOps`、`DetailNode.Mode`；`checkParams` 拦 `params.op`。
- 服务：`AgentCanvasService.SetModelOps`（`NewAgentBridge` 用模型注册表设置）、`ApplyOps` 带校验和 `warnings`、`Detail` 带 `mode`；`WriteResult.Warnings`；`pickOp` 改为调用 `canvasgraph.PickOp`。
- 声明与提示词：`tool_decls.json` 给 `canvas_apply_ops` 加 `mode`、补充两个工具的说明；系统提示词升到版本 14；`testdata/tool_defs_baseline.json` 里这两个工具的快照同步更新。
- 测试：`internal/tests/agent/canvasgraph/mode_test.go`、`internal/tests/service/agent/mode_test.go`，先写测试、确认红灯再实现。
- **已核对**：改动补丁里 `data` 只展开一层（后端 `diff.go` 的 `flatten`、前端 `graph-changes.ts` 的 `unflatten` 一致），所以 `params.op` 的变化以整个 `data.params` 的形式走和已有 params 修改相同的合并路径，前端不需要改动。
- **未验证**：没有在浏览器里看过 Agent 设置方式后画布页签的实际显示；没有用真实模型评估 Agent 是否会稳定选对方式，需要上线后用 `/agent-review` 看运行记录。

# Agent 失败分类（failure taxonomy）

> **种子分类，前 1–2 次分析要先开放编码再收敛，修改需人确认。**
>
> 使用方：`/agent-review` skill（`.claude/skills/agent-review/SKILL.md`）。分析时 `failure_codes` 取自本表；表里没有的新问题写 `new:<短名>`，同一个 `new:*` 出现 ≥ 3 次才建议并入本表，由人确认后改动。

判定时一次运行可以有多个 code，但**主因**写在第一个。`component` 取 `prompt` / `tool_desc` / `tool_impl` / `skill` / `runtime` / `model` / `product_gap` / `user_side` 之一，表示“改哪里最可能修好”，不是“谁的错”。

| code | 一句话 | 常见组件 |
| --- | --- | --- |
| `misread_intent` | 没理解或曲解用户要什么 | prompt |
| `over_ask` | 不该问的问了，或问太多轮 | prompt |
| `under_ask` | 该问没问，带着猜测就动手 | prompt |
| `wrong_tool` | 选错工具，或参数填错 | tool_desc |
| `tool_failure` | 工具本身报错，或结果不符合预期 | tool_impl |
| `canvas_mess` | 画布改动错误、布局混乱、改了不该改的 | tool_impl / prompt |
| `incomplete` | 没做完就结束 | runtime / prompt |
| `loop_waste` | 重复调用，步数或积分白白浪费 | prompt / runtime |
| `skill_gap` | 缺技能，或技能写法导致做不对 | skill |
| `gen_quality` | 流程对了，但生成的图片或视频质量不达预期 | model / product_gap |
| `product_gap` | 用户要的事产品当前就不支持 | product_gap |

---

## misread_intent

- **定义**：Agent 对用户目标的理解与用户真实意图不符：做了另一件事，或只抓住了一半。
- **典型信号**：用户随后改口或纠正（“不是这个意思”）；点踩且评论提到“没听懂”；产出与 `request_head` 明显对不上；用户插话（steer）重复强调同一要求。
- **常见组件**：prompt（对模糊请求的解读规则）；偶尔是 model。
- **判定示例**：用户说“把第二个镜头的图换成夜景”，Agent 在 #4 重新生成了整套镜头图。目标是改一个节点，Agent 理解成重做全部 → `misread_intent`。
- **区分**：
  - 与 `under_ask`：misread 是 Agent **以为自己懂了**但理解错；under_ask 是请求本身有歧义、Agent 本该先问却没问。能从请求里合理读出正确含义的，归 misread；请求确实有两种合理读法的，归 under_ask。
  - 与 `wrong_tool`：意图对、动作选错工具，归 wrong_tool。

## over_ask

- **定义**：信息已经足够或可以合理默认，Agent 仍向用户提问，或连问多轮拖慢进度。
- **典型信号**：提问的答案已在请求或画布里；一次运行里 `ask` 类步骤 ≥ 2；用户回复“都行 / 你看着办”；用户在等待输入后点踩。
- **常见组件**：prompt（何时该问的规则）。
- **判定示例**：用户说“给这张图做个 5 秒视频”，#2 问“要多少秒？”，#5 又问“要什么比例？”。时长已给出，比例可沿用图片比例 → `over_ask`。
- **区分**：
  - 与 `under_ask`：方向相反。同一运行可能先 over_ask 再 under_ask（问了不重要的，漏了关键的）。
  - 与 `product_gap`：因为产品缺能力而不得不问用户，不算 over_ask。

## under_ask

- **定义**：请求有关键歧义或缺少必要信息，Agent 没问就猜着做，导致结果不符或要返工。
- **典型信号**：返工、撤销或被拒绝审批；用户随后补充上本该一开始问的信息；花了积分的生成因此作废。
- **常见组件**：prompt。
- **判定示例**：用户说“做一个产品宣传视频”，没给产品、时长、风格。Agent 在 #3 直接提交了生成审批，用户 #4 拒绝并补充说明 → `under_ask`。
- **区分**：
  - 与 `misread_intent`：见 misread_intent。
  - 与 `incomplete`：under_ask 是“猜着做完了但做偏”；incomplete 是“没做完”。

## wrong_tool

- **定义**：任务本可由现有工具完成，但 Agent 选错了工具，或给对工具填了错误参数（类型、范围、引用了不存在的节点）。
- **典型信号**：工具返回参数校验类错误；同一目标换了另一个工具才成功；工具描述里本有说明却没按它用。
- **常见组件**：tool_desc（描述不清、参数名误导）；也可能是 prompt。
- **判定示例**：想给节点改名，Agent 在 #6 调用“创建节点”工具新建了一个同名节点，原节点保留；#7 才改调“更新节点”。#6 选错工具 → `wrong_tool`。
- **区分**：
  - 与 `tool_failure`：wrong_tool 是**调用方**的错（工具按设计工作）；tool_failure 是**工具实现**有问题（同样的正确调用也会失败）。判断方法：把参数换成正确的，工具能不能工作。
  - 与 `skill_gap`：该用技能（流程性知识）而没用或用错，归 skill_gap。

## tool_failure

- **定义**：Agent 的调用本身合理，工具却报错、超时，或返回的结果与其描述不符。
- **典型信号**：`tool_errors` > 0 且错误是服务端错误、超时、结果缺字段；正确参数下仍失败；多次运行在同一工具上出现同一类错误。
- **常见组件**：tool_impl；偶尔是 runtime（桥接、超时）。
- **判定示例**：#8 调用“读取画布”，参数正确，返回 500 “internal error”；#9 重试同样失败，Agent 之后凭记忆继续操作 → `tool_failure`。
- **区分**：
  - 与 `wrong_tool`：见 wrong_tool。
  - 与 `gen_quality`：生成类任务被供应商拒绝或失败（积分、风控）属 tool_failure；生成成功但画面不好属 gen_quality。
  - 与 `incomplete`：因工具失败导致没做完，主因记 tool_failure，incomplete 作为次要 code。

## canvas_mess

- **定义**：对画布的改动本身有问题：位置重叠或杂乱、连线错误、改了或删了用户没要求动的节点、批量操作范围过大。
- **典型信号**：用户撤销（`undone`）；`mutation_count` 很大而请求很小；点踩评论提到“乱了 / 删了我的东西”；用户事后手动大量调整。
- **常见组件**：tool_impl（布局算法、默认位置）；prompt（改动范围的约束）。
- **判定示例**：用户让“加一个字幕节点”，Agent 在 #5 把新节点放在已有节点正上方形成重叠，#9 为了“整理”移动了 12 个无关节点，用户随后撤销 → `canvas_mess`。
- **区分**：
  - 与 `misread_intent`：Agent 理解对了要做什么，只是改画布的方式差，归 canvas_mess；做了完全不同的事归 misread。
  - 与 `wrong_tool`：由选错工具直接造成的画布问题，主因记 wrong_tool，canvas_mess 为次要 code；工具按设计工作但默认布局差，归 canvas_mess。

## incomplete

- **定义**：任务明明还没完成，运行却结束了：只做了一部分、中途放弃，或最后的回复宣称完成但实际没做。
- **典型信号**：状态是 `step_limit` / `budget_exhausted` / `timeout` / `interrupted`；最后回复里有“接下来可以……”但没做；结尾回复与画布实际状态不符；用户重发了同一请求。
- **常见组件**：runtime（步数/预算上限、断点续跑）；prompt（收尾检查）。
- **判定示例**：用户要 6 个镜头的图，Agent 做完 4 个后在 #22 回复“已完成所有镜头”，实际画布上只有 4 个 → `incomplete`。
- **区分**：
  - 与 `loop_waste`：incomplete 看**结果没做完**；loop_waste 看**过程浪费**。两者常同时出现：因空转耗尽步数而没做完，主因记 loop_waste，同时标 incomplete。
  - 与 `tool_failure`：见 tool_failure。
  - 与 `product_gap`：因为产品做不到而停下，归 product_gap。

## loop_waste

- **定义**：重复或无效的调用消耗了步数或积分：同一调用反复执行、来回改同一处、反复读取同一状态、不必要的重复生成。
- **典型信号**：同名工具 + 相近参数连续出现 ≥ 3 次；`steps` / `tool_calls` 远高于同类请求的中位数；`spent_credits` 高但最终产出少；步数达上限。
- **常见组件**：prompt（何时停手）；runtime（重复调用检测、预算提示）。
- **判定示例**：#7 到 #16 十步里连续 5 次调用“移动节点”，每次只微调几个像素，之后仍未达到满意位置 → `loop_waste`。
- **区分**：
  - 与 `incomplete`：见 incomplete。
  - 与 `tool_failure`：因工具失败而重试，重试本身合理，不算 loop_waste；Agent 明知失败还原样重试 ≥ 3 次，才同时记 loop_waste。
  - 与 `gen_quality`：为了追求质量多次重生成，属用户可见的合理尝试，不算 loop_waste，除非参数完全相同。

## skill_gap

- **定义**：缺少某个本该有的技能（流程性知识），或已有技能的写法让 Agent 做不对：没触发、触发错、步骤有缺漏。
- **典型信号**：请求属于某类固定流程（如分镜、批量出图、风格统一），Agent 却摸索着做；技能被加载但 Agent 没按正文执行；多次运行在同一类任务上表现差。
- **常见组件**：skill。
- **判定示例**：用户要求“按这个角色图保持一致性生成 4 个镜头”，运行里没有加载任何相关技能，#5–#12 逐个试探不同提示词，角色每张都不同 → `skill_gap`。
- **区分**：
  - 与 `wrong_tool`：缺的是“流程知识”归 skill_gap；缺的是“单个工具的用法”归 wrong_tool。
  - 与 `product_gap`：技能可以靠写 prompt 和现有工具补出来的，是 skill_gap；需要新增产品能力的，是 product_gap。

## gen_quality

- **定义**：流程和调用都正确，但图片或视频的生成结果质量不达预期：画面崩坏、与提示词不符、风格不一致、人物走样。
- **典型信号**：点踩评论提到“画得不好 / 不像 / 崩了”；用户重新生成多次；工具调用都成功、没有报错。
- **常见组件**：model（供应商模型本身）；product_gap（缺少可控手段）；有时是 prompt（生成提示词写得差）。
- **判定示例**：#10 生成成功，提示词合理，但视频中人物手部变形；用户在评论里点踩说“手崩了” → `gen_quality`，component 为 model。
- **区分**：
  - 与 `misread_intent`：提示词偏离了用户要求（Agent 写错了提示词），归 misread 或 prompt 类问题；提示词忠实但模型画不好，归 gen_quality。
  - 与 `tool_failure`：见 tool_failure。

## product_gap

- **定义**：用户要的事产品当前就不支持，Agent 无论怎么做都做不到。
- **典型信号**：Agent 回复“目前不支持……”；用户请求的动作没有对应工具或模型能力；同类请求在多个用户的运行里重复出现且都无法完成。
- **常见组件**：product_gap。
- **判定示例**：用户要求“给视频自动加配乐并对齐节拍”，没有任何音频相关工具，Agent 在 #3 如实说明无法完成 → `product_gap`（Agent 行为本身是正确的）。
- **区分**：
  - 与 `skill_gap`：见 skill_gap。
  - 与 `incomplete`：Agent 能做却没做完，是 incomplete；做不到并如实告知，是 product_gap，且 `outcome` 通常是 `failed` 或 `partial`，但不应算 Agent 的失误。
  - 与 `user_side`（component）：用户误用或理解有误导致的，component 记 `user_side`，不是 product_gap。

---

## 修订记录

| 日期 | 改动 | 确认人 |
| --- | --- | --- |
| 2026-10-21 | 初版种子分类（设计 6.6） | 待确认 |

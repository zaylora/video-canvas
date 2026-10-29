# 单集分镜与视频提示词

## 输入变量

- `script_md`：该集已经锁定的完整剧本；`episode_no` 为集号。
- `assets`：本集或全集可用的画布资产，包含 `node_id`、`type`、`name`、`look`、`prompt_summary`。
- `max_seconds`：单个片段时长上限，默认15秒，取值4–60。
- `ratio`、`style`：项目画面比例和画风。
- `validation_error`：如有，为上次结构校验反馈，只用于修正结果。

## 正文

把本集剧本按情节推进拆成可以独立生成视频的连续片段。忠于原有动作、台词、人物动机和信息揭示顺序，不增加情节；每个动作只完成一次，其他镜头若重复涉及，应承担反应或细节职责。

先决定镜头需要观众看清什么，再选择景别与摄影机。写清主体、动作触发、起点与终点、站位、视线、左右手及持物；跨镜保持服装造型、道具归属、空间方向和光线连续性。摄影机运动服务注意力或人物关系，避免无理由切镜和多种相互冲突的运镜。

每段prompt是提交给视频模型的唯一内容，必须可直接用于视频生成：交代场景与参与角色，按先后排列每个镜头的起止秒数、景别和运镜、可见动作、逐字对白和声音时机，以及片段终点。各镜头秒数是正整数，加起来正好等于片段seconds。动作和对白的负荷必须放得进时长，放不下就拆成下一段。对白保留原文，不把资产引用放进要念出的台词。使用输入的画风与造型，不能自行改写资产事实。

角色、场景、道具的稳定引用格式为 `@[名称](node_id)`；每次引用同一对象均使用同一个输入node_id，不编造节点id，不引用其他集专用资产。没有对应资产时使用纯文字描述。片段seconds为正整数且不得超过max_seconds；不能靠延长静止或重复动作填时长。

## 输出 JSON Schema

只输出一个JSON对象，不带Markdown围栏、注释或解释。所有列出的字段都必须出现，不允许其他字段或null。

```json
{"type":"object","additionalProperties":false,"required":["segments"],"properties":{"segments":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"required":["seconds","prompt"],"properties":{"seconds":{"type":"integer","minimum":1,"maximum":60},"prompt":{"type":"string","minLength":1}}}}}}
```

## 校验规则

1. segments非空；每段seconds为正整数且≤max_seconds。
2. 每段prompt不能为空，镜头秒数写在prompt里，合计等于该段seconds。
3. 所有资产引用node_id来自assets；未知引用会降级为纯文字名称。
4. 遵守剧本时间顺序和连续性，不增加未发生的动作或对白。
